package diagnostics

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"vektor/internal/ctl"
	"vektor/internal/hostmgmt"
	"vektor/internal/incus"
	"vektor/internal/observability"
)

type fakeFileInfo struct{ mode os.FileMode }

func (f fakeFileInfo) Name() string       { return "resolv.conf" }
func (f fakeFileInfo) Size() int64        { return 64 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

func healthyRunner() *Runner {
	h := observability.NewHealthTracker()
	return &Runner{
		Health: h,
		DBPing: func(context.Context) error { return nil },
		readAdjtimex: func() (int64, error) {
			return 250_000, nil // 0.25s
		},
		unitState: func(_ context.Context, unit string) (string, string, string, bool) {
			switch unit {
			case "systemd-timesyncd.service", "systemd-resolved.service", "vektor-ctl.socket", "incus.service", "incus.socket":
				return "loaded", "active", "running", true
			default:
				return "not-found", "inactive", "dead", true
			}
		},
		unitLogs: func(context.Context, string, int, string) ([]hostmgmt.LogEntry, error) {
			return []hostmgmt.LogEntry{{Message: "ok"}}, nil
		},
		lookPath: func(string) (string, error) { return "/usr/bin/incus", nil },
		lstat:    func(string) (os.FileInfo, error) { return fakeFileInfo{mode: 0o644}, nil },
		stat:     func(string) (os.FileInfo, error) { return fakeFileInfo{mode: 0o644}, nil },
		readFile: func(string) ([]byte, error) { return []byte("nameserver 127.0.0.53\n"), nil },
		ctlIncusStatus: func(context.Context) (incus.Status, error) {
			return incus.Status{Available: true, ServerVersion: "6.8", InstanceCount: 2, RunningCount: 1}, nil
		},
	}
}

func findCheck(t *testing.T, rep Report, id string) Check {
	t.Helper()
	for _, c := range rep.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("check %q not found", id)
	return Check{}
}

func TestRunnerAllHealthy(t *testing.T) {
	rep := healthyRunner().Run(context.Background())
	if rep.Status != StatusOK {
		t.Fatalf("expected ok, got %s: %+v", rep.Status, rep.Checks)
	}
	if len(rep.Checks) != 6 {
		t.Fatalf("expected 6 checks, got %d", len(rep.Checks))
	}
}

func TestRunnerUnsyncedClock(t *testing.T) {
	r := healthyRunner()
	r.readAdjtimex = func() (int64, error) { return 16_000_000, nil }
	r.Health.SetClockError("Linux does not report a synchronized wall clock")

	rep := r.Run(context.Background())
	if rep.Status != StatusFail {
		t.Fatalf("expected fail, got %s", rep.Status)
	}
	c := findCheck(t, rep, "clock")
	if c.Status != StatusFail || !strings.Contains(c.Command, "systemd-timesyncd") {
		t.Fatalf("unexpected clock check: %+v", c)
	}
}

func TestRunnerBrokenResolvConfSymlink(t *testing.T) {
	r := healthyRunner()
	r.lstat = func(string) (os.FileInfo, error) { return fakeFileInfo{mode: os.ModeSymlink}, nil }
	r.stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	rep := r.Run(context.Background())
	c := findCheck(t, rep, "dns")
	if c.Status != StatusFail || !strings.Contains(c.Command, "stub-resolv.conf") {
		t.Fatalf("unexpected dns check: %+v", c)
	}
}

func TestRunnerIncusInstalledButConflictingProcess(t *testing.T) {
	r := healthyRunner()
	r.ctlIncusStatus = func(context.Context) (incus.Status, error) {
		return incus.Status{Available: false, Reason: "incus unix socket unavailable"}, nil
	}
	r.unitState = func(_ context.Context, unit string) (string, string, string, bool) {
		switch unit {
		case "incus.service":
			return "loaded", "failed", "failed", true
		case "incus.socket":
			return "loaded", "inactive", "dead", true
		case "snap.lxd.daemon.service":
			return "loaded", "active", "running", true
		case "systemd-timesyncd.service", "systemd-resolved.service", "vektor-ctl.socket":
			return "loaded", "active", "running", true
		default:
			return "not-found", "inactive", "dead", true
		}
	}
	r.unitLogs = func(_ context.Context, unit string, _ int, _ string) ([]hostmgmt.LogEntry, error) {
		if unit == "incus.service" {
			return []hostmgmt.LogEntry{{Priority: "err", Message: "dnsmasq: failed to create listening socket: Address already in use"}}, nil
		}
		return []hostmgmt.LogEntry{{Message: "ok"}}, nil
	}

	rep := r.Run(context.Background())
	c := findCheck(t, rep, "incus")
	if c.Status != StatusFail {
		t.Fatalf("expected fail for failed incus, got %+v", c)
	}
	if !strings.Contains(c.Detail, "snap.lxd.daemon.service") || !strings.Contains(c.Detail, "Address already in use") {
		t.Fatalf("expected conflict and log detail, got: %q", c.Detail)
	}
	if !strings.Contains(c.Hint, "Conflito") {
		t.Fatalf("expected conflict hint, got: %q", c.Hint)
	}
}

func TestRunnerIncusNotInstalledIsOptionalOK(t *testing.T) {
	r := healthyRunner()
	r.ctlIncusStatus = func(context.Context) (incus.Status, error) {
		return incus.Status{Available: false, Reason: "socket missing"}, nil
	}
	r.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	r.unitState = func(_ context.Context, unit string) (string, string, string, bool) {
		if unit == "incus.service" || unit == "incus.socket" {
			return "not-found", "inactive", "dead", true
		}
		return "loaded", "active", "running", true
	}

	rep := r.Run(context.Background())
	c := findCheck(t, rep, "incus")
	if c.Status != StatusOK {
		t.Fatalf("expected ok when incus is not installed, got %+v", c)
	}
}

func TestRunnerJournaldPermissionAndCollectorDetail(t *testing.T) {
	r := healthyRunner()
	r.Health.RecordCollectorErrorDetail("sensors", errors.New("hwmon read timeout"))
	r.unitLogs = func(context.Context, string, int, string) ([]hostmgmt.LogEntry, error) {
		return nil, hostmgmt.ErrPermission
	}
	r.stat = func(path string) (os.FileInfo, error) {
		if path == ctl.DefaultSocketPath {
			return nil, os.ErrNotExist
		}
		return fakeFileInfo{mode: 0o644}, nil
	}

	rep := r.Run(context.Background())
	if rep.Status != StatusWarn {
		t.Fatalf("expected warn overall, got %s", rep.Status)
	}
	dbCheck := findCheck(t, rep, "database")
	if dbCheck.Status != StatusWarn || !strings.Contains(dbCheck.Detail, "hwmon read timeout") {
		t.Fatalf("expected collector last error detail, got %+v", dbCheck)
	}
	jCheck := findCheck(t, rep, "journald")
	if jCheck.Status != StatusWarn || !strings.Contains(jCheck.Command, "systemd-journal") {
		t.Fatalf("expected journald permission warning, got %+v", jCheck)
	}
	ctlCheck := findCheck(t, rep, "ctl")
	if ctlCheck.Status != StatusWarn {
		t.Fatalf("expected ctl socket warning, got %+v", ctlCheck)
	}
}
