package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"vektor/internal/config"
)

func TestOpenMigrateConstraintsAndBackup(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	db, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := db.QuickCheck(ctx); err != nil {
		t.Fatalf("QuickCheck failed: %v", err)
	}
	if err := db.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	// Reader must reject writes (query_only + mode=ro).
	if _, err := db.Reader().ExecContext(ctx, `INSERT INTO system_meta(key, value) VALUES('k','v')`); err == nil {
		t.Fatal("expected reader pool to reject writes")
	}

	// Foreign key enforcement on writer must reject unknown resource_id.
	if _, err := db.Writer().ExecContext(ctx, `
		INSERT INTO metric_buckets(resource_id, metric_key, resolution_s, bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last, last_sample_ms)
		VALUES ('missing:res', 'cpu.busy_pct', 30, 0, 1, 10, 10, 10, 10, 1000)
	`); err == nil {
		t.Fatal("expected foreign key constraint violation on unknown resource_id")
	}

	// CHECK constraint on resolution_s and sample_count > 0.
	nowMs := time.Now().UTC().UnixMilli()
	if _, err := db.Writer().ExecContext(ctx, `
		INSERT INTO resources(id, kind, external_key, display_name, metadata_json, active, first_seen_ms, last_seen_ms)
		VALUES ('host:local', 'host', 'local', 'node1', '{}', 1, ?, ?)
	`, nowMs, nowMs); err != nil {
		t.Fatalf("insert host resource failed: %v", err)
	}
	if _, err := db.Writer().ExecContext(ctx, `
		INSERT INTO metric_buckets(resource_id, metric_key, resolution_s, bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last, last_sample_ms)
		VALUES ('host:local', 'cpu.busy_pct', 15, 0, 1, 10, 10, 10, 10, 1000)
	`); err == nil {
		t.Fatal("expected CHECK violation for invalid resolution_s=15")
	}
	if _, err := db.Writer().ExecContext(ctx, `
		INSERT INTO metric_buckets(resource_id, metric_key, resolution_s, bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last, last_sample_ms)
		VALUES ('host:local', 'cpu.busy_pct', 30, 0, 0, 0, 0, 0, 0, 1000)
	`); err == nil {
		t.Fatal("expected CHECK violation for sample_count=0")
	}

	// Re-opening existing DB must be idempotent on migrations.
	if err := db.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	db, err = Open(ctx, cfg)
	if err != nil {
		t.Fatalf("re-Open failed: %v", err)
	}
	defer db.Close()

	// Write a row, check concurrent reader, and verify VACUUM INTO consistent backup.
	if _, err := db.Writer().ExecContext(ctx, `
		INSERT INTO metric_buckets(resource_id, metric_key, resolution_s, bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last, last_sample_ms)
		VALUES ('host:local', 'cpu.busy_pct', 30, 30000, 1, 42.5, 42.5, 42.5, 42.5, 35000)
	`); err != nil {
		t.Fatalf("insert bucket failed: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "backup", "vektor-snap.db")
	if err := db.Backup(ctx, backupPath); err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	backupCfg := config.Default()
	backupCfg.DBPath = backupPath
	backupDB, err := Open(ctx, backupCfg)
	if err != nil {
		t.Fatalf("open backup DB failed: %v", err)
	}
	defer backupDB.Close()

	if err := backupDB.QuickCheck(ctx); err != nil {
		t.Fatalf("backup QuickCheck failed: %v", err)
	}
	var sum float64
	if err := backupDB.Reader().QueryRowContext(ctx, `SELECT value_sum FROM metric_buckets WHERE resource_id='host:local' AND bucket_start_ms=30000`).Scan(&sum); err != nil {
		t.Fatalf("read from backup failed: %v", err)
	}
	if sum != 42.5 {
		t.Fatalf("backup value_sum = %v, want 42.5", sum)
	}

	dbSz, _, _, err := db.FileSizes()
	if err != nil || dbSz <= 0 {
		t.Fatalf("expected positive DB file size, got %d (err=%v)", dbSz, err)
	}
	if err := db.Checkpoint(ctx, "PASSIVE"); err != nil {
		t.Fatalf("Checkpoint PASSIVE failed: %v", err)
	}
	if err := db.Optimize(ctx); err != nil {
		t.Fatalf("Optimize failed: %v", err)
	}
}
