package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/host"
	"vektor/internal/incus"
	"vektor/internal/metrics"
)

func TestIncusCollectorMetricsAndCPUDelta(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(t.TempDir(), "data")
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	reg := host.NewRegistry(db)
	start := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	clk := metrics.NewFakeClock(start)

	cpuNs := int64(1_000_000_000)
	col := NewIncusCollectorWithFunc(clk, reg, func(context.Context) ([]incus.Instance, error) {
		return []incus.Instance{
			{
				Name:             "app-01",
				Type:             "container",
				OCI:              true,
				Status:           "Running",
				PID:              9001,
				CPUUsageNs:       cpuNs,
				MemoryUsageBytes: 256 << 20,
				DiskUsageBytes:   1 << 30,
				Processes:        12,
			},
		}, nil
	})

	// First pass: registers resource + emits memory, disk, processes (seeds CPU baseline).
	snap1, err := col.Collect(ctx, clk.Now())
	if err != nil || len(snap1.Samples) != 3 {
		t.Fatalf("expected 3 baseline samples, got %d err=%v", len(snap1.Samples), err)
	}

	// Second pass after 10s wall with 5s of CPU time -> 50% CPU.
	clk.Advance(10 * time.Second)
	cpuNs += 5_000_000_000
	snap2, err := col.Collect(ctx, clk.Now())
	if err != nil || len(snap2.Samples) != 4 {
		t.Fatalf("expected 4 samples on second pass, got %d err=%v", len(snap2.Samples), err)
	}

	foundCPU := false
	for _, s := range snap2.Samples {
		if s.ResourceID == "incus:app-01" && s.Key == metrics.KeyIncusCPUPct {
			foundCPU = true
			if s.GaugeValue < 49.9 || s.GaugeValue > 50.1 {
				t.Fatalf("expected ~50%% CPU, got %v", s.GaugeValue)
			}
		}
	}
	if !foundCPU {
		t.Fatalf("expected %s sample in %+v", metrics.KeyIncusCPUPct, snap2.Samples)
	}

	res, err := reg.Get(ctx, "incus:app-01")
	if err != nil || res.Kind != host.KindIncusInstance {
		t.Fatalf("expected registered incus_instance resource, got %+v err=%v", res, err)
	}
}
