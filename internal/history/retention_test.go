package history

import (
	"context"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/observability"
)

func TestRetentionNeverDeletesUnrolledDataAndBatchesSafely(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t)
	health := observability.NewHealthTracker()

	cfg := config.Default()
	cfg.RawRetention = 24 * time.Hour
	cfg.MediumRetention = 7 * 24 * time.Hour
	cfg.LongRetention = 90 * 24 * time.Hour

	writer := NewRawWriter(db, health, nil, nil)
	rollup := NewRollupEngine(db, cfg, health, nil)
	retention := NewRetentionEngine(db, cfg, health, nil)
	retention.batchSize = 5 // Force multiple batched delete transactions.

	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	nowMs := now.UnixMilli()

	// Seed 12 raw buckets older than 24h (at now - 30h), and 3 recent raw buckets (at now - 2h).
	oldBase := now.Add(-30 * time.Hour).Truncate(time.Hour).UnixMilli()
	recentBase := now.Add(-2 * time.Hour).Truncate(time.Hour).UnixMilli()

	var pts []metrics.MetricPoint
	for i := 0; i < 12; i++ {
		pts = append(pts, metrics.MetricPoint{
			ResourceID:  host.HostResourceID,
			Key:         metrics.KeyCPUBusyPct,
			TimestampMs: oldBase + int64(i)*30_000 + 5_000,
			Value:       25.0,
			Valid:       true,
		})
	}
	for i := 0; i < 3; i++ {
		pts = append(pts, metrics.MetricPoint{
			ResourceID:  host.HostResourceID,
			Key:         metrics.KeyCPUBusyPct,
			TimestampMs: recentBase + int64(i)*30_000 + 5_000,
			Value:       75.0,
			Valid:       true,
		})
	}
	if _, err := writer.WriteBatch(ctx, pts); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	// Step 1: Retention BEFORE rollup runs MUST skip raw deletion!
	repBefore, err := retention.Run(ctx, nowMs)
	if err != nil {
		t.Fatalf("retention before rollup: %v", err)
	}
	if !repBefore.SkippedRaw || repBefore.DeletedRaw != 0 {
		t.Fatalf("expected raw deletion skipped before rollup watermark exists, got %+v", repBefore)
	}
	if got := len(queryBucketsByRes(t, ctx, db, ResolutionRawS)); got != 15 {
		t.Fatalf("expected all 15 raw buckets preserved before rollup, got %d", got)
	}

	// Step 2: Advance 5m watermark only halfway through the old hour (say oldBase + 3*30_000? No, aligned 5m = oldBase + 300_000),
	// verifying retention clamps raw deletion by closedThrough300 even when rawRetentionMs is older!
	if err := rollup.setWatermark(ctx, ResolutionMediumS, oldBase+300_000); err != nil {
		t.Fatalf("setWatermark partial: %v", err)
	}
	repPartial, err := retention.Run(ctx, nowMs)
	if err != nil {
		t.Fatalf("retention partial watermark: %v", err)
	}
	// First 10 raw buckets are in [oldBase, oldBase+300_000), next 2 are >= oldBase+300_000.
	if repPartial.DeletedRaw != 10 {
		t.Fatalf("expected exactly 10 raw rows deleted up to watermark, got %+v", repPartial)
	}
	if got := len(queryBucketsByRes(t, ctx, db, ResolutionRawS)); got != 5 {
		t.Fatalf("expected 5 raw rows remaining (2 un-rolled + 3 recent), got %d", got)
	}

	// Step 3: Full MaintenanceRunner (Rollup -> Retention) rolls up the rest and deletes expired remaining old raw rows.
	maint := NewMaintenanceRunner(db, rollup, retention, nil)
	rollRep, retRep, err := maint.RunCycle(ctx, nowMs)
	if err != nil {
		t.Fatalf("MaintenanceRunner.RunCycle: %v", err)
	}
	if rollRep.MediumBucketsUpserted == 0 || rollRep.LongBucketsUpserted == 0 {
		t.Fatalf("expected rollups to run before retention, got %+v", rollRep)
	}
	if retRep.DeletedRaw != 2 {
		t.Fatalf("expected remaining 2 old raw rows deleted after rollup, got %+v", retRep)
	}
	if got := len(queryBucketsByRes(t, ctx, db, ResolutionRawS)); got != 3 {
		t.Fatalf("expected 3 recent raw buckets kept, got %d", got)
	}
	if got := len(queryBucketsByRes(t, ctx, db, ResolutionLongS)); got != 2 {
		t.Fatalf("expected 2 hourly rolled-up buckets kept within 90d retention, got %d", got)
	}

	// Step 4: Verify long-tier expiry at now + 91 days.
	futureMs := now.Add(91 * 24 * time.Hour).UnixMilli()
	_, retFuture, err := maint.RunCycle(ctx, futureMs)
	if err != nil {
		t.Fatalf("future RunCycle: %v", err)
	}
	if retFuture.DeletedRaw != 3 || retFuture.DeletedMedium == 0 || retFuture.DeletedLong != 2 {
		t.Fatalf("expected all 91-day-old tiers pruned after rollup, got %+v", retFuture)
	}
}
