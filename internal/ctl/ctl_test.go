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
