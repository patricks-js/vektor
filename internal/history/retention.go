package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/observability"
)

const defaultRetentionBatchSize = 5000

// RetentionReport summarizes deleted bucket counts and skipped unsafe tiers.
type RetentionReport struct {
	DeletedRaw    int64
	DeletedMedium int64
	DeletedLong   int64
	SkippedRaw    bool
	SkippedMedium bool
}

// RetentionEngine safely expires old metric buckets in indexed batches,
// refusing to delete source buckets before downstream rollups have consolidated them.
type RetentionEngine struct {
	db        *database.DB
	health    *observability.HealthTracker
	logger    *slog.Logger
	rawMs     int64
	mediumMs  int64
	longMs    int64
	batchSize int
}

// NewRetentionEngine constructs a RetentionEngine from config retention durations.
func NewRetentionEngine(db *database.DB, cfg config.Config, health *observability.HealthTracker, logger *slog.Logger) *RetentionEngine {
	if logger == nil {
		logger = observability.NewLogger(nil, slog.LevelInfo)
	}
	return &RetentionEngine{
		db:        db,
		health:    health,
		logger:    logger,
		rawMs:     cfg.RawRetention.Milliseconds(),
		mediumMs:  cfg.MediumRetention.Milliseconds(),
		longMs:    cfg.LongRetention.Milliseconds(),
		batchSize: defaultRetentionBatchSize,
	}
}

const deleteBatchSQL = `
DELETE FROM metric_buckets
WHERE (resource_id, metric_key, resolution_s, bucket_start_ms) IN (
	SELECT resource_id, metric_key, resolution_s, bucket_start_ms
	FROM metric_buckets
	WHERE resolution_s = ? AND bucket_start_ms < ?
	ORDER BY bucket_start_ms ASC
	LIMIT ?
)
`

// Run deletes expired buckets per tier after clamping cutoffs by rollup watermarks.
func (e *RetentionEngine) Run(ctx context.Context, nowMs int64) (RetentionReport, error) {
	var rep RetentionReport

	// 1. Raw tier (30s): never delete beyond rollup_state[300].
	closed300, has300, err := e.readWatermark(ctx, ResolutionMediumS)
	if err != nil {
		return rep, fmt.Errorf("read 5m rollup watermark: %w", err)
	}
	if !has300 {
		rep.SkippedRaw = true
	} else {
		cutoff := nowMs - e.rawMs
		if closed300 < cutoff {
			cutoff = closed300
		}
		deleted, err := e.deleteTierBefore(ctx, ResolutionRawS, cutoff)
		if err != nil {
			if e.health != nil {
				e.health.RecordDBError(err)
			}
			return rep, fmt.Errorf("prune raw tier: %w", err)
		}
		rep.DeletedRaw = deleted
	}

	// 2. Medium tier (5m): never delete beyond rollup_state[3600].
	closed3600, has3600, err := e.readWatermark(ctx, ResolutionLongS)
	if err != nil {
		return rep, fmt.Errorf("read 1h rollup watermark: %w", err)
	}
	if !has3600 {
		rep.SkippedMedium = true
	} else {
		cutoff := nowMs - e.mediumMs
		if closed3600 < cutoff {
			cutoff = closed3600
		}
		deleted, err := e.deleteTierBefore(ctx, ResolutionMediumS, cutoff)
		if err != nil {
			if e.health != nil {
				e.health.RecordDBError(err)
			}
			return rep, fmt.Errorf("prune medium tier: %w", err)
		}
		rep.DeletedMedium = deleted
	}

	// 3. Long tier (1h): top tier, expires directly by longRetention.
	deletedLong, err := e.deleteTierBefore(ctx, ResolutionLongS, nowMs-e.longMs)
	if err != nil {
		if e.health != nil {
			e.health.RecordDBError(err)
		}
		return rep, fmt.Errorf("prune long tier: %w", err)
	}
	rep.DeletedLong = deletedLong

	return rep, nil
}

func (e *RetentionEngine) readWatermark(ctx context.Context, resolutionS int) (int64, bool, error) {
	var wm int64
	err := e.db.Reader().QueryRowContext(ctx, `SELECT closed_through_ms FROM rollup_state WHERE resolution_s = ?`, resolutionS).Scan(&wm)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return wm, true, nil
}

func (e *RetentionEngine) deleteTierBefore(ctx context.Context, resolutionS int, cutoffMs int64) (int64, error) {
	var totalDeleted int64
	for {
		if err := ctx.Err(); err != nil {
			return totalDeleted, err
		}
		tx, err := e.db.Writer().BeginTx(ctx, nil)
		if err != nil {
			return totalDeleted, err
		}
		res, err := tx.ExecContext(ctx, deleteBatchSQL, resolutionS, cutoffMs, e.batchSize)
		if err != nil {
			_ = tx.Rollback()
			return totalDeleted, err
		}
		affected, _ := res.RowsAffected()
		if err := tx.Commit(); err != nil {
			return totalDeleted, err
		}
		totalDeleted += affected
		if affected < int64(e.batchSize) {
			break
		}
	}
	return totalDeleted, nil
}

// MaintenanceRunner executes rollups before retention and runs low-frequency DB optimization.
type MaintenanceRunner struct {
	rollup    *RollupEngine
	retention *RetentionEngine
	db        *database.DB
	logger    *slog.Logger
	cycles    int
}

// NewMaintenanceRunner creates a MaintenanceRunner coordinating rollup, retention, and PRAGMA optimize.
func NewMaintenanceRunner(db *database.DB, rollup *RollupEngine, retention *RetentionEngine, logger *slog.Logger) *MaintenanceRunner {
	if logger == nil {
		logger = observability.NewLogger(nil, slog.LevelInfo)
	}
	return &MaintenanceRunner{
		db:        db,
		rollup:    rollup,
		retention: retention,
		logger:    logger,
	}
}

// RunCycle executes Rollup -> Retention in strict dependency order, plus periodic PRAGMA optimize.
func (m *MaintenanceRunner) RunCycle(ctx context.Context, nowMs int64) (RollupReport, RetentionReport, error) {
	rollRep, err := m.rollup.Run(ctx, nowMs)
	if err != nil {
		return rollRep, RetentionReport{}, err
	}
	retRep, err := m.retention.Run(ctx, nowMs)
	if err != nil {
		return rollRep, retRep, err
	}
	m.cycles++
	if m.db != nil && m.cycles%12 == 0 {
		if optErr := m.db.Optimize(ctx); optErr != nil {
			m.logger.Warn("sqlite pragma optimize failed", slog.String("error", optErr.Error()))
		}
	}
	return rollRep, retRep, nil
}
