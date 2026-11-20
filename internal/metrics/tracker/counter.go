package tracker

import (
	"fmt"
	"math"
	"sync"
	"time"

	"vektor/internal/metrics"
)

// WarningCode categorizes non-fatal data-quality events detected during normalization.
type WarningCode string

const (
	WarningBootChanged    WarningCode = "boot_changed"
	WarningCounterReset   WarningCode = "counter_reset"
	WarningBadElapsed     WarningCode = "non_positive_elapsed"
	WarningWallClockJump  WarningCode = "wall_clock_jump"
	WarningInvalidSample  WarningCode = "invalid_sample"
	WarningUnknownMetric  WarningCode = "unknown_metric"
)

// Warning describes a quality or counter anomaly observed without aborting the cycle.
type Warning struct {
	Code       WarningCode
	ResourceID string
	Key        string
	Message    string
}

func (w Warning) Error() string {
	return fmt.Sprintf("%s (%s/%s): %s", w.Code, w.ResourceID, w.Key, w.Message)
}

type counterKey struct {
	profile    metrics.Profile
	resourceID string
	key        string
}

type counterState struct {
	prev     uint64
	prevMono time.Duration
	prevWall time.Time
	lastSeen time.Time
}

// CounterTracker derives non-negative rates from cumulative integer counters per profile.
type CounterTracker struct {
	mu     sync.Mutex
	bootID string
	states map[counterKey]counterState
}

// NewCounterTracker constructs an empty CounterTracker.
func NewCounterTracker() *CounterTracker {
	return &CounterTracker{
		states: make(map[counterKey]counterState),
	}
}

// SetBootID updates the active boot ID and invalidates all baselines if it changed.
func (t *CounterTracker) SetBootID(bootID string) bool {
	if bootID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.bootID == "" {
		t.bootID = bootID
		return false
	}
	if t.bootID == bootID {
		return false
	}
	t.bootID = bootID
	clear(t.states)
	return true
}

