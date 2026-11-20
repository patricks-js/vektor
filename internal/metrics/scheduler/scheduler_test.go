package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/metrics/collector"
	"vektor/internal/metrics/tracker"
	"vektor/internal/observability"
)

type stubCollector struct {
	name string
	fn   func(context.Context) (metrics.RawSnapshot, error)
}

func (s *stubCollector) Name() string { return s.name }
func (s *stubCollector) Collect(ctx context.Context) (metrics.RawSnapshot, error) {
	return s.fn(ctx)
}

func openSchedulerTestDB(t *testing.T) (*database.DB, *host.Registry) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	db, err := database.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	reg := host.NewRegistry(db)
	if err := reg.EnsureHost(context.Background(), host.Inventory{Hostname: "sched-host", BootID: "boot-1"}, 1000); err != nil {
		t.Fatalf("EnsureHost: %v", err)
	}
	return db, reg
}

func TestBlockedCollectorDoesNotStallSiblingsOrLeakGoroutines(t *testing.T) {
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	health := observability.NewHealthTracker()
	norm := tracker.NewNormalizer(nil)
	db, reg := openSchedulerTestDB(t)
	writer := history.NewRawWriter(db, config.Default(), health, nil, clk)

	releaseHung := make(chan struct{})
	defer close(releaseHung)

	var blockedCalls atomic.Int32
	var fastCalls atomic.Int32

	blockedCol := &stubCollector{
		name: "blocked_io",
		fn: func(ctx context.Context) (metrics.RawSnapshot, error) {
			blockedCalls.Add(1)
			<-releaseHung // Simulates uninterruptible kernel D-state syscall ignoring ctx.
			return metrics.RawSnapshot{}, nil
		},
	}
	fastCol := &stubCollector{
		name: "fast_mem",
		fn: func(ctx context.Context) (metrics.RawSnapshot, error) {
			n := fastCalls.Add(1)
			return metrics.RawSnapshot{
				CollectedAt: clk.Now(),
				Monotonic:   clk.Monotonic(),
				Samples: []metrics.RawSample{
					metrics.NewGaugeSample(host.HostResourceID, metrics.KeyLoad1m, float64(n), clk.Now(), clk.Monotonic()),
				},
			}, nil
		},
	}

	cfg := config.Default()
	cfg.CollectorTimeout = 20 * time.Millisecond
	cfg.MaxFastConcurrency = 2

	sched := New(Deps{
		Config:         cfg,
		Clock:          clk,
		Health:         health,
		Registry:       reg,
		Normalizer:     norm,
		FastCollectors: []metrics.Collector{blockedCol, fastCol},
		RawWriter:      writer,
		BootIDProvider: func() (string, error) { return "boot-1", nil },
	})

	baseGoroutines := runtime.NumGoroutine()
	for i := 0; i < 15; i++ {
		clk.Advance(30 * time.Second)
		_ = sched.RunPersistOnce(context.Background())
	}

	if got := fastCalls.Load(); got != 15 {
		t.Fatalf("expected fast collector to run all 15 ticks despite blocked sibling, got %d", got)
	}
	if got := blockedCalls.Load(); got != 1 {
		t.Fatalf("expected in-flight guard to restrict blocked collector to 1 goroutine, got %d", got)
	}
	afterGoroutines := runtime.NumGoroutine()
	if afterGoroutines-baseGoroutines > 4 {
		t.Fatalf("goroutine leak detected: before=%d after=%d", baseGoroutines, afterGoroutines)
	}

	// Verify all 15 fast buckets landed in SQLite.
	var count int
	if err := db.Reader().QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM metric_buckets WHERE resource_id=? AND metric_key=?
	`, host.HostResourceID, metrics.KeyLoad1m).Scan(&count); err != nil {
		t.Fatalf("query buckets: %v", err)
	}
	if count != 15 {
		t.Fatalf("expected 15 persisted fast buckets, got %d", count)
	}
}

func TestLiveHubOnDemandZeroSQLiteWritesAndSlowSubscriberDrop(t *testing.T) {
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	health := observability.NewHealthTracker()
	norm := tracker.NewNormalizer(nil)
	db, reg := openSchedulerTestDB(t)
	writer := history.NewRawWriter(db, config.Default(), health, nil, clk)

	var rxBytes atomic.Uint64
	rxBytes.Store(1000)

	netCol := &stubCollector{
		name: "net_stub",
		fn: func(ctx context.Context) (metrics.RawSnapshot, error) {
			v := rxBytes.Add(200)
			return metrics.RawSnapshot{
				CollectedAt: clk.Now(),
				Monotonic:   clk.Monotonic(),
				Samples: []metrics.RawSample{
					metrics.NewCounterSample(host.HostResourceID, metrics.RawNetRxBytesTotal, v, 64, clk.Now(), clk.Monotonic()),
				},
			}, nil
		},
	}

	cfg := config.Default()
	cfg.LiveInterval = 15 * time.Millisecond
	cfg.PersistInterval = 10 * time.Second
	cfg.DiscoveryInterval = 10 * time.Second
	cfg.RollupInterval = 10 * time.Second

	hub := NewLiveHub(health, nil, nil)
	sched := New(Deps{
		Config:         cfg,
		Clock:          clk,
		Health:         health,
		Registry:       reg,
		Normalizer:     norm,
		FastCollectors: []metrics.Collector{netCol},
		RawWriter:      writer,
		LiveHub:        hub,
		BootIDProvider: func() (string, error) { return "boot-1", nil },
	})

	// Test slow subscriber drop directly via Subscribe + Publish.
	_, slowCh, unsubSlow := hub.Subscribe()
	_, fastCh, unsubFast := hub.Subscribe()

	for i := 0; i < 8; i++ {
		clk.Advance(2 * time.Second)
		if err := sched.RunLiveOnce(context.Background()); err != nil {
			t.Fatalf("RunLiveOnce: %v", err)
		}
	}

	snapHealth := health.Snapshot()
	if snapHealth.DroppedLiveEvents == 0 {
		t.Fatalf("expected dropped_live_events > 0 for slow consumer, got %+v", snapHealth)
	}
	if snapHealth.ActiveLiveSubscribers != 2 {
		t.Fatalf("expected ActiveLiveSubscribers=2, got %d", snapHealth.ActiveLiveSubscribers)
	}
	if norm.CounterBaselineCount(metrics.ProfileLive) == 0 {
		t.Fatal("expected ProfileLive baseline populated while subscribed")
	}

	// Drain fast channel to ensure non-empty snapshots arrived.
	receivedFast := 0
	for len(fastCh) > 0 {
		<-fastCh
		receivedFast++
	}
	if receivedFast == 0 {
		t.Fatal("expected live snapshots delivered on subscriber channel")
	}
	_ = slowCh

	// Verify ZERO rows written to SQLite by live loop!
	var bucketCount int
	if err := db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM metric_buckets`).Scan(&bucketCount); err != nil {
		t.Fatal(err)
	}
	if bucketCount != 0 {
		t.Fatalf("expected zero SQLite rows written by live mode, got %d", bucketCount)
	}

	// Unsubscribe all -> ProfileLive baselines must be cleared automatically!
	unsubSlow()
	unsubFast()
	if hub.SubscriberCount() != 0 || health.Snapshot().ActiveLiveSubscribers != 0 {
		t.Fatalf("expected 0 subscribers after unsubscribe")
	}
	// Trigger onLast hook behavior explicitly through Run loop as well:
	// SetHooks wired Run's stopLive, OR test Run loop live activation:
	runCtx, cancelRun := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- sched.Run(runCtx)
	}()

	_, liveCh, unsubRun := hub.Subscribe()
	select {
	case <-liveCh:
	case <-time.After(1 * time.Second):
		cancelRun()
		t.Fatal("timed out waiting for on-demand live loop to publish")
	}
	unsubRun()

	// Wait briefly for live loop to process last-subscriber exit and clear ProfileLive baselines.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if norm.CounterBaselineCount(metrics.ProfileLive) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if norm.CounterBaselineCount(metrics.ProfileLive) != 0 {
		t.Fatalf("expected ProfileLive baselines cleared on disconnect, got %d", norm.CounterBaselineCount(metrics.ProfileLive))
	}

	cancelRun()
	<-doneCh
}

