package observability

import (
	"maps"
	"sync"
	"time"
)

// HealthStatus represents overall daemon health state.
type HealthStatus string

const (
	StatusOK       HealthStatus = "ok"
	StatusDegraded HealthStatus = "degraded"
)

// CollectorErrorDetail records the most recent error message and timestamp for a collector.
type CollectorErrorDetail struct {
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// HealthSnapshot is a point-in-time read-only view of internal daemon health counters.
type HealthSnapshot struct {
	Status                HealthStatus                    `json:"status"`
	DBOK                  bool                            `json:"db_ok"`
	LastDBError           string                          `json:"last_db_error,omitempty"`
	LastCollectAt         *time.Time                      `json:"last_collect_at,omitempty"`
	LastWriteAt           *time.Time                      `json:"last_write_at,omitempty"`
	RollupLagS            int64                           `json:"rollup_lag_s"`
	CollectorErrorCount   int64                           `json:"collector_error_count"`
	CollectorErrorsByName map[string]int64                `json:"collector_errors_by_name,omitempty"`
	CollectorLastErrors   map[string]CollectorErrorDetail `json:"collector_last_errors,omitempty"`
	DroppedLiveEvents     int64                           `json:"dropped_live_events"`
	ActiveLiveSubscribers int                             `json:"active_live_subscribers"`
	WallClockJumpCount    int64                           `json:"wall_clock_jump_count"`
	ClockError            string                          `json:"clock_error,omitempty"`
}

// HealthTracker tracks thread-safe runtime health signals required by TECHNICAL_DESIGN_V0.1.md.
type HealthTracker struct {
	mu                    sync.RWMutex
	dbOK                  bool
	lastDBError           string
	lastCollectAt         time.Time
	lastWriteAt           time.Time
	rollupLagS            int64
	collectorErrorCount   int64
	collectorErrorsByName map[string]int64
	collectorLastErrors   map[string]CollectorErrorDetail
	droppedLiveEvents     int64
	activeLiveSubscribers int
	wallClockJumpCount    int64
	clockError            string
}

// NewHealthTracker initializes a HealthTracker assuming healthy DB startup until proven otherwise.
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{
		dbOK:                  true,
		collectorErrorsByName: make(map[string]int64),
		collectorLastErrors:   make(map[string]CollectorErrorDetail),
	}
}

func (h *HealthTracker) RecordCollect(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastCollectAt = now.UTC()
}

func (h *HealthTracker) RecordWriteOK(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dbOK = true
	h.lastDBError = ""
	h.lastWriteAt = now.UTC()
}

func (h *HealthTracker) RecordDBError(err error) {
	if err == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dbOK = false
	h.lastDBError = err.Error()
}

func (h *HealthTracker) RecordCollectorError(name string) {
	h.RecordCollectorErrorDetail(name, nil)
}

func (h *HealthTracker) RecordCollectorErrorDetail(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.collectorErrorCount++
	if name != "" {
		h.collectorErrorsByName[name]++
		if err != nil {
			msg := err.Error()
			if len(msg) > 240 {
				msg = msg[:237] + "..."
			}
			h.collectorLastErrors[name] = CollectorErrorDetail{
				Message: msg,
				At:      time.Now().UTC(),
			}
		}
	}
}

func (h *HealthTracker) SetRollupLag(seconds int64) {
	if seconds < 0 {
		seconds = 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rollupLagS = seconds
}

func (h *HealthTracker) RecordDroppedLiveEvent() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.droppedLiveEvents++
}

func (h *HealthTracker) SetLiveSubscribers(n int) {
	if n < 0 {
		n = 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.activeLiveSubscribers = n
}

func (h *HealthTracker) RecordWallClockJump() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.wallClockJumpCount++
}

func (h *HealthTracker) SetClockError(reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clockError = reason
}

func (h *HealthTracker) Snapshot() HealthSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()

	status := StatusOK
	if !h.dbOK || h.clockError != "" {
		status = StatusDegraded
	}

	var collectPtr, writePtr *time.Time
	if !h.lastCollectAt.IsZero() {
		t := h.lastCollectAt
		collectPtr = &t
	}
	if !h.lastWriteAt.IsZero() {
		t := h.lastWriteAt
		writePtr = &t
	}

	var byName map[string]int64
	if len(h.collectorErrorsByName) > 0 {
		byName = maps.Clone(h.collectorErrorsByName)
	}
	var lastErrors map[string]CollectorErrorDetail
	if len(h.collectorLastErrors) > 0 {
		lastErrors = maps.Clone(h.collectorLastErrors)
	}

	return HealthSnapshot{
		Status:                status,
		DBOK:                  h.dbOK,
		LastDBError:           h.lastDBError,
		LastCollectAt:         collectPtr,
		LastWriteAt:           writePtr,
		RollupLagS:            h.rollupLagS,
		CollectorErrorCount:   h.collectorErrorCount,
		CollectorErrorsByName: byName,
		CollectorLastErrors:   lastErrors,
		DroppedLiveEvents:     h.droppedLiveEvents,
		ActiveLiveSubscribers: h.activeLiveSubscribers,
		WallClockJumpCount:    h.wallClockJumpCount,
		ClockError:            h.clockError,
	}
}
