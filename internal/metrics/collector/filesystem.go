package collector

import (
	"context"
	"fmt"
	"sync"
	"syscall"
	"time"

	"vektor/internal/metrics"
)

// FSStat represents filesystem capacity and inode statistics returned by StatfsFunc.
type FSStat struct {
	TotalBytes     uint64
	AvailableBytes uint64
	UsedBytes      uint64
	TotalInodes    uint64
	FreeInodes     uint64
}

// StatfsFunc abstracts syscall.Statfs for deterministic unit testing.
type StatfsFunc func(path string) (FSStat, error)

// DefaultStatfs queries Linux syscall.Statfs on mountpoint without elevated privileges.
func DefaultStatfs(path string) (FSStat, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return FSStat{}, err
	}
	bsize := uint64(st.Bsize)
	total := st.Blocks * bsize
	avail := st.Bavail * bsize
	var used uint64
	if st.Blocks >= st.Bfree {
		used = (st.Blocks - st.Bfree) * bsize
	}
	return FSStat{
		TotalBytes:     total,
		AvailableBytes: avail,
		UsedBytes:      used,
		TotalInodes:    st.Files,
		FreeInodes:     st.Ffree,
	}, nil
}

type cachedFSReading struct {
	ResourceID string
	Mountpoint string
	ObservedAt time.Time
	Mono       time.Duration
	Stat       FSStat
}

// FilesystemCollector probes discovered local filesystems with per-mountpoint in-flight guards,
// bounded concurrency, and stale-aware caching.
type FilesystemCollector struct {
	clock      metrics.Clock
	targets    func() []FilesystemTarget
	statfs     StatfsFunc
	sem        chan struct{}
	inFlightMu sync.Mutex
	inFlight   map[string]bool
	cacheMu    sync.RWMutex
	cache      map[string]cachedFSReading
}

// NewFilesystemCollector constructs a FilesystemCollector with bounded statfs concurrency.
func NewFilesystemCollector(clock metrics.Clock, targets func() []FilesystemTarget, statfs StatfsFunc) *FilesystemCollector {
	if statfs == nil {
		statfs = DefaultStatfs
	}
	return &FilesystemCollector{
		clock:    clock,
		targets:  targets,
		statfs:   statfs,
		sem:      make(chan struct{}, 2),
		inFlight: make(map[string]bool),
		cache:    make(map[string]cachedFSReading),
	}
}

func (c *FilesystemCollector) Name() string {
	return "filesystem"
}

// Collect probes each active mountpoint without letting one stalled mount block others or leak goroutines.
func (c *FilesystemCollector) Collect(ctx context.Context) (metrics.RawSnapshot, error) {
	var targets []FilesystemTarget
	if c.targets != nil {
		targets = c.targets()
	}

	now := c.clock.Now()
	mono := c.clock.Monotonic()
	snap := metrics.RawSnapshot{
		CollectedAt: now,
		Monotonic:   mono,
	}
	if len(targets) == 0 {
		return snap, nil
	}

	resCh := make(chan error, len(targets))
	launched := 0

	for _, tg := range targets {
		if ctx.Err() != nil {
			break
		}
		c.inFlightMu.Lock()
		busy := c.inFlight[tg.Mountpoint]
		if !busy {
			c.inFlight[tg.Mountpoint] = true
		}
		c.inFlightMu.Unlock()

		if busy {
			snap.Warnings = append(snap.Warnings, fmt.Errorf("filesystem %s probe skipped: previous statfs still in flight", tg.Mountpoint))
			continue
		}

		// Acquire semaphore slot before launching so blocked kernel syscalls stay strictly bounded.
		select {
		case c.sem <- struct{}{}:
		case <-ctx.Done():
			c.clearInFlight(tg.Mountpoint)
			snap.Warnings = append(snap.Warnings, fmt.Errorf("filesystem %s probe skipped: %w", tg.Mountpoint, ctx.Err()))
			continue
		}

		launched++
		go func(target FilesystemTarget) {
			defer func() {
				<-c.sem
				c.clearInFlight(target.Mountpoint)
			}()
			st, err := c.statfs(target.Mountpoint)
			if err != nil {
				resCh <- fmt.Errorf("statfs %s: %w", target.Mountpoint, err)
				return
			}
			if ctx.Err() == nil {
				c.cacheMu.Lock()
				c.cache[target.ResourceID] = cachedFSReading{
					ResourceID: target.ResourceID,
					ObservedAt: c.clock.Now(),
					Mono:       c.clock.Monotonic(),
					Mountpoint: target.Mountpoint,
					Stat:       st,
				}
				c.cacheMu.Unlock()
			}
			resCh <- nil
		}(tg)
	}

waitLoop:
	for range launched {
		select {
		case err := <-resCh:
			if err != nil {
				snap.Warnings = append(snap.Warnings, err)
			}
		case <-ctx.Done():
			snap.Warnings = append(snap.Warnings, fmt.Errorf("filesystem collect stopped waiting: %w", ctx.Err()))
			break waitLoop
		}
	}

	snap.Samples = c.buildCachedSamples(targets)
	return snap, nil
}

// CachedSnapshot returns the latest cached filesystem gauge samples preserving each reading's ObservedAt timestamp.
func (c *FilesystemCollector) CachedSnapshot() metrics.RawSnapshot {
	var targets []FilesystemTarget
	if c.targets != nil {
		targets = c.targets()
	}
	return metrics.RawSnapshot{
		CollectedAt: c.clock.Now(),
		Monotonic:   c.clock.Monotonic(),
		Samples:     c.buildCachedSamples(targets),
	}
}

func (c *FilesystemCollector) buildCachedSamples(targets []FilesystemTarget) []metrics.RawSample {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	var samples []metrics.RawSample
	for _, tg := range targets {
		rd, ok := c.cache[tg.ResourceID]
		if !ok {
			continue
		}
		samples = append(samples, fsStatToSamples(rd.ResourceID, rd.Stat, rd.ObservedAt, rd.Mono)...)
	}
	return samples
}

func (c *FilesystemCollector) clearInFlight(mountpoint string) {
	c.inFlightMu.Lock()
	delete(c.inFlight, mountpoint)
	c.inFlightMu.Unlock()
}

func fsStatToSamples(resID string, st FSStat, ts time.Time, mono time.Duration) []metrics.RawSample {
	samples := []metrics.RawSample{
		metrics.NewGaugeSample(resID, metrics.KeyFilesystemTotalBytes, float64(st.TotalBytes), ts, mono),
		metrics.NewGaugeSample(resID, metrics.KeyFilesystemUsedBytes, float64(st.UsedBytes), ts, mono),
		metrics.NewGaugeSample(resID, metrics.KeyFilesystemAvailableBytes, float64(st.AvailableBytes), ts, mono),
	}
	if st.TotalBytes > 0 {
		usedPct := (float64(st.UsedBytes) / float64(st.TotalBytes)) * 100.0
		samples = append(samples,
			metrics.NewGaugeSample(resID, metrics.KeyFilesystemUsedPct, usedPct, ts, mono),
		)
	}
	if st.TotalInodes > 0 && st.TotalInodes >= st.FreeInodes {
		inodesPct := (float64(st.TotalInodes-st.FreeInodes) / float64(st.TotalInodes)) * 100.0
		samples = append(samples,
			metrics.NewGaugeSample(resID, metrics.KeyFilesystemInodesUsedPct, inodesPct, ts, mono),
		)
	}
	return samples
}
