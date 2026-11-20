package scheduler

import (
	"sync"
	"time"

	"vektor/internal/metrics"
	"vektor/internal/observability"
)

const defaultSubscriberBuffer = 4

// LiveSnapshot is published to active live subscribers at LiveInterval without persistence.
type LiveSnapshot struct {
	CollectedAt time.Time             `json:"collected_at"`
	Points      []metrics.MetricPoint `json:"points"`
	Warnings    []string              `json:"warnings,omitempty"`
}

type liveSubscriber struct {
	id     uint64
	ch     chan LiveSnapshot
	closed bool
}

// LiveHub fans out ephemeral live snapshots to subscribers with bounded non-blocking queues.
type LiveHub struct {
	mu      sync.RWMutex
	nextID  uint64
	subs    map[uint64]*liveSubscriber
	health  *observability.HealthTracker
	onFirst func()
	onLast  func()
}

// NewLiveHub creates a LiveHub wired to health telemetry and optional lifecycle callbacks.
func NewLiveHub(health *observability.HealthTracker, onFirst, onLast func()) *LiveHub {
	return &LiveHub{
		subs:    make(map[uint64]*liveSubscriber),
		health:  health,
		onFirst: onFirst,
		onLast:  onLast,
	}
}

// SetHooks sets callbacks invoked on 0->1 and 1->0 subscriber transitions.
func (h *LiveHub) SetHooks(onFirst, onLast func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onFirst = onFirst
	h.onLast = onLast
}

// Subscribe registers a bounded subscriber channel and returns an idempotent unsubscribe closure.
func (h *LiveHub) Subscribe() (uint64, <-chan LiveSnapshot, func()) {
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	sub := &liveSubscriber{
		id: id,
		ch: make(chan LiveSnapshot, defaultSubscriberBuffer),
	}
	wasEmpty := len(h.subs) == 0
	h.subs[id] = sub
	count := len(h.subs)
	onFirst := h.onFirst
	h.mu.Unlock()

	if h.health != nil {
		h.health.SetLiveSubscribers(count)
	}
	if wasEmpty && onFirst != nil {
		onFirst()
	}

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			h.removeSubscriber(id)
		})
	}
	return id, sub.ch, unsub
}

func (h *LiveHub) removeSubscriber(id uint64) {
	h.mu.Lock()
	sub, ok := h.subs[id]
	if ok {
		delete(h.subs, id)
		sub.closed = true
		close(sub.ch)
	}
	count := len(h.subs)
	nowEmpty := ok && count == 0
	onLast := h.onLast
	h.mu.Unlock()

	if h.health != nil {
		h.health.SetLiveSubscribers(count)
	}
	if nowEmpty && onLast != nil {
		onLast()
	}
}

// Publish sends snap to all subscribers non-blockingly, dropping for slow consumers.
func (h *LiveHub) Publish(snap LiveSnapshot) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, sub := range h.subs {
		if sub.closed {
			continue
		}
		select {
		case sub.ch <- snap:
		default:
			if h.health != nil {
				h.health.RecordDroppedLiveEvent()
			}
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (h *LiveHub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
