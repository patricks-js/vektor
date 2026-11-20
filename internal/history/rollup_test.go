package history

import (
	"context"
	"math"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/observability"
)

type bucketRow struct {
	bucketStartMs int64
	sampleCount   int64
	valueSum      float64
	valueMin      float64
	valueMax      float64
	valueLast     float64
	lastSampleMs  int64
}

func queryBucketsByRes(t *testing.T, ctx context.Context, db *database.DB, res int) []bucketRow {
	t.Helper()
	rows, err := db.Reader().QueryContext(ctx, `
		SELECT bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last, last_sample_ms
		FROM metric_buckets
		WHERE resolution_s = ?
		ORDER BY bucket_start_ms ASC
	`, res)
	if err != nil {
		t.Fatalf("query buckets res=%d: %v", res, err)
	}
	defer rows.Close()
	var out []bucketRow
	for rows.Next() {
		var r bucketRow
		if err := rows.Scan(&r.bucketStartMs, &r.sampleCount, &r.valueSum, &r.valueMin, &r.valueMax, &r.valueLast, &r.lastSampleMs); err != nil {
			t.Fatalf("scan bucket: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	return out
}

func TestRollupWeightedMathGapsPartialWindowsAndReplay(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t)
	health := observability.NewHealthTracker()
	cfg := config.Default()

	hourStart := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	baseMs := hourStart.UnixMilli()

	clk := metrics.NewFakeClock(hourStart)
	writer := NewRawWriter(db, cfg, health, nil, clk)
	rollup := NewRollupEngine(db, cfg, health, nil)

	// Window 1 [10:00, 10:05): 10 raw samples at 20.0.
	var pts []metrics.MetricPoint
	for i := 0; i < 10; i++ {
		ts := baseMs + int64(i)*30_000 + 5_000
		pts = append(pts, metrics.MetricPoint{
			ResourceID:  host.HostResourceID,
			Key:         metrics.KeyCPUBusyPct,
			TimestampMs: ts,
			Value:       20.0,
			Valid:       true,
		})
	}
	// Window 2 [10:05, 10:10): empty gap.
	// Window 3 [10:10, 10:15): 2 raw samples at 80.0 (partial window).
	lastTs := baseMs + 10*60_000 + 30_000 + 5_000
	pts = append(pts,
		metrics.MetricPoint{ResourceID: host.HostResourceID, Key: metrics.KeyCPUBusyPct, TimestampMs: baseMs + 10*60_000 + 5_000, Value: 80.0, Valid: true},
		metrics.MetricPoint{ResourceID: host.HostResourceID, Key: metrics.KeyCPUBusyPct, TimestampMs: lastTs, Value: 80.0, Valid: true},
	)
	if _, err := writer.WriteBatch(ctx, pts); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	// Inside 60s grace for window 3 ([10:10, 10:15)): only windows 1..2 are closed.
	repEarly, err := rollup.Run(ctx, baseMs+15*60_000+30_000)
	if err != nil {
		t.Fatalf("early rollup Run: %v", err)
	}
	if repEarly.ClosedThroughMediumMs != baseMs+10*60_000 {
		t.Fatalf("expected 5m watermark at 10:10 (%d), got %d", baseMs+10*60_000, repEarly.ClosedThroughMediumMs)
	}
	if repEarly.MediumBucketsUpserted != 1 {
		t.Fatalf("expected 1 non-empty 5m bucket, got %d", repEarly.MediumBucketsUpserted)
	}

	// Past 11:00 + 60s grace: all 5m and 1h windows close.
	repHour, err := rollup.Run(ctx, baseMs+61*60_000)
	if err != nil {
		t.Fatalf("hour rollup Run: %v", err)
	}
	if repHour.ClosedThroughMediumMs != baseMs+60*60_000 || repHour.ClosedThroughLongMs != baseMs+60*60_000 {
		t.Fatalf("expected watermarks at 11:00, got %+v", repHour)
	}

	rows5m := queryBucketsByRes(t, ctx, db, ResolutionMediumS)
	if len(rows5m) != 2 {
		t.Fatalf("expected 2 non-empty 5m buckets (empty gap omitted), got %d: %+v", len(rows5m), rows5m)
	}
	if rows5m[0].bucketStartMs != baseMs || rows5m[0].sampleCount != 10 || rows5m[0].valueSum != 200.0 {
		t.Fatalf("unexpected 5m bucket[0]: %+v", rows5m[0])
	}
	if rows5m[1].bucketStartMs != baseMs+10*60_000 || rows5m[1].sampleCount != 2 || rows5m[1].valueSum != 160.0 {
		t.Fatalf("unexpected 5m bucket[1]: %+v", rows5m[1])
	}

	rows1h := queryBucketsByRes(t, ctx, db, ResolutionLongS)
	if len(rows1h) != 1 {
		t.Fatalf("expected 1 hourly bucket, got %d", len(rows1h))
	}
	b := rows1h[0]
	if b.sampleCount != 12 || math.Abs(b.valueSum/float64(b.sampleCount)-30.0) > 1e-9 ||
		b.valueMin != 20.0 || b.valueMax != 80.0 || b.valueLast != 80.0 || b.lastSampleMs != lastTs {
		t.Fatalf("unexpected weighted 1h bucket: %+v", b)
	}

	// Idempotent replay.
	if _, err := rollup.RecomputeRange(ctx, ResolutionRawS, ResolutionMediumS, baseMs, baseMs+60*60_000); err != nil {
		t.Fatalf("RecomputeRange 5m: %v", err)
	}
	if _, err := rollup.RecomputeRange(ctx, ResolutionMediumS, ResolutionLongS, baseMs, baseMs+60*60_000); err != nil {
		t.Fatalf("RecomputeRange 1h: %v", err)
	}
	rows1hAfter := queryBucketsByRes(t, ctx, db, ResolutionLongS)
	if len(rows1hAfter) != 1 || rows1hAfter[0] != b {
		t.Fatalf("replay changed 1h bucket: before=%+v after=%+v", b, rows1hAfter)
	}
}

func TestLateRawRepairsClosedRollupsBeforeRetention(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t)
	cfg := config.Default()
	start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	clk := metrics.NewFakeClock(start)
	writer := NewRawWriter(db, cfg, nil, nil, clk)
	rollup := NewRollupEngine(db, cfg, nil, nil)
	retention := NewRetentionEngine(db, cfg, nil, nil)
	maintenance := NewMaintenanceRunner(db, rollup, retention, nil)

	point := metrics.MetricPoint{
		ResourceID: host.HostResourceID, Key: metrics.KeyLoad1m,
		TimestampMs: start.Add(5 * time.Second).UnixMilli(), Value: 20, Valid: true,
	}
	write := func(wantWritten, wantExpired int) {
		t.Helper()
		written, err := writer.WriteBatch(ctx, []metrics.MetricPoint{point})
		if err != nil || written.Written != wantWritten || written.SkippedExpired != wantExpired {
			t.Fatalf("WriteBatch: %+v, %v", written, err)
		}
	}
	write(1, 0)
	clk.Advance(time.Hour + time.Minute)
	if _, _, err := maintenance.RunCycle(ctx, clk.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	point.TimestampMs += ResolutionRawMs
	point.Value = 80
	write(1, 0)
	write(0, 0)

	clk.Advance(24 * time.Hour)
	if rep, err := retention.Run(ctx, clk.Now().UnixMilli()); err != nil || rep.DeletedRaw != 0 {
		t.Fatalf("retention removed pending source: %+v %v", rep, err)
	}
	if _, _, err := maintenance.RunCycle(ctx, clk.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	for _, tier := range []int{ResolutionMediumS, ResolutionLongS} {
		rows := queryBucketsByRes(t, ctx, db, tier)
		if len(rows) != 1 || rows[0].sampleCount != 2 || rows[0].valueSum != 100 || rows[0].valueLast != 80 {
			t.Fatalf("tier %ds: %+v", tier, rows)
		}
	}
	clk.SetWall(start)
	write(0, 1)
	if rows := queryBucketsByRes(t, ctx, db, ResolutionRawS); len(rows) != 0 {
		t.Fatalf("expired raw was reinserted: %+v", rows)
	}
}

func TestRollupRollbackCatchUpAndMultiDayChunking(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t)
	cfg := config.Default()
	rollup := NewRollupEngine(db, cfg, nil, nil)

	start := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).UnixMilli()
	writer := NewRawWriter(db, cfg, nil, nil, metrics.NewFakeClock(time.UnixMilli(start)))
	if _, err := writer.WriteBatch(ctx, []metrics.MetricPoint{
		{ResourceID: host.HostResourceID, Key: metrics.KeyCPUBusyPct, TimestampMs: start + 10_000, Value: 15.0, Valid: true},
		{ResourceID: host.HostResourceID, Key: metrics.KeyCPUBusyPct, TimestampMs: start + 36*3600_000 + 10_000, Value: 45.0, Valid: true},
	}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	// Simulate rolled-back rollup transaction before commit.
	tx, err := db.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO rollup_state(resolution_s, closed_through_ms) VALUES (300, ?)`, start+300_000); err != nil {
		t.Fatalf("tx exec: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if _, ok, err := rollup.getWatermark(ctx, db.Reader(), ResolutionMediumS); err != nil || ok {
		t.Fatalf("expected no watermark after rollback, got ok=%v err=%v", ok, err)
	}

	nowMs := start + 38*3600_000
	rep, err := rollup.Run(ctx, nowMs)
	if err != nil {
		t.Fatalf("catch-up Run: %v", err)
	}
	if rep.MediumBucketsUpserted != 2 || rep.LongBucketsUpserted != 2 {
		t.Fatalf("expected 2 5m and 2 1h buckets across 36h span, got %+v", rep)
	}
}

func TestRollup1hWaitsFor5mWatermarkAndHandlesSourceGap(t *testing.T) {
	ctx := context.Background()
	db, _ := openTestDB(t)
	cfg := config.Default()
	rollup := NewRollupEngine(db, cfg, nil, nil)

	hour0 := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC).UnixMilli()
	// Pre-populate a 5m bucket in hour0 without rollup_state[300]: 1h must NOT roll ahead of rollup_state[300].
	if _, err := db.Writer().ExecContext(ctx, `
		INSERT INTO metric_buckets(resource_id, metric_key, resolution_s, bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last, last_sample_ms)
		VALUES (?, ?, 300, ?, 10, 500, 50, 50, 50, ?)
	`, host.HostResourceID, metrics.KeyCPUBusyPct, hour0, hour0+290_000); err != nil {
		t.Fatalf("seed 5m bucket: %v", err)
	}

	rep, err := rollup.Run(ctx, hour0+2*3600_000)
	if err != nil {
		t.Fatalf("Run without 30s/5m watermark: %v", err)
	}
	if rep.LongBucketsUpserted != 0 {
		t.Fatalf("expected 1h rollup not to run ahead of 5m watermark, got %+v", rep)
	}
	if rows1h := queryBucketsByRes(t, ctx, db, ResolutionLongS); len(rows1h) != 0 {
		t.Fatalf("expected 0 hourly buckets without 5m watermark, got %+v", rows1h)
	}
}
