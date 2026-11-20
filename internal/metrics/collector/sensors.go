package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"vektor/internal/metrics"
)

type cachedSensorReading struct {
	ResourceID string
	Celsius    float64
	ObservedAt time.Time
	Mono       time.Duration
}

// SensorCollector reads hwmon temperature inputs with per-sensor in-flight guards and caching.
type SensorCollector struct {
	clock      metrics.Clock
	targets    func() []SensorTarget
	sem        chan struct{}
	inFlightMu sync.Mutex
	inFlight   map[string]bool
	cacheMu    sync.RWMutex
	cache      map[string]cachedSensorReading
}

// NewSensorCollector constructs a SensorCollector.
func NewSensorCollector(clock metrics.Clock, targets func() []SensorTarget) *SensorCollector {
	return &SensorCollector{
		clock:    clock,
		targets:  targets,
		sem:      make(chan struct{}, 2),
		inFlight: make(map[string]bool),
		cache:    make(map[string]cachedSensorReading),
	}
}

func (c *SensorCollector) Name() string {
	return "sensors"
}

func (c *SensorCollector) Collect(ctx context.Context) (metrics.RawSnapshot, error) {
	var targets []SensorTarget
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

	type result struct {
		target SensorTarget
		val    float64
		ts     time.Time
		mono   time.Duration
		err    error
	}

	resCh := make(chan result, len(targets))
	launched := 0

	for _, tg := range targets {
		c.inFlightMu.Lock()
		busy := c.inFlight[tg.InputPath]
		if !busy {
			c.inFlight[tg.InputPath] = true
		}
		c.inFlightMu.Unlock()

		if busy {
			snap.Warnings = append(snap.Warnings, fmt.Errorf("sensor %s read skipped: previous read still in flight", tg.InputPath))
			continue
		}

		select {
		case c.sem <- struct{}{}:
		case <-ctx.Done():
			c.clearInFlight(tg.InputPath)
			snap.Warnings = append(snap.Warnings, fmt.Errorf("sensor %s read skipped: %w", tg.InputPath, ctx.Err()))
			continue
		}

		launched++
		go func(target SensorTarget) {
			defer func() {
				<-c.sem
				c.clearInFlight(target.InputPath)
			}()
			val, err := readSensorCelsius(target.InputPath)
			resCh <- result{
				target: target,
				val:    val,
				ts:     c.clock.Now(),
				mono:   c.clock.Monotonic(),
				err:    err,
			}
		}(tg)
	}

	collected := 0
waitLoop:
	for collected < launched {
		select {
		case r := <-resCh:
			collected++
			if r.err != nil {
				snap.Warnings = append(snap.Warnings, fmt.Errorf("read sensor %s: %w", r.target.InputPath, r.err))
				continue
			}
			c.cacheMu.Lock()
			c.cache[r.target.ResourceID] = cachedSensorReading{
				ResourceID: r.target.ResourceID,
				Celsius:    r.val,
				ObservedAt: r.ts,
				Mono:       r.mono,
			}
			c.cacheMu.Unlock()
		case <-ctx.Done():
			snap.Warnings = append(snap.Warnings, fmt.Errorf("sensor collect timed out waiting for %d probe(s): %w", launched-collected, ctx.Err()))
			break waitLoop
		}
	}

	snap.Samples = c.buildCachedSamples(targets)
	return snap, nil
}

// CachedSnapshot returns cached sensor readings with original observation timestamps.
func (c *SensorCollector) CachedSnapshot() metrics.RawSnapshot {
	var targets []SensorTarget
	if c.targets != nil {
		targets = c.targets()
	}
	return metrics.RawSnapshot{
		CollectedAt: c.clock.Now(),
		Monotonic:   c.clock.Monotonic(),
		Samples:     c.buildCachedSamples(targets),
	}
}

func (c *SensorCollector) buildCachedSamples(targets []SensorTarget) []metrics.RawSample {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	var samples []metrics.RawSample
	for _, tg := range targets {
		rd, ok := c.cache[tg.ResourceID]
		if !ok {
			continue
		}
		samples = append(samples, metrics.NewGaugeSample(
			rd.ResourceID,
			metrics.KeySensorTemperatureCelsius,
			rd.Celsius,
			rd.ObservedAt,
			rd.Mono,
		))
	}
	return samples
}

func (c *SensorCollector) clearInFlight(path string) {
	c.inFlightMu.Lock()
	delete(c.inFlight, path)
	c.inFlightMu.Unlock()
}

func readSensorCelsius(path string) (float64, error) {
	data, err := readFileBounded(path, 256)
	if err != nil {
		return 0, err
	}
	milli, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse millidegrees %q: %w", strings.TrimSpace(string(data)), err)
	}
	return float64(milli) / 1000.0, nil
}
