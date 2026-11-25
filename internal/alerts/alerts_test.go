package alerts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
)

func openTestDB(t *testing.T) *database.DB {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(t.TempDir(), "data")
	db, err := database.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestStoreCRUDAndValidation(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openTestDB(t))

	rule, err := store.CreateRule(ctx, RuleInput{
		Name:            "CPU alta",
		ResourceID:      "host:local",
		MetricKey:       "cpu.busy_pct",
		Operator:        "gt",
		Threshold:       85,
		DurationSeconds: 60,
		Enabled:         true,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if rule.ID <= 0 || !rule.Enabled {
		t.Fatalf("unexpected rule: %+v", rule)
	}

	updated, err := store.UpdateRule(ctx, rule.ID, RuleInput{
		Name:            "CPU crítica",
		ResourceID:      "host:local",
		MetricKey:       "cpu.busy_pct",
		Operator:        "gt",
		Threshold:       90,
		DurationSeconds: 120,
		Enabled:         false,
	})
	if err != nil {
		t.Fatalf("update rule: %v", err)
	}
	if updated.Threshold != 90 || updated.Enabled {
		t.Fatalf("unexpected updated rule: %+v", updated)
	}

	if _, err := store.CreateChannel(ctx, ChannelInput{Name: "Bad", URL: "ftp://example.com", Enabled: true}); err == nil {
		t.Fatal("expected ftp URL to fail validation")
	}
	ch, err := store.CreateChannel(ctx, ChannelInput{Name: "Webhook", URL: "https://example.com/hook", Enabled: true})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	channels, err := store.ListChannels(ctx)
	if err != nil || len(channels) != 1 || channels[0].ID != ch.ID {
		t.Fatalf("list channels: %v, %+v", err, channels)
	}

	if err := store.DeleteChannel(ctx, ch.ID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	if err := store.DeleteRule(ctx, rule.ID); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
}

func TestEvaluatorFiringResolutionAndWebhookDelivery(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	store := NewStore(db)

	var mu sync.Mutex
	var received []WebhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p WebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := store.CreateChannel(ctx, ChannelInput{Name: "Local hook", URL: srv.URL, Enabled: true}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := store.CreateRule(ctx, RuleInput{
		Name:            "CPU > 80%",
		ResourceID:      "host:local",
		MetricKey:       "cpu.busy_pct",
		Operator:        "gt",
		Threshold:       80,
		DurationSeconds: 60,
		Enabled:         true,
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	if _, err := db.Writer().ExecContext(ctx, `
		INSERT INTO resources (id, kind, external_key, display_name, metadata_json, active, first_seen_ms, last_seen_ms)
		VALUES ('host:local', 'host', 'host:local', 'test-host', '{}', 1, ?, ?)
	`, now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatalf("insert host resource: %v", err)
	}
	insertSample := func(bucketMs int64, val float64) {
		t.Helper()
		_, err := db.Writer().ExecContext(ctx, `
			INSERT OR REPLACE INTO metric_buckets (resource_id, metric_key, resolution_s, bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last, last_sample_ms)
			VALUES ('host:local', 'cpu.busy_pct', 30, ?, 3, ?, ?, ?, ?, ?)
		`, bucketMs, val*3, val, val, val, bucketMs+20_000)
		if err != nil {
			t.Fatalf("insert sample: %v", err)
		}
	}

	notifier := NewNotifier()
	if err := notifier.TestURL(ctx, srv.URL); err != nil {
		t.Fatalf("test webhook: %v", err)
	}

	eval := NewEvaluator(store, notifier, nil)
	eval.now = func() time.Time { return now }

	// First breach starts pending window (duration_seconds = 60).
	insertSample(now.UnixMilli(), 91.5)
	if err := eval.EvaluateOnce(ctx); err != nil {
		t.Fatalf("eval 1: %v", err)
	}
	events, _ := store.ListEvents(ctx, 10)
	if len(events) != 0 {
		t.Fatalf("expected no firing event before duration elapsed, got %d", len(events))
	}

	// After 60s still breaching -> fires.
	now = now.Add(60 * time.Second)
	insertSample(now.UnixMilli(), 93.0)
	if err := eval.EvaluateOnce(ctx); err != nil {
		t.Fatalf("eval 2: %v", err)
	}
	events, _ = store.ListEvents(ctx, 10)
	if len(events) != 1 || events[0].Status != "firing" {
		t.Fatalf("expected 1 firing event, got %+v", events)
	}

	// Drops below threshold -> resolves.
	now = now.Add(30 * time.Second)
	insertSample(now.UnixMilli(), 42.0)
	if err := eval.EvaluateOnce(ctx); err != nil {
		t.Fatalf("eval 3: %v", err)
	}
	events, _ = store.ListEvents(ctx, 10)
	if len(events) != 2 || events[0].Status != "resolved" {
		t.Fatalf("expected resolved event, got %+v", events)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 3 || received[0].Status != "test" || received[1].Status != "firing" || received[2].Status != "resolved" {
		t.Fatalf("unexpected webhook payloads: %+v", received)
	}
}

func TestEvaluatorAsyncWebhookDoesNotBlock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := openTestDB(t)
	store := NewStore(db)

	releaseHook := make(chan struct{})
	delivered := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-releaseHook
		w.WriteHeader(http.StatusOK)
		delivered <- struct{}{}
	}))
	defer srv.Close()

	if _, err := store.CreateChannel(ctx, ChannelInput{Name: "Slow hook", URL: srv.URL, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRule(ctx, RuleInput{
		Name: "CPU > 50%", ResourceID: "host:local", MetricKey: "cpu.busy_pct",
		Operator: "gt", Threshold: 50, DurationSeconds: 0, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Minute)
	_, _ = db.Writer().ExecContext(ctx, `INSERT INTO resources (id, kind, external_key, display_name, metadata_json, active, first_seen_ms, last_seen_ms) VALUES ('host:local', 'host', 'host:local', 'test', '{}', 1, ?, ?)`, now.UnixMilli(), now.UnixMilli())
	_, _ = db.Writer().ExecContext(ctx, `INSERT INTO metric_buckets (resource_id, metric_key, resolution_s, bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last, last_sample_ms) VALUES ('host:local', 'cpu.busy_pct', 30, ?, 1, 90, 90, 90, 90, ?)`, now.UnixMilli(), now.UnixMilli())

	eval := NewEvaluator(store, NewNotifier(), nil)
	eval.interval = time.Hour
	eval.now = func() time.Time { return now }

	runDone := make(chan error, 1)
	go func() { runDone <- eval.Run(ctx) }()

	// Wait until worker is active.
	for !eval.workerActive.Load() {
		time.Sleep(5 * time.Millisecond)
	}

	// EvaluateOnce must return immediately even though webhook server is blocked.
	start := time.Now()
	if err := eval.EvaluateOnce(ctx); err != nil {
		t.Fatalf("evaluate once: %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("EvaluateOnce blocked on slow webhook (%v)", time.Since(start))
	}

	close(releaseHook)
	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("async webhook worker did not deliver notification")
	}
	cancel()
	<-runDone
}
