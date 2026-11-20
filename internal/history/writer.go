package history

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/metrics"
	"vektor/internal/observability"
)

const (
	ResolutionRawS    = 30
	ResolutionMediumS = 300
	ResolutionLongS   = 3600

	ResolutionRawMs    = int64(ResolutionRawS) * 1000
	ResolutionMediumMs = int64(ResolutionMediumS) * 1000
	ResolutionLongMs   = int64(ResolutionLongS) * 1000
)

// WriteResult summarizes one RawWriter.WriteBatch call.
type WriteResult struct {
	Attempted           int
	Written             int
	SkippedInvalid      int
	SkippedUnknown      int
	SkippedUnregistered int
	SkippedExpired      int
}

// RawWriter persists normalized 30s metric points idempotently into SQLite WAL.
type RawWriter struct {
	db             *database.DB
	health         *observability.HealthTracker
	logger         *slog.Logger
	clock          metrics.Clock
	rawRetentionMs int64
}

// NewRawWriter constructs a RawWriter.
func NewRawWriter(db *database.DB, cfg config.Config, health *observability.HealthTracker, logger *slog.Logger, clock metrics.Clock) *RawWriter {
	if logger == nil {
		logger = observability.NewLogger(nil, slog.LevelInfo)
	}
	if clock == nil {
		clock = metrics.NewRealClock()
	}
	return &RawWriter{
		db:             db,
		health:         health,
		logger:         logger,
		clock:          clock,
		rawRetentionMs: cfg.RawRetention.Milliseconds(),
	}
}

const rawUpsertSQL = `
INSERT INTO metric_buckets (
	resource_id, metric_key, resolution_s, bucket_start_ms,
	sample_count, value_sum, value_min, value_max, value_last, last_sample_ms
) VALUES (?, ?, 30, ?, 1, ?, ?, ?, ?, ?)
ON CONFLICT(resource_id, metric_key, resolution_s, bucket_start_ms)
DO UPDATE SET
	sample_count = 1,
	value_sum = excluded.value_sum,
	value_min = excluded.value_min,
	value_max = excluded.value_max,
	value_last = excluded.value_last,
	last_sample_ms = excluded.last_sample_ms
WHERE excluded.last_sample_ms > metric_buckets.last_sample_ms
   OR (excluded.last_sample_ms = metric_buckets.last_sample_ms AND excluded.value_last <> metric_buckets.value_last)
`

// WriteBatch filters valid catalog metrics and writes them in one idempotent transaction.
func (w *RawWriter) WriteBatch(ctx context.Context, points []metrics.MetricPoint) (WriteResult, error) {
	var res WriteResult
	valid := make([]metrics.MetricPoint, 0, len(points))

	for _, p := range points {
		res.Attempted++
		if !p.Valid || p.ResourceID == "" || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			res.SkippedInvalid++
			continue
		}
		if !metrics.IsKnownMetric(p.Key) {
			res.SkippedUnknown++
			continue
		}
		valid = append(valid, p)
	}

	if len(valid) == 0 {
		return res, nil
	}

	backoffs := []time.Duration{15 * time.Millisecond, 45 * time.Millisecond}
	var lastErr error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		if err := ctx.Err(); err != nil {
			lastErr = err
			break
		}
		result, err := w.writeOnce(ctx, valid)
		if err == nil {
			res.Written = result.Written
			res.SkippedUnregistered = result.SkippedUnregistered
			res.SkippedExpired = result.SkippedExpired
			if res.SkippedUnregistered > 0 {
				w.logger.Warn("metrics skipped for unregistered resources", slog.Int("points", res.SkippedUnregistered))
			}
			if w.health != nil && res.Written > 0 {
				w.health.RecordWriteOK(w.clock.Now())
			}
			return res, nil
		}
		lastErr = err
		if !database.IsTransientSQLiteError(err) || attempt == len(backoffs) {
			break
		}
		w.logger.Warn("transient sqlite error during raw write; retrying",
			slog.Int("attempt", attempt+1),
			slog.String("error", err.Error()),
		)
		timer := time.NewTimer(backoffs[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			lastErr = ctx.Err()
			break
		case <-timer.C:
		}
	}

	if w.health != nil {
		w.health.RecordDBError(lastErr)
	}
	w.logger.Error("raw metric batch write failed",
		slog.Int("points", len(valid)),
		slog.String("error", lastErr.Error()),
	)
	return res, fmt.Errorf("write raw metric batch: %w", lastErr)
}

func (w *RawWriter) writeOnce(ctx context.Context, valid []metrics.MetricPoint) (WriteResult, error) {
	var result WriteResult
	tx, err := w.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()

	var prunedBefore, closedMedium int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM system_meta WHERE key = 'raw_pruned_before_ms'), ?),
		       COALESCE((SELECT closed_through_ms FROM rollup_state WHERE resolution_s = 300), ?)
	`, int64(math.MinInt64), int64(math.MinInt64)).Scan(&prunedBefore, &closedMedium); err != nil {
		return result, err
	}
	expiredBefore := metrics.AlignBucketMs(w.clock.Now().UnixMilli()-w.rawRetentionMs, ResolutionRawMs)

	stmt, err := tx.PrepareContext(ctx, rawUpsertSQL)
	if err != nil {
		return result, err
	}
	defer stmt.Close()

	var dirty map[int64]struct{}
	for _, p := range valid {
		bucketStartMs := metrics.AlignBucketMs(p.TimestampMs, ResolutionRawMs)
		if bucketStartMs < max(expiredBefore, prunedBefore) {
			result.SkippedExpired++
			continue
		}
		change, err := stmt.ExecContext(ctx,
			p.ResourceID, p.Key, bucketStartMs,
			p.Value, p.Value, p.Value, p.Value, p.TimestampMs,
		)
		if err != nil {
			var sqliteErr *sqlite.Error
			if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
				result.SkippedUnregistered++
				continue
			}
			return result, err
		}
		affected, err := change.RowsAffected()
		if err != nil {
			return result, err
		}
		result.Written += int(affected)
		if affected > 0 && bucketStartMs < closedMedium {
			if dirty == nil {
				dirty = make(map[int64]struct{})
			}
			dirty[metrics.AlignBucketMs(bucketStartMs, ResolutionMediumMs)] = struct{}{}
		}
	}
	for bucket := range dirty {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO rollup_dirty(bucket_start_ms) VALUES (?)", bucket); err != nil {
			return result, err
		}
	}

	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
