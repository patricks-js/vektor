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

func TestClientOCIProxiesAndBackupExport(t *testing.T) {
	var createdBody map[string]any
	var proxyPatched bool
	var proxyRemoved bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /1.0", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "sync",
			"metadata": map[string]any{
				"api_extensions": []string{"instance_oci"},
				"environment":    map[string]any{"server_version": "7.0", "driver": "lxc | qemu"},
			},
		})
	})
	mux.HandleFunc("GET /1.0/instances", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "sync",
			"metadata": []map[string]any{
				{
					"name":   "nginx-app",
					"type":   "container",
					"status": "Running",
					"config": map[string]string{"oci.entrypoint": "nginx", "environment.PORT": "80"},
					"devices": map[string]map[string]string{
						"http": {"type": "proxy", "listen": "tcp:0.0.0.0:8080", "connect": "tcp:127.0.0.1:80"},
					},
				},
			},
		})
	})
	mux.HandleFunc("POST /1.0/instances", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&createdBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "status_code": 200})
	})
	mux.HandleFunc("PATCH /1.0/instances/nginx-app", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["devices"]["https"] == nil {
			proxyRemoved = true
		} else {
			proxyPatched = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "status_code": 200})
	})
	mux.HandleFunc("GET /1.0/instances/nginx-app/backups/snap1/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("TARBALL-BYTES"))
	})

	sock := startMockIncus(t, mux)
	c := NewClient(sock)
	ctx := context.Background()

	st, err := c.Status(ctx)
	if err != nil || !st.OCISupported || st.OCICount != 1 {
		t.Fatalf("expected OCISupported=true OCICount=1, got %+v err=%v", st, err)
	}
	list, err := c.ListInstances(ctx)
	if err != nil || len(list) != 1 || !list[0].OCI || list[0].Env["PORT"] != "80" || len(list[0].Proxies) != 1 {
		t.Fatalf("unexpected OCI instance: %+v err=%v", list, err)
	}
	if err := c.CreateInstance(ctx, CreateInstanceRequest{
		Name: "redis-oci", Protocol: "oci", Image: "redis:7-alpine",
		Env: map[string]string{"REDIS_ARGS": "--appendonly yes"},
		Proxies: []ProxyDevice{{Name: "redis", Listen: "tcp:127.0.0.1:6379", Connect: "tcp:127.0.0.1:6379"}},
	}); err != nil {
		t.Fatalf("create OCI instance: %v", err)
	}
	src := createdBody["source"].(map[string]any)
	if src["protocol"] != "oci" || src["alias"] != "library/redis:7-alpine" {
		t.Fatalf("unexpected OCI source: %+v", src)
	}

	if err := c.AddProxy(ctx, "nginx-app", ProxyDevice{Name: "https", Listen: "tcp:0.0.0.0:8443", Connect: "tcp:127.0.0.1:443"}); err != nil || !proxyPatched {
		t.Fatalf("add proxy failed: err=%v patched=%v", err, proxyPatched)
	}
	if err := c.RemoveProxy(ctx, "nginx-app", "https"); err != nil || !proxyRemoved {
		t.Fatalf("remove proxy failed: err=%v removed=%v", err, proxyRemoved)
	}

	rc, err := c.ExportBackup(ctx, "nginx-app", "snap1")
	if err != nil {
		t.Fatalf("export backup: %v", err)
	}
	defer rc.Close()
	buf := make([]byte, 32)
	n, _ := rc.Read(buf)
	if string(buf[:n]) != "TARBALL-BYTES" {
		t.Fatalf("unexpected export bytes: %q", string(buf[:n]))
	}
}
