package collector

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vektor/internal/metrics"
	"vektor/internal/metrics/tracker"
)

func writeFixtureFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func TestCPUAndLoadCollectorFixtures(t *testing.T) {
	proc := t.TempDir()
	writeFixtureFile(t, proc, "sys/kernel/random/boot_id", "boot-uuid-1\n")
	writeFixtureFile(t, proc, "stat", `cpu  100 20 30 850 10 5 5 0 0 0
cpu0 50 10 15 425 5 2 3 0 0 0
cpu1 50 10 15 425 5 3 2 0 0 0
intr 12345
`)
	writeFixtureFile(t, proc, "loadavg", "1.25 0.75 0.50 2/300 4123\n")

	bootID, err := ReadBootID(proc)
	if err != nil || bootID != "boot-uuid-1" {
		t.Fatalf("ReadBootID = %q, err=%v", bootID, err)
	}

	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	col := NewCPUCollector(proc, clk, true)
	snap1, err := col.Collect(context.Background())
	if err != nil {
		t.Fatalf("first Collect failed: %v", err)
	}
	if len(snap1.CPUSamples) != 3 {
		t.Fatalf("expected total + 2 cores (3 samples), got %d", len(snap1.CPUSamples))
	}
	if len(snap1.Samples) != 3 {
		t.Fatalf("expected 3 loadavg gauges, got %d", len(snap1.Samples))
	}

	norm := tracker.NewNormalizer(nil)
	res1 := norm.Normalize(metrics.ProfilePersist, bootID, snap1)
	// First tick: only loadavg gauges are valid; CPU rates have no baseline yet.
	if len(res1.Points) != 3 {
		t.Fatalf("expected 3 load points on first tick, got %+v", res1.Points)
	}

	// Second tick: +20 user, +10 system, +70 idle on total (100 total jiffies => 30% busy).
	clk.Advance(30 * time.Second)
	writeFixtureFile(t, proc, "stat", `cpu  120 20 40 920 10 5 5 0 0 0
cpu0 70 10 20 450 5 2 3 0 0 0
cpu1 50 10 20 470 5 3 2 0 0 0
`)
	snap2, err := col.Collect(context.Background())
	if err != nil {
		t.Fatalf("second Collect failed: %v", err)
	}
	res2 := norm.Normalize(metrics.ProfilePersist, bootID, snap2)
	assertPoint(t, res2.Points, metrics.HostResourceID, metrics.KeyCPUBusyPct, 30.0)
	assertPoint(t, res2.Points, metrics.HostResourceID, metrics.KeyCPUUserPct, 20.0)
	assertPoint(t, res2.Points, metrics.HostResourceID, metrics.KeyCPUSystemPct, 10.0)
	assertPoint(t, res2.Points, "cpu:0", metrics.KeyCPUCoreBusyPct, 50.0)
	assertPoint(t, res2.Points, "cpu:1", metrics.KeyCPUCoreBusyPct, 10.0)
	assertPoint(t, res2.Points, metrics.HostResourceID, metrics.KeyLoad1m, 1.25)

	// Partial failure: remove loadavg -> stat still returns partial snapshot with warning.
	_ = os.Remove(filepath.Join(proc, "loadavg"))
	snap3, err := col.Collect(context.Background())
	if err != nil {
		t.Fatalf("expected partial success when loadavg missing, got err=%v", err)
	}
	if len(snap3.Warnings) == 0 || len(snap3.CPUSamples) != 3 {
		t.Fatalf("expected warning + cpu samples on partial failure, got %+v", snap3)
	}
}

