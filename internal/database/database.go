package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	sqlite "modernc.org/sqlite/lib"

	"vektor/internal/config"
	"vektor/migrations"
)

const (
	sqliteBusy   = 5
	sqliteLocked = 6
)

// DB encapsulates separated writer (MaxOpenConns=1) and read-only reader WAL pools.
type DB struct {
	writer *sql.DB
	reader *sql.DB
	path   string
}

// Open initializes the SQLite database file in WAL mode, applies embedded migrations,
// and prepares separate writer and read-only query pools.
func Open(ctx context.Context, cfg config.Config) (*DB, error) {
	dbPath := cfg.EffectiveDBPath()
	if strings.TrimSpace(dbPath) == "" {
		return nil, errors.New("database path must not be empty")
	}
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	if err := restrictStateFiles(absPath); err != nil {
		return nil, fmt.Errorf("restrict database file permissions: %w", err)
	}

	busyMs := cfg.SQLiteBusyTimeoutMs
	if busyMs <= 0 {
		busyMs = 5000
	}
	syncMode := strings.ToUpper(strings.TrimSpace(cfg.SQLiteSynchronous))
	if syncMode == "" {
		syncMode = config.SynchronousNormal
	}

	writerDSN := buildDSN(absPath, false, busyMs, syncMode)
	writer, err := sql.Open("sqlite", writerDSN)
	if err != nil {
		return nil, fmt.Errorf("open writer pool: %w", err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	writer.SetConnMaxLifetime(0)

	if err := verifyWriterPragmas(ctx, writer); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := migrate(ctx, writer); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	readerDSN := buildDSN(absPath, true, busyMs, syncMode)
	reader, err := sql.Open("sqlite", readerDSN)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open reader pool: %w", err)
	}
	reader.SetMaxOpenConns(4)
	reader.SetMaxIdleConns(4)
	reader.SetConnMaxLifetime(0)

	if err := reader.PingContext(ctx); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, fmt.Errorf("ping reader pool: %w", err)
	}

	return &DB{
		writer: writer,
		reader: reader,
		path:   absPath,
	}, nil
}

func restrictStateFiles(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if err := errors.Join(f.Chmod(0o600), f.Close()); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Chmod(path+suffix, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func buildDSN(absPath string, readOnly bool, busyTimeoutMs int, syncMode string) string {
	q := url.Values{}
	q.Set("_busy_timeout", strconv.Itoa(busyTimeoutMs))
	q.Set("_foreign_keys", "1")
	if readOnly {
		q.Set("mode", "ro")
		q.Set("_query_only", "1")
	} else {
		q.Set("_journal_mode", "WAL")
		q.Set("_synchronous", syncMode)
		q.Set("_txlock", "immediate")
	}
	u := url.URL{
		Scheme:   "file",
		Path:     absPath,
		RawQuery: q.Encode(),
	}
	return u.String()
}

func verifyWriterPragmas(ctx context.Context, writer *sql.DB) error {
	var fk int
	if err := writer.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		return fmt.Errorf("check PRAGMA foreign_keys: %w", err)
	}
	if fk != 1 {
		return fmt.Errorf("expected PRAGMA foreign_keys=1, got %d", fk)
	}

	var mode string
	if err := writer.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("check PRAGMA journal_mode: %w", err)
	}
	if !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("expected PRAGMA journal_mode=wal, got %q", mode)
	}
	return nil
}

func migrate(ctx context.Context, writer *sql.DB) error {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	type migrationFile struct {
		version int
		name    string
		sql     string
	}
	var list []migrationFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return fmt.Errorf("invalid migration filename %q (expected NNN_name.sql)", entry.Name())
		}
		ver, err := strconv.Atoi(prefix)
		if err != nil || ver <= 0 {
			return fmt.Errorf("invalid migration version in %q: %w", entry.Name(), err)
		}
		raw, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		list = append(list, migrationFile{
			version: ver,
			name:    entry.Name(),
			sql:     string(raw),
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })

	if _, err := writer.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at_ms INTEGER NOT NULL
		) STRICT
	`); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	applied := make(map[int]bool)
	rows, err := writer.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for _, m := range list {
		if applied[m.version] {
			continue
		}
		tx, err := writer.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.name, err)
		}
		// Claim the version with a write inside the transaction. A CLI command and
		// the daemon may have both found this migration pending before either wrote.
		result, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, name, applied_at_ms) VALUES (?, ?, ?)
			 ON CONFLICT(version) DO NOTHING`,
			m.version, m.name, time.Now().UTC().UnixMilli(),
		)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("claim migration %s: %w", m.name, err)
		}
		if count == 1 {
			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("exec migration %s: %w", m.name, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
	}
	return nil
}

