package history

import (
	"context"
	"database/sql"
	"math"
	"net/url"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/observability"
)

func openTestDB(t *testing.T, mutateCfg ...func(*config.Config)) (*database.DB, *host.Registry) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	for _, fn := range mutateCfg {
		fn(&cfg)
	}
	ctx := context.Background()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	reg := host.NewRegistry(db)
	if err := reg.EnsureHost(ctx, host.Inventory{Hostname: "test-host", BootID: "boot-1"}, 1000); err != nil {
		t.Fatalf("EnsureHost: %v", err)
	}
	return db, reg
}

func TestRawWriterIdempotentRetryAndOrdering(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t)
	health := observability.NewHealthTracker()
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 10, 0, 10, 0, time.UTC))
	w := NewRawWriter(db, config.Default(), health, nil, clk)

	ts1 := clk.Now().UnixMilli()
	batch := []metrics.MetricPoint{
		{ResourceID: host.HostResourceID, Key: metrics.KeyCPUBusyPct, TimestampMs: ts1, Value: 42.0, Valid: true},
		{ResourceID: "cpu:99", Key: metrics.KeyCPUCoreBusyPct, TimestampMs: ts1, Value: 20, Valid: true},
		{ResourceID: host.HostResourceID, Key: metrics.KeyLoad1m, TimestampMs: ts1, Value: math.NaN(), Valid: true},
		{ResourceID: host.HostResourceID, Key: "unknown.key", TimestampMs: ts1, Value: 1.0, Valid: true},
		{ResourceID: host.HostResourceID, Key: metrics.KeyLoad5m, TimestampMs: ts1, Value: 0.5, Valid: false},
	}

	res1, err := w.WriteBatch(ctx, batch)
	if err != nil {
		t.Fatalf("WriteBatch 1: %v", err)
	}
	if res1.Written != 1 || res1.SkippedInvalid != 2 || res1.SkippedUnknown != 1 || res1.SkippedUnregistered != 1 {
		t.Fatalf("unexpected write result: %+v", res1)
	}

	// Retry exact same batch: must keep sample_count=1 and value_sum=42.0 (not 2 / 84.0).
	if _, err := w.WriteBatch(ctx, batch); err != nil {
		t.Fatalf("WriteBatch retry: %v", err)
	}

	var count int
	var sum, last float64
	var lastTs int64
	bucketStart := metrics.AlignBucketMs(ts1, ResolutionRawMs)
	if err := db.Reader().QueryRowContext(ctx, `
		SELECT sample_count, value_sum, value_last, last_sample_ms
		FROM metric_buckets
		WHERE resource_id=? AND metric_key=? AND resolution_s=30 AND bucket_start_ms=?
	`, host.HostResourceID, metrics.KeyCPUBusyPct, bucketStart).Scan(&count, &sum, &last, &lastTs); err != nil {
		t.Fatalf("query bucket: %v", err)
	}
	if count != 1 || sum != 42.0 || last != 42.0 || lastTs != ts1 {
		t.Fatalf("expected count=1 sum=42.0 lastTs=%d, got count=%d sum=%v last=%v lastTs=%d", ts1, count, sum, last, lastTs)
	}

	// Newer sample in same 30s bucket replaces older value.
	ts2 := ts1 + 5000
	if _, err := w.WriteBatch(ctx, []metrics.MetricPoint{
		{ResourceID: host.HostResourceID, Key: metrics.KeyCPUBusyPct, TimestampMs: ts2, Value: 65.0, Valid: true},
	}); err != nil {
		t.Fatalf("WriteBatch newer sample: %v", err)
	}
	// Stale retry from ts1 must NOT overwrite ts2.
	if _, err := w.WriteBatch(ctx, []metrics.MetricPoint{
		{ResourceID: host.HostResourceID, Key: metrics.KeyCPUBusyPct, TimestampMs: ts1, Value: 10.0, Valid: true},
	}); err != nil {
		t.Fatalf("WriteBatch stale sample: %v", err)
	}

	if err := db.Reader().QueryRowContext(ctx, `
		SELECT sample_count, value_sum, value_last, last_sample_ms
		FROM metric_buckets
		WHERE resource_id=? AND metric_key=? AND resolution_s=30 AND bucket_start_ms=?
	`, host.HostResourceID, metrics.KeyCPUBusyPct, bucketStart).Scan(&count, &sum, &last, &lastTs); err != nil {
		t.Fatalf("query updated bucket: %v", err)
	}
	if count != 1 || sum != 65.0 || last != 65.0 || lastTs != ts2 {
		t.Fatalf("expected newer sample 65.0 @ %d preserved, got count=%d sum=%v lastTs=%d", ts2, count, sum, lastTs)
	}
}

func TestRawWriterTransientSQLiteBusyRetryAndDegradedHealthOnFailure(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t, func(c *config.Config) {
		c.SQLiteBusyTimeoutMs = 100
	})
	health := observability.NewHealthTracker()
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC))
	w := NewRawWriter(db, config.Default(), health, nil, clk)

	// Hold an external immediate write transaction briefly so first WriteBatch attempt hits SQLITE_BUSY
	// and succeeds on retry once lock is released.
	u := url.URL{Scheme: "file", Path: db.Path(), RawQuery: "_busy_timeout=100"}
	locker, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatalf("open locker conn: %v", err)
	}
	defer locker.Close()

	tx, err := locker.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("locker BeginTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO system_meta(key, value) VALUES ('lock_test', '1')`); err != nil {
		t.Fatalf("locker write: %v", err)
	}

	go func() {
		time.Sleep(130 * time.Millisecond)
		_ = tx.Rollback()
	}()

	_, err = w.WriteBatch(ctx, []metrics.MetricPoint{
		{ResourceID: host.HostResourceID, Key: metrics.KeyCPUBusyPct, TimestampMs: clk.Now().UnixMilli(), Value: 55.0, Valid: true},
	})
	if err != nil {
		t.Fatalf("expected WriteBatch to recover after transient SQLITE_BUSY, got %v", err)
	}
	if snap := health.Snapshot(); snap.Status != observability.StatusOK || !snap.DBOK {
		t.Fatalf("expected healthy status after retry recovery, got %+v", snap)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// A fatal database error degrades health.
	_, err = w.WriteBatch(ctx, []metrics.MetricPoint{
		{ResourceID: host.HostResourceID, Key: metrics.KeyCPUBusyPct, TimestampMs: clk.Now().UnixMilli(), Value: 55.0, Valid: true},
	})
	if err == nil {
		t.Fatal("expected error for closed database")
	}
	snap := health.Snapshot()
	if snap.Status != observability.StatusDegraded || snap.DBOK || snap.LastDBError == "" {
		t.Fatalf("expected degraded health on write failure, got %+v", snap)
	}
}
