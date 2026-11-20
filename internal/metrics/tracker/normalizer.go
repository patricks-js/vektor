package tracker

import (
	"math"
	"sync"
	"time"

	"vektor/internal/metrics"
)

// NICIncludeFunc reports whether a NIC resource ID contributes to the host aggregate network rate.
type NICIncludeFunc func(resourceID string) bool

// NormalizeResult contains derived MetricPoints and quality warnings from one snapshot normalization.
type NormalizeResult struct {
	Points      []metrics.MetricPoint
	Warnings    []Warning
	BootChanged bool
}

// Normalizer converts raw gauges, raw counters, and CPU jiffies into catalog-validated MetricPoints.
type Normalizer struct {
	counters *CounterTracker
	cpu      *CPUTracker

	mu           sync.RWMutex
	includeInNet NICIncludeFunc
}

// NewNormalizer creates a Normalizer coordinating CounterTracker and CPUTracker.
func NewNormalizer(includeInNet NICIncludeFunc) *Normalizer {
	return &Normalizer{
		counters:     NewCounterTracker(),
		cpu:          NewCPUTracker(),
		includeInNet: includeInNet,
	}
}

// SetNICIncludeFunc sets or updates the host-aggregate NIC filter callback.
func (n *Normalizer) SetNICIncludeFunc(fn NICIncludeFunc) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.includeInNet = fn
}

// Normalize transforms snap for the given profile and bootID into finite catalog MetricPoints.
func (n *Normalizer) Normalize(profile metrics.Profile, bootID string, snap metrics.RawSnapshot) NormalizeResult {
	var res NormalizeResult

	// Ensure a boot_id change invalidates both counter and CPU baselines together.
	if n.counters.SetBootID(bootID) {
		res.BootChanged = true
	}
	if n.cpu.SetBootID(bootID) {
		res.BootChanged = true
	}
	if res.BootChanged {
		res.Warnings = append(res.Warnings, Warning{
			Code:       WarningBootChanged,
			ResourceID: metrics.HostResourceID,
			Key:        "boot_id",
			Message:    "boot_id changed; invalidated all counter and CPU baselines",
		})
	}

	for _, cs := range snap.CPUSamples {
		pts, warns := n.cpu.Update(profile, bootID, cs)
		res.Points = append(res.Points, pts...)
		res.Warnings = append(res.Warnings, warns...)
	}

	n.mu.RLock()
	includeFn := n.includeInNet
	n.mu.RUnlock()

	var aggRx, aggTx float64
	var aggRxTsMs, aggTxTsMs int64
	var hasAggRx, hasAggTx bool

	for _, s := range snap.Samples {
		switch s.Kind {
		case metrics.KindGauge:
			if !metrics.IsKnownMetric(s.Key) {
				res.Warnings = append(res.Warnings, Warning{
					Code:       WarningUnknownMetric,
					ResourceID: s.ResourceID,
					Key:        s.Key,
					Message:    "dropped unknown gauge metric key",
				})
				continue
			}
			if math.IsNaN(s.GaugeValue) || math.IsInf(s.GaugeValue, 0) {
				res.Warnings = append(res.Warnings, Warning{
					Code:       WarningInvalidSample,
					ResourceID: s.ResourceID,
					Key:        s.Key,
					Message:    "dropped non-finite gauge sample",
				})
				continue
			}
			res.Points = append(res.Points, metrics.MetricPoint{
				ResourceID:  s.ResourceID,
				Key:         s.Key,
				TimestampMs: s.Time.UTC().UnixMilli(),
				Value:       s.GaugeValue,
				Valid:       true,
			})

		case metrics.KindCounter:
			outKey, ok := metrics.CounterOutputKey[s.Key]
			if !ok || !metrics.IsKnownMetric(outKey) {
				res.Warnings = append(res.Warnings, Warning{
					Code:       WarningUnknownMetric,
					ResourceID: s.ResourceID,
					Key:        s.Key,
					Message:    "dropped unknown counter metric key",
				})
				continue
			}
			pt, warns := n.counters.Update(profile, bootID, s, outKey)
			res.Warnings = append(res.Warnings, warns...)
			if !pt.Valid {
				continue
			}
			res.Points = append(res.Points, pt)

			if includeFn != nil && includeFn(s.ResourceID) {
				switch outKey {
				case metrics.KeyNetRxBytesPerSec:
					aggRx += pt.Value
					if pt.TimestampMs > aggRxTsMs {
						aggRxTsMs = pt.TimestampMs
					}
					hasAggRx = true
				case metrics.KeyNetTxBytesPerSec:
					aggTx += pt.Value
					if pt.TimestampMs > aggTxTsMs {
						aggTxTsMs = pt.TimestampMs
					}
					hasAggTx = true
				}
			}

		default:
			res.Warnings = append(res.Warnings, Warning{
				Code:       WarningInvalidSample,
				ResourceID: s.ResourceID,
				Key:        s.Key,
				Message:    "unknown sample kind",
			})
		}
	}

	if hasAggRx {
		res.Points = append(res.Points, metrics.MetricPoint{
			ResourceID:  metrics.HostResourceID,
			Key:         metrics.KeyNetRxBytesPerSec,
			TimestampMs: aggRxTsMs,
			Value:       aggRx,
			Valid:       true,
		})
	}
	if hasAggTx {
		res.Points = append(res.Points, metrics.MetricPoint{
			ResourceID:  metrics.HostResourceID,
			Key:         metrics.KeyNetTxBytesPerSec,
			TimestampMs: aggTxTsMs,
			Value:       aggTx,
			Valid:       true,
		})
	}

	return res
}

// ClearProfile removes baselines for the given profile from both CounterTracker and CPUTracker.
func (n *Normalizer) ClearProfile(profile metrics.Profile) {
	n.counters.ClearProfile(profile)
	n.cpu.ClearProfile(profile)
}

// EvictStale prunes stale counter and CPU baselines older than maxAge.
func (n *Normalizer) EvictStale(now time.Time, maxAge time.Duration) int {
	return n.counters.EvictStale(now, maxAge) + n.cpu.EvictStale(now, maxAge)
}

// CounterBaselineCount returns active counter baselines for a profile.
func (n *Normalizer) CounterBaselineCount(profile metrics.Profile) int {
	return n.counters.BaselineCount(profile)
}