// Writer returns the single-connection serialized write pool.
func (db *DB) Writer() *sql.DB { return db.writer }

// Reader returns the concurrent read-only query pool.
func (db *DB) Reader() *sql.DB { return db.reader }

// Path returns the resolved absolute path to the primary SQLite file.
func (db *DB) Path() string { return db.path }

// Close closes both reader and writer connection pools.
func (db *DB) Close() error {
	var err1, err2 error
	if db.reader != nil {
		err1 = db.reader.Close()
	}
	if db.writer != nil {
		err2 = db.writer.Close()
	}
	return errors.Join(err1, err2)
}

// Ping verifies basic read responsiveness against the reader pool.
func (db *DB) Ping(ctx context.Context) error {
	var one int
	if err := db.reader.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return err
	}
	if one != 1 {
		return fmt.Errorf("unexpected SELECT 1 result: %d", one)
	}
	return nil
}

// QuickCheck runs SQLite's structural integrity check.
func (db *DB) QuickCheck(ctx context.Context) error {
	var result string
	if err := db.reader.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return fmt.Errorf("pragma quick_check: %w", err)
	}
	if !strings.EqualFold(result, "ok") {
		return fmt.Errorf("sqlite quick_check failed: %s", result)
	}
	return nil
}

// Optimize updates query planner statistics via PRAGMA optimize.
func (db *DB) Optimize(ctx context.Context) error {
	_, err := db.writer.ExecContext(ctx, "PRAGMA optimize")
	return err
}

// Checkpoint executes a WAL checkpoint using one of PASSIVE, FULL, RESTART, or TRUNCATE.
func (db *DB) Checkpoint(ctx context.Context, mode string) error {
	m := strings.ToUpper(strings.TrimSpace(mode))
	switch m {
	case "PASSIVE", "FULL", "RESTART", "TRUNCATE":
	default:
		return fmt.Errorf("invalid WAL checkpoint mode %q", mode)
	}
	_, err := db.writer.ExecContext(ctx, "PRAGMA wal_checkpoint("+m+")")
	return err
}

// Backup writes a transactionally consistent snapshot of the live WAL database to destPath via VACUUM INTO.
func (db *DB) Backup(ctx context.Context, destPath string) error {
	if strings.TrimSpace(destPath) == "" {
		return errors.New("backup destination path must not be empty")
	}
	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return fmt.Errorf("resolve backup path: %w", err)
	}
	if _, err := os.Stat(absDest); err == nil {
		return fmt.Errorf("backup destination already exists: %s", absDest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat backup destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absDest), 0o750); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if _, err := db.writer.ExecContext(ctx, "VACUUM INTO ?", absDest); err != nil {
		return fmt.Errorf("vacuum into %s: %w", absDest, err)
	}
	return nil
}

// FileSizes reports the byte sizes of the main DB file and optional -wal / -shm sidecars.
func (db *DB) FileSizes() (dbBytes, walBytes, shmBytes int64, err error) {
	dbBytes, err = statSize(db.path)
	if err != nil {
		return 0, 0, 0, err
	}
	walBytes, err = statSizeOrZero(db.path + "-wal")
	if err != nil {
		return 0, 0, 0, err
	}
	shmBytes, err = statSizeOrZero(db.path + "-shm")
	if err != nil {
		return 0, 0, 0, err
	}
	return dbBytes, walBytes, shmBytes, nil
}

func statSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func statSizeOrZero(path string) (int64, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// IsTransientSQLiteError detects SQLITE_BUSY or SQLITE_LOCKED (including extended codes).
func IsTransientSQLiteError(err error) bool {
	if err == nil {
		return false
	}
	var coder interface{ Code() int }
	if errors.As(err, &coder) {
		base := coder.Code() & 0xFF
		return base == sqliteBusy || base == sqliteLocked
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "SQLITE_LOCKED")
}

// IsUnavailable reports storage errors for which an HTTP client can retry later.
func IsUnavailable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, sql.ErrConnDone) || errors.Is(err, driver.ErrBadConn) {
		return true
	}
	var coder interface{ Code() int }
	if errors.As(err, &coder) {
		switch coder.Code() & 0xff {
		case sqlite.SQLITE_BUSY, sqlite.SQLITE_LOCKED, sqlite.SQLITE_READONLY, sqlite.SQLITE_IOERR,
			sqlite.SQLITE_FULL, sqlite.SQLITE_CANTOPEN, sqlite.SQLITE_PROTOCOL:
			return true
		}
	}
	// database/sql's closed-pool error is private.
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if cause.Error() == "sql: database is closed" {
			return true
		}
	}
	return false
}
