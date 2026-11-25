package ctl

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type stubExec struct {
	last Request
}

func (s *stubExec) Execute(_ context.Context, req Request) (string, error) {
	s.last = req
	return "executed " + req.Action, nil
}

func TestValidateRequest(t *testing.T) {
	good := []Request{
		{Action: "service.restart", Target: "caddy.service"},
		{Action: "service.restart", Target: "vektord.service"},
		{Action: "process.signal", Target: "1234", Signal: "TERM"},
		{Action: "host.reboot"},
		{Action: "host.poweroff"},
	}
	for _, r := range good {
		if err := ValidateRequest(r); err != nil {
			t.Errorf("expected valid %+v, got %v", r, err)
		}
	}

	bad := []Request{
		{Action: "shell.exec", Target: "id"},
		{Action: "service.stop", Target: "vektord.service"},
		{Action: "service.disable", Target: "vektord.service"},
		{Action: "service.restart", Target: "--all"},
		{Action: "process.signal", Target: "1", Signal: "KILL"},
		{Action: "process.signal", Target: "100", Signal: "HUP"},
	}
	for _, r := range bad {
		if err := ValidateRequest(r); err == nil {
			t.Errorf("expected invalid %+v", r)
		}
	}
}

func TestUnixSocketPeerCredAndAllowlist(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ctl.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()

	exec := &stubExec{}
	srv := NewServer([]uint32{uint32(os.Getuid())}, exec, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()

	client := NewClient(sock)
	resp, err := client.Do(ctx, Request{Action: "service.restart", Target: "ssh.service"})
	if err != nil || !resp.OK {
		t.Fatalf("expected ok response, err=%v resp=%+v", err, resp)
	}
	if exec.last.Target != "ssh.service" {
		t.Fatalf("unexpected executed request: %+v", exec.last)
	}

	cancel()
	_ = ln.Close()
	<-done
}

type slowAndFastExec struct {
	started chan struct{}
	release chan struct{}
}

func (e *slowAndFastExec) Execute(ctx context.Context, req Request) (string, error) {
	if req.Target == "slow.service" {
		close(e.started)
		select {
		case <-e.release:
			return "slow done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "fast done", nil
}

func TestConcurrentRequestsDoNotBlockOrIdleExit(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ctl.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()

	exec := &slowAndFastExec{started: make(chan struct{}), release: make(chan struct{})}
	// Short idle timeout (100ms) verifies Serve does not exit while a slow op is in flight.
	srv := NewServer([]uint32{uint32(os.Getuid())}, exec, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()

	client := NewClient(sock)
	slowDone := make(chan error, 1)
	go func() {
		resp, err := client.Do(ctx, Request{Action: "service.restart", Target: "slow.service"})
		if err != nil || !resp.OK {
			slowDone <- err
			return
		}
		slowDone <- nil
	}()

	<-exec.started
	// Wait past the 100ms idle timeout while slow op is still running.
	time.Sleep(150 * time.Millisecond)

	// Fast request must succeed immediately without waiting for slow.service.
	fastResp, err := client.Do(ctx, Request{Action: "service.restart", Target: "fast.service"})
	if err != nil || !fastResp.OK || fastResp.Message != "fast done" {
		t.Fatalf("concurrent fast request failed: err=%v resp=%+v", err, fastResp)
	}

	close(exec.release)
	if err := <-slowDone; err != nil {
		t.Fatalf("slow request failed: %v", err)
	}

	// Once idle with 0 active requests, server should exit cleanly after ~100ms.
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected clean idle exit, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit after becoming idle")
	}
}
