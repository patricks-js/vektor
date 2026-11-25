package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vektor/internal/audit"
	"vektor/internal/auth"
	"vektor/internal/config"
	"vektor/internal/ctl"
	"vektor/internal/database"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/httpapi"
	"vektor/internal/incus"
	"vektor/internal/observability"
)

func TestIncusEndToEndThroughCtlAndAudit(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	// 1. Start mock Incus Unix socket
	incusSock := filepath.Join(tmp, "incus.sock")
	incusLn, err := net.Listen("unix", incusSock)
	if err != nil {
		t.Fatal(err)
	}
	defer incusLn.Close()

	stateCalls := 0
	incusMux := http.NewServeMux()
	incusMux.HandleFunc("GET /1.0", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "sync",
			"metadata": map[string]any{
				"environment": map[string]any{
					"server_version": "6.8",
					"driver":         "lxc | qemu",
					"storage":        "zfs",
				},
			},
		})
	})
	incusMux.HandleFunc("GET /1.0/instances", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "sync",
			"metadata": []map[string]any{
				{"name": "app-ct", "type": "container", "status": "Running", "profiles": []string{"default"}},
				{"name": "ubuntu-vm", "type": "virtual-machine", "status": "Stopped", "profiles": []string{"default"}},
			},
		})
	})
	incusMux.HandleFunc("PUT /1.0/instances/ubuntu-vm/state", func(w http.ResponseWriter, r *http.Request) {
		stateCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "status_code": 200})
	})
	incusMux.HandleFunc("PATCH /1.0/instances/app-ct", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "status_code": 200})
	})
	incusMux.HandleFunc("GET /1.0/instances/app-ct/backups/daily/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("ARCHIVE-STREAM-PAYLOAD"))
	})
	incusSrv := &http.Server{Handler: incusMux}
	go func() { _ = incusSrv.Serve(incusLn) }()
	defer incusSrv.Close()

	// 2. Start vektor-ctl Unix socket pointing to mock Incus
	ctlSock := filepath.Join(tmp, "ctl.sock")
	ctlLn, err := net.ListenUnix("unix", &net.UnixAddr{Name: ctlSock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer ctlLn.Close()
	ctlCtx, ctlCancel := context.WithCancel(ctx)
	defer ctlCancel()
	ctlSrv := ctl.NewServer([]uint32{uint32(os.Getuid())}, ctl.SystemExecutor{Incus: incus.NewClient(incusSock)}, 10*time.Second)
	go func() { _ = ctlSrv.Serve(ctlCtx, ctlLn) }()

	// 3. Start HTTP API with SQLite + auth + audit
	cfg := config.Default()
	cfg.DataDir = tmp
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessions := auth.NewStore(db, time.Hour)
	if err := sessions.Bootstrap(ctx, []byte("supersecretpass")); err != nil {
		t.Fatal(err)
	}
	reg := host.NewRegistry(db)
	health := observability.NewHealthTracker()
	qs := history.NewQueryService(db, reg, cfg, health)
	auditStore := audit.NewStore(db)
	api := httpapi.New(cfg.HTTP, qs, sessions, nil).WithControl(ctl.NewClient(ctlSock), auditStore)

	srv := httptest.NewServer(api)
	defer srv.Close()

	// Authenticate
	loginBody := bytes.NewBufferString(`{"username":"admin","password":"supersecretpass"}`)
	loginReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("Origin", srv.URL)
	loginResp, err := srv.Client().Do(loginReq)
	if err != nil || loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: err=%v status=%v", err, loginResp)
	}
	var sessionInfo struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.NewDecoder(loginResp.Body).Decode(&sessionInfo)
	loginResp.Body.Close()
	cookies := loginResp.Cookies()

	doReq := func(method, path, body string) *http.Response {
		t.Helper()
		var rdr *bytes.Buffer
		if body != "" {
			rdr = bytes.NewBufferString(body)
		} else {
			rdr = bytes.NewBuffer(nil)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rdr)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if method != http.MethodGet {
			req.Header.Set("Origin", srv.URL)
			req.Header.Set("X-CSRF-Token", sessionInfo.CSRFToken)
		}
		for _, c := range cookies {
			req.AddCookie(c)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}

	// GET /api/v1/incus/status
	stResp := doReq(http.MethodGet, "/api/v1/incus/status", "")
	defer stResp.Body.Close()
	if stResp.StatusCode != http.StatusOK {
		t.Fatalf("status HTTP %d", stResp.StatusCode)
	}
	var st incus.Status
	_ = json.NewDecoder(stResp.Body).Decode(&st)
	if !st.Available || st.InstanceCount != 2 || st.VMCount != 1 || st.ContainerCount != 1 {
		t.Fatalf("unexpected incus status: %+v", st)
	}

	// GET /api/v1/incus/instances
	listResp := doReq(http.MethodGet, "/api/v1/incus/instances", "")
	defer listResp.Body.Close()
	var listPayload struct {
		Instances []incus.Instance `json:"instances"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&listPayload)
	if len(listPayload.Instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(listPayload.Instances))
	}

	// POST /api/v1/incus/instances/ubuntu-vm/state
	actResp := doReq(http.MethodPost, "/api/v1/incus/instances/ubuntu-vm/state", `{"action":"start"}`)
	defer actResp.Body.Close()
	if actResp.StatusCode != http.StatusOK || stateCalls != 1 {
		t.Fatalf("state action failed: HTTP %d stateCalls=%d", actResp.StatusCode, stateCalls)
	}

	// Verify audit trail recorded incus.instance.start
	entries, err := auditStore.List(ctx, 10)
	if err != nil || len(entries) == 0 || entries[0].Action != "incus.instance.start" || entries[0].Target != "incus:ubuntu-vm" {
		t.Fatalf("expected audit entry for incus.instance.start, got %+v err=%v", entries, err)
	}

	// SSRF protection: reject internal/non-HTTPS image server on create
	badSrvResp := doReq(http.MethodPost, "/api/v1/incus/instances", `{"name":"evil-ct","type":"container","protocol":"oci","server":"http://169.254.169.254","image":"nginx:alpine"}`)
	badSrvResp.Body.Close()
	if badSrvResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for SSRF image server, got %d", badSrvResp.StatusCode)
	}

	// Invalid proxy port 99999 rejected
	badProxyResp := doReq(http.MethodPost, "/api/v1/incus/instances/app-ct/proxies", `{"name":"bad","listen":"tcp:0.0.0.0:99999","connect":"tcp:127.0.0.1:80"}`)
	badProxyResp.Body.Close()
	if badProxyResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range proxy port, got %d", badProxyResp.StatusCode)
	}

	// Valid proxy add + remove + PATCH update
	addProxyResp := doReq(http.MethodPost, "/api/v1/incus/instances/app-ct/proxies", `{"name":"http","listen":"tcp:0.0.0.0:8080","connect":"tcp:127.0.0.1:80"}`)
	addProxyResp.Body.Close()
	if addProxyResp.StatusCode != http.StatusOK {
		t.Fatalf("add proxy failed: HTTP %d", addProxyResp.StatusCode)
	}
	delProxyResp := doReq(http.MethodDelete, "/api/v1/incus/instances/app-ct/proxies/http", "")
	delProxyResp.Body.Close()
	if delProxyResp.StatusCode != http.StatusOK {
		t.Fatalf("remove proxy failed: HTTP %d", delProxyResp.StatusCode)
	}
	patchResp := doReq(http.MethodPatch, "/api/v1/incus/instances/app-ct", `{"cpu_limit":"2","env":{"TZ":"UTC"}}`)
	patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("patch instance failed: HTTP %d", patchResp.StatusCode)
	}

	// Streaming backup export through vektor-ctl DoStream + audit verification
	expResp := doReq(http.MethodGet, "/api/v1/incus/instances/app-ct/backups/daily/export", "")
	defer expResp.Body.Close()
	rawArchive, _ := io.ReadAll(expResp.Body)
	if expResp.StatusCode != http.StatusOK || string(rawArchive) != "ARCHIVE-STREAM-PAYLOAD" {
		t.Fatalf("unexpected backup export: HTTP %d body=%q", expResp.StatusCode, string(rawArchive))
	}
	entries, err = auditStore.List(ctx, 10)
	if err != nil || len(entries) == 0 || entries[0].Action != "incus.backup.export" || entries[0].Status != "ok" {
		t.Fatalf("expected audit entry for incus.backup.export, got %+v err=%v", entries, err)
	}
}
