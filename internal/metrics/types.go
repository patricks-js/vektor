package metrics

import (
	"context"
	"sync"
	"time"
)

const HostResourceID = "host:local"

// Profile identifies an independent collection and rate-tracking baseline stream.
type Profile string

const (
	ProfilePersist Profile = "persist"
	ProfileLive    Profile = "live"
)

// MetricKind distinguishes instantaneous gauges from cumulative counters at the raw boundary.
type MetricKind string

const (
	KindGauge   MetricKind = "gauge"
	KindCounter MetricKind = "counter"
)

// MetricModality distinguishes stored gauge vs derived rate series in the central catalog.
type MetricModality string

const (
	ModalityGauge MetricModality = "gauge"
	ModalityRate  MetricModality = "rate"
)

// Clock supplies both UTC wall time (for bucket alignment) and monotonic duration (for rate intervals).
type Clock interface {
	Now() time.Time
	Monotonic() time.Duration
}

// RealClock uses the process monotonic clock anchored at construction time and UTC wall clock.
type RealClock struct {
	anchor time.Time
}

// NewRealClock constructs a Clock whose monotonic durations never regress across NTP jumps.
func NewRealClock() *RealClock {
	return &RealClock{anchor: time.Now()}
}

func (c *RealClock) Now() time.Time {
	return time.Now().UTC()
}

func (c *RealClock) Monotonic() time.Duration {
	return time.Since(c.anchor)
}

// FakeClock is a thread-safe deterministic Clock for unit and integration tests.
type FakeClock struct {
	mu   sync.Mutex
	wall time.Time
	mono time.Duration
}

// NewFakeClock initializes a deterministic clock at wall (UTC) and monotonic zero.
func NewFakeClock(wall time.Time) *FakeClock {
	return &FakeClock{wall: wall.UTC()}
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wall
}

func (c *FakeClock) Monotonic() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mono
}

// Advance moves both wall clock and monotonic duration forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = c.wall.Add(d)
	c.mono += d
}

// Step allows shifting wall clock and monotonic duration independently (e.g. NTP jumps).
func (c *FakeClock) Step(wallDelta, monoDelta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = c.wall.Add(wallDelta)
	c.mono += monoDelta
}

// SetWall sets the UTC wall clock directly without altering the monotonic clock.
func (c *FakeClock) SetWall(wall time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = wall.UTC()
}

// RawSample represents either a raw gauge reading or a raw 64-bit/32-bit monotonic counter.
type RawSample struct {
	ResourceID   string
	Key          string
	Kind         MetricKind
	GaugeValue   float64
	CounterValue uint64
	CounterBits  uint8 // Defaults to 64 when 0; set to 32 only for documented 32-bit wrapping Linux fields.
	Time         time.Time
	Monotonic    time.Duration
}

// NewGaugeSample builds a raw gauge sample.
func NewGaugeSample(resourceID, key string, val float64, wall time.Time, mono time.Duration) RawSample {
	return RawSample{
		ResourceID: resourceID,
		Key:        key,
		Kind:       KindGauge,
		GaugeValue: val,
		Time:       wall.UTC(),
		Monotonic:  mono,
	}
}

// NewCounterSample builds a raw counter sample preserving exact uint64 precision.
func NewCounterSample(resourceID, key string, val uint64, bits uint8, wall time.Time, mono time.Duration) RawSample {
	if bits == 0 {
		bits = 64
	}
	return RawSample{
		ResourceID:   resourceID,
		Key:          key,
		Kind:         KindCounter,
		CounterValue: val,
		CounterBits:  bits,
		Time:         wall.UTC(),
		Monotonic:    mono,
	}
}

// CPUSample holds cumulative Linux /proc/stat jiffies for total CPU or an individual core.
type CPUSample struct {
	ResourceID string
	Core       string // "total" or core index ("0", "1", ...)
	User       uint64
	Nice       uint64
	System     uint64
	Idle       uint64
	IOWait     uint64
	IRQ        uint64
	SoftIRQ    uint64
	Steal      uint64
	Time       time.Time
	Monotonic  time.Duration
}

// RawSnapshot is the output of one Collector execution.
type RawSnapshot struct {
	CollectedAt time.Time
	Monotonic   time.Duration
	Samples     []RawSample
	CPUSamples  []CPUSample
	Warnings    []error
}

// Collector reads raw host telemetry without owning persistence or rate state.
type Collector interface {
	Name() string
	Collect(ctx context.Context) (RawSnapshot, error)
}

// MetricPoint is a normalized finite scalar point ready for persistence or live streaming.
type MetricPoint struct {
	ResourceID  string
	Key         string
	TimestampMs int64
	Value       float64
	Valid       bool
}
