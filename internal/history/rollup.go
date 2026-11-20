package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/metrics"
	"vektor/internal/observability"
)

const (
	rollupChunk5mMs = int64(24 * time.Hour / time.Millisecond)
	rollupChunk1hMs = int64(7 * 24 * time.Hour / time.Millisecond)
)

// RollupReport summarizes one RollupEngine.Run execution.
type RollupReport struct {
	MediumBucketsUpserted int64
	LongBucketsUpserted   int64
	ClosedThroughMediumMs int64
	ClosedThroughLongMs   int64
	LagSeconds            int64
}

// RollupEngine performs idempotent, weighted SQLite rollups (30s -> 300s -> 3600s).
type RollupEngine struct {
	db      *database.DB
	health  *observability.HealthTracker
	logger  *slog.Logger
	graceMs int64
}

// NewRollupEngine constructs a RollupEngine configured with cfg.RollupGrace.
func NewRollupEngine(db *database.DB, cfg config.Config, health *observability.HealthTracker, logger *slog.Logger) *RollupEngine {
	if logger == nil {
		logger = observability.NewLogger(nil, slog.LevelInfo)
	}
	graceMs := cfg.RollupGrace.Milliseconds()
	if graceMs <= 0 {
		graceMs = 60_000
	}
	return &RollupEngine{
		db:      db,
		health:  health,
		logger:  logger,
		graceMs: graceMs,
	}
}

const rollupRangeSQL = `
WITH agg AS (
	SELECT
		resource_id,
		metric_key,
		(bucket_start_ms - ((bucket_start_ms % ?1 + ?1) % ?1)) AS parent_bucket_ms,
		SUM(sample_count) AS sample_count,
		SUM(value_sum) AS value_sum,
		MIN(value_min) AS value_min,
		MAX(value_max) AS value_max,
		MAX(last_sample_ms) AS max_last_sample_ms
	FROM metric_buckets
	WHERE resolution_s = ?2
	  AND bucket_start_ms >= ?3
	  AND bucket_start_ms < ?4
	GROUP BY resource_id, metric_key, parent_bucket_ms
)
INSERT INTO metric_buckets (
	resource_id, metric_key, resolution_s, bucket_start_ms,
	sample_count, value_sum, value_min, value_max, value_last, last_sample_ms
)
SELECT
	a.resource_id,
	a.metric_key,
	?5,
	a.parent_bucket_ms,
	a.sample_count,
	a.value_sum,
	a.value_min,
	a.value_max,
	(
		SELECT c2.value_last
		FROM metric_buckets c2
		WHERE c2.resource_id = a.resource_id
		  AND c2.metric_key = a.metric_key
		  AND c2.resolution_s = ?2
		  AND c2.bucket_start_ms >= a.parent_bucket_ms
		  AND c2.bucket_start_ms < a.parent_bucket_ms + ?1
		ORDER BY c2.last_sample_ms DESC, c2.bucket_start_ms DESC
		LIMIT 1
	),
	a.max_last_sample_ms
FROM agg a
WHERE 1
ON CONFLICT(resource_id, metric_key, resolution_s, bucket_start_ms)
DO UPDATE SET
	sample_count = excluded.sample_count,
	value_sum = excluded.value_sum,
	value_min = excluded.value_min,
	value_max = excluded.value_max,
	value_last = excluded.value_last,
	last_sample_ms = excluded.last_sample_ms
`