func TestOverlappingLiveAndPersistShareFastSample(t *testing.T) {
	ctx := context.Background()
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	cfg := config.Default()
	db, _ := openSchedulerTestDB(t)
	hub := NewLiveHub(nil, nil, nil)
	_, live, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	col := &stubCollector{name: "shared", fn: func(ctx context.Context) (metrics.RawSnapshot, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return metrics.RawSnapshot{Samples: []metrics.RawSample{
			metrics.NewGaugeSample(host.HostResourceID, metrics.KeyLoad1m, 3, clk.Now(), clk.Monotonic()),
		}}, nil
	}}
	sched := New(Deps{
		Config: cfg, Clock: clk, LiveHub: hub, Normalizer: tracker.NewNormalizer(nil),
		FastCollectors: []metrics.Collector{col}, RawWriter: history.NewRawWriter(db, cfg, nil, nil, clk),
	})
	liveDone := make(chan error, 1)
	go func() { liveDone <- sched.RunLiveOnce(ctx) }()
	<-started
	persistDone := make(chan error, 1)
	go func() { persistDone <- sched.RunPersistOnce(ctx) }()
	select {
	case err := <-persistDone:
		close(release)
		t.Fatalf("persistence skipped the running collector: %v", err)
	case <-time.After(20 * time.Millisecond):
		close(release)
	}
	if err := <-liveDone; err != nil {
		t.Fatal(err)
	}
	if err := <-persistDone; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len((<-live).Points) != 1 {
		t.Fatalf("expected one collection and a live point, calls = %d", calls.Load())
	}
	var n int
	if err := db.Reader().QueryRowContext(ctx, "SELECT COUNT(*) FROM metric_buckets").Scan(&n); err != nil || n != 1 {
		t.Fatalf("expected the same reading in SQLite: %d, %v", n, err)
	}
}

