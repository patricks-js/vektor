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
	"time"

	"vektor/internal/metrics"
)

// CPUCollector reads cumulative CPU jiffies from <procRoot>/stat and load averages from <procRoot>/loadavg.
type CPUCollector struct {
	procRoot       string
	clock          metrics.Clock
	includePerCore bool
}

// NewCPUCollector constructs a CPUCollector.
func NewCPUCollector(procRoot string, clock metrics.Clock, includePerCore bool) *CPUCollector {
	if procRoot == "" {
		procRoot = "/proc"
	}
	return &CPUCollector{
		procRoot:       procRoot,
		clock:          clock,
		includePerCore: includePerCore,
	}
}

func (c *CPUCollector) Name() string {
	return "cpu"
}

func (c *CPUCollector) Collect(ctx context.Context) (metrics.RawSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return metrics.RawSnapshot{}, err
	}

	now := c.clock.Now()
	mono := c.clock.Monotonic()
	snap := metrics.RawSnapshot{
		CollectedAt: now,
		Monotonic:   mono,
	}

	var statOK, loadOK bool

	statBytes, statErr := readFileBounded(filepath.Join(c.procRoot, "stat"), maxProcSysFileBytes)
	if statErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Errorf("read /proc/stat: %w", statErr))
	} else {
		cpuSamples, parseErr := parseProcStat(statBytes, c.includePerCore, now, mono)
		if parseErr != nil {
			snap.Warnings = append(snap.Warnings, fmt.Errorf("parse /proc/stat: %w", parseErr))
		} else {
			snap.CPUSamples = cpuSamples
			statOK = true
		}
	}

	loadBytes, loadErr := readFileBounded(filepath.Join(c.procRoot, "loadavg"), 4096)
	if loadErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Errorf("read /proc/loadavg: %w", loadErr))
	} else {
		loadSamples, parseErr := parseProcLoadavg(loadBytes, now, mono)
		if parseErr != nil {
			snap.Warnings = append(snap.Warnings, fmt.Errorf("parse /proc/loadavg: %w", parseErr))
		} else {
			snap.Samples = append(snap.Samples, loadSamples...)
			loadOK = true
		}
	}

	if !statOK && !loadOK {
		return snap, errors.Join(snap.Warnings...)
	}
	return snap, nil
}

func parseProcStat(data []byte, includePerCore bool, now time.Time, mono time.Duration) ([]metrics.CPUSample, error) {
	var samples []metrics.CPUSample
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "cpu") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		label := fields[0]
		var resID, core string
		if label == "cpu" {
			resID = metrics.HostResourceID
			core = "total"
		} else {
			suffix := strings.TrimPrefix(label, "cpu")
			if !isAllDigits(suffix) {
				continue
			}
			if !includePerCore {
				continue
			}
			resID = "cpu:" + suffix
			core = suffix
		}

		vals := make([]uint64, 8)
		limit := min(len(fields)-1, 8)
		parseFailed := false
		for i := 0; i < limit; i++ {
			v, err := strconv.ParseUint(fields[i+1], 10, 64)
			if err != nil {
				parseFailed = true
				break
			}
			vals[i] = v
		}
		if parseFailed {
			continue
		}

		samples = append(samples, metrics.CPUSample{
			ResourceID: resID,
			Core:       core,
			User:       vals[0],
			Nice:       vals[1],
			System:     vals[2],
			Idle:       vals[3],
			IOWait:     vals[4],
			IRQ:        vals[5],
			SoftIRQ:    vals[6],
			Steal:      vals[7],
			Time:       now.UTC(),
			Monotonic:  mono,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return nil, errors.New("no cpu lines found in /proc/stat")
	}
	return samples, nil
}

func parseProcLoadavg(data []byte, now time.Time, mono time.Duration) ([]metrics.RawSample, error) {
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil, fmt.Errorf("expected at least 3 fields in /proc/loadavg, got %d", len(fields))
	}
	keys := []string{metrics.KeyLoad1m, metrics.KeyLoad5m, metrics.KeyLoad15m}
	samples := make([]metrics.RawSample, 0, 3)
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", keys[i], err)
		}
		samples = append(samples, metrics.NewGaugeSample(metrics.HostResourceID, keys[i], v, now, mono))
	}
	return samples, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
