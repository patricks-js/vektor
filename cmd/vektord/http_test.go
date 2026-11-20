package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"vektor/internal/auth"
	"vektor/internal/history"
	"vektor/migrations"
)

const processTestPassword = "example test administrator"

func TestVektordChildProcess(t *testing.T) {
	if os.Getenv("VEKTOR_TEST_CHILD") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"vektord"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing child process arguments")
}

func daemonCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestVektordChildProcess$", "--"}, args...)...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "VEKTOR_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "VEKTOR_TEST_CHILD=1")
	return cmd
}

type daemonLog struct {
	mu      sync.Mutex
	body    bytes.Buffer
	address chan string
}

func (l *daemonLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var line struct {
		Message string `json:"msg"`
		Address string `json:"address"`
	}
	for _, raw := range bytes.Split(p, []byte("\n")) {
		if json.Unmarshal(raw, &line) == nil && line.Message == "HTTP listener bound" && line.Address != "" {
			select {
			case l.address <- line.Address:
			default:
			}
		}
	}
	return l.body.Write(p)
}

func (l *daemonLog) safe(t *testing.T, secrets ...string) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(l.body.String(), secret) {
			t.Fatal("a credential, session cookie or CSRF token appeared in daemon logs")
		}
	}
}

type testDaemon struct {
	cmd  *exec.Cmd
	log  *daemonLog
	done chan struct{}
	err  error
	addr string
}

func startTestDaemon(t *testing.T, dir, address string) *testDaemon {
	t.Helper()
	d := &testDaemon{
		cmd:  daemonCommand(t, "--data-dir", dir, "--http-addr", address),
		log:  &daemonLog{address: make(chan string, 1)},
		done: make(chan struct{}),
	}
	d.cmd.Stderr = d.log
	if err := d.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		d.err = d.cmd.Wait()
		close(d.done)
	}()
	t.Cleanup(func() {
		select {
		case <-d.done:
		default:
			_ = d.cmd.Process.Kill()
			<-d.done
		}
	})
	select {
	case d.addr = <-d.log.address:
	case <-d.done:
		t.Fatalf("vektord exited before binding: %v", d.err)
	case <-time.After(10 * time.Second):
		t.Fatal("vektord did not bind")
	}
	return d
}

func (d *testDaemon) stop(t *testing.T, signal os.Signal) {
	t.Helper()
	if err := d.cmd.Process.Signal(signal); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.done:
		if d.err != nil {
			t.Fatalf("vektord shutdown: %v", d.err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("vektord did not exit after signal")
	}
	conn, err := net.DialTimeout("tcp", d.addr, time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("vektord still accepts connections after shutdown")
	}
}

func seedV01History(t *testing.T, dir string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "vektor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i, name := range []string{"001_initial_schema.sql", "002_rollup_repairs.sql"} {
		schema, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(schema)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (?, ?, 0)`, i+1, name); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().Add(-5 * time.Minute).Truncate(30 * time.Second).UnixMilli()
	if _, err := db.Exec(`
		INSERT INTO resources VALUES ('host:local', 'host', 'local', 'legacy', '{"hostname":"legacy"}', 1, ?, ?);
		INSERT INTO metric_buckets VALUES ('host:local','load.1m',30,?,1,12,12,12,12,?)
	`, at, at, at, at); err != nil {
		t.Fatal(err)
	}
	return at
}

func TestDaemonUpgradeRestartAndSignals(t *testing.T) {
	dir := t.TempDir()
	at := seedV01History(t, dir)

	first := startTestDaemon(t, dir, "127.0.0.1:0")
	base := "http://" + first.addr
	client := &http.Client{Timeout: 8 * time.Second}
	defer client.CloseIdleConnections()
	call := func(method, path, body string, cookie *http.Cookie, status int) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(context.Background(), method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			response.Body.Close()
			t.Fatalf("%s %s status=%d, want %d", method, path, response.StatusCode, status)
		}
		return response
	}
	call("GET", "/api/v1/host", "", nil, 401).Body.Close()

	bootstrap := daemonCommand(t, "bootstrap-admin", "--data-dir", dir)
	bootstrap.Stdin = strings.NewReader(processTestPassword + "\n")
	out, err := bootstrap.CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("Administrator created")) || bytes.Contains(out, []byte(processTestPassword)) {
		t.Fatalf("bootstrap command failed or emitted a secret: %v", err)
	}
	duplicated := daemonCommand(t, "bootstrap-admin", "--data-dir", dir)
	duplicated.Stdin = strings.NewReader("a different example password\n")
	out, err = duplicated.CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte(auth.ErrAlreadyBootstrapped.Error())) {
		t.Fatalf("duplicate bootstrap did not fail as expected: %v", err)
	}
	if state, err := os.Stat(filepath.Join(dir, "vektor.db")); err != nil || state.Mode().Perm() != 0o600 {
		t.Fatalf("database was not restricted to its owner: %v", err)
	}

	login := call("POST", "/api/v1/auth/login", `{"username":"admin","password":"`+processTestPassword+`"}`, nil, 200)
	var session auth.Session
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil || len(login.Cookies()) != 1 || session.CSRFToken == "" {
		t.Fatalf("invalid login response: %v", err)
	}
	login.Body.Close()
	cookie := login.Cookies()[0]
	historyPath := fmt.Sprintf("/api/v1/metrics?resource_id=host:local&metric=load.1m&from=%d&to=%d&step=30", at, at+30_000)
	readHistory := func() history.SeriesResult {
		t.Helper()
		response := call("GET", historyPath, "", cookie, 200)
		defer response.Body.Close()
		var series history.SeriesResult
		if err := json.NewDecoder(response.Body).Decode(&series); err != nil || len(series.Points) != 1 || series.Points[0].Avg != 12 {
			t.Fatalf("pre-v0.2 history not readable: %+v, err=%v", series, err)
		}
		return series
	}
	previous := readHistory()

	stream := call("GET", "/api/v1/stream/live", "", cookie, 200)
	defer stream.Body.Close()
	first.stop(t, syscall.SIGTERM)
	if _, err := io.Copy(io.Discard, stream.Body); err != nil {
		t.Fatalf("stream did not end gracefully after SIGTERM: %v", err)
	}
	first.log.safe(t, processTestPassword, cookie.Value, session.CSRFToken)

	second := startTestDaemon(t, dir, first.addr)
	current := call("GET", "/api/v1/auth/me", "", cookie, 200)
	var restored auth.Session
	if err := json.NewDecoder(current.Body).Decode(&restored); err != nil || restored != session {
		t.Fatalf("session did not survive restart: %v", err)
	}
	current.Body.Close()
	if actual := readHistory(); actual.Points[0] != previous.Points[0] || actual.ResolutionS != previous.ResolutionS {
		t.Fatalf("historical bucket changed on restart: %+v", actual)
	}

	revoke := daemonCommand(t, "revoke-sessions", "--data-dir", dir)
	if out, err := revoke.CombinedOutput(); err != nil || !bytes.Contains(out, []byte("Administrative sessions revoked")) {
		t.Fatalf("CLI revocation failed: %v", err)
	}
	call("GET", "/api/v1/auth/me", "", cookie, 401).Body.Close()
	second.stop(t, syscall.SIGINT)
	second.log.safe(t, processTestPassword, cookie.Value, session.CSRFToken)
}