func TestMemoryCollectorFixtures(t *testing.T) {
	proc := t.TempDir()
	writeFixtureFile(t, proc, "meminfo", `MemTotal:       16000000 kB
MemFree:         2000000 kB
MemAvailable:    8000000 kB
Buffers:          500000 kB
Cached:          3500000 kB
SwapTotal:       4000000 kB
SwapFree:        3000000 kB
`)
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	col := NewMemoryCollector(proc, clk)
	snap, err := col.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect memory failed: %v", err)
	}

	norm := tracker.NewNormalizer(nil)
	res := norm.Normalize(metrics.ProfilePersist, "boot-1", snap)
	assertPoint(t, res.Points, metrics.HostResourceID, metrics.KeyMemoryTotalBytes, 16000000*1024)
	assertPoint(t, res.Points, metrics.HostResourceID, metrics.KeyMemoryUsedBytes, 8000000*1024)
	assertPoint(t, res.Points, metrics.HostResourceID, metrics.KeyMemoryUsedPct, 50.0)
	assertPoint(t, res.Points, metrics.HostResourceID, metrics.KeySwapUsedBytes, 1000000*1024)
	assertPoint(t, res.Points, metrics.HostResourceID, metrics.KeySwapUsedPct, 25.0)

	// SwapTotal: 0 -> must not emit swap.used_pct (avoid 0/0 NaN or fake pct).
	writeFixtureFile(t, proc, "meminfo", `MemTotal:       8000000 kB
MemFree:        4000000 kB
Buffers:        1000000 kB
Cached:         1000000 kB
SwapTotal:            0 kB
SwapFree:             0 kB
`)
	snap2, err := col.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect no-swap failed: %v", err)
	}
	res2 := norm.Normalize(metrics.ProfilePersist, "boot-1", snap2)
	assertPoint(t, res2.Points, metrics.HostResourceID, metrics.KeyMemoryAvailableBytes, 6000000*1024)
	for _, p := range res2.Points {
		if p.Key == metrics.KeySwapUsedPct {
			t.Fatalf("did not expect swap.used_pct when SwapTotal=0, got %+v", p)
		}
	}
}

