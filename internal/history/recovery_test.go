package history

import (
	"bufio"
	"bytes"
	"context"
	"database/sql/driver"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"modernc.org/sqlite"
	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/host"
	"vektor/internal/metrics"
)

var recoveryStart = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

func TestLateSampleSurvivesKillAndRestart(t *testing.T) {
	for _, tc := range []struct {
		phase                       string
		wantRaw, wantDirty, wantAgg int
	}{
		{"raw-before-dirty", 1, 0, 1},
		{"dirty-before-commit", 1, 0, 1},
		{"after-raw-commit", 2, 1, 1},
		{"between-rollups", 2, 1, 1},
		{"before-repair-commit", 2, 1, 1},
		{"during-retention", 2, 0, 2},
	} {
		t.Run(tc.phase, func(t *testing.T) {
			ctx := context.Background()
			cfg := config.Default()
			db, _ := openTestDB(t)
			cfg.DBPath = db.Path()
			clk := metrics.NewFakeClock(recoveryStart)
			writer := NewRawWriter(db, cfg, nil, nil, clk)
			rollup := NewRollupEngine(db, cfg, nil, nil)
			if _, err := writer.WriteBatch(ctx, []metrics.MetricPoint{recoveryPoint(0, 20)}); err != nil {
				t.Fatal(err)
			}
			clk.Advance(time.Hour + time.Minute)
			if _, err := rollup.Run(ctx, clk.Now().UnixMilli()); err != nil {
				t.Fatal(err)
			}
			switch tc.phase {
			case "between-rollups", "before-repair-commit", "during-retention":
				if _, err := writer.WriteBatch(ctx, []metrics.MetricPoint{recoveryPoint(30*time.Second, 80)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			killRecoveryWorker(t, cfg.DBPath, tc.phase)

			db, err := database.Open(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.QuickCheck(ctx); err != nil {
				t.Fatal(err)
			}
			var raw, dirty, medium, long, fence int
			err = db.Reader().QueryRowContext(ctx, `
				SELECT (SELECT COUNT(*) FROM metric_buckets WHERE resolution_s = 30),
				       (SELECT COUNT(*) FROM rollup_dirty),
				       (SELECT sample_count FROM metric_buckets WHERE resolution_s = 300),
				       (SELECT sample_count FROM metric_buckets WHERE resolution_s = 3600),
				       (SELECT COUNT(*) FROM system_meta WHERE key = 'raw_pruned_before_ms')
			`).Scan(&raw, &dirty, &medium, &long, &fence)
			if err != nil || raw != tc.wantRaw || dirty != tc.wantDirty || medium != tc.wantAgg || long != tc.wantAgg || fence != 0 {
				t.Fatalf("WAL recovery: raw=%d dirty=%d 5m=%d 1h=%d pruning fence=%d err=%v", raw, dirty, medium, long, fence, err)
			}

			writer = NewRawWriter(db, cfg, nil, nil, clk)
			point := recoveryPoint(30*time.Second, 80)
			if res, err := writer.WriteBatch(ctx, []metrics.MetricPoint{point}); err != nil || res.Written != 2-tc.wantRaw {
				t.Fatalf("retry late write: %+v, %v", res, err)
			}
			maint := NewMaintenanceRunner(db, NewRollupEngine(db, cfg, nil, nil), NewRetentionEngine(db, cfg, nil, nil), nil)
			query := NewQueryService(db, host.NewRegistry(db), cfg, nil)
			now := recoveryStart.Add(25 * time.Hour).UnixMilli()
			for range 2 {
				if _, _, err := maint.RunCycle(ctx, now); err != nil {
					t.Fatal(err)
				}
				for _, tier := range []struct {
					step     string
					expected int64
				}{{"5m", 10}, {"1h", 120}} {
					series, err := query.QuerySeries(ctx, SeriesQuery{
						ResourceID: host.HostResourceID, MetricKey: metrics.KeyLoad1m, Step: tier.step,
						FromMs: recoveryStart.UnixMilli(), ToMs: recoveryStart.Add(time.Hour).UnixMilli(),
					})
					if err != nil || len(series.Points) != 1 {
						t.Fatalf("%s after replay: %+v, %v", tier.step, series, err)
					}
					p := series.Points[0]
					if p.SampleCount != 2 || p.ExpectedSamples != tier.expected || p.Coverage != 2/float64(tier.expected) ||
						p.Avg != 50 || p.Min != 20 || p.Max != 80 || p.Last != 80 {
						t.Fatalf("%s after replay: %+v", tier.step, p)
					}
				}
			}
			if err := db.Reader().QueryRowContext(ctx, "SELECT COUNT(*) FROM metric_buckets WHERE resolution_s = 30").Scan(&raw); err != nil || raw != 0 {
				t.Fatalf("retention after restart: raw=%d err=%v", raw, err)
			}
			if res, err := writer.WriteBatch(ctx, []metrics.MetricPoint{point}); err != nil || res.SkippedExpired != 1 || res.Written != 0 {
				t.Fatalf("expired retry bypassed durable pruning fence: %+v, %v", res, err)
			}
		})
	}
}

func recoveryPoint(offset time.Duration, value float64) metrics.MetricPoint {
	return metrics.MetricPoint{
		ResourceID: host.HostResourceID, Key: metrics.KeyLoad1m,
		TimestampMs: recoveryStart.Add(offset).UnixMilli(), Value: value, Valid: true,
	}
}

func killRecoveryWorker(t *testing.T, path, phase string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecoveryCrashWorker$")
	cmd.Env = append(os.Environ(), "VEKTOR_TEST_CRASH_DB="+path, "VEKTOR_TEST_CRASH_PHASE="+phase)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	hold, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(output)
	var childOutput []string
	for scan.Scan() {
		if scan.Text() == "crash checkpoint" {
			if err := cmd.Process.Kill(); err != nil {
				t.Error(err)
			}
			err := cmd.Wait()
			status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if err == nil || !ok || status.Signal() != syscall.SIGKILL {
				t.Fatalf("expected SIGKILL at %s, got %v", phase, err)
			}
			return
		}
		childOutput = append(childOutput, scan.Text())
	}
	t.Fatalf("worker did not reach %s: %v %v stdout=%v stderr=%s", phase, scan.Err(), cmd.Wait(), childOutput, stderr.String())
}

func TestRecoveryCrashWorker(t *testing.T) {
	path := os.Getenv("VEKTOR_TEST_CRASH_DB")
	if path == "" {
		t.Skip("subprocess fixture")
	}
	phase := os.Getenv("VEKTOR_TEST_CRASH_PHASE")
	pause := func() {
		fmt.Println("crash checkpoint")
		var unused [1]byte
		_, _ = os.Stdin.Read(unused[:])
	}
	sqlite.MustRegisterScalarFunction("vektor_test_crash", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
		pause()
		return nil, fmt.Errorf("checkpoint released without SIGKILL")
	})
	ctx := context.Background()
	cfg := config.Default()
	cfg.DBPath = path
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if trigger := map[string]string{
		"raw-before-dirty":     "AFTER INSERT ON metric_buckets WHEN NEW.resolution_s = 30",
		"dirty-before-commit":  "AFTER INSERT ON rollup_dirty",
		"between-rollups":      "AFTER UPDATE ON metric_buckets WHEN NEW.resolution_s = 300",
		"before-repair-commit": "AFTER DELETE ON rollup_dirty",
		"during-retention":     "AFTER DELETE ON metric_buckets WHEN OLD.resolution_s = 30",
	}[phase]; trigger != "" {
		if _, err := db.Writer().ExecContext(ctx, "CREATE TEMP TRIGGER checkpoint "+trigger+" BEGIN SELECT vektor_test_crash(); END"); err != nil {
			t.Fatal(err)
		}
	}
	now := recoveryStart.Add(time.Hour + time.Minute)
	switch phase {
	case "raw-before-dirty", "dirty-before-commit", "after-raw-commit":
		writer := NewRawWriter(db, cfg, nil, nil, metrics.NewFakeClock(now))
		if _, err := writer.WriteBatch(ctx, []metrics.MetricPoint{recoveryPoint(30*time.Second, 80)}); err != nil {
			t.Fatal(err)
		}
		if phase == "after-raw-commit" {
			pause()
		}
	case "between-rollups", "before-repair-commit", "during-retention":
		if phase == "during-retention" {
			now = recoveryStart.Add(25 * time.Hour)
		}
		maint := NewMaintenanceRunner(db, NewRollupEngine(db, cfg, nil, nil), NewRetentionEngine(db, cfg, nil, nil), nil)
		if _, _, err := maint.RunCycle(ctx, now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("fault checkpoint unexpectedly returned")
}
