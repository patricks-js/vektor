package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"vektor/internal/audit"
	"vektor/internal/auth"
	"vektor/internal/config"
	"vektor/internal/ctl"
	"vektor/internal/database"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/hostmgmt"
	"vektor/internal/httpapi"
	"vektor/internal/metrics/scheduler"
	"vektor/internal/observability"
)

type recordingExecutor struct {
	requests []ctl.Request
}

func (r *recordingExecutor) Execute(_ context.Context, req ctl.Request) (string, error) {
	r.requests = append(r.requests, req)
	return "ok:" + req.Action + ":" + req.Target, nil
}

func TestHTTPHostMgmtActionsCSRFAndAudit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Default()
	cfg.DataDir = filepath.Join(t.TempDir(), "data")
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	sessions := auth.NewStore(db, time.Hour)
	if err := sessions.Bootstrap(ctx, []byte("vektor-hostmgmt-test-pass")); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	token, session, err := sessions.Login(ctx, auth.Username, []byte("vektor-hostmgmt-test-pass"))
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	sock := filepath.Join(t.TempDir(), "ctl.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()

	execStub := &recordingExecutor{}
	ctlSrv := ctl.NewServer([]uint32{uint32(os.Getuid())}, execStub, 10*time.Second)
	go func() { _ = ctlSrv.Serve(ctx, ln) }()

	health := observability.NewHealthTracker()
	qs := history.NewQueryService(db, host.NewRegistry(db), cfg, health)
	hub := scheduler.NewLiveHub(health, nil, nil)
	auditStore := audit.NewStore(db)
	mgmt := hostmgmt.New("/proc")

	api := httpapi.New(cfg.HTTP, qs, sessions, hub).
		WithHostMgmt(mgmt).
		WithControl(ctl.NewClient(sock), auditStore)
	srv := httptest.NewServer(api)
	defer srv.Close()

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

	// GET /api/v1/processes works when authenticated
	resp := doReq(http.MethodGet, "/api/v1/processes?sort=memory&limit=5", nil, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 processes, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Missing CSRF on service action rejected with 403
	resp = doReq(http.MethodPost, "/api/v1/services/caddy.service/action", map[string]string{"action": "restart"}, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF, got %d", resp.StatusCode)
	}

	// Stopping vektord.service itself is rejected with 400
	resp = doReq(http.MethodPost, "/api/v1/services/vektord.service/action", map[string]string{"action": "stop"}, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 stopping vektord.service, got %d", resp.StatusCode)
	}

	// Protected PID 1 and own PID are rejected with 400
	for _, pid := range []int{1, os.Getpid()} {
		resp = doReq(http.MethodPost, "/api/v1/processes/"+strconv.Itoa(pid)+"/signal", map[string]string{"signal": "TERM"}, true)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for protected pid %d, got %d", pid, resp.StatusCode)
		}
	}

	// Valid service restart succeeds and writes audit_log
	resp = doReq(http.MethodPost, "/api/v1/services/caddy.service/action", map[string]string{"action": "restart"}, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 service restart, got %d", resp.StatusCode)
	}

	// Valid host power action succeeds and writes audit_log
	resp = doReq(http.MethodPost, "/api/v1/host/power", map[string]string{"action": "reboot"}, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 host reboot, got %d", resp.StatusCode)
	}

	// GET /api/v1/audit returns both entries
	resp = doReq(http.MethodGet, "/api/v1/audit?limit=10", nil, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 audit, got %d", resp.StatusCode)
	}
	var auditResp struct {
		Entries []audit.Entry `json:"entries"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&auditResp)
	resp.Body.Close()
	if len(auditResp.Entries) != 2 || auditResp.Entries[0].Action != "host.reboot" || auditResp.Entries[1].Action != "service.restart" {
		t.Fatalf("unexpected audit entries: %+v", auditResp.Entries)
	}
	if len(execStub.requests) != 2 {
		t.Fatalf("expected 2 executed helper requests, got %d", len(execStub.requests))
	}
}
