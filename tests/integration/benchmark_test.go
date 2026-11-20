package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/observability"
)

type benchSeries struct {
	resID string
	key   string
}

func build45BenchSeries(t testing.TB, ctx context.Context, reg *host.Registry) []benchSeries {
	t.Helper()
	if err := reg.EnsureHost(ctx, host.Inventory{Hostname: "bench-node", BootID: "bench-boot"}, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.EnsureCPUCores(ctx, []string{"0", "1", "2", "3", "4", "5", "6", "7"}, 1000); err != nil {
		t.Fatal(err)
	}
	disks, err := reg.Reconcile(ctx, host.KindDisk, []host.DiscoveredResource{
		{ExternalKey: "wwid:nvme0", DisplayName: "nvme0n1"},
		{ExternalKey: "dm:lvm0", DisplayName: "ubuntu-vg-lv"},
	}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	fss, err := reg.Reconcile(ctx, host.KindFilesystem, []host.DiscoveredResource{
		{ExternalKey: "uuid:r:/", DisplayName: "/"},
		{ExternalKey: "uuid:h:/home", DisplayName: "/home"},
		{ExternalKey: "uuid:d:/mnt/data", DisplayName: "/mnt/data"},
	}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	nics, err := reg.Reconcile(ctx, host.KindNIC, []host.DiscoveredResource{
		{ExternalKey: "pci:0000:01:00.0", DisplayName: "enp1s0"},
		{ExternalKey: "virtual:lo", DisplayName: "lo"},
	}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	sensors, err := reg.Reconcile(ctx, host.KindSensor, []host.DiscoveredResource{
		{ExternalKey: "hwmon:coretemp:0:temp1", DisplayName: "Package"},
		{ExternalKey: "hwmon:nvme:0:temp1", DisplayName: "NVMe"},
		{ExternalKey: "hwmon:acpitz:0:temp1", DisplayName: "ACPI"},
		{ExternalKey: "hwmon:coretemp:0:temp2", DisplayName: "Core0"},
	}, 1000)
	if err != nil {
		t.Fatal(err)
	}

	var series []benchSeries
	for _, k := range []string{
		metrics.KeyCPUBusyPct, metrics.KeyCPUUserPct, metrics.KeyCPUSystemPct, metrics.KeyCPUIOWaitPct, metrics.KeyCPUStealPct,
		metrics.KeyLoad1m, metrics.KeyLoad5m, metrics.KeyLoad15m,
		metrics.KeyMemoryTotalBytes, metrics.KeyMemoryUsedBytes, metrics.KeyMemoryAvailableBytes,
		metrics.KeyMemoryCachedBytes, metrics.KeyMemoryBuffersBytes, metrics.KeyMemoryUsedPct,
		metrics.KeySwapTotalBytes, metrics.KeySwapUsedBytes,
		metrics.KeyNetRxBytesPerSec, metrics.KeyNetTxBytesPerSec,
	} {
		series = append(series, benchSeries{resID: host.HostResourceID, key: k})
	}
	for i := 0; i < 8; i++ {
		series = append(series, benchSeries{resID: fmt.Sprintf("cpu:%d", i), key: metrics.KeyCPUCoreBusyPct})
	}
	for _, d := range disks {
		series = append(series,
			benchSeries{resID: d.ID, key: metrics.KeyDiskReadBytesPerSec},
			benchSeries{resID: d.ID, key: metrics.KeyDiskWriteBytesPerSec},
			benchSeries{resID: d.ID, key: metrics.KeyDiskReadsPerSec},
			benchSeries{resID: d.ID, key: metrics.KeyDiskWritesPerSec},
		)
	}
	for _, f := range fss {
		series = append(series,
			benchSeries{resID: f.ID, key: metrics.KeyFilesystemUsedBytes},
			benchSeries{resID: f.ID, key: metrics.KeyFilesystemUsedPct},
		)
	}
	for _, n := range nics {
		series = append(series,
			benchSeries{resID: n.ID, key: metrics.KeyNetRxBytesPerSec},
			benchSeries{resID: n.ID, key: metrics.KeyNetTxBytesPerSec},
		)
	}
	for _, s := range sensors {
		series = append(series, benchSeries{resID: s.ID, key: metrics.KeySensorTemperatureCelsius})
	}
	// Total = 18 + 8 + 8 + 6 + 4 + 4 = 48 series (~45 representative series).
	return series
}

func TestBenchmark24hFootprint(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	health := observability.NewHealthTracker()
	reg := host.NewRegistry(db)
	series := build45BenchSeries(t, ctx, reg)

	start := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	clk := metrics.NewFakeClock(start)
	writer := history.NewRawWriter(db, cfg, health, nil, clk)
	rollup := history.NewRollupEngine(db, cfg, health, nil)
	qs := history.NewQueryService(db, reg, cfg, health)

	const totalTicks = 2880 // 24h * 120 ticks/h
	batch := make([]metrics.MetricPoint, len(series))

	writeStart := time.Now()
	for tick := range totalTicks {
		tsMs := start.Add(time.Duration(tick)*30*time.Second + 5*time.Second).UnixMilli()
		for i, s := range series {
			batch[i] = metrics.MetricPoint{
				ResourceID:  s.resID,
				Key:         s.key,
				TimestampMs: tsMs,
				Value:       float64((tick+i)%100) + 0.5,
				Valid:       true,
			}
		}
		if _, err := writer.WriteBatch(ctx, batch); err != nil {
			t.Fatalf("WriteBatch tick %d: %v", tick, err)
		}
	}
	writeElapsed := time.Since(writeStart)

	endMs := start.Add(24*time.Hour + 2*time.Minute).UnixMilli()
	clk.SetWall(time.UnixMilli(endMs))
	rollStart := time.Now()
	rollRep, err := rollup.Run(ctx, endMs)
	if err != nil {
		t.Fatalf("rollup.Run 24h: %v", err)
	}
	rollElapsed := time.Since(rollStart)

	q1hStart := time.Now()
	res1h, err := qs.QuerySeries(ctx, history.SeriesQuery{
		ResourceID: host.HostResourceID,
		MetricKey:  metrics.KeyCPUBusyPct,
		FromMs:     start.Add(23 * time.Hour).UnixMilli(),
		ToMs:       start.Add(24 * time.Hour).UnixMilli(),
		Step:       "auto",
	})
	if err != nil || len(res1h.Points) != 120 {
		t.Fatalf("query 1h failed: %v len=%d", err, len(res1h.Points))
	}
	q1hElapsed := time.Since(q1hStart)

	q24hStart := time.Now()
	res24h, err := qs.QuerySeries(ctx, history.SeriesQuery{
		ResourceID: host.HostResourceID,
		MetricKey:  metrics.KeyCPUBusyPct,
		FromMs:     start.UnixMilli(),
		ToMs:       start.Add(24 * time.Hour).UnixMilli(),
		Step:       "auto",
	})
	if err != nil || res24h.ResolutionS != 300 || len(res24h.Points) != 288 {
		t.Fatalf("query 24h failed: %v res=%d len=%d", err, res24h.ResolutionS, len(res24h.Points))
	}
	q24hElapsed := time.Since(q24hStart)

	_ = db.Checkpoint(ctx, "PASSIVE")
	dbBytes, walBytes, shmBytes, err := db.FileSizes()
	if err != nil {
		t.Fatal(err)
	}

	var totalRows int64
	if err := db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM metric_buckets`).Scan(&totalRows); err != nil {
		t.Fatal(err)
	}

	t.Logf("24h footprint summary: series=%d raw_ticks=%d total_rows=%d rollup_5m=%d rollup_1h=%d db_bytes=%d (%.2f MiB) wal_bytes=%d shm_bytes=%d write_24h_total=%s (avg_batch=%s) rollup_24h=%s query_1h=%s query_24h=%s",
		len(series), totalTicks, totalRows, rollRep.MediumBucketsUpserted, rollRep.LongBucketsUpserted,
		dbBytes, float64(dbBytes)/(1024*1024), walBytes, shmBytes,
		writeElapsed, writeElapsed/totalTicks, rollElapsed, q1hElapsed, q24hElapsed,
	)
}

func BenchmarkStorageAndQuery24h(b *testing.B) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.DataDir = b.TempDir()

	db, err := database.Open(ctx, cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	health := observability.NewHealthTracker()
	reg := host.NewRegistry(db)
	series := build45BenchSeries(b, ctx, reg)

	start := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	clk := metrics.NewFakeClock(start)
	writer := history.NewRawWriter(db, cfg, health, nil, clk)
	rollup := history.NewRollupEngine(db, cfg, health, nil)
	qs := history.NewQueryService(db, reg, cfg, health)

	// Pre-seed 24h of buckets for steady-state query/write benchmarking.
	const totalTicks = 2880
	batch := make([]metrics.MetricPoint, len(series))
	for tick := range totalTicks {
		tsMs := start.Add(time.Duration(tick)*30*time.Second + 5*time.Second).UnixMilli()
		for i, s := range series {
			batch[i] = metrics.MetricPoint{
				ResourceID:  s.resID,
				Key:         s.key,
				TimestampMs: tsMs,
				Value:       float64((tick+i)%100) + 0.5,
				Valid:       true,
			}
		}
		if _, err := writer.WriteBatch(ctx, batch); err != nil {
			b.Fatal(err)
		}
	}
	endMs := start.Add(24*time.Hour + 2*time.Minute).UnixMilli()
	clk.SetWall(time.UnixMilli(endMs))
	if _, err := rollup.Run(ctx, endMs); err != nil {
		b.Fatal(err)
	}

	b.Run("WriteBatch48Series", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			tsMs := start.Add(24*time.Hour + time.Duration(i)*30*time.Second + 5*time.Second).UnixMilli()
			for j := range batch {
				batch[j].TimestampMs = tsMs
			}
			if _, err := writer.WriteBatch(ctx, batch); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Query1hRaw", func(b *testing.B) {
		b.ReportAllocs()
		q := history.SeriesQuery{
			ResourceID: host.HostResourceID,
			MetricKey:  metrics.KeyCPUBusyPct,
			FromMs:     start.Add(23 * time.Hour).UnixMilli(),
			ToMs:       start.Add(24 * time.Hour).UnixMilli(),
			Step:       "30",
		}
		for i := 0; i < b.N; i++ {
			if _, err := qs.QuerySeries(ctx, q); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Query24hAuto", func(b *testing.B) {
		b.ReportAllocs()
		q := history.SeriesQuery{
			ResourceID: host.HostResourceID,
			MetricKey:  metrics.KeyCPUBusyPct,
			FromMs:     start.UnixMilli(),
			ToMs:       start.Add(24 * time.Hour).UnixMilli(),
			Step:       "auto",
		}
		for i := 0; i < b.N; i++ {
			if _, err := qs.QuerySeries(ctx, q); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Query7dHourly", func(b *testing.B) {
		b.ReportAllocs()
		q := history.SeriesQuery{
			ResourceID: host.HostResourceID,
			MetricKey:  metrics.KeyCPUBusyPct,
			FromMs:     start.Add(-6 * 24 * time.Hour).UnixMilli(),
			ToMs:       start.Add(24 * time.Hour).UnixMilli(),
			Step:       "3600",
		}
		for i := 0; i < b.N; i++ {
			if _, err := qs.QuerySeries(ctx, q); err != nil {
				b.Fatal(err)
			}
		}
	})

	dbBytes, walBytes, _, err := db.FileSizes()
	if err == nil {
		b.ReportMetric(float64(dbBytes)/(1024*1024), "db_MiB")
		b.ReportMetric(float64(walBytes)/(1024*1024), "wal_MiB")
	}
}