// Run consolidates all closed 5m windows (from 30s) and then all closed 1h windows (from 300s).
func (e *RollupEngine) Run(ctx context.Context, nowMs int64) (RollupReport, error) {
	var report RollupReport

	eligible5m := metrics.AlignBucketMs(nowMs-e.graceMs, ResolutionMediumMs)
	up5m, closed5m, has5m, err := e.advanceTier(ctx, ResolutionRawS, ResolutionMediumS, ResolutionMediumMs, eligible5m, rollupChunk5mMs)
	if err != nil {
		if e.health != nil {
			e.health.RecordDBError(err)
		}
		return report, fmt.Errorf("rollup 30->300: %w", err)
	}
	report.MediumBucketsUpserted = up5m
	report.ClosedThroughMediumMs = closed5m

	eligible1h := metrics.AlignBucketMs(nowMs-e.graceMs, ResolutionLongMs)
	if has5m {
		closed5mAlignedTo1h := metrics.AlignBucketMs(closed5m, ResolutionLongMs)
		if closed5mAlignedTo1h < eligible1h {
			eligible1h = closed5mAlignedTo1h
		}
	} else {
		// No 5m watermark yet: 1h cannot advance ahead of 5m consolidation.
		eligible1h = 0
	}

	var closed1h int64
	if has5m && eligible1h > 0 {
		up1h, c1h, _, err := e.advanceTier(ctx, ResolutionMediumS, ResolutionLongS, ResolutionLongMs, eligible1h, rollupChunk1hMs)
		if err != nil {
			if e.health != nil {
				e.health.RecordDBError(err)
			}
			return report, fmt.Errorf("rollup 300->3600: %w", err)
		}
		report.LongBucketsUpserted = up1h
		closed1h = c1h
	} else if c1h, ok, err := e.getWatermark(ctx, e.db.Reader(), ResolutionLongS); err == nil && ok {
		closed1h = c1h
	}
	report.ClosedThroughLongMs = closed1h

	lagS := computeRollupLagSeconds(nowMs, e.graceMs, has5m, closed5m)
	report.LagSeconds = lagS
	if e.health != nil {
		e.health.SetRollupLag(lagS)
	}
	return report, nil
}

func (e *RollupEngine) advanceTier(
	ctx context.Context,
	sourceRes, targetRes int,
	targetMs, eligibleEndMs, chunkMs int64,
) (upserted int64, closedThrough int64, hasWatermark bool, err error) {
	wm, hasWM, err := e.getWatermark(ctx, e.db.Reader(), targetRes)
	if err != nil {
		return 0, 0, false, err
	}

	minSourceMs, hasSource, err := e.minBucketStart(ctx, e.db.Reader(), sourceRes)
	if err != nil {
		return 0, 0, hasWM, err
	}

	var fromMs int64
	if hasWM {
		fromMs = wm
		if hasSource && minSourceMs-wm > targetMs {
			e.logger.Warn("unrecoverable source gap ahead of rollup watermark; advancing across missing interval",
				slog.Int("source_res_s", sourceRes),
				slog.Int("target_res_s", targetRes),
				slog.Int64("watermark_ms", wm),
				slog.Int64("oldest_source_ms", minSourceMs),
			)
		}
	} else {
		if !hasSource {
			return 0, 0, false, nil
		}
		fromMs = metrics.AlignBucketMs(minSourceMs, targetMs)
	}

	if fromMs >= eligibleEndMs {
		return 0, wm, hasWM, nil
	}

	if hasSource {
		alignedMin := metrics.AlignBucketMs(minSourceMs, targetMs)
		if alignedMin > fromMs && alignedMin <= eligibleEndMs {
			// Skip pure empty historical wasteland before oldest surviving source bucket in one metadata step,
			// while still running normal chunked aggregation from alignedMin onwards.
			if err := e.setWatermark(ctx, targetRes, alignedMin); err != nil {
				return 0, wm, hasWM, err
			}
			fromMs = alignedMin
			wm = alignedMin
			hasWM = true
		}
	}

	for cursor := fromMs; cursor < eligibleEndMs; {
		if err := ctx.Err(); err != nil {
			return upserted, wm, hasWM, err
		}
		chunkEnd := cursor + chunkMs
		if chunkEnd > eligibleEndMs {
			chunkEnd = eligibleEndMs
		}
		rows, err := e.rollupChunkTx(ctx, sourceRes, targetRes, targetMs, cursor, chunkEnd, true)
		if err != nil {
			return upserted, wm, hasWM, err
		}
		upserted += rows
		wm = chunkEnd
		hasWM = true
		cursor = chunkEnd
	}

	return upserted, wm, hasWM, nil
}

