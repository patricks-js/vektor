package collector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/metrics/tracker"
)

func createSymlink(t *testing.T, root, linkRel, target string) {
	t.Helper()
	linkPath := filepath.Join(root, linkRel)
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatalf("mkdir symlink parent: %v", err)
	}
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatalf("symlink %s -> %s: %v", linkPath, target, err)
	}
}

func TestUbuntuLVMBridgesBondsSensorsDiscoveryAndNoNetDoubleCount(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	proc := filepath.Join(tmp, "proc")
	sys := filepath.Join(tmp, "sys")
	devDisk := filepath.Join(tmp, "dev_disk")

	// 1. Block devices: nvme0n1, LVM dm-0, plus ignored zram0 & loop0.
	writeFixtureFile(t, sys, "block/nvme0n1/size", "1953525168\n")
	writeFixtureFile(t, sys, "block/nvme0n1/queue/rotational", "0\n")
	writeFixtureFile(t, sys, "block/nvme0n1/device/model", "Samsung SSD 980\n")
	writeFixtureFile(t, sys, "block/nvme0n1/device/serial", "S64G1234\n")
	writeFixtureFile(t, sys, "block/nvme0n1/device/wwid", "nvme.144d-S64G1234\n")

	writeFixtureFile(t, sys, "block/dm-0/size", "209715200\n")
	writeFixtureFile(t, sys, "block/dm-0/dm/name", "ubuntu--vg-ubuntu--lv\n")
	writeFixtureFile(t, sys, "block/dm-0/dm/uuid", "LVM-abcdef123456\n")
	writeFixtureFile(t, sys, "block/dm-0/slaves/nvme0n1p3", "")

	writeFixtureFile(t, sys, "block/loop0/size", "1024\n")
	writeFixtureFile(t, sys, "block/zram0/size", "8192\n")

	// 2. Mountinfo: root LVM ext4, /boot/efi, escaped space mount, bind mount duplicate, tmpfs & fuse.sshfs & nfs4 ignored.
	createSymlink(t, devDisk, "by-uuid/1111-2222-3333", "../../dm-0")
	createSymlink(t, devDisk, "by-uuid/ABCD-EF01", "../../nvme0n1p1")
	writeFixtureFile(t, proc, "self/mountinfo", `25 1 253:0 / / rw,relatime shared:1 - ext4 /dev/mapper/ubuntu--vg-ubuntu--lv rw
26 25 253:0 / /var/lib/bindcopy rw,relatime - ext4 /dev/mapper/ubuntu--vg-ubuntu--lv rw
27 25 259:1 / /boot/efi rw,relatime - vfat /dev/nvme0n1p1 rw
28 25 8:16 / /mnt/data\040store rw,relatime - ext4 /dev/sdb1 rw
29 25 0:25 / /run tmpfs rw - tmpfs tmpfs rw
30 25 0:44 / /mnt/remote rw - nfs4 10.0.0.5:/export rw
31 25 0:45 / /mnt/ssh rw - fuse.sshfs user@nas:/media rw
`)

	// 3. NICs: lo, enp1s0 -> br0 <- veth1, and enp2s0 + enp3s0 -> bond0.
	// lo
	writeFixtureFile(t, sys, "class/net/lo/ifindex", "1\n")
	writeFixtureFile(t, sys, "class/net/lo/type", "772\n")
	writeFixtureFile(t, sys, "class/net/lo/address", "00:00:00:00:00:00\n")
	writeFixtureFile(t, sys, "class/net/lo/operstate", "unknown\n")
	createSymlink(t, sys, "class/net/lo/device", "../../../devices/virtual/net/lo/device")

	// enp1s0 (physical NIC bridged to br0)
	writeFixtureFile(t, sys, "class/net/enp1s0/ifindex", "2\n")
	writeFixtureFile(t, sys, "class/net/enp1s0/type", "1\n")
	writeFixtureFile(t, sys, "class/net/enp1s0/address", "52:54:00:11:22:01\n")
	writeFixtureFile(t, sys, "class/net/enp1s0/operstate", "up\n")
	createSymlink(t, sys, "class/net/enp1s0/device", "../../../devices/pci0000:00/0000:01:00.0")
	createSymlink(t, sys, "class/net/enp1s0/master", "../../br0")

	// br0 (bridge)
	writeFixtureFile(t, sys, "class/net/br0/ifindex", "3\n")
	writeFixtureFile(t, sys, "class/net/br0/type", "1\n")
	writeFixtureFile(t, sys, "class/net/br0/address", "52:54:00:aa:bb:01\n")
	writeFixtureFile(t, sys, "class/net/br0/operstate", "up\n")
	if err := os.MkdirAll(filepath.Join(sys, "class/net/br0/bridge"), 0o755); err != nil {
		t.Fatal(err)
	}

	// veth1 (virtual container veth bridged to br0)
	writeFixtureFile(t, sys, "class/net/veth1/ifindex", "4\n")
	writeFixtureFile(t, sys, "class/net/veth1/type", "1\n")
	writeFixtureFile(t, sys, "class/net/veth1/address", "fe:54:00:99:88:01\n")
	writeFixtureFile(t, sys, "class/net/veth1/operstate", "up\n")
	createSymlink(t, sys, "class/net/veth1/master", "../../br0")

	// enp2s0 & enp3s0 enslaved to bond0
	for idx, iface := range []struct {
		name string
		ifx  string
		mac  string
		pci  string
	}{
		{"enp2s0", "5", "52:54:00:11:22:02", "0000:02:00.0"},
		{"enp3s0", "6", "52:54:00:11:22:03", "0000:03:00.0"},
	} {
		_ = idx
		writeFixtureFile(t, sys, "class/net/"+iface.name+"/ifindex", iface.ifx+"\n")
		writeFixtureFile(t, sys, "class/net/"+iface.name+"/type", "1\n")
		writeFixtureFile(t, sys, "class/net/"+iface.name+"/address", iface.mac+"\n")
		writeFixtureFile(t, sys, "class/net/"+iface.name+"/operstate", "up\n")
		createSymlink(t, sys, "class/net/"+iface.name+"/device", "../../../devices/pci0000:00/"+iface.pci)
		createSymlink(t, sys, "class/net/"+iface.name+"/master", "../../bond0")
	}
	writeFixtureFile(t, sys, "class/net/bond0/ifindex", "7\n")
	writeFixtureFile(t, sys, "class/net/bond0/type", "1\n")
	writeFixtureFile(t, sys, "class/net/bond0/address", "52:54:00:11:22:02\n")
	writeFixtureFile(t, sys, "class/net/bond0/operstate", "up\n")
	if err := os.MkdirAll(filepath.Join(sys, "class/net/bond0/bonding"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 4. Sensors: hwmon0 (coretemp) + unreadable sensor
	writeFixtureFile(t, sys, "class/hwmon/hwmon0/name", "coretemp\n")
	createSymlink(t, sys, "class/hwmon/hwmon0/device", "../../../devices/platform/coretemp.0")
	writeFixtureFile(t, sys, "class/hwmon/hwmon0/temp1_input", "47500\n")
	writeFixtureFile(t, sys, "class/hwmon/hwmon0/temp1_label", "Package id 0\n")
	writeFixtureFile(t, sys, "class/hwmon/hwmon0/temp2_input", "not-a-number\n")

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer db.Close()

	reg := host.NewRegistry(db)
	if err := reg.EnsureHost(ctx, host.Inventory{Hostname: "ubuntu-lab", BootID: "boot-1"}, 1000); err != nil {
		t.Fatal(err)
	}

	disc := NewDiscovery(proc, sys, devDisk, cfg.ExcludedFSTypes, true)
	writeFixtureFile(t, proc, "stat", "cpu 10 0 0 10\ncpu0 5 0 0 5\ncpu2 5 0 0 5\n")
	if err := disc.ReconcileAll(ctx, reg, 1000, true); err != nil {
		t.Fatalf("ReconcileAll: %v", err)
	}
	cores, err := reg.List(ctx, host.KindCPUCore, false)
	if err != nil || len(cores) != 2 || cores[0].ID != "cpu:0" || cores[1].ID != "cpu:2" {
		t.Fatalf("expected real CPU IDs 0 and 2: %+v, %v", cores, err)
	}

	// Verify disks: dm-0 + nvme0n1 only (no loop0/zram0).
	disks := disc.Topology().DiskLookup()
	if len(disks) != 2 || disks["dm-0"] == "" || disks["nvme0n1"] == "" {
		t.Fatalf("unexpected discovered disks: %+v", disks)
	}
	diskRes, _ := reg.List(ctx, host.KindDisk, false)
	foundLVMKey := false
	for _, r := range diskRes {
		if r.ExternalKey == "dm:LVM-abcdef123456" && r.DisplayName == "ubuntu--vg-ubuntu--lv" {
			foundLVMKey = true
		}
	}
	if !foundLVMKey {
		t.Fatalf("expected LVM external key & display name in %+v", diskRes)
	}

	// Verify filesystems: bind duplicate removed, escaped space preserved, nfs4/fuse.sshfs/tmpfs excluded, UUIDs matched.
	fsTargets := disc.Topology().Filesystems()
	if len(fsTargets) != 3 {
		t.Fatalf("expected 3 deduplicated local filesystems, got %+v", fsTargets)
	}
	fsRes, _ := reg.List(ctx, host.KindFilesystem, false)
	foundRootUUID, foundSpaceMount := false, false
	for _, r := range fsRes {
		if r.ExternalKey == "uuid:1111-2222-3333:/" {
			foundRootUUID = true
		}
		if r.DisplayName == "/mnt/data store" {
			foundSpaceMount = true
		}
	}
	if !foundRootUUID || !foundSpaceMount {
		t.Fatalf("unexpected filesystem resources: %+v", fsRes)
	}

	// Verify NICs: ALL 7 NICs (including virtuals) are inventoried, and ONLY enp1s0 + bond0 count toward host aggregate!
	nicMap := disc.Topology().NICLookup()
	if len(nicMap) != 7 {
		t.Fatalf("expected all 7 NICs inventoried, got %d: %+v", len(nicMap), nicMap)
	}
	wantInclude := map[string]bool{
		"lo":     false,
		"enp1s0": true,
		"br0":    false,
		"veth1":  false,
		"enp2s0": false,
		"enp3s0": false,
		"bond0":  true,
	}
	for name, want := range wantInclude {
		if nicMap[name].IncludeInHostAggregate != want {
			t.Fatalf("NIC %s IncludeInHostAggregate = %v, want %v", name, nicMap[name].IncludeInHostAggregate, want)
		}
	}

	// Feed /proc/net/dev with equal traffic on enp1s0/br0/veth1 and slaves/bond0 -> host aggregate = enp1s0 + bond0 only!
	writeFixtureFile(t, proc, "net/dev", `Inter-| Receive | Transmit
 face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed
 lo: 1000 10 0 0 0 0 0 0 1000 10 0 0 0 0 0 0
 enp1s0: 1000 10 0 0 0 0 0 0 2000 10 0 0 0 0 0 0
 br0: 1000 10 0 0 0 0 0 0 2000 10 0 0 0 0 0 0
 veth1: 1000 10 0 0 0 0 0 0 2000 10 0 0 0 0 0 0
 enp2s0: 3000 10 0 0 0 0 0 0 4000 10 0 0 0 0 0 0
 enp3s0: 2000 10 0 0 0 0 0 0 1000 10 0 0 0 0 0 0
 bond0: 5000 20 0 0 0 0 0 0 5000 20 0 0 0 0 0 0
`)
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	netCol := NewNetworkCollector(proc, clk, disc.Topology().NICLookup)
	norm := tracker.NewNormalizer(disc.Topology().IncludeNICInHostAggregate)
	s1, err := netCol.Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	norm.Normalize(metrics.ProfilePersist, "boot-1", s1)

	clk.Advance(10 * time.Second)
	writeFixtureFile(t, proc, "net/dev", `Inter-| Receive | Transmit
 face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed
 lo: 99000 10 0 0 0 0 0 0 99000 10 0 0 0 0 0 0
 enp1s0: 11000 20 0 0 0 0 0 0 22000 20 0 0 0 0 0 0
 br0: 11000 20 0 0 0 0 0 0 22000 20 0 0 0 0 0 0
 veth1: 11000 20 0 0 0 0 0 0 22000 20 0 0 0 0 0 0
 enp2s0: 13000 20 0 0 0 0 0 0 14000 20 0 0 0 0 0 0
 enp3s0: 12000 20 0 0 0 0 0 0 11000 20 0 0 0 0 0 0
 bond0: 25000 40 0 0 0 0 0 0 25000 40 0 0 0 0 0 0
`)
	s2, err := netCol.Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nRes := norm.Normalize(metrics.ProfilePersist, "boot-1", s2)
	// enp1s0 rx delta=10000/10s=1000; bond0 rx delta=20000/10s=2000 => host rx = 3000 B/s.
	// enp1s0 tx delta=20000/10s=2000; bond0 tx delta=20000/10s=2000 => host tx = 4000 B/s.
	assertPoint(t, nRes.Points, metrics.HostResourceID, metrics.KeyNetRxBytesPerSec, 3000.0)
	assertPoint(t, nRes.Points, metrics.HostResourceID, metrics.KeyNetTxBytesPerSec, 4000.0)

	// Verify sensors & SensorCollector partial failure tolerance.
	sensorCol := NewSensorCollector(clk, disc.Topology().Sensors)
	senSnap, err := sensorCol.Collect(ctx)
	if err != nil {
		t.Fatalf("SensorCollector failed: %v", err)
	}
	if len(senSnap.Warnings) == 0 || len(senSnap.Samples) != 1 {
		t.Fatalf("expected 1 valid sensor reading + 1 parse warning, got samples=%d warns=%d", len(senSnap.Samples), len(senSnap.Warnings))
	}
	if senSnap.Samples[0].GaugeValue != 47.5 {
		t.Fatalf("expected 47.5°C, got %v", senSnap.Samples[0].GaugeValue)
	}
}

func TestSharedDeviceNICsKeepSeparatePersistentIDs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	sys := filepath.Join(root, "sys")
	writeFixtureFile(t, proc, "self/mountinfo", "")
	if err := os.MkdirAll(filepath.Join(sys, "block"), 0o755); err != nil {
		t.Fatal(err)
	}
	addPort := func(name, mac, port string) {
		t.Helper()
		iface := "class/net/" + name
		writeFixtureFile(t, sys, iface+"/address", mac)
		writeFixtureFile(t, sys, iface+"/dev_port", port)
		createSymlink(t, sys, iface+"/device", "../../../devices/pci0000:00/0000:01:00.0")
	}
	cfg := config.Default()
	cfg.DataDir = filepath.Join(root, "db")
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reg := host.NewRegistry(db)
	disc := NewDiscovery(proc, sys, filepath.Join(root, "dev"), nil, false)
	discover := func() map[string]NICTarget {
		t.Helper()
		if err := disc.ReconcileAll(ctx, reg, 1000, false); err != nil {
			t.Fatal(err)
		}
		return disc.Topology().NICLookup()
	}

	addPort("eth0", "52:54:00:aa:11:01", "0")
	original := discover()["eth0"].ResourceID
	addPort("eth1", "52:54:00:aa:11:02", "1")
	targets := discover()
	second := targets["eth1"].ResourceID
	if original == "" || second == "" || second == original || targets["eth0"].ResourceID != original {
		t.Fatalf("expected separate ports and preservation of the legacy ID: %+v", targets)
	}

	if err := os.Rename(filepath.Join(sys, "class/net/eth0"), filepath.Join(root, "offline")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(sys, "class/net/eth1"), filepath.Join(sys, "class/net/enp1s0")); err != nil {
		t.Fatal(err)
	}
	if target := discover()["enp1s0"]; target.ResourceID != second {
		t.Fatalf("remaining renamed port changed ID: %+v", target)
	}
}

func TestFilesystemCollectorStalledMountIsolationAndInFlightGuard(t *testing.T) {
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	targets := []FilesystemTarget{
		{ResourceID: "fs:root", Mountpoint: "/"},
		{ResourceID: "fs:hung", Mountpoint: "/mnt/hung"},
	}

	unblock := make(chan struct{})
	defer close(unblock)

	var hungCalls atomic.Int32
	fsCol := NewFilesystemCollector(clk, func() []FilesystemTarget { return targets }, func(path string) (FSStat, error) {
		if path == "/mnt/hung" {
			hungCalls.Add(1)
			<-unblock
			return FSStat{}, errors.New("unblocked")
		}
		return FSStat{
			TotalBytes:     1000,
			UsedBytes:      400,
			AvailableBytes: 500,
			TotalInodes:    100,
			FreeInodes:     80,
		}, nil
	})

	ctx1, cancel1 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel1()
	snap1, err := fsCol.Collect(ctx1)
	if err != nil {
		t.Fatalf("FilesystemCollector returned fatal error instead of partial snapshot: %v", err)
	}
	// "/" succeeded even though "/mnt/hung" stalled!
	if len(snap1.Samples) == 0 || len(snap1.Warnings) == 0 {
		t.Fatalf("expected cached samples for '/' and timeout warning for '/mnt/hung', got %+v", snap1)
	}

	// Second call while /mnt/hung is STILL in-flight must skip /mnt/hung without spawning another hung call!
	ctx2, cancel2 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel2()
	snap2, err := fsCol.Collect(ctx2)
	if err != nil {
		t.Fatalf("second Collect failed: %v", err)
	}
	if hungCalls.Load() != 1 {
		t.Fatalf("expected in-flight guard to keep hungCalls=1, got %d", hungCalls.Load())
	}
	if len(snap2.Samples) == 0 {
		t.Fatal("expected '/' samples on second Collect")
	}
}

func TestDiscoveryAddRemoveReAddPreservesHistoryAndTopology(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	proc := filepath.Join(tmp, "proc")
	sys := filepath.Join(tmp, "sys")
	devDisk := filepath.Join(tmp, "dev_disk")

	writeFixtureFile(t, proc, "self/mountinfo", `25 1 259:2 / / rw,relatime - ext4 /dev/nvme0n1p2 rw
`)
	writeFixtureFile(t, sys, "block/nvme0n1/size", "1000000\n")
	writeFixtureFile(t, sys, "block/nvme0n1/device/serial", "NVME-1\n")
	writeFixtureFile(t, sys, "class/net/enp1s0/ifindex", "2\n")
	writeFixtureFile(t, sys, "class/net/enp1s0/type", "1\n")
	writeFixtureFile(t, sys, "class/net/enp1s0/address", "52:54:00:00:00:01\n")
	createSymlink(t, sys, "class/net/enp1s0/device", "../../../devices/pci0000:00/0000:01:00.0")

	// Hot-pluggable USB disk sda
	writeFixtureFile(t, sys, "block/sda/size", "500000\n")
	writeFixtureFile(t, sys, "block/sda/device/serial", "USB-BACKUP-1\n")

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	reg := host.NewRegistry(db)
	_ = reg.EnsureHost(ctx, host.Inventory{Hostname: "hotplug", BootID: "boot-1"}, 1000)

	disc := NewDiscovery(proc, sys, devDisk, cfg.ExcludedFSTypes, true)
	if err := disc.ReconcileAll(ctx, reg, 1000, false); err != nil {
		t.Fatalf("ReconcileAll 1: %v", err)
	}
	sdaID := disc.Topology().DiskLookup()["sda"]
	if sdaID == "" {
		t.Fatal("expected sda in topology")
	}

	// Unplug sda -> active=0 in DB, removed from Topology, history preserved.
	_ = os.RemoveAll(filepath.Join(sys, "block/sda"))
	if err := disc.ReconcileAll(ctx, reg, 2000, false); err != nil {
		t.Fatalf("ReconcileAll 2: %v", err)
	}
	if _, stillInTopo := disc.Topology().DiskLookup()["sda"]; stillInTopo {
		t.Fatal("expected removed sda to leave active topology")
	}
	allDisks, _ := reg.List(ctx, host.KindDisk, true)
	if len(allDisks) != 2 {
		t.Fatalf("expected 2 disk rows (inactive sda kept), got %d", len(allDisks))
	}

	// Replug USB disk under different kernel name sdb with SAME hardware serial -> reactivates SAME sdaID!
	writeFixtureFile(t, sys, "block/sdb/size", "500000\n")
	writeFixtureFile(t, sys, "block/sdb/device/serial", "USB-BACKUP-1\n")
	if err := disc.ReconcileAll(ctx, reg, 3000, false); err != nil {
		t.Fatalf("ReconcileAll 3: %v", err)
	}
	if gotID := disc.Topology().DiskLookup()["sdb"]; gotID != sdaID {
		t.Fatalf("expected replugged disk sdb to preserve original ID %q, got %q", sdaID, gotID)
	}
	allDisksAfter, _ := reg.List(ctx, host.KindDisk, true)
	if len(allDisksAfter) != 2 {
		t.Fatalf("expected no duplicate resource row on re-add, got %d", len(allDisksAfter))
	}
}
