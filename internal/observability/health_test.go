package observability

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestHealthTrackerTransitionsAndConcurrency(t *testing.T) {
	h := NewHealthTracker()
	snap := h.Snapshot()
	if snap.Status != StatusOK || !snap.DBOK {
		t.Fatalf("expected initial healthy status, got %+v", snap)
	}

	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	h.RecordCollect(now)
	h.RecordCollectorError("sensors")
	h.RecordDBError(errors.New("disk full"))
	h.SetRollupLag(620)
	h.RecordDroppedLiveEvent()
	h.SetLiveSubscribers(2)
	h.RecordWallClockJump()

	snap = h.Snapshot()
	if snap.Status != StatusDegraded || snap.DBOK || snap.LastDBError != "disk full" {
		t.Fatalf("expected degraded db status, got %+v", snap)
	}
	if snap.CollectorErrorCount != 1 || snap.CollectorErrorsByName["sensors"] != 1 {
		t.Fatalf("unexpected collector error counts: %+v", snap)
	}
	if snap.RollupLagS != 620 || snap.DroppedLiveEvents != 1 || snap.ActiveLiveSubscribers != 2 || snap.WallClockJumpCount != 1 {
		t.Fatalf("unexpected health counters: %+v", snap)
	}

	h.RecordWriteOK(now.Add(5 * time.Second))
	snap = h.Snapshot()
	if snap.Status != StatusOK || !snap.DBOK || snap.LastDBError != "" || snap.LastWriteAt == nil {
		t.Fatalf("expected recovery after write ok, got %+v", snap)
	}

	// Verify defensive copy & race safety.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				h.RecordCollect(now)
				h.RecordCollectorError("cpu")
				_ = h.Snapshot()
			}
		}()
	}
	wg.Wait()
}

func TestNewLoggerJSON(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, ParseLogLevel("debug"))
	log.Debug("collector tick", slog.String("collector", "cpu"), slog.Int("samples", 5))

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("expected valid JSON log output, got %q: %v", buf.String(), err)
	}
	if decoded["msg"] != "collector tick" || decoded["collector"] != "cpu" {
		t.Fatalf("unexpected log fields: %v", decoded)
	}
}
