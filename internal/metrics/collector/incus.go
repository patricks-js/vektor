package collector

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"vektor/internal/ctl"
	"vektor/internal/host"
	"vektor/internal/incus"
	"vektor/internal/metrics"
)

const (
	defaultIncusPollInterval = 30 * time.Second
	incusUnavailableBackoff  = 5 * time.Minute
)

type incusCPUBaseline struct {
	pid   int64
	cpuNs int64
	mono  time.Duration
}

// IncusCollector samples per-instance resource usage from Incus via vektor-ctl,
// reconciles instance identities in host.Registry, and emits historical metrics.
type IncusCollector struct {
	clock        metrics.Clock
	registry     *host.Registry
	listFn       func(context.Context) ([]incus.Instance, bool, error)
	pollInterval time.Duration

	mu           sync.Mutex
	baselines    map[string]incusCPUBaseline
	lastAttempt  time.Duration
	backoffUntil time.Duration
	hasAttempted bool
}

// NewIncusCollector constructs a collector backed by vektor-ctl.socket.
func NewIncusCollector(clk metrics.Clock, registry *host.Registry, ctlClient *ctl.Client) *IncusCollector {
	listFn := func(ctx context.Context) ([]incus.Instance, bool, error) {
		if ctlClient == nil {
			return nil, false, nil
		}
		resp, err := ctlClient.Do(ctx, ctl.Request{Action: "incus.instances.list"})
		if err != nil || !resp.OK {
			// Incus or vektor-ctl is unavailable; signal caller to back off.
			return nil, false, nil
		}
		var instances []incus.Instance
		if err := json.Unmarshal(resp.Data, &instances); err != nil {
			return nil, false, nil
		}
		return instances, true, nil
	}
	col := NewIncusCollectorWithFunc(clk, registry, func(ctx context.Context) ([]incus.Instance, error) {
		items, _, err := listFn(ctx)
		return items, err
	})
	col.listFn = listFn
	col.pollInterval = defaultIncusPollInterval
	return col
}

// NewIncusCollectorWithFunc constructs an IncusCollector with an injectable instance lister for testing.
func NewIncusCollectorWithFunc(clk metrics.Clock, registry *host.Registry, listFn func(context.Context) ([]incus.Instance, error)) *IncusCollector {
	return &IncusCollector{
		clock:    clk,
		registry: registry,
		listFn: func(ctx context.Context) ([]incus.Instance, bool, error) {
			items, err := listFn(ctx)
			if err != nil {
				return nil, false, err
			}
			return items, true, nil
		},
		baselines: make(map[string]incusCPUBaseline),
	}
}

func (c *IncusCollector) Collect(ctx context.Context, now time.Time) (metrics.RawSnapshot, error) {
	if c == nil || c.listFn == nil {
		return metrics.RawSnapshot{}, nil
	}
	mono := c.clock.Monotonic()
	c.mu.Lock()
	if c.backoffUntil > 0 && mono < c.backoffUntil {
		c.mu.Unlock()
		return metrics.RawSnapshot{}, nil
	}
	if c.hasAttempted && c.pollInterval > 0 && mono-c.lastAttempt < c.pollInterval {
		c.mu.Unlock()
		return metrics.RawSnapshot{}, nil
	}
	c.hasAttempted = true
	c.lastAttempt = mono
	c.mu.Unlock()

	instances, available, err := c.listFn(ctx)
	if !available {
		c.mu.Lock()
		c.backoffUntil = mono + incusUnavailableBackoff
		c.mu.Unlock()
		return metrics.RawSnapshot{}, nil
	}
	if err != nil || len(instances) == 0 {
		return metrics.RawSnapshot{}, nil
	}
	if c.registry != nil {
		discovered := make([]host.DiscoveredResource, 0, len(instances))
		for _, inst := range instances {
			metaRaw, _ := json.Marshal(map[string]any{
				"type":         inst.Type,
				"oci":          inst.OCI,
				"cpu_limit":    inst.CPULimit,
				"memory_limit": inst.MemoryLimit,
			})
			discovered = append(discovered, host.DiscoveredResource{
				ExternalKey:  inst.Name,
				DisplayName:  inst.Name,
				MetadataJSON: string(metaRaw),
			})
		}
		if err := c.registry.EnsureIncusInstances(ctx, discovered, now.UnixMilli()); err != nil {
			return metrics.RawSnapshot{}, err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	seen := make(map[string]bool, len(instances))
	samples := make([]metrics.RawSample, 0, len(instances)*4)

	for _, inst := range instances {
		seen[inst.Name] = true
		resID := "incus:" + inst.Name

		if inst.DiskUsageBytes > 0 {
			samples = append(samples, metrics.NewGaugeSample(resID, metrics.KeyIncusDiskBytes, float64(inst.DiskUsageBytes), now, mono))
		}

		if !strings.EqualFold(inst.Status, "Running") {
			delete(c.baselines, inst.Name)
			continue
		}

		samples = append(samples,
			metrics.NewGaugeSample(resID, metrics.KeyIncusMemoryBytes, float64(inst.MemoryUsageBytes), now, mono),
			metrics.NewGaugeSample(resID, metrics.KeyIncusProcesses, float64(inst.Processes), now, mono),
		)

		if prev, ok := c.baselines[inst.Name]; ok && prev.pid == inst.PID && inst.PID > 0 && mono > prev.mono && inst.CPUUsageNs >= prev.cpuNs {
			deltaCPU := float64(inst.CPUUsageNs - prev.cpuNs)
			deltaWall := float64((mono - prev.mono).Nanoseconds())
			if deltaWall > 0 {
				pct := (deltaCPU / deltaWall) * 100.0
				if pct < 0 {
					pct = 0
				}
				samples = append(samples, metrics.NewGaugeSample(resID, metrics.KeyIncusCPUPct, pct, now, mono))
			}
		}
		if inst.PID > 0 {
			c.baselines[inst.Name] = incusCPUBaseline{pid: inst.PID, cpuNs: inst.CPUUsageNs, mono: mono}
		}
	}

	for name := range c.baselines {
		if !seen[name] {
			delete(c.baselines, name)
		}
	}

	return metrics.RawSnapshot{
		CollectedAt: now.UTC(),
		Monotonic:   mono,
		Samples:     samples,
	}, nil
}
