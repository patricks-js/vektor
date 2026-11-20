package tracker

import (
	"math"
	"testing"
	"time"

	"vektor/internal/metrics"
)

func TestCounterFirstCollectionProducesNoRate(t *testing.T) {
	tr := NewCounterTracker()
	t0 := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	pt, warns := tr.Update(metrics.ProfilePersist, "boot-1",
		metrics.NewCounterSample("nic:eth0", metrics.RawNetRxBytesTotal, 100000, 64, t0, 0),
		metrics.KeyNetRxBytesPerSec,
	)
	if pt.Valid {
		t.Fatalf("expected invalid point on first collection, got %+v", pt)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings on initial baseline: %+v", warns)
	}
}

func TestCounterResetAndWrap32Bit(t *testing.T) {
	t.Run("64-bit reset discards one interval without negative or spike", func(t *testing.T) {
		tr := NewCounterTracker()
		clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC))

		pt, _ := tr.Update(metrics.ProfilePersist, "boot-1",
			metrics.NewCounterSample("disk:nvme0", metrics.RawDiskReadBytesTotal, 5000, 64, clk.Now(), clk.Monotonic()),
			metrics.KeyDiskReadBytesPerSec,
		)
		if pt.Valid {
			t.Fatal("first sample must not emit rate")
		}

		clk.Advance(10 * time.Second)
		pt, warns := tr.Update(metrics.ProfilePersist, "boot-1",
			metrics.NewCounterSample("disk:nvme0", metrics.RawDiskReadBytesTotal, 100, 64, clk.Now(), clk.Monotonic()),
			metrics.KeyDiskReadBytesPerSec,
		)
		if pt.Valid {
			t.Fatalf("expected reset sample to be discarded, got %+v", pt)
		}
		if len(warns) == 0 || warns[0].Code != WarningCounterReset {
			t.Fatalf("expected WarningCounterReset, got %+v", warns)
		}

		clk.Advance(10 * time.Second)
		pt, warns = tr.Update(metrics.ProfilePersist, "boot-1",
			metrics.NewCounterSample("disk:nvme0", metrics.RawDiskReadBytesTotal, 1100, 64, clk.Now(), clk.Monotonic()),
			metrics.KeyDiskReadBytesPerSec,
		)
		if !pt.Valid || len(warns) != 0 {
			t.Fatalf("expected valid post-reset rate, got pt=%+v warns=%+v", pt, warns)
		}
		if math.Abs(pt.Value-100.0) > 1e-9 {
			t.Fatalf("expected rate 100.0 B/s, got %v", pt.Value)
		}
	})

	t.Run("32-bit wrap computes exact delta while small decrease still resets", func(t *testing.T) {
		tr := NewCounterTracker()
		clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC))

		// Near 2^32 wrap: 0xFFFFFF00 -> 0x00000100 over 10s => delta 512 => 51.2/s.
		tr.Update(metrics.ProfilePersist, "boot-1",
			metrics.NewCounterSample("nic:legacy", metrics.RawNetRxBytesTotal, 0xFFFFFF00, 32, clk.Now(), clk.Monotonic()),
			metrics.KeyNetRxBytesPerSec,
		)
		clk.Advance(10 * time.Second)
		pt, warns := tr.Update(metrics.ProfilePersist, "boot-1",
			metrics.NewCounterSample("nic:legacy", metrics.RawNetRxBytesTotal, 0x00000100, 32, clk.Now(), clk.Monotonic()),
			metrics.KeyNetRxBytesPerSec,
		)
		if !pt.Valid || len(warns) != 0 {
			t.Fatalf("expected valid 32-bit wrap rate, got pt=%+v warns=%+v", pt, warns)
		}
		if math.Abs(pt.Value-51.2) > 1e-9 {
			t.Fatalf("expected rate 51.2, got %v", pt.Value)
		}

		// Small decrease 500 -> 10 even with CounterBits=32 must be treated as reset, not 4GiB wrap.
		tr.Update(metrics.ProfilePersist, "boot-1",
			metrics.NewCounterSample("nic:reset32", metrics.RawNetRxBytesTotal, 500, 32, clk.Now(), clk.Monotonic()),
			metrics.KeyNetRxBytesPerSec,
		)
		clk.Advance(10 * time.Second)
		pt, warns = tr.Update(metrics.ProfilePersist, "boot-1",
			metrics.NewCounterSample("nic:reset32", metrics.RawNetRxBytesTotal, 10, 32, clk.Now(), clk.Monotonic()),
			metrics.KeyNetRxBytesPerSec,
		)
		if pt.Valid {
			t.Fatalf("expected reset instead of false 32-bit wrap, got %+v", pt)
		}
		if len(warns) == 0 || warns[0].Code != WarningCounterReset {
			t.Fatalf("expected WarningCounterReset, got %+v", warns)
		}
	})
}

