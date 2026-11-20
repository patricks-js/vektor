package host

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
)

func writeHostFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func TestCollectInventoryFixtures(t *testing.T) {
	proc := t.TempDir()
	etc := t.TempDir()
	writeHostFile(t, etc, "os-release", `NAME="Ubuntu"
VERSION_ID="26.04"
PRETTY_NAME="Ubuntu 26.04 LTS"
`)
	writeHostFile(t, proc, "version", "Linux version 6.14.0-10-generic (buildd@lcy02) #10 SMP\n")
	writeHostFile(t, proc, "cpuinfo", `processor	: 0
model name	: AMD Ryzen 7 7840U
physical id	: 0
core id		: 0

processor	: 1
model name	: AMD Ryzen 7 7840U
physical id	: 0
core id		: 0

processor	: 2
model name	: AMD Ryzen 7 7840U
physical id	: 0
core id		: 1

processor	: 3
model name	: AMD Ryzen 7 7840U
physical id	: 0
core id		: 1
`)
	writeHostFile(t, proc, "meminfo", "MemTotal:       32768000 kB\n")
	writeHostFile(t, proc, "sys/kernel/random/boot_id", "abc-boot-123\n")

	now := time.Date(2026, 11, 20, 14, 0, 0, 0, time.UTC)
	inv := CollectInventory(proc, etc, now)

	if inv.OSName != "Ubuntu" || inv.OSVersion != "26.04" || inv.Kernel != "6.14.0-10-generic" {
		t.Fatalf("unexpected OS/kernel inventory: %+v", inv)
	}
	if inv.CPUModel != "AMD Ryzen 7 7840U" || inv.CPUCores != 2 || inv.CPUThreads != 4 {
		t.Fatalf("unexpected CPU inventory: %+v", inv)
	}
	if inv.MemoryTotalBytes != 32768000*1024 || inv.BootID != "abc-boot-123" || inv.Arch != runtime.GOARCH {
		t.Fatalf("unexpected memory/boot inventory: %+v", inv)
	}
}

func TestRegistryReconcileAddRemoveReactivatePreservesIDAndHistory(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer db.Close()

	reg := NewRegistry(db)
	inv := Inventory{
		Hostname:         "homelab-01",
		OSPretty:         "Ubuntu 26.04 LTS",
		Kernel:           "6.14.0",
		Arch:             "amd64",
		CPUCores:         2,
		CPUThreads:       4,
		MemoryTotalBytes: 16 << 30,
		BootID:           "boot-1",
	}
	if err := reg.EnsureHost(ctx, inv, 1000); err != nil {
		t.Fatalf("EnsureHost: %v", err)
	}
	hostRes, gotInv, err := reg.GetHost(ctx)
	if err != nil {
		t.Fatalf("GetHost: %v", err)
	}
	if hostRes.ID != HostResourceID || gotInv.Hostname != "homelab-01" {
		t.Fatalf("unexpected host: %+v inv=%+v", hostRes, gotInv)
	}

	// Step 1: Add NIC enp1s0 (identified by stable MAC) and enp2s0.
	res1, err := reg.Reconcile(ctx, KindNIC, []DiscoveredResource{
		{ExternalKey: "mac:aa:bb:cc:dd:ee:01", DisplayName: "enp1s0", MetadataJSON: `{"iface":"enp1s0"}`},
		{ExternalKey: "mac:aa:bb:cc:dd:ee:02", DisplayName: "enp2s0", MetadataJSON: `{"iface":"enp2s0"}`},
	}, 1000)
	if err != nil || len(res1) != 2 {
		t.Fatalf("initial reconcile failed: %v %+v", err, res1)
	}
	id1 := res1[0].ID
	id2 := res1[1].ID

	// Insert historical sample for id2 so we verify removal keeps it intact.
	if _, err := db.Writer().ExecContext(ctx, `
		INSERT INTO metric_buckets(resource_id, metric_key, resolution_s, bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last, last_sample_ms)
		VALUES (?, 'net.rx_bytes_per_sec', 30, 0, 1, 500, 500, 500, 500, 1000)
	`, id2); err != nil {
		t.Fatalf("insert historical metric: %v", err)
	}

	// Step 2: enp2s0 disappears; enp1s0 renamed to eth0 keeping same external_key.
	res2, err := reg.Reconcile(ctx, KindNIC, []DiscoveredResource{
		{ExternalKey: "mac:aa:bb:cc:dd:ee:01", DisplayName: "eth0", MetadataJSON: `{"iface":"eth0"}`},
	}, 2000)
	if err != nil || len(res2) != 1 || res2[0].ID != id1 || res2[0].FirstSeenMs != 1000 {
		t.Fatalf("expected preserved ID and first_seen_ms on rename: %+v err=%v", res2, err)
	}

	activeOnly, err := reg.List(ctx, KindNIC, false)
	if err != nil || len(activeOnly) != 1 {
		t.Fatalf("expected 1 active NIC, got %+v err=%v", activeOnly, err)
	}
	allNICs, err := reg.List(ctx, KindNIC, true)
	if err != nil || len(allNICs) != 2 {
		t.Fatalf("expected 2 total NICs including inactive, got %+v err=%v", allNICs, err)
	}

	// Step 3: enp2s0 reappears -> reactivated with same resource_id, no duplicate row!
	res3, err := reg.Reconcile(ctx, KindNIC, []DiscoveredResource{
		{ExternalKey: "mac:aa:bb:cc:dd:ee:01", DisplayName: "eth0", MetadataJSON: `{"iface":"eth0"}`},
		{ExternalKey: "mac:aa:bb:cc:dd:ee:02", DisplayName: "enp2s0", MetadataJSON: `{"iface":"enp2s0"}`},
	}, 3000)
	if err != nil || len(res3) != 2 {
		t.Fatalf("reactivation reconcile failed: %v %+v", err, res3)
	}
	if res3[1].ID != id2 || !res3[1].Active || res3[1].FirstSeenMs != 1000 || res3[1].LastSeenMs != 3000 {
		t.Fatalf("expected reactivated resource %s preserving first_seen_ms=1000, got %+v", id2, res3[1])
	}
}
