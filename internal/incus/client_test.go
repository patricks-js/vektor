package incus

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

func startMockIncus(t *testing.T, handler http.Handler) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "incus.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = ln.Close()
	})
	return sock
}

func TestClientStatusUnavailable(t *testing.T) {
	c := NewClient(filepath.Join(t.TempDir(), "missing.sock"))
	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Available {
		t.Fatalf("expected unavailable status, got %+v", st)
	}
}

func TestClientLifecycleAndResources(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /1.0", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":        "sync",
			"status_code": 200,
			"metadata": map[string]any{
				"environment": map[string]any{
					"server_version": "6.8",
					"kernel_version": "6.14.0",
					"driver":         "lxc | qemu",
					"storage":        "zfs | dir",
				},
			},
		})
	})
	mux.HandleFunc("GET /1.0/instances", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":        "sync",
			"status_code": 200,
			"metadata": []map[string]any{
				{
					"name":         "web-01",
					"type":         "container",
					"status":       "Running",
					"architecture": "x86_64",
					"profiles":     []string{"default"},
					"expanded_config": map[string]string{
						"limits.cpu":    "2",
						"limits.memory": "1GiB",
					},
					"snapshots": []map[string]any{{"name": "snap0"}},
					"state": map[string]any{
						"status":    "Running",
						"pid":       4242,
						"processes": 18,
						"cpu":       map[string]any{"usage": 123456},
						"memory":    map[string]any{"usage": 268435456, "usage_peak": 536870912},
						"disk":      map[string]any{"root": map[string]any{"usage": 1073741824}},
						"network": map[string]any{
							"eth0": map[string]any{
								"addresses": []map[string]any{
									{"family": "inet", "address": "10.0.10.21", "scope": "global"},
								},
							},
						},
					},
				},
			},
		})
	})
	mux.HandleFunc("PUT /1.0/instances/web-01/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":      "async",
			"operation": "/1.0/operations/op-1",
		})
	})
	mux.HandleFunc("GET /1.0/operations/op-1/wait", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":     "sync",
			"metadata": map[string]any{"status": "Success", "status_code": 200},
		})
	})
	mux.HandleFunc("GET /1.0/instances/web-01/snapshots", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "sync",
			"metadata": []map[string]any{
				{"name": "pre-upgrade", "stateful": false, "size": 4096},
			},
		})
	})
	mux.HandleFunc("GET /1.0/storage-pools", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":     "sync",
			"metadata": []map[string]any{{"name": "default", "driver": "zfs", "status": "Created"}},
		})
	})
	mux.HandleFunc("GET /1.0/storage-pools/default/resources", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":     "sync",
			"metadata": map[string]any{"space": map[string]any{"used": 1024, "total": 4096}},
		})
	})
	mux.HandleFunc("GET /1.0/networks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "sync",
			"metadata": []map[string]any{
				{"name": "incusbr0", "type": "bridge", "managed": true, "config": map[string]string{"ipv4.address": "10.0.10.1/24"}},
			},
		})
	})
	mux.HandleFunc("GET /1.0/profiles", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":     "sync",
			"metadata": []map[string]any{{"name": "default", "description": "Default Incus profile"}},
		})
	})

	sock := startMockIncus(t, mux)
	c := NewClient(sock)
	ctx := context.Background()

	st, err := c.Status(ctx)
	if err != nil || !st.Available || st.ServerVersion != "6.8" || st.RunningCount != 1 {
		t.Fatalf("unexpected status: %+v err=%v", st, err)
	}

	instances, err := c.ListInstances(ctx)
	if err != nil || len(instances) != 1 || instances[0].IPv4[0] != "10.0.10.21" || instances[0].SnapshotsCount != 1 {
		t.Fatalf("unexpected instances: %+v err=%v", instances, err)
	}

	if err := c.ChangeState(ctx, "web-01", "restart"); err != nil {
		t.Fatalf("change state failed: %v", err)
	}

	snaps, err := c.ListSnapshots(ctx, "web-01")
	if err != nil || len(snaps) != 1 || snaps[0].Name != "pre-upgrade" {
		t.Fatalf("unexpected snapshots: %+v err=%v", snaps, err)
	}

	res, err := c.ListResources(ctx)
	if err != nil || len(res.StoragePools) != 1 || res.StoragePools[0].TotalBytes != 4096 || len(res.Networks) != 1 || len(res.Profiles) != 1 {
		t.Fatalf("unexpected resources: %+v err=%v", res, err)
	}
}

func TestClientUpdateExecConsoleAndBackups(t *testing.T) {
	patched := false
	deletedLogs := 0
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /1.0/instances/web-01", func(w http.ResponseWriter, r *http.Request) {
		patched = true
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "status_code": 200})
	})
	mux.HandleFunc("POST /1.0/instances/web-01/exec", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "async", "operation": "/1.0/operations/exec-1"})
	})
	mux.HandleFunc("GET /1.0/operations/exec-1/wait", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "sync",
			"metadata": map[string]any{
				"status":      "Success",
				"status_code": 200,
				"metadata": map[string]any{
					"return": 0,
					"output": map[string]string{
						"1": "/1.0/instances/web-01/logs/exec_out",
						"2": "/1.0/instances/web-01/logs/exec_err",
					},
				},
			},
		})
	})
	mux.HandleFunc("GET /1.0/instances/web-01/logs/exec_out", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello from incus\n"))
	})
	mux.HandleFunc("GET /1.0/instances/web-01/logs/exec_err", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(""))
	})
	mux.HandleFunc("DELETE /1.0/instances/web-01/logs/{log}", func(w http.ResponseWriter, r *http.Request) {
		deletedLogs++
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /1.0/instances/web-01/console", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("systemd[1]: Reached target multi-user.target\n"))
	})
	mux.HandleFunc("GET /1.0/instances/web-01/backups", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "sync",
			"metadata": []map[string]any{
				{"name": "daily-01", "instance_only": true, "optimized_storage": true},
			},
		})
	})

	sock := startMockIncus(t, mux)
	c := NewClient(sock)
	ctx := context.Background()

	cpu := "4"
	autostart := true
	if err := c.UpdateInstance(ctx, "web-01", UpdateInstanceRequest{CPULimit: &cpu, Autostart: &autostart}); err != nil || !patched {
		t.Fatalf("update failed: err=%v patched=%v", err, patched)
	}

	execRes, err := c.Exec(ctx, "web-01", ExecRequest{Command: "echo hello"})
	if err != nil || execRes.ExitCode != 0 || execRes.Stdout != "hello from incus\n" || deletedLogs != 2 {
		t.Fatalf("unexpected exec result: %+v deletedLogs=%d err=%v", execRes, deletedLogs, err)
	}

	logText, err := c.ConsoleLog(ctx, "web-01")
	if err != nil || logText == "" {
		t.Fatalf("unexpected console log: %q err=%v", logText, err)
	}

	backups, err := c.ListBackups(ctx, "web-01")
	if err != nil || len(backups) != 1 || backups[0].Name != "daily-01" {
		t.Fatalf("unexpected backups: %+v err=%v", backups, err)
	}
}
