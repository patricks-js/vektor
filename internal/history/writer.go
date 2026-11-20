package history

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

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
	Attempted      int
	Written        int
	SkippedInvalid int
	SkippedUnknown int
}

// RawWriter persists normalized 30s metric points idempotently into SQLite WAL.
type RawWriter struct {
	db     *database.DB
	health *observability.HealthTracker
	logger *slog.Logger
	clock  metrics.Clock
}

// NewRawWriter constructs a RawWriter.
func NewRawWriter(db *database.DB, health *observability.HealthTracker, logger *slog.Logger, clock metrics.Clock) *RawWriter {
	if logger == nil {
		logger = observability.NewLogger(nil, slog.LevelInfo)
	}
	if clock == nil {
		clock = metrics.NewRealClock()
	}
	return &RawWriter{
		db:     db,
		health: health,
		logger: logger,
		clock:  clock,
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
WHERE excluded.last_sample_ms >= metric_buckets.last_sample_ms
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
		written, err := w.writeOnce(ctx, valid)
		if err == nil {
			res.Written = written
			if w.health != nil {
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

func (w *RawWriter) writeOnce(ctx context.Context, valid []metrics.MetricPoint) (int, error) {
	tx, err := w.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, rawUpsertSQL)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	written := 0
	for _, p := range valid {
		bucketStartMs := metrics.AlignBucketMs(p.TimestampMs, ResolutionRawMs)
		if _, err := stmt.ExecContext(ctx,
			p.ResourceID, p.Key, bucketStartMs,
			p.Value, p.Value, p.Value, p.Value, p.TimestampMs,
		); err != nil {
			return 0, err
		}
		written++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return written, nil
}