func TestMaintenancePausesWhenWallTimeJumps(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	clk := metrics.NewFakeClock(start)
	db, reg := openSchedulerTestDB(t)
	cfg := config.Default()
	writer := history.NewRawWriter(db, cfg, nil, nil, clk)
	if _, err := writer.WriteBatch(ctx, []metrics.MetricPoint{{
		ResourceID: host.HostResourceID, Key: metrics.KeyLoad1m, TimestampMs: start.UnixMilli(), Value: 20, Valid: true,
	}}); err != nil {
		t.Fatal(err)
	}
	maint := history.NewMaintenanceRunner(db, history.NewRollupEngine(db, cfg, nil, nil), history.NewRetentionEngine(db, cfg, nil, nil), nil)
	fs, err := reg.Reconcile(ctx, host.KindFilesystem, []host.DiscoveredResource{{ExternalKey: "test-fs"}}, start.UnixMilli())
	if err != nil || len(fs) != 1 {
		t.Fatalf("register filesystem: %v", err)
	}
	fsCollector := collector.NewFilesystemCollector(clk, func() []collector.FilesystemTarget {
		return []collector.FilesystemTarget{{ResourceID: fs[0].ID, Mountpoint: "/"}}
	}, func(string) (collector.FSStat, error) { return collector.FSStat{TotalBytes: 10}, nil })
	syncOK := true
	sched := New(Deps{Config: cfg, Clock: clk, Maintenance: maint, RawWriter: writer,
		FSCollector: fsCollector, Normalizer: tracker.NewNormalizer(nil), ClockSynchronized: func() bool { return syncOK }})
	clk.Advance(time.Hour + time.Minute)
	if err := sched.RunMaintenanceOnce(ctx); err != nil {
		t.Fatal(err)
	}
	clk.Step(91*24*time.Hour, time.Minute)
	syncOK = false
	if err := sched.RunSlowFSOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sched.RunMaintenanceOnce(ctx); err == nil {
		t.Fatal("maintenance ran with an untrusted wall clock")
	}
	clk.Advance(time.Minute)
	if err := sched.RunPersistOnce(ctx); !errors.Is(err, ErrClockUntrusted) {
		t.Fatalf("a stable but unsynchronized clock enabled writes: %v", err)
	}
	var count int
	var watermark int64
	err = db.Reader().QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM metric_buckets),
		       (SELECT closed_through_ms FROM rollup_state WHERE resolution_s = 300)
	`).Scan(&count, &watermark)
	if err != nil || count != 3 || watermark != start.Add(time.Hour).UnixMilli() {
		t.Fatalf("clock jump changed history: buckets=%d watermark=%d err=%v", count, watermark, err)
	}
	clk.Step(-91*24*time.Hour+2*time.Minute, time.Minute)
	syncOK = true
	if err := sched.RunMaintenanceOnce(ctx); !errors.Is(err, ErrClockUntrusted) {
		t.Fatalf("recovery skipped stabilization: %v", err)
	}
	clk.Advance(clockRecovery)
	if err := sched.RunMaintenanceOnce(ctx); err != nil {
		t.Fatalf("maintenance did not resume on clock correction: %v", err)
	}
	if err := sched.RunPersistOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Reader().QueryRowContext(ctx, "SELECT COUNT(*) FROM metric_buckets WHERE resource_id = ?", fs[0].ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("filesystem reading taken during the jump entered history: count=%d err=%v", count, err)
	}
}

func TestClockTrustAtStartupAndBackwardStep(t *testing.T) {
	for _, tc := range []struct {
		name         string
		initialShift time.Duration
		step         time.Duration
		syncOK       bool
	}{
		{"boot-already-ahead", 91 * 24 * time.Hour, 0, false},
		{"boot-behind-history-even-if-synchronized", -2 * time.Hour, 0, true},
		{"backward-step-before-first-write", 0, -2 * time.Hour, true},
		{"forward-step-before-first-write", 0, 91 * 24 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
			cfg := config.Default()
			db, reg := openSchedulerTestDB(t)
			clk := metrics.NewFakeClock(start)
			health := observability.NewHealthTracker()
			writer := history.NewRawWriter(db, cfg, health, nil, clk)
			if _, err := writer.WriteBatch(ctx, []metrics.MetricPoint{{
				ResourceID: host.HostResourceID, Key: metrics.KeyLoad1m, TimestampMs: start.UnixMilli(), Value: 20, Valid: true,
			}}); err != nil {
				t.Fatal(err)
			}
			query := history.NewQueryService(db, reg, cfg, health)
			floor, err := query.ClockFloor(ctx)
			if err != nil || !floor.Equal(start) {
				t.Fatalf("durable clock floor = %v, err = %v", floor, err)
			}
			clk.SetWall(start.Add(tc.initialShift))
			synced := tc.syncOK
			col := &stubCollector{name: "load", fn: func(context.Context) (metrics.RawSnapshot, error) {
				ts, mono := clk.Now(), clk.Monotonic()
				return metrics.RawSnapshot{CollectedAt: ts, Monotonic: mono, Samples: []metrics.RawSample{
					metrics.NewGaugeSample(host.HostResourceID, metrics.KeyLoad1m, 80, ts, mono),
				}}, nil
			}}
			maintenance := history.NewMaintenanceRunner(db, history.NewRollupEngine(db, cfg, health, nil), history.NewRetentionEngine(db, cfg, health, nil), nil)
			sched := New(Deps{Config: cfg, Clock: clk, ClockFloor: floor, Health: health, Normalizer: tracker.NewNormalizer(nil),
				ClockSynchronized: func() bool { return synced }, FastCollectors: []metrics.Collector{col}, RawWriter: writer, Maintenance: maintenance})
			clk.Step(tc.step+time.Second, time.Second)
			for range 3 {
				if err := sched.RunPersistOnce(ctx); !errors.Is(err, ErrClockUntrusted) {
					t.Fatalf("write with untrusted time: %v", err)
				}
				if err := sched.RunMaintenanceOnce(ctx); !errors.Is(err, ErrClockUntrusted) {
					t.Fatalf("maintenance with untrusted time: %v", err)
				}
				clk.Advance(time.Minute)
			}
			floor, err = query.ClockFloor(ctx)
			if err != nil || !floor.Equal(start) || health.Snapshot().Status != observability.StatusDegraded {
				t.Fatalf("unsafe time changed history or health did not degrade: %v %v %+v", floor, err, health.Snapshot())
			}

			// Reacquire a corrected epoch even if it differs from the startup reference.
			clk.SetWall(start.Add(2 * time.Hour))
			synced = true
			if err := sched.RunPersistOnce(ctx); !errors.Is(err, ErrClockUntrusted) {
				t.Fatalf("expected recovery wait: %v", err)
			}
			clk.Advance(clockRecovery)
			if err := sched.RunPersistOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if err := sched.RunMaintenanceOnce(ctx); err != nil {
				t.Fatal(err)
			}
			series, err := query.QuerySeries(ctx, history.SeriesQuery{
				ResourceID: host.HostResourceID, MetricKey: metrics.KeyLoad1m, Step: "30",
				FromMs: start.Add(-24 * time.Hour).UnixMilli(), ToMs: start.Add(100 * 24 * time.Hour).UnixMilli(),
			})
			if err != nil || len(series.Points) != 2 || series.Points[0].Avg != 20 || series.Points[1].Avg != 80 ||
				health.Snapshot().Status != observability.StatusOK {
				t.Fatalf("unexpected history after automatic recovery: %+v err=%v health=%+v", series, err, health.Snapshot())
			}
		})
	}
}

func TestDiscoveryWaitIsCancellableAndCannotOverlap(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	cfg := config.Default()
	cfg.PersistInterval = 5 * time.Millisecond
	fastRan := make(chan struct{}, 1)
	sched := New(Deps{
		Config: cfg, Clock: metrics.NewFakeClock(time.Now()), BootIDProvider: func() (string, error) {
			close(started)
			<-release
			return "boot", nil
		},
		FastCollectors: []metrics.Collector{&stubCollector{name: "fast", fn: func(context.Context) (metrics.RawSnapshot, error) {
			select {
			case fastRan <- struct{}{}:
			default:
			}
			return metrics.RawSnapshot{}, nil
		}}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sched.Run(ctx) }()
	<-started
	select {
	case <-fastRan:
	case <-time.After(time.Second):
		t.Fatal("blocked discovery also stopped fast collection")
	}
	if err := sched.RunDiscoveryOnce(ctx); err == nil {
		t.Fatal("a second discovery started while the first was blocked")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked discovery prevented shutdown")
	}
}

func TestStartupAndShutdownWithStalledSlowProbes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.Default()
	cfg.PersistInterval = 5 * time.Millisecond
	cfg.SlowFSInterval, cfg.SensorInterval = 10*time.Millisecond, 10*time.Millisecond
	cfg.SlowCollectorTimeout = 25 * time.Millisecond
	clk := metrics.NewFakeClock(time.Now())
	root := t.TempDir()
	cfg.ProcRoot, cfg.EtcRoot = root, root
	blockedSensor := filepath.Join(root, "blocked_input")
	goodSensor := filepath.Join(root, "good_input")
	if err := syscall.Mkfifo(blockedSensor, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goodSensor, []byte("42000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sensors := collector.NewSensorCollector(clk, func() []collector.SensorTarget {
		return []collector.SensorTarget{{ResourceID: "blocked-sensor", InputPath: blockedSensor}, {ResourceID: "good-sensor", InputPath: goodSensor}}
	})
	releaseFS := make(chan struct{})
	defer close(releaseFS)
	var blockedFS, fast atomic.Int32
	fs := collector.NewFilesystemCollector(clk, func() []collector.FilesystemTarget {
		return []collector.FilesystemTarget{{ResourceID: "blocked-fs", Mountpoint: "/blocked"}, {ResourceID: "good-fs", Mountpoint: "/good"}}
	}, func(path string) (collector.FSStat, error) {
		if path == "/blocked" {
			blockedFS.Add(1)
			<-releaseFS
		}
		return collector.FSStat{TotalBytes: 100}, nil
	})
	sched := New(Deps{Config: cfg, Clock: clk, FSCollector: fs, SensorCollector: sensors,
		FastCollectors: []metrics.Collector{&stubCollector{name: "fast", fn: func(context.Context) (metrics.RawSnapshot, error) {
			fast.Add(1)
			return metrics.RawSnapshot{}, nil
		}}},
	})
	baseline := runtime.NumGoroutine()
	done := make(chan error, 1)
	go func() { done <- sched.Run(ctx) }()

	// A connected writer that sends no bytes leaves the sensor read blocked in the kernel.
	deadline := time.Now().Add(3 * time.Second)
	for {
		fd, err := syscall.Open(blockedSensor, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			defer syscall.Close(fd)
			break
		}
		if !errors.Is(err, syscall.ENXIO) || time.Now().After(deadline) {
			t.Fatalf("sensor never opened the FIFO: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	for fast.Load() < 20 || len(sensors.CachedSnapshot().Samples) != 1 || len(fs.CachedSnapshot().Samples) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("healthy probes stalled: fast=%d sensor=%d fs=%d", fast.Load(), len(sensors.CachedSnapshot().Samples), len(fs.CachedSnapshot().Samples))
		}
		time.Sleep(time.Millisecond)
	}
	if blockedFS.Load() != 1 || runtime.NumGoroutine() > baseline+20 {
		t.Fatalf("blocked probe was relaunched or goroutines accumulated: calls=%d goroutines=%d baseline=%d", blockedFS.Load(), runtime.NumGoroutine(), baseline)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler waited for blocked probes during shutdown")
	}
	if blockedFS.Load() != 1 || runtime.NumGoroutine() > baseline+4 {
		t.Fatalf("shutdown did not release the scheduler loops: calls=%d goroutines=%d baseline=%d", blockedFS.Load(), runtime.NumGoroutine(), baseline)
	}
}