func TestDiskIOAndNetworkCollectorsWithNormalization(t *testing.T) {
	proc := t.TempDir()
	writeFixtureFile(t, proc, "diskstats", ` 259 0 nvme0n1 100 0 1000 50 200 0 2000 100 0 500 600
 259 1 nvme0n1p1 80 0 800 40 150 0 1500 80 0 400 500
 253 0 dm-0 50 0 400 20 100 0 800 40 0 200 250
`)
	writeFixtureFile(t, proc, "net/dev", `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000000 1000 0 0 0 0 0 0 1000000 1000 0 0 0 0 0 0
enp1s0: 10000 100 1 0 0 0 0 0 20000 200 0 2 0 0 0 0
   br0: 10000 100 0 0 0 0 0 0 20000 200 0 0 0 0 0 0
`)

	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	diskCol := NewDiskIOCollector(proc, clk, func() map[string]string {
		return map[string]string{
			"nvme0n1": "disk:nvme0",
			"dm-0":    "disk:lvm-root",
		}
	})
	netCol := NewNetworkCollector(proc, clk, func() map[string]NICTarget {
		return map[string]NICTarget{
			"lo":     {ResourceID: "nic:lo", IncludeInHostAggregate: false},
			"enp1s0": {ResourceID: "nic:enp1s0", IncludeInHostAggregate: true},
			"br0":    {ResourceID: "nic:br0", IncludeInHostAggregate: false},
		}
	})

	norm := tracker.NewNormalizer(func(resID string) bool {
		return resID == "nic:enp1s0"
	})

	d1, err := diskCol.Collect(context.Background())
	if err != nil {
		t.Fatalf("diskCol 1 failed: %v", err)
	}
	n1, err := netCol.Collect(context.Background())
	if err != nil {
		t.Fatalf("netCol 1 failed: %v", err)
	}
	norm.Normalize(metrics.ProfilePersist, "boot-1", d1)
	norm.Normalize(metrics.ProfilePersist, "boot-1", n1)

	clk.Advance(10 * time.Second)
	writeFixtureFile(t, proc, "diskstats", ` 259 0 nvme0n1 150 0 1200 70 240 0 2400 140 0 750 850
 259 1 nvme0n1p1 90 0 900 45 160 0 1600 85 0 420 520
 253 0 dm-0 70 0 500 30 120 0 1000 50 0 300 350
`)
	writeFixtureFile(t, proc, "net/dev", `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 9999999 9999 0 0 0 0 0 0 9999999 9999 0 0 0 0 0 0
enp1s0: 20000 200 11 0 0 0 0 0 50000 400 0 12 0 0 0 0
   br0: 20000 200 0 0 0 0 0 0 50000 400 0 0 0 0 0 0
`)

	d2, err := diskCol.Collect(context.Background())
	if err != nil {
		t.Fatalf("diskCol 2 failed: %v", err)
	}
	n2, err := netCol.Collect(context.Background())
	if err != nil {
		t.Fatalf("netCol 2 failed: %v", err)
	}

	dRes := norm.Normalize(metrics.ProfilePersist, "boot-1", d2)
	// nvme0n1: +200 sectors read * 512 = 102400 bytes / 10s = 10240 B/s
	assertPoint(t, dRes.Points, "disk:nvme0", metrics.KeyDiskReadBytesPerSec, 10240.0)
	// nvme0n1: +400 sectors written * 512 = 204800 bytes / 10s = 20480 B/s
	assertPoint(t, dRes.Points, "disk:nvme0", metrics.KeyDiskWriteBytesPerSec, 20480.0)
	// dm-0: +200 sectors written * 512 = 102400 bytes / 10s = 10240 B/s
	assertPoint(t, dRes.Points, "disk:lvm-root", metrics.KeyDiskWriteBytesPerSec, 10240.0)
	for _, p := range dRes.Points {
		if p.ResourceID == "nvme0n1p1" || p.ResourceID == "disk:nvme0n1p1" {
			t.Fatalf("partition must not be reported when filtered out: %+v", p)
		}
	}

	nRes := norm.Normalize(metrics.ProfilePersist, "boot-1", n2)
	// enp1s0: +10000 rx / 10s = 1000 B/s; br0 also +1000 B/s, lo ignored in host aggregate.
	assertPoint(t, nRes.Points, "nic:enp1s0", metrics.KeyNetRxBytesPerSec, 1000.0)
	assertPoint(t, nRes.Points, "nic:br0", metrics.KeyNetRxBytesPerSec, 1000.0)
	assertPoint(t, nRes.Points, "nic:enp1s0", metrics.KeyNetRxErrorsPerSec, 1.0)
	assertPoint(t, nRes.Points, "nic:enp1s0", metrics.KeyNetTxDropsPerSec, 1.0)
	// Host aggregate must equal ONLY enp1s0 (1000 rx, 3000 tx), NOT enp1s0+br0+lo!
	assertPoint(t, nRes.Points, metrics.HostResourceID, metrics.KeyNetRxBytesPerSec, 1000.0)
	assertPoint(t, nRes.Points, metrics.HostResourceID, metrics.KeyNetTxBytesPerSec, 3000.0)
}

func TestLiveProcSmoke(t *testing.T) {
	clk := metrics.NewRealClock()
	bootID, err := ReadBootID("/proc")
	if err != nil || bootID == "" {
		t.Fatalf("ReadBootID(/proc) failed: %q %v", bootID, err)
	}
	if snap, err := NewCPUCollector("/proc", clk, true).Collect(context.Background()); err != nil || len(snap.CPUSamples) == 0 {
		t.Fatalf("live CPUCollector failed: %v", err)
	}
	if snap, err := NewMemoryCollector("/proc", clk).Collect(context.Background()); err != nil || len(snap.Samples) == 0 {
		t.Fatalf("live MemoryCollector failed: %v", err)
	}
	if snap, err := NewNetworkCollector("/proc", clk, nil).Collect(context.Background()); err != nil || len(snap.Samples) == 0 {
		t.Fatalf("live NetworkCollector failed: %v", err)
	}
}

func assertPoint(t *testing.T, pts []metrics.MetricPoint, resID, key string, want float64) {
	t.Helper()
	for _, p := range pts {
		if p.ResourceID == resID && p.Key == key {
			if !p.Valid {
				t.Fatalf("point %s/%s invalid", resID, key)
			}
			if math.Abs(p.Value-want) > 1e-6 {
				t.Fatalf("point %s/%s = %v, want %v", resID, key, p.Value, want)
			}
			return
		}
	}
	t.Fatalf("point %s/%s missing from %+v", resID, key, pts)
}
