package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"vektor/internal/alerts"
	"vektor/internal/auth"
	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/httpapi"
	"vektor/internal/metrics/scheduler"
	"vektor/internal/observability"
)

func TestHTTPAlertsCRUDAndCSRF(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(t.TempDir(), "data")
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	sessions := auth.NewStore(db, time.Hour)
	if err := sessions.Bootstrap(ctx, []byte("vektor-alerts-test-pass")); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	token, session, err := sessions.Login(ctx, auth.Username, []byte("vektor-alerts-test-pass"))
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	health := observability.NewHealthTracker()
	qs := history.NewQueryService(db, host.NewRegistry(db), cfg, health)
	hub := scheduler.NewLiveHub(health, nil, nil)
	alertStore := alerts.NewStore(db)
	alertNotifier := alerts.NewNotifier()

	api := httpapi.New(cfg.HTTP, qs, sessions, hub).WithAlerts(alertStore, alertNotifier)
	srv := httptest.NewServer(api)
	defer srv.Close()

	hookCalled := false
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hookCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	doReq := func(method, path string, body any, csrf bool) *http.Response {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}
		req, err := http.NewRequest(method, srv.URL+path, reader)
		if err != nil {
			t.Fatalf("new req: %v", err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.AddCookie(&http.Cookie{Name: "vektor_session", Value: token})
		if csrf {
			req.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do %s %s: %v", method, path, err)
		}
		return resp
	}

	// Missing CSRF rejected on POST
	resp := doReq(http.MethodPost, "/api/v1/alerts/rules", alerts.RuleInput{
		Name: "CPU", ResourceID: "host:local", MetricKey: "cpu.busy_pct", Operator: "gt", Threshold: 85, DurationSeconds: 60, Enabled: true,
	}, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF, got %d", resp.StatusCode)
	}

	// Create rule
	resp = doReq(http.MethodPost, "/api/v1/alerts/rules", alerts.RuleInput{
		Name: "CPU alta", ResourceID: "host:local", MetricKey: "cpu.busy_pct", Operator: "gt", Threshold: 85, DurationSeconds: 60, Enabled: true,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 create rule, got %d", resp.StatusCode)
	}
	var created alerts.Rule
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	// Create channel & test webhook
	resp = doReq(http.MethodPost, "/api/v1/alerts/channels", alerts.ChannelInput{
		Name: "Hook", Kind: "webhook", URL: hook.URL, Enabled: true,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 create channel, got %d", resp.StatusCode)
	}
	var ch alerts.Channel
	_ = json.NewDecoder(resp.Body).Decode(&ch)
	resp.Body.Close()

	resp = doReq(http.MethodPost, "/api/v1/alerts/channels/test", map[string]string{"url": hook.URL}, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !hookCalled {
		t.Fatalf("expected webhook test 200 and delivery, status=%d called=%v", resp.StatusCode, hookCalled)
	}

	// List events
	resp = doReq(http.MethodGet, "/api/v1/alerts/events", nil, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 list events, got %d", resp.StatusCode)
	}

	// Delete rule & channel
	resp = doReq(http.MethodDelete, "/api/v1/alerts/rules/"+strconv.FormatInt(created.ID, 10), nil, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 delete rule, got %d", resp.StatusCode)
	}
	resp = doReq(http.MethodDelete, "/api/v1/alerts/channels/"+strconv.FormatInt(ch.ID, 10), nil, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 delete channel, got %d", resp.StatusCode)
	}
}
