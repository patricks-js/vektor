package tracker

import (
	"fmt"
	"sync"
	"time"

	"vektor/internal/metrics"
)

type cpuKey struct {
	profile    metrics.Profile
	resourceID string
}

type cpuState struct {
	prev     metrics.CPUSample
	lastSeen time.Time
}

// CPUTracker derives bounded percentage CPU utilization from cumulative /proc/stat jiffies.
type CPUTracker struct {
	mu     sync.Mutex
	bootID string
	states map[cpuKey]cpuState
}

// NewCPUTracker constructs an empty CPUTracker.
func NewCPUTracker() *CPUTracker {
	return &CPUTracker{
		states: make(map[cpuKey]cpuState),
	}
}

// SetBootID updates the active boot ID and invalidates all CPU baselines if it changed.
func (t *CPUTracker) SetBootID(bootID string) bool {
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

// Update processes one CPUSample and emits derived percentage MetricPoints when a valid baseline exists.
func (t *CPUTracker) Update(profile metrics.Profile, bootID string, curr metrics.CPUSample) ([]metrics.MetricPoint, []Warning) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var warnings []Warning
	if bootID != "" {
		if t.bootID != "" && t.bootID != bootID {
			clear(t.states)
			warnings = append(warnings, Warning{
				Code:       WarningBootChanged,
				ResourceID: curr.ResourceID,
				Key:        "cpu",
				Message:    fmt.Sprintf("boot_id changed from %q to %q; cleared CPU baselines", t.bootID, bootID),
			})
		}
		t.bootID = bootID
	}

	k := cpuKey{profile: profile, resourceID: curr.ResourceID}
	wall := curr.Time.UTC()
	prevEntry, exists := t.states[k]
	if !exists {
		t.states[k] = cpuState{prev: curr, lastSeen: wall}
		return nil, warnings
	}

	prev := prevEntry.prev
	elapsed := curr.Monotonic - prev.Monotonic
	if elapsed <= 0 {
		warnings = append(warnings, Warning{
			Code:       WarningBadElapsed,
			ResourceID: curr.ResourceID,
			Key:        "cpu",
			Message:    fmt.Sprintf("discarded CPU interval with monotonic elapsed=%s", elapsed),
		})
		if curr.Monotonic < prev.Monotonic {
			t.states[k] = cpuState{prev: curr, lastSeen: wall}
		}
		return nil, warnings
	}

	if wallJumpDetected(prev.Time.UTC(), wall, elapsed) {
		warnings = append(warnings, Warning{
			Code:       WarningWallClockJump,
			ResourceID: curr.ResourceID,
			Key:        "cpu",
			Message:    fmt.Sprintf("wall clock stepped from %s to %s during CPU interval", prev.Time.UTC().Format(time.RFC3339), wall.Format(time.RFC3339)),
		})
	}

	if curr.User < prev.User ||
		curr.Nice < prev.Nice ||
		curr.System < prev.System ||
		curr.Idle < prev.Idle ||
		curr.IOWait < prev.IOWait ||
		curr.IRQ < prev.IRQ ||
		curr.SoftIRQ < prev.SoftIRQ ||
		curr.Steal < prev.Steal {
		t.states[k] = cpuState{prev: curr, lastSeen: wall}
		warnings = append(warnings, Warning{
			Code:       WarningCounterReset,
			ResourceID: curr.ResourceID,
			Key:        "cpu",
			Message:    "CPU jiffy counter decreased; reset baseline",
		})
		return nil, warnings
	}

	dUser := curr.User - prev.User
	dNice := curr.Nice - prev.Nice
	dSystem := curr.System - prev.System
	dIdle := curr.Idle - prev.Idle
	dIOWait := curr.IOWait - prev.IOWait
	dIRQ := curr.IRQ - prev.IRQ
	dSoftIRQ := curr.SoftIRQ - prev.SoftIRQ
	dSteal := curr.Steal - prev.Steal

	dTotal := dUser + dNice + dSystem + dIdle + dIOWait + dIRQ + dSoftIRQ + dSteal
	t.states[k] = cpuState{prev: curr, lastSeen: wall}

	if dTotal == 0 {
		return nil, warnings
	}

	dBusy := dTotal - (dIdle + dIOWait)
	totalF := float64(dTotal)
	tsMs := wall.UnixMilli()

	if curr.Core == "total" || curr.ResourceID == metrics.HostResourceID {
		return []metrics.MetricPoint{
			{ResourceID: curr.ResourceID, Key: metrics.KeyCPUBusyPct, TimestampMs: tsMs, Value: 100 * float64(dBusy) / totalF, Valid: true},
			{ResourceID: curr.ResourceID, Key: metrics.KeyCPUUserPct, TimestampMs: tsMs, Value: 100 * float64(dUser+dNice) / totalF, Valid: true},
			{ResourceID: curr.ResourceID, Key: metrics.KeyCPUSystemPct, TimestampMs: tsMs, Value: 100 * float64(dSystem+dIRQ+dSoftIRQ) / totalF, Valid: true},
			{ResourceID: curr.ResourceID, Key: metrics.KeyCPUIOWaitPct, TimestampMs: tsMs, Value: 100 * float64(dIOWait) / totalF, Valid: true},
			{ResourceID: curr.ResourceID, Key: metrics.KeyCPUStealPct, TimestampMs: tsMs, Value: 100 * float64(dSteal) / totalF, Valid: true},
		}, warnings
	}

	return []metrics.MetricPoint{
		{ResourceID: curr.ResourceID, Key: metrics.KeyCPUCoreBusyPct, TimestampMs: tsMs, Value: 100 * float64(dBusy) / totalF, Valid: true},
	}, warnings
}

// ClearProfile discards all CPU baselines belonging to the specified profile.
func (t *CPUTracker) ClearProfile(profile metrics.Profile) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range t.states {
		if k.profile == profile {
			delete(t.states, k)
		}
	}
}

// EvictStale removes CPU baselines older than maxAge relative to now.
func (t *CPUTracker) EvictStale(now time.Time, maxAge time.Duration) int {
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
