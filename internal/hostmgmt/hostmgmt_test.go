package hostmgmt

import (
	"context"
	"testing"
)

func TestParseProcStatAndUID(t *testing.T) {
	raw := "1234 (kworker/u16:2 (test)) S 2 0 0 0 -1 4194368 0 0 0 0 150 45 0 0 20 0 4 0 1000 0 256 18446744073709551615"
	p, ticks, startTime, ok := parseProcStat(1234, raw, 4096)
	if !ok {
		t.Fatal("expected parseProcStat to succeed")
	}
	if p.Name != "kworker/u16:2 (test)" || p.State != "S" || p.PPID != 2 || p.Threads != 4 {
		t.Fatalf("unexpected process: %+v", p)
	}
	if ticks != 195 || startTime != 1000 || p.RSSBytes != 256*4096 {
		t.Fatalf("unexpected ticks=%d startTime=%d rss=%d", ticks, startTime, p.RSSBytes)
	}

	uid, ok := parseProcUID("Name:\tvektord\nUid:\t998\t998\t998\t998\n")
	if !ok || uid != 998 {
		t.Fatalf("expected uid 998, got %d (ok=%v)", uid, ok)
	}
}

func TestListProcessesRealProc(t *testing.T) {
	mgr := New("/proc")
	procs, err := mgr.ListProcesses(context.Background(), "memory", 10)
	if err != nil {
		t.Fatalf("list processes: %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("expected at least one process from /proc")
	}
}

func TestValidUnitNameAndParsers(t *testing.T) {
	for _, good := range []string{"vektord.service", "ssh.service", "user@1000.service", "incus.service"} {
		if !ValidUnitName(good) {
			t.Errorf("expected %q to be valid", good)
		}
	}
	for _, bad := range []string{"", "-f", "--all", "foo;rm", "../etc/passwd", "a b.service"} {
		if ValidUnitName(bad) {
			t.Errorf("expected %q to be rejected", bad)
		}
	}

	unitsJSON := []byte(`[
		{"unit":"nginx.service","load":"loaded","active":"active","sub":"running","description":"Nginx"},
		{"unit":"broken.service","load":"loaded","active":"failed","sub":"failed","description":"Broken"},
		{"unit":"old.service","load":"loaded","active":"inactive","sub":"dead","description":"Old"}
	]`)
	services, err := parseSystemctlUnits(unitsJSON, map[string]string{"nginx.service": "enabled"}, "all")
	if err != nil {
		t.Fatalf("parse units: %v", err)
	}
	if len(services) != 3 || services[0].Name != "broken.service" || services[1].Name != "nginx.service" || services[1].UnitFile != "enabled" {
		t.Fatalf("unexpected sorted services: %+v", services)
	}

	failedOnly, err := parseSystemctlUnits(unitsJSON, nil, "failed")
	if err != nil || len(failedOnly) != 1 || failedOnly[0].Name != "broken.service" {
		t.Fatalf("unexpected failed filter: %+v", failedOnly)
	}

	journalRaw := []byte("{\"__REALTIME_TIMESTAMP\":\"1795474036524000\",\"PRIORITY\":\"3\",\"_SYSTEMD_UNIT\":\"vektord.service\",\"MESSAGE\":\"disk full\"}\n")
	logs := parseJournalJSON(journalRaw)
	if len(logs) != 1 || logs[0].Priority != "err" || logs[0].Message != "disk full" || logs[0].TimestampMs != 1795474036524 {
		t.Fatalf("unexpected journal logs: %+v", logs)
	}
}
