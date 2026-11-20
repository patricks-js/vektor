package collector

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"vektor/internal/metrics"
)

// MemoryCollector reads RAM and swap gauges from <procRoot>/meminfo.
type MemoryCollector struct {
	procRoot string
	clock    metrics.Clock
}

// NewMemoryCollector constructs a MemoryCollector.
func NewMemoryCollector(procRoot string, clock metrics.Clock) *MemoryCollector {
	if procRoot == "" {
		procRoot = "/proc"
	}
	return &MemoryCollector{
		procRoot: procRoot,
		clock:    clock,
	}
}

func (c *MemoryCollector) Name() string {
	return "memory"
}

func (c *MemoryCollector) Collect(ctx context.Context) (metrics.RawSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return metrics.RawSnapshot{}, err
	}

	now := c.clock.Now()
	mono := c.clock.Monotonic()
	snap := metrics.RawSnapshot{
		CollectedAt: now,
		Monotonic:   mono,
	}

	data, err := readFileBounded(filepath.Join(c.procRoot, "meminfo"), maxProcSysFileBytes)
	if err != nil {
		return snap, fmt.Errorf("read /proc/meminfo: %w", err)
	}

	kv := make(map[string]uint64, 16)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		key := line[:colon]
		fields := strings.Fields(line[colon+1:])
		if len(fields) == 0 {
			continue
		}
		val, parseErr := strconv.ParseUint(fields[0], 10, 64)
		if parseErr != nil {
			continue
		}
		if len(fields) >= 2 && strings.EqualFold(fields[1], "kB") {
			val *= 1024
		}
		kv[key] = val
	}
	if err := scanner.Err(); err != nil {
		return snap, fmt.Errorf("scan /proc/meminfo: %w", err)
	}

	memTotal, hasTotal := kv["MemTotal"]
	if !hasTotal || memTotal == 0 {
		return snap, errors.New("MemTotal missing or zero in /proc/meminfo")
	}

	memFree := kv["MemFree"]
	buffers := kv["Buffers"]
	cached := kv["Cached"]
	memAvail, hasAvail := kv["MemAvailable"]
	if !hasAvail {
		memAvail = memFree + buffers + cached
	}
	if memAvail > memTotal {
		memAvail = memTotal
	}
	memUsed := memTotal - memAvail
	memUsedPct := (float64(memUsed) / float64(memTotal)) * 100.0

	samples := []metrics.RawSample{
		metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeyMemoryTotalBytes, float64(memTotal), now, mono),
		metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeyMemoryUsedBytes, float64(memUsed), now, mono),
		metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeyMemoryAvailableBytes, float64(memAvail), now, mono),
		metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeyMemoryCachedBytes, float64(cached), now, mono),
		metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeyMemoryBuffersBytes, float64(buffers), now, mono),
		metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeyMemoryUsedPct, memUsedPct, now, mono),
	}

	if swapTotal, hasSwap := kv["SwapTotal"]; hasSwap {
		swapFree := kv["SwapFree"]
		var swapUsed uint64
		if swapTotal >= swapFree {
			swapUsed = swapTotal - swapFree
		}
		samples = append(samples,
			metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeySwapTotalBytes, float64(swapTotal), now, mono),
			metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeySwapUsedBytes, float64(swapUsed), now, mono),
		)
		if swapTotal > 0 {
			swapUsedPct := (float64(swapUsed) / float64(swapTotal)) * 100.0
			samples = append(samples,
				metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeySwapUsedPct, swapUsedPct, now, mono),
			)
		}
	}

	snap.Samples = samples
	return snap, nil
}
