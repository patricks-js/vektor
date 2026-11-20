package scheduler

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/metrics"
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
	writer := history.NewRawWriter(db, health, nil, clk)

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
	writer := history.NewRawWriter(db, health, nil, clk)

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
