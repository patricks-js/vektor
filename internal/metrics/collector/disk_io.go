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

const linuxSectorSizeBytes = 512

// DiskLookupFunc returns a mapping from Linux kernel block device name (e.g. "nvme0n1", "dm-0")
// to Vektor's persistent internal resource_id.
type DiskLookupFunc func() map[string]string

// DiskIOCollector reads cumulative block-device I/O counters from <procRoot>/diskstats.
type DiskIOCollector struct {
	procRoot string
	clock    metrics.Clock
	lookup   DiskLookupFunc
}

// NewDiskIOCollector constructs a DiskIOCollector filtered to discovered block devices.
func NewDiskIOCollector(procRoot string, clock metrics.Clock, lookup DiskLookupFunc) *DiskIOCollector {
	if procRoot == "" {
		procRoot = "/proc"
	}
	return &DiskIOCollector{
		procRoot: procRoot,
		clock:    clock,
		lookup:   lookup,
	}
}

func (c *DiskIOCollector) Name() string {
	return "disk_io"
}

func (c *DiskIOCollector) Collect(ctx context.Context) (metrics.RawSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return metrics.RawSnapshot{}, err
	}

	now := c.clock.Now()
	mono := c.clock.Monotonic()
	snap := metrics.RawSnapshot{
		CollectedAt: now,
		Monotonic:   mono,
	}

	var activeDisks map[string]string
	if c.lookup != nil {
		activeDisks = c.lookup()
	}
	if len(activeDisks) == 0 {
		return snap, nil
	}

	data, err := readFileBounded(filepath.Join(c.procRoot, "diskstats"), maxProcSysFileBytes)
	if err != nil {
		return snap, fmt.Errorf("read /proc/diskstats: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 13 {
			continue
		}
		devName := fields[2]
		resID, ok := activeDisks[devName]
		if !ok || resID == "" {
			continue
		}

		readsCompleted, err1 := strconv.ParseUint(fields[3], 10, 64)
		sectorsRead, err2 := strconv.ParseUint(fields[5], 10, 64)
		writesCompleted, err3 := strconv.ParseUint(fields[7], 10, 64)
		sectorsWritten, err4 := strconv.ParseUint(fields[9], 10, 64)
		ioTimeMs, err5 := strconv.ParseUint(fields[12], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			snap.Warnings = append(snap.Warnings, fmt.Errorf("parse diskstats line for %s", devName))
			continue
		}

		snap.Samples = append(snap.Samples,
			metrics.NewCounterSample(resID, metrics.RawDiskReadsTotal, readsCompleted, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawDiskReadBytesTotal, sectorsRead*linuxSectorSizeBytes, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawDiskWritesTotal, writesCompleted, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawDiskWriteBytesTotal, sectorsWritten*linuxSectorSizeBytes, 64, now, mono),
			metrics.NewCounterSample(resID, metrics.RawDiskIOTimeMsTotal, ioTimeMs, 64, now, mono),
		)
	}
	if err := scanner.Err(); err != nil {
		return snap, fmt.Errorf("scan /proc/diskstats: %w", err)
	}
	return snap, nil
}