// Update processes one RawCounter sample and derives a rate MetricPoint when a valid prior baseline exists.
func (t *CounterTracker) Update(profile metrics.Profile, bootID string, sample metrics.RawSample, outputKey string) (metrics.MetricPoint, []Warning) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var warnings []Warning
	if bootID != "" {
		if t.bootID != "" && t.bootID != bootID {
			clear(t.states)
			warnings = append(warnings, Warning{
				Code:       WarningBootChanged,
				ResourceID: sample.ResourceID,
				Key:        outputKey,
				Message:    fmt.Sprintf("boot_id changed from %q to %q; cleared all baselines", t.bootID, bootID),
			})
		}
		t.bootID = bootID
	}

	k := counterKey{
		profile:    profile,
		resourceID: sample.ResourceID,
		key:        sample.Key,
	}

	wall := sample.Time.UTC()
	prev, exists := t.states[k]
	if !exists {
		t.states[k] = counterState{
			prev:     sample.CounterValue,
			prevMono: sample.Monotonic,
			prevWall: wall,
			lastSeen: wall,
		}
		return metrics.MetricPoint{ResourceID: sample.ResourceID, Key: outputKey, Valid: false}, warnings
	}

	elapsed := sample.Monotonic - prev.prevMono
	if elapsed <= 0 {
		warnings = append(warnings, Warning{
			Code:       WarningBadElapsed,
			ResourceID: sample.ResourceID,
			Key:        outputKey,
			Message:    fmt.Sprintf("discarded interval with monotonic elapsed=%s", elapsed),
		})
		if sample.Monotonic < prev.prevMono {
			// Monotonic regression shouldn't occur within a process, re-baseline defensively.
			t.states[k] = counterState{
				prev:     sample.CounterValue,
				prevMono: sample.Monotonic,
				prevWall: wall,
				lastSeen: wall,
			}
		}
		return metrics.MetricPoint{ResourceID: sample.ResourceID, Key: outputKey, Valid: false}, warnings
	}

	if wallJumpDetected(prev.prevWall, wall, elapsed) {
		warnings = append(warnings, Warning{
			Code:       WarningWallClockJump,
			ResourceID: sample.ResourceID,
			Key:        outputKey,
			Message:    fmt.Sprintf("wall clock stepped from %s to %s while monotonic elapsed=%s", prev.prevWall.Format(time.RFC3339), wall.Format(time.RFC3339), elapsed),
		})
	}

	var delta uint64
	if sample.CounterValue >= prev.prev {
		delta = sample.CounterValue - prev.prev
	} else if isPlausibleWrap32(sample.CounterBits, prev.prev, sample.CounterValue) {
		delta = (uint64(1)<<32 - prev.prev) + sample.CounterValue
	} else {
		// Counter reset or device re-created: reinitialize baseline and drop one interval.
		t.states[k] = counterState{
			prev:     sample.CounterValue,
			prevMono: sample.Monotonic,
			prevWall: wall,
			lastSeen: wall,
		}
		warnings = append(warnings, Warning{
			Code:       WarningCounterReset,
			ResourceID: sample.ResourceID,
			Key:        outputKey,
			Message:    fmt.Sprintf("counter decreased (%d -> %d); reset baseline", prev.prev, sample.CounterValue),
		})
		return metrics.MetricPoint{ResourceID: sample.ResourceID, Key: outputKey, Valid: false}, warnings
	}

	rate := float64(delta) / elapsed.Seconds()
	t.states[k] = counterState{
		prev:     sample.CounterValue,
		prevMono: sample.Monotonic,
		prevWall: wall,
		lastSeen: wall,
	}

	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
		warnings = append(warnings, Warning{
			Code:       WarningInvalidSample,
			ResourceID: sample.ResourceID,
			Key:        outputKey,
			Message:    "derived rate was non-finite or negative",
		})
		return metrics.MetricPoint{ResourceID: sample.ResourceID, Key: outputKey, Valid: false}, warnings
	}

	return metrics.MetricPoint{
		ResourceID:  sample.ResourceID,
		Key:         outputKey,
		TimestampMs: wall.UnixMilli(),
		Value:       rate,
		Valid:       true,
	}, warnings
}

// ClearProfile discards all baselines belonging to the specified profile (e.g. when live subscribers disconnect).
func (t *CounterTracker) ClearProfile(profile metrics.Profile) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range t.states {
		if k.profile == profile {
			delete(t.states, k)
		}
	}
}

// EvictStale removes baselines whose last_seen timestamp is older than maxAge relative to now.
func (t *CounterTracker) EvictStale(now time.Time, maxAge time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := now.UTC().Add(-maxAge)
	evicted := 0
	for k, st := range t.states {
		if st.lastSeen.Before(cutoff) {
			delete(t.states, k)
			evicted++
		}
	}
	return evicted
}

// BaselineCount returns the number of stored counter baselines (useful for diagnostics/tests).
func (t *CounterTracker) BaselineCount(profile metrics.Profile) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for k := range t.states {
		if k.profile == profile {
			n++
		}
	}
	return n
}

func isPlausibleWrap32(bits uint8, prev, curr uint64) bool {
	if bits != 32 {
		return false
	}
	if prev > math.MaxUint32 || curr > math.MaxUint32 {
		return false
	}
	// Require clear evidence of wrapping near 2^32 rather than a small reset (e.g., 500 -> 10).
	return prev >= 0x80000000 && curr < 0x80000000
}

func wallJumpDetected(prevWall, currWall time.Time, monoElapsed time.Duration) bool {
	if currWall.Before(prevWall) {
		return true
	}
	wallElapsed := currWall.Sub(prevWall)
	skew := wallElapsed - monoElapsed
	if skew < 0 {
		skew = -skew
	}
	return skew > 60*time.Second
}
