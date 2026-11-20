package collector

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"vektor/internal/metrics"
)

// NICTarget binds a kernel interface name to a persistent resource_id.
type NICTarget struct {
	ResourceID             string
	IncludeInHostAggregate bool
}

// NICLookupFunc maps Linux interface name (e.g. "enp1s0") to its discovered NICTarget.
type NICLookupFunc func() map[string]NICTarget

// NetworkCollector reads per-interface traffic/error/drop counters from <procRoot>/net/dev.
type NetworkCollector struct {
	procRoot string
	clock    metrics.Clock
	lookup   NICLookupFunc
}

// NewNetworkCollector constructs a NetworkCollector.
func NewNetworkCollector(procRoot string, clock metrics.Clock, lookup NICLookupFunc) *NetworkCollector {
	if procRoot == "" {
		procRoot = "/proc"
	}
	return &NetworkCollector{
		procRoot: procRoot,
		clock:    clock,
		lookup:   lookup,
	}
}

func (c *NetworkCollector) Name() string {
	return "network"
}

func (c *NetworkCollector) Collect(ctx context.Context) (metrics.RawSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return metrics.RawSnapshot{}, err
	}

	now := c.clock.Now()
	mono := c.clock.Monotonic()
	snap := metrics.RawSnapshot{
		CollectedAt: now,
		Monotonic:   mono,
	}

	var activeNICs map[string]NICTarget
	if c.lookup != nil {
		activeNICs = c.lookup()
	}

	data, err := readFileBounded(filepath.Join(c.procRoot, "net/dev"), maxProcSysFileBytes)
	if err != nil {
		return snap, fmt.Errorf("read /proc/net/dev: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 {
			continue // Skip 2 header rows of /proc/net/dev.
		}
		line := scanner.Text()
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		iface := strings.TrimSpace(line[:colon])
		if iface == "" {
			continue
		}

		var resID string
		if activeNICs != nil {
			target, ok := activeNICs[iface]
			if !ok || target.ResourceID == "" {
				continue
			}
			resID = target.ResourceID
		} else {
			resID = "nic:" + iface
		}

		fields := strings.Fields(line[colon+1:])
		if len(fields) < 12 {
			snap.Warnings = append(snap.Warnings, fmt.Errorf("short /proc/net/dev row for %s", iface))
			continue
		}

		rxBytes, e0 := strconv.ParseUint(fields[0], 10, 64)
		rxPkts, e1 := strconv.ParseUint(fields[1], 10, 64)
		rxErrs, e2 := strconv.ParseUint(fields[2], 10, 64)
		rxDrops, e3 := strconv.ParseUint(fields[3], 10, 64)
		txBytes, e8 := strconv.ParseUint(fields[8], 10, 64)
		txPkts, e9 := strconv.ParseUint(fields[9], 10, 64)
		txErrs, e10 := strconv.ParseUint(fields[10], 10, 64)
		txDrops, e11 := strconv.ParseUint(fields[11], 10, 64)
		if e0 != nil || e1 != nil || e2 != nil || e3 != nil || e8 != nil || e9 != nil || e10 != nil || e11 != nil {
			snap.Warnings = append(snap.Warnings, fmt.Errorf("parse /proc/net/dev counters for %s", iface))
			continue
		}

		snap.Samples = append(snap.Samples,
			metrics.NewCounterSample(resID, metrics.RawNetRxBytesTotal, rxBytes, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawNetTxBytesTotal, txBytes, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawNetRxPacketsTotal, rxPkts, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawNetTxPacketsTotal, txPkts, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawNetRxErrorsTotal, rxErrs, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawNetTxErrorsTotal, txErrs, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawNetRxDropsTotal, rxDrops, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawNetTxDropsTotal, txDrops, 64, now, mono),
		)
	}
	if err := scanner.Err(); err != nil {
		return snap, fmt.Errorf("scan /proc/net/dev: %w", err)
	}
	return snap, nil
}
