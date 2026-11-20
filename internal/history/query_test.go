package history

import (
	"context"
	"math"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/observability"
)

func TestQuerySeriesAutoStepCoverageGapsAndValidation(t *testing.T) {
	ctx := context.Background()
	db, reg := openTestDB(t)
	health := observability.NewHealthTracker()
	cfg := config.Default()

	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	clk := metrics.NewFakeClock(now)

	writer := NewRawWriter(db, health, nil, clk)
	rollup := NewRollupEngine(db, cfg, health, nil)
	qs := NewQueryService(db, reg, cfg, health, clk)

	// Seed 5m + 1h data 36h ago (beyond 24h raw retention), and 30s raw data 1h ago.
	oldStart := now.Add(-36 * time.Hour).Truncate(time.Hour).UnixMilli()
	recentStart := now.Add(-1 * time.Hour).Truncate(time.Hour).UnixMilli()

	// Seed 5m buckets at oldStart (3/10 coverage in 1st 5m, gap in 2nd 5m) directly as consolidated 300s + roll up to 3600s,
	// plus recent 30s raw samples.
	var rawPts []metrics.MetricPoint
	// Recent hour: 4 samples in [recentStart, recentStart+2m)
	for i := 0; i < 4; i++ {
		rawPts = append(rawPts, metrics.MetricPoint{
			ResourceID:  host.HostResourceID,
			Key:         metrics.KeyCPUBusyPct,
			TimestampMs: recentStart + int64(i)*30_000 + 5_000,
			Value:       40.0 + float64(i)*10.0,
			Valid:       true,
		})
	}
	if _, err := writer.WriteBatch(ctx, rawPts); err != nil {
		t.Fatalf("WriteBatch recent raw: %v", err)
	}
	if _, err := rollup.Run(ctx, now.UnixMilli()); err != nil {
		t.Fatalf("rollup recent: %v", err)
	}

	// Insert historical 300s & 3600s buckets at oldStart (where 30s raw has already expired).
	if _, err := db.Writer().ExecContext(ctx, `
		INSERT INTO metric_buckets(resource_id, metric_key, resolution_s, bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last, last_sample_ms)
		VALUES
		  (?, ?, 300, ?, 5, 100.0, 10.0, 30.0, 25.0, ?),
		  (?, ?, 3600, ?, 5, 100.0, 10.0, 30.0, 25.0, ?)
	`,
		host.HostResourceID, metrics.KeyCPUBusyPct, oldStart, oldStart+150_000,
		host.HostResourceID, metrics.KeyCPUBusyPct, oldStart, oldStart+150_000,
	); err != nil {
		t.Fatalf("seed expired-raw older buckets: %v", err)
	}

	// 1. Recent 1h window with auto step -> selects 30s resolution, preserves gaps (only 4 points, no zero fill!).
	resRecent, err := qs.QuerySeries(ctx, SeriesQuery{
		ResourceID: host.HostResourceID,
		MetricKey:  metrics.KeyCPUBusyPct,
		FromMs:     recentStart,
		ToMs:       recentStart + 3600_000,
		Step:       "auto",
	})
	if err != nil {
		t.Fatalf("QuerySeries recent auto: %v", err)
	}
	if resRecent.ResolutionS != 30 || resRecent.Unit != "%" || len(resRecent.Points) != 4 {
		t.Fatalf("expected 30s resolution with 4 non-gap points, got res=%d unit=%q len=%d", resRecent.ResolutionS, resRecent.Unit, len(resRecent.Points))
	}
	if resRecent.Points[0].Coverage != 1.0 || resRecent.Points[0].ExpectedSamples != 1 {
		t.Fatalf("unexpected raw point coverage: %+v", resRecent.Points[0])
	}

	// 2. Query spanning oldStart (36h ago) to now with high MaxPoints=20000:
	// Auto MUST NOT pretend 30s resolution when 30s only exists for last hour! It falls back to 300s (or 3600s).
	resOldSpan, err := qs.QuerySeries(ctx, SeriesQuery{
		ResourceID: host.HostResourceID,
		MetricKey:  metrics.KeyCPUBusyPct,
		FromMs:     oldStart,
		ToMs:       now.UnixMilli(),
		Step:       "auto",
		MaxPoints:  2000, // 2000 still < 4320 for 30s, so check MaxPoints=10000 via explicit check below? No, cap is 2000; test 2h window beyond raw horizon!
	})
	if err != nil {
		t.Fatalf("QuerySeries old span: %v", err)
	}
	if resOldSpan.ResolutionS == 30 {
		t.Fatalf("auto step must not choose 30s over 36h span without 30s coverage")
	}

	// Test a 2-hour window starting at oldStart (240 raw slots <= 600 MaxPoints!) -> still MUST fall back to 300s because 30s does not exist there!
	res2hOld, err := qs.QuerySeries(ctx, SeriesQuery{
		ResourceID: host.HostResourceID,
		MetricKey:  metrics.KeyCPUBusyPct,
		FromMs:     oldStart,
		ToMs:       oldStart + 2*3600_000,
		Step:       "auto",
		MaxPoints:  600,
	})
	if err != nil {
		t.Fatalf("QuerySeries 2h old: %v", err)
	}
	if res2hOld.ResolutionS != 300 || len(res2hOld.Points) != 1 {
		t.Fatalf("expected honest fallback to 300s with 1 partial point, got res=%d pts=%+v", res2hOld.ResolutionS, res2hOld.Points)
	}
	if math.Abs(res2hOld.Points[0].Avg-20.0) > 1e-9 || math.Abs(res2hOld.Points[0].Coverage-0.5) > 1e-9 || res2hOld.Points[0].ExpectedSamples != 10 {
		t.Fatalf("unexpected 5m point stats: %+v", res2hOld.Points[0])
	}

	// 3. Explicit step & invalid query checks.
	if _, err := qs.QuerySeries(ctx, SeriesQuery{ResourceID: host.HostResourceID, MetricKey: "bogus.key", FromMs: 0, ToMs: 1000}); err == nil {
		t.Fatal("expected error for unknown metric_key")
	}
	if _, err := qs.QuerySeries(ctx, SeriesQuery{ResourceID: host.HostResourceID, MetricKey: metrics.KeyCPUBusyPct, FromMs: 1000, ToMs: 1000}); err == nil {
		t.Fatal("expected error for empty/invalid time range")
	}
	if _, err := qs.QuerySeries(ctx, SeriesQuery{ResourceID: host.HostResourceID, MetricKey: metrics.KeyCPUBusyPct, FromMs: 0, ToMs: 1000, Step: "45"}); err == nil {
		t.Fatal("expected error for unsupported step=45")
	}

	// 4. GetHost, ListResources, GetHealth.
	hResp, err := qs.GetHost(ctx)
	if err != nil || hResp.Resource.ID != host.HostResourceID {
		t.Fatalf("GetHost failed: %v %+v", err, hResp)
	}
	resList, err := qs.ListResources(ctx, host.KindHost, false)
	if err != nil || len(resList) != 1 {
		t.Fatalf("ListResources failed: %v %+v", err, resList)
	}
	healthResp, err := qs.GetHealth(ctx)
	if err != nil || !healthResp.DBPingOK || healthResp.DBBytes <= 0 {
		t.Fatalf("GetHealth failed: %v %+v", err, healthResp)
	}
}