func TestCounterLarge64BitPrecisionAbove2Pow53(t *testing.T) {
	tr := NewCounterTracker()
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC))

	base := uint64(1)<<55 + 1000
	tr.Update(metrics.ProfilePersist, "boot-1",
		metrics.NewCounterSample("nic:10g", metrics.RawNetRxBytesTotal, base, 64, clk.Now(), clk.Monotonic()),
		metrics.KeyNetRxBytesPerSec,
	)
	clk.Advance(10 * time.Second)
	pt, _ := tr.Update(metrics.ProfilePersist, "boot-1",
		metrics.NewCounterSample("nic:10g", metrics.RawNetRxBytesTotal, base+5000, 64, clk.Now(), clk.Monotonic()),
		metrics.KeyNetRxBytesPerSec,
	)
	if !pt.Valid || math.Abs(pt.Value-500.0) > 1e-9 {
		t.Fatalf("expected exact 500.0 B/s above 2^53, got %+v", pt)
	}
}

func TestProfilesIndependentAndBootIDReset(t *testing.T) {
	norm := NewNormalizer(func(id string) bool { return id == "nic:eth0" })
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC))

	makeSnap := func(rx uint64, cpuBusy, cpuIdle uint64) metrics.RawSnapshot {
		return metrics.RawSnapshot{
			CollectedAt: clk.Now(),
			Monotonic:   clk.Monotonic(),
			Samples: []metrics.RawSample{
				metrics.NewCounterSample("nic:eth0", metrics.RawNetRxBytesTotal, rx, 64, clk.Now(), clk.Monotonic()),
			},
			CPUSamples: []metrics.CPUSample{
				{
					ResourceID: metrics.HostResourceID,
					Core:       "total",
					User:       cpuBusy,
					Idle:       cpuIdle,
					Time:       clk.Now(),
					Monotonic:  clk.Monotonic(),
				},
			},
		}
	}

	// T=0s: persist baseline
	r0 := norm.Normalize(metrics.ProfilePersist, "boot-1", makeSnap(1000, 100, 900))
	if len(r0.Points) != 0 {
		t.Fatalf("expected 0 points on initial persist baseline, got %+v", r0.Points)
	}

	// T=2s: live baseline
	clk.Advance(2 * time.Second)
	l0 := norm.Normalize(metrics.ProfileLive, "boot-1", makeSnap(1200, 110, 990))
	if len(l0.Points) != 0 {
		t.Fatalf("expected 0 points on initial live baseline, got %+v", l0.Points)
	}

	// T=4s: live second sample -> 200 B/2s = 100 B/s
	clk.Advance(2 * time.Second)
	l1 := norm.Normalize(metrics.ProfileLive, "boot-1", makeSnap(1400, 130, 1070))
	assertHasMetric(t, l1.Points, "nic:eth0", metrics.KeyNetRxBytesPerSec, 100.0)
	assertHasMetric(t, l1.Points, metrics.HostResourceID, metrics.KeyNetRxBytesPerSec, 100.0)
	assertHasMetric(t, l1.Points, metrics.HostResourceID, metrics.KeyCPUBusyPct, 20.0)

	// Clear live baselines on disconnect; persist must remain intact.
	norm.ClearProfile(metrics.ProfileLive)
	if norm.CounterBaselineCount(metrics.ProfileLive) != 0 || norm.CounterBaselineCount(metrics.ProfilePersist) != 1 {
		t.Fatalf("unexpected baseline counts after clearing live profile")
	}

	// T=30s: persist second sample -> (4000 - 1000) / 30s = 100 B/s
	clk.Advance(26 * time.Second)
	r1 := norm.Normalize(metrics.ProfilePersist, "boot-1", makeSnap(4000, 250, 1750))
	assertHasMetric(t, r1.Points, "nic:eth0", metrics.KeyNetRxBytesPerSec, 100.0)
	assertHasMetric(t, r1.Points, metrics.HostResourceID, metrics.KeyCPUBusyPct, 15.0)

	// Reboot (new boot_id) invalidates all baselines even if counters look bigger.
	clk.Advance(30 * time.Second)
	rReboot := norm.Normalize(metrics.ProfilePersist, "boot-2", makeSnap(8000, 500, 2500))
	if !rReboot.BootChanged || len(rReboot.Points) != 0 {
		t.Fatalf("expected BootChanged and 0 points on first sample after reboot, got %+v", rReboot)
	}
}