// RecomputeRange deterministically recomputes targetRes buckets from sourceRes within [fromMs, toMs)
// aligned to targetMs boundaries without modifying rollup_state.
func (e *RollupEngine) RecomputeRange(ctx context.Context, sourceRes, targetRes int, fromMs, toMs int64) (int64, error) {
	targetMs, err := resolutionDurationMs(targetRes)
	if err != nil {
		return 0, err
	}
	if _, err := resolutionDurationMs(sourceRes); err != nil {
		return 0, err
	}
	alignedFrom := metrics.AlignBucketMs(fromMs, targetMs)
	alignedTo := metrics.AlignBucketMs(toMs, targetMs)
	if alignedTo <= alignedFrom {
		return 0, nil
	}
	return e.rollupChunkTx(ctx, sourceRes, targetRes, targetMs, alignedFrom, alignedTo, false)
}

func (e *RollupEngine) rollupChunkTx(
	ctx context.Context,
	sourceRes, targetRes int,
	targetMs, fromMs, toMs int64,
	advanceWatermark bool,
) (int64, error) {
	tx, err := e.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, rollupRangeSQL, targetMs, sourceRes, fromMs, toMs, targetRes)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()

	if advanceWatermark {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO rollup_state(resolution_s, closed_through_ms)
			VALUES (?, ?)
			ON CONFLICT(resolution_s) DO UPDATE SET
				closed_through_ms = MAX(rollup_state.closed_through_ms, excluded.closed_through_ms)
		`, targetRes, toMs); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return affected, nil
}

func (e *RollupEngine) setWatermark(ctx context.Context, targetRes int, closedThroughMs int64) error {
	_, err := e.db.Writer().ExecContext(ctx, `
		INSERT INTO rollup_state(resolution_s, closed_through_ms)
		VALUES (?, ?)
		ON CONFLICT(resolution_s) DO UPDATE SET
			closed_through_ms = MAX(rollup_state.closed_through_ms, excluded.closed_through_ms)
	`, targetRes, closedThroughMs)
	return err
}

func (e *RollupEngine) getWatermark(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, resolutionS int) (int64, bool, error) {
	var wm int64
	err := q.QueryRowContext(ctx, `SELECT closed_through_ms FROM rollup_state WHERE resolution_s = ?`, resolutionS).Scan(&wm)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return wm, true, nil
}

func (e *RollupEngine) minBucketStart(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, resolutionS int) (int64, bool, error) {
	var minMs sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MIN(bucket_start_ms) FROM metric_buckets WHERE resolution_s = ?`, resolutionS).Scan(&minMs); err != nil {
		return 0, false, err
	}
	if !minMs.Valid {
		return 0, false, nil
	}
	return minMs.Int64, true, nil
}

// LagSeconds computes current 5m rollup lag in seconds relative to nowMs - graceMs.
func (e *RollupEngine) LagSeconds(ctx context.Context, nowMs int64) (int64, error) {
	wm, hasWM, err := e.getWatermark(ctx, e.db.Reader(), ResolutionMediumS)
	if err != nil {
		return 0, err
	}
	return computeRollupLagSeconds(nowMs, e.graceMs, hasWM, wm), nil
}

func computeRollupLagSeconds(nowMs, graceMs int64, hasWM bool, wmMs int64) int64 {
	if !hasWM {
		return 0
	}
	eligible := metrics.AlignBucketMs(nowMs-graceMs, ResolutionMediumMs)
	if eligible <= wmMs {
		return 0
	}
	return (eligible - wmMs) / 1000
}

func resolutionDurationMs(resolutionS int) (int64, error) {
	switch resolutionS {
	case ResolutionRawS:
		return ResolutionRawMs, nil
	case ResolutionMediumS:
		return ResolutionMediumMs, nil
	case ResolutionLongS:
		return ResolutionLongMs, nil
	default:
		return 0, fmt.Errorf("unsupported resolution_s %d", resolutionS)
	}
}
