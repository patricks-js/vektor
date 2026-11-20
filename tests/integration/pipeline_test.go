package integration

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/metrics/collector"
	"vektor/internal/metrics/scheduler"
	"vektor/internal/metrics/tracker"
	"vektor/internal/observability"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func symlinkFile(t *testing.T, root, linkRel, target string) {
	t.Helper()
	p := filepath.Join(root, linkRel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatalf("symlink %s -> %s: %v", p, target, err)
	}
}

func setupUbuntuFixture(t *testing.T, root string) (proc, sys, etc, devDisk string) {
	t.Helper()
	proc = filepath.Join(root, "proc")
	sys = filepath.Join(root, "sys")
	etc = filepath.Join(root, "etc")
	devDisk = filepath.Join(root, "dev_disk")

	writeFile(t, etc, "os-release", `NAME="Ubuntu"
VERSION_ID="26.04"
PRETTY_NAME="Ubuntu 26.04 LTS"
`)
	writeFile(t, proc, "version", "Linux version 6.14.0-20-generic (buildd@ubuntu) #20 SMP\n")
	writeFile(t, proc, "cpuinfo", `processor	: 0
model name	: AMD EPYC 4244P
physical id	: 0
core id		: 0

processor	: 2
model name	: AMD EPYC 4244P
physical id	: 0
core id		: 1
`)
	writeFile(t, proc, "meminfo", `MemTotal:       16777216 kB
MemFree:         4194304 kB
MemAvailable:    8388608 kB
Buffers:          524288 kB
Cached:          4194304 kB
SwapTotal:       2097152 kB
SwapFree:        2097152 kB
`)
	writeFile(t, proc, "loadavg", "0.50 0.40 0.30 1/200 1000\n")
	writeFile(t, proc, "sys/kernel/random/boot_id", "boot-uuid-001\n")

	// Disks: nvme0n1 + LVM dm-0
	writeFile(t, sys, "block/nvme0n1/size", "1953525168\n")
	writeFile(t, sys, "block/nvme0n1/device/serial", "NVME-001\n")
	writeFile(t, sys, "block/dm-0/size", "419430400\n")
	writeFile(t, sys, "block/dm-0/dm/name", "ubuntu--vg-ubuntu--lv\n")
	writeFile(t, sys, "block/dm-0/dm/uuid", "LVM-root-001\n")

	// Mountinfo
	symlinkFile(t, devDisk, "by-uuid/root-fs-uuid", "../../dm-0")
	writeFile(t, proc, "self/mountinfo", `25 1 253:0 / / rw,relatime - ext4 /dev/mapper/ubuntu--vg-ubuntu--lv rw
26 25 0:25 / /run tmpfs rw - tmpfs tmpfs rw
`)

	// NICs: lo, enp1s0 -> br0 <- veth1
	writeFile(t, sys, "class/net/lo/ifindex", "1\n")
	writeFile(t, sys, "class/net/lo/type", "772\n")
	writeFile(t, sys, "class/net/lo/address", "00:00:00:00:00:00\n")
	symlinkFile(t, sys, "class/net/lo/device", "../../../devices/virtual/net/lo/device")

	writeFile(t, sys, "class/net/enp1s0/ifindex", "2\n")
	writeFile(t, sys, "class/net/enp1s0/type", "1\n")
	writeFile(t, sys, "class/net/enp1s0/address", "52:54:00:aa:11:01\n")
	symlinkFile(t, sys, "class/net/enp1s0/device", "../../../devices/pci0000:00/0000:01:00.0")
	symlinkFile(t, sys, "class/net/enp1s0/master", "../../br0")

	writeFile(t, sys, "class/net/br0/ifindex", "3\n")
	writeFile(t, sys, "class/net/br0/type", "1\n")
	writeFile(t, sys, "class/net/br0/address", "52:54:00:aa:11:01\n")
	if err := os.MkdirAll(filepath.Join(sys, "class/net/br0/bridge"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, sys, "class/net/veth1/ifindex", "4\n")
	writeFile(t, sys, "class/net/veth1/type", "1\n")
	writeFile(t, sys, "class/net/veth1/address", "fe:54:00:11:22:33\n")
	symlinkFile(t, sys, "class/net/veth1/master", "../../br0")

	// Sensors
	writeFile(t, sys, "class/hwmon/hwmon0/name", "k10temp\n")
	symlinkFile(t, sys, "class/hwmon/hwmon0/device", "../../../devices/pci0000:00/0000:00:18.3")
	writeFile(t, sys, "class/hwmon/hwmon0/temp1_input", "42500\n")
	writeFile(t, sys, "class/hwmon/hwmon0/temp1_label", "Tctl\n")

	return proc, sys, etc, devDisk
}

func updateDynamicProc(t *testing.T, proc string, tick int) {
	t.Helper()
	// Each 30s tick: CPU advances 100 jiffies total: 25% busy for first 120 ticks, 50% busy afterwards.
	var busyTotal, idleTotal uint64
	if tick <= 120 {
		busyTotal = uint64(tick) * 25
		idleTotal = uint64(tick) * 75
	} else {
		busyTotal = 120*25 + uint64(tick-120)*50
		idleTotal = 120*75 + uint64(tick-120)*50
	}
	writeFile(t, proc, "stat", fmt.Sprintf(
		"cpu  %d 0 0 %d 0 0 0 0 0 0\ncpu0 %d 0 0 %d 0 0 0 0 0 0\ncpu2 %d 0 0 %d 0 0 0 0 0 0\n",
		busyTotal, idleTotal, busyTotal/2, idleTotal/2, busyTotal/2, idleTotal/2,
	))

	// Disk & Net grow monotonically by 30000 B per 30s tick (1000 B/s).
	sectors := uint64(tick) * 60 // 60 * 512 = 30720 B => 1024 B/s
	writeFile(t, proc, "diskstats", fmt.Sprintf(
		" 259 0 nvme0n1 %d 0 %d 10 %d 0 %d 20 0 30 30\n 253 0 dm-0 %d 0 %d 5 %d 0 %d 10 0 15 15\n",
		tick*10, sectors, tick*5, sectors, tick*10, sectors, tick*5, sectors,
	))
	netBytes := uint64(tick) * 30_000
	writeFile(t, proc, "net/dev", fmt.Sprintf(`Inter-| Receive | Transmit
 face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed
 lo: %d 10 0 0 0 0 0 0 %d 10 0 0 0 0 0 0
 enp1s0: %d 10 0 0 0 0 0 0 %d 10 0 0 0 0 0 0
 br0: %d 10 0 0 0 0 0 0 %d 10 0 0 0 0 0 0
 veth1: %d 10 0 0 0 0 0 0 %d 10 0 0 0 0 0 0
`, netBytes*5, netBytes*5, netBytes, netBytes*2, netBytes, netBytes*2, netBytes, netBytes*2))
}

func buildEngine(
	t *testing.T,
	ctx context.Context,
	cfg config.Config,
	clk metrics.Clock,
) (*database.DB, *scheduler.Scheduler, *history.QueryService) {
	t.Helper()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	health := observability.NewHealthTracker()
	reg := host.NewRegistry(db)
	disc := collector.NewDiscovery(cfg.ProcRoot, cfg.SysRoot, cfg.DevDiskRoot, cfg.ExcludedFSTypes, cfg.IncludeSensors)
	topo := disc.Topology()
	norm := tracker.NewNormalizer(topo.IncludeNICInHostAggregate)

	fastCols := []metrics.Collector{
		collector.NewCPUCollector(cfg.ProcRoot, clk, cfg.IncludePerCoreCPU),
		collector.NewMemoryCollector(cfg.ProcRoot, clk),
		collector.NewDiskIOCollector(cfg.ProcRoot, clk, topo.DiskLookup),
		collector.NewNetworkCollector(cfg.ProcRoot, clk, topo.NICLookup),
	}
	fsCol := collector.NewFilesystemCollector(clk, topo.Filesystems, func(path string) (collector.FSStat, error) {
		return collector.FSStat{
			TotalBytes:     100 << 30,
			UsedBytes:      40 << 30,
			AvailableBytes: 55 << 30,
			TotalInodes:    1000000,
			FreeInodes:     900000,
		}, nil
	})
	senCol := collector.NewSensorCollector(clk, topo.Sensors)
	writer := history.NewRawWriter(db, cfg, health, nil, clk)
	rollup := history.NewRollupEngine(db, cfg, health, nil)
	retention := history.NewRetentionEngine(db, cfg, health, nil)
	maint := history.NewMaintenanceRunner(db, rollup, retention, nil)
	hub := scheduler.NewLiveHub(health, nil, nil)
	qs := history.NewQueryService(db, reg, cfg, health)

	sched := scheduler.New(scheduler.Deps{
		Config:          cfg,
		Clock:           clk,
		Health:          health,
		Registry:        reg,
		Discovery:       disc,
		Normalizer:      norm,
		FastCollectors:  fastCols,
		FSCollector:     fsCol,
		SensorCollector: senCol,
		RawWriter:       writer,
		Maintenance:     maint,
		LiveHub:         hub,
	})
	return db, sched, qs
}

func TestEndToEndPipelineRollupQueryBackupAndReboot(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	proc, sys, etc, devDisk := setupUbuntuFixture(t, tmp)
	dataDir := filepath.Join(tmp, "data")

	start := time.Date(2026, 11, 20, 10, 0, 5, 0, time.UTC)
	clk := metrics.NewFakeClock(start)

	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.ProcRoot = proc
	cfg.SysRoot = sys
	cfg.EtcRoot = etc
	cfg.DevDiskRoot = devDisk

	// Boot 1: run discovery + 2h 5m of 30s cycles (250 ticks).
	db1, sched1, qs1 := buildEngine(t, ctx, cfg, clk)
	updateDynamicProc(t, proc, 1)
	if err := sched1.RunOnce(ctx); err != nil {
		t.Fatalf("initial RunOnce: %v", err)
	}
	for tick := 2; tick <= 250; tick++ {
		clk.Advance(30 * time.Second)
		updateDynamicProc(t, proc, tick)
		if tick%4 == 0 {
			_ = sched1.RunSlowFSOnce(ctx)
			_ = sched1.RunSensorsOnce(ctx)
		}
		if err := sched1.RunPersistOnce(ctx); err != nil {
			t.Fatalf("RunPersistOnce tick %d: %v", tick, err)
		}
	}
	if err := sched1.RunMaintenanceOnce(ctx); err != nil {
		t.Fatalf("RunMaintenanceOnce: %v", err)
	}

	// Query 1h window (auto should pick 30s), 2h+ window with small MaxPoints (auto picks 300s/3600s), and explicit 3600s.
	hour0 := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC).UnixMilli()
	series30, err := qs1.QuerySeries(ctx, history.SeriesQuery{
		ResourceID: host.HostResourceID,
		MetricKey:  metrics.KeyCPUBusyPct,
		FromMs:     hour0,
		ToMs:       hour0 + 3600_000,
		Step:       "auto",
	})
	if err != nil {
		t.Fatalf("QuerySeries 1h auto: %v", err)
	}
	// Tick 1 was baseline (no CPU rate); ticks 2..120 fall inside hour 10:00..11:00 => 119 raw buckets.
	if series30.ResolutionS != 30 || len(series30.Points) != 119 {
		t.Fatalf("expected 30s resolution with 119 points in hour 10, got res=%d len=%d", series30.ResolutionS, len(series30.Points))
	}
	if math.Abs(series30.Points[0].Avg-25.0) > 1e-6 {
		t.Fatalf("expected 25.0%% CPU busy in hour 10, got %v", series30.Points[0].Avg)
	}
	core, err := qs1.QuerySeries(ctx, history.SeriesQuery{
		ResourceID: "cpu:2", MetricKey: metrics.KeyCPUCoreBusyPct, Step: "30s",
		FromMs: hour0, ToMs: hour0 + 3600_000,
	})
	if err != nil || len(core.Points) != 119 {
		t.Fatalf("expected persisted points for sparse CPU ID 2: points=%d error=%v", len(core.Points), err)
	}

	series1h, err := qs1.QuerySeries(ctx, history.SeriesQuery{
		ResourceID: host.HostResourceID,
		MetricKey:  metrics.KeyCPUBusyPct,
		FromMs:     hour0,
		ToMs:       hour0 + 2*3600_000,
		Step:       "3600",
	})
	if err != nil {
		t.Fatalf("QuerySeries 1h explicit: %v", err)
	}
	if len(series1h.Points) != 2 {
		t.Fatalf("expected 2 closed hourly buckets, got %d", len(series1h.Points))
	}
	if math.Abs(series1h.Points[0].Avg-25.0) > 1e-6 || math.Abs(series1h.Points[1].Avg-50.0) > 1e-6 {
		t.Fatalf("unexpected hourly averages: h0=%v h1=%v", series1h.Points[0].Avg, series1h.Points[1].Avg)
	}

	// Host net aggregate must equal enp1s0 only (1000 rx, 2000 tx B/s), never br0+veth1+lo.
	netSeries, err := qs1.QuerySeries(ctx, history.SeriesQuery{
		ResourceID: host.HostResourceID,
		MetricKey:  metrics.KeyNetRxBytesPerSec,
		FromMs:     hour0,
		ToMs:       hour0 + 3600_000,
		Step:       "300",
	})
	if err != nil || len(netSeries.Points) == 0 {
		t.Fatalf("QuerySeries net rx failed: %v", err)
	}
	if math.Abs(netSeries.Points[0].Avg-1000.0) > 1e-6 {
		t.Fatalf("expected host net rx aggregate = 1000 B/s (no bridge/veth/lo double count), got %v", netSeries.Points[0].Avg)
	}

	// Verify VACUUM INTO backup integrity while DB is active.
	backupFile := filepath.Join(tmp, "backups/vektor.bak.db")
	if err := db1.Backup(ctx, backupFile); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	bakCfg := config.Default()
	bakCfg.DBPath = backupFile
	bakDB, err := database.Open(ctx, bakCfg)
	if err != nil {
		t.Fatalf("open backup DB: %v", err)
	}
	if err := bakDB.QuickCheck(ctx); err != nil {
		t.Fatalf("backup QuickCheck: %v", err)
	}
	_ = bakDB.Close()

	// Close DB cleanly simulating graceful SIGTERM before host reboot.
	if err := db1.Close(); err != nil {
		t.Fatalf("close db1: %v", err)
	}

	// Simulated host reboot: new boot_id and counters reset back near zero!
	clk.Advance(30 * time.Second)
	rebootBucketStartMs := metrics.AlignBucketMs(clk.Now().UnixMilli(), 30_000)
	writeFile(t, proc, "sys/kernel/random/boot_id", "boot-uuid-002-rebooted\n")
	updateDynamicProc(t, proc, 1) // Counters reset to tick=1 values!

	db2, sched2, qs2 := buildEngine(t, ctx, cfg, clk)
	defer db2.Close()

	// First tick post-reboot must NOT produce negative rates or spikes, while preserving prior history!
	if err := sched2.RunOnce(ctx); err != nil {
		t.Fatalf("post-reboot RunOnce: %v", err)
	}
	postRebootCheck, err := qs2.QuerySeries(ctx, history.SeriesQuery{
		ResourceID: host.HostResourceID,
		MetricKey:  metrics.KeyCPUBusyPct,
		FromMs:     hour0,
		ToMs:       clk.Now().UnixMilli() + 1000,
		Step:       "3600",
	})
	if err != nil || len(postRebootCheck.Points) != 2 {
		t.Fatalf("expected prior 2 hourly buckets preserved across reboot, got %+v err=%v", postRebootCheck, err)
	}

	// Second tick post-reboot produces clean positive rates again.
	clk.Advance(30 * time.Second)
	updateDynamicProc(t, proc, 2)
	if err := sched2.RunPersistOnce(ctx); err != nil {
		t.Fatalf("post-reboot second persist tick: %v", err)
	}
	recentNet, err := qs2.QuerySeries(ctx, history.SeriesQuery{
		ResourceID: host.HostResourceID,
		MetricKey:  metrics.KeyNetRxBytesPerSec,
		FromMs:     rebootBucketStartMs,
		ToMs:       clk.Now().Add(time.Minute).UnixMilli(),
		Step:       "30",
	})
	if err != nil || len(recentNet.Points) != 1 || recentNet.Points[0].BucketStartMs != rebootBucketStartMs+30_000 {
		t.Fatalf("expected exactly 1 valid post-reboot rate point at +30s (first tick at %d suppressed), got %+v err=%v", rebootBucketStartMs, recentNet.Points, err)
	}
	if math.Abs(recentNet.Points[0].Last-1000.0) > 1e-6 {
		t.Fatalf("expected clean 1000 B/s post-reboot rate, got %v", recentNet.Points[0].Last)
	}
}