func TestNTPBackwardWallJumpUsesMonotonicClock(t *testing.T) {
	norm := NewNormalizer(nil)
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC))

	norm.Normalize(metrics.ProfilePersist, "boot-1", metrics.RawSnapshot{
		CollectedAt: clk.Now(),
		Monotonic:   clk.Monotonic(),
		Samples: []metrics.RawSample{
			metrics.NewCounterSample("disk:sda", metrics.RawDiskWriteBytesTotal, 1000, 64, clk.Now(), clk.Monotonic()),
		},
	})

	// Wall clock jumps BACKWARD 2 hours via NTP while monotonic clock moves +10s.
	clk.Step(-2*time.Hour, 10*time.Second)
	res := norm.Normalize(metrics.ProfilePersist, "boot-1", metrics.RawSnapshot{
		CollectedAt: clk.Now(),
		Monotonic:   clk.Monotonic(),
		Samples: []metrics.RawSample{
			metrics.NewCounterSample("disk:sda", metrics.RawDiskWriteBytesTotal, 3500, 64, clk.Now(), clk.Monotonic()),
		},
	})
	assertHasMetric(t, res.Points, "disk:sda", metrics.KeyDiskWriteBytesPerSec, 250.0)

	foundJumpWarn := false
	for _, w := range res.Warnings {
		if w.Code == WarningWallClockJump {
			foundJumpWarn = true
			break
		}
	}
	if !foundJumpWarn {
		t.Fatalf("expected WarningWallClockJump on backward NTP step, got %+v", res.Warnings)
	}
}

func TestNormalizerSkipsNonFiniteAndEvictsStale(t *testing.T) {
	norm := NewNormalizer(nil)
	clk := metrics.NewFakeClock(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC))

	res := norm.Normalize(metrics.ProfilePersist, "boot-1", metrics.RawSnapshot{
		CollectedAt: clk.Now(),
		Monotonic:   clk.Monotonic(),
		Samples: []metrics.RawSample{
			metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeyLoad1m, math.NaN(), clk.Now(), clk.Monotonic()),
			metrics.NewGaugeSample(metrics.HostResourceID, metrics.KeyLoad5m, 1.25, clk.Now(), clk.Monotonic()),
			metrics.NewCounterSample("nic:gone", metrics.RawNetRxBytesTotal, 100, 64, clk.Now(), clk.Monotonic()),
		},
	})
	if len(res.Points) != 1 || res.Points[0].Key != metrics.KeyLoad5m {
		t.Fatalf("expected only finite load.5m point, got %+v", res.Points)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Code != WarningInvalidSample {
		t.Fatalf("expected WarningInvalidSample for NaN, got %+v", res.Warnings)
	}

	clk.Advance(2 * time.Hour)
	evicted := norm.EvictStale(clk.Now(), time.Hour)
	if evicted != 1 {
		t.Fatalf("expected 1 stale baseline evicted, got %d", evicted)
	}
}

func assertHasMetric(t *testing.T, pts []metrics.MetricPoint, resID, key string, want float64) {
	t.Helper()
	for _, p := range pts {
		if p.ResourceID == resID && p.Key == key {
			if !p.Valid {
				t.Fatalf("metric %s/%s was invalid", resID, key)
			}
			if math.Abs(p.Value-want) > 1e-6 {
				t.Fatalf("metric %s/%s = %v, want %v", resID, key, p.Value, want)
			}
			return
		}
	}
	t.Fatalf("metric %s/%s not found in %+v", resID, key, pts)
}
