package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

const (
	heartbeatInterval = 15 * time.Second
	streamWriteLimit  = 3 * time.Second
	maxSnapshotPoints = 8192
)

type livePoint struct {
	ResourceID  string  `json:"resource_id"`
	Metric      string  `json:"metric"`
	TimestampMs int64   `json:"timestamp_ms"`
	Value       float64 `json:"value"`
}

type liveEvent struct {
	CollectedAt  time.Time   `json:"collected_at"`
	Points       []livePoint `json:"points"`
	WarningCount int         `json:"warning_count"`
	Truncated    bool        `json:"truncated"`
}

func (a *API) live(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	id := r.Context().Value(identityKey{}).(identity)
	ctx := r.Context()
	controller := http.NewResponseController(w)
	send := func(frame string) bool {
		deadline := time.Now().Add(streamWriteLimit)
		if id.session.ExpiresAt.Before(deadline) {
			deadline = id.session.ExpiresAt
		}
		if controller.SetWriteDeadline(deadline) != nil {
			return false
		}
		if _, err := io.WriteString(w, frame); err != nil {
			return false
		}
		return controller.Flush() == nil && controller.SetWriteDeadline(time.Time{}) == nil
	}

	_, snapshots, unsubscribe := a.hub.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	if !send(": connected\n\n") {
		return
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	expiry := time.NewTimer(time.Until(id.session.ExpiresAt))
	defer expiry.Stop()

	checkSession := func() bool {
		// Capture the notification before reading in case a logout races this read.
		id.revocations = a.auth.Revocations()
		checkCtx, cancel := context.WithTimeout(ctx, streamWriteLimit)
		defer cancel()
		_, err := a.auth.Authenticate(checkCtx, id.token)
		return err == nil
	}
	for {
		// Prefer cancellation and revocation to already buffered snapshots.
		select {
		case <-ctx.Done():
			return
		case <-id.revocations:
			if !checkSession() {
				return
			}
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-expiry.C:
			return
		case <-id.revocations:
			if !checkSession() {
				return
			}
		case <-heartbeat.C:
			if !checkSession() || !send(": heartbeat\n\n") {
				return
			}
		case snapshot, ok := <-snapshots:
			if !ok {
				return
			}
			event := liveEvent{
				CollectedAt:  snapshot.CollectedAt,
				Points:       make([]livePoint, 0, min(len(snapshot.Points), maxSnapshotPoints)),
				WarningCount: len(snapshot.Warnings),
			}
			for _, p := range snapshot.Points {
				if !p.Valid {
					continue
				}
				if len(event.Points) == maxSnapshotPoints {
					event.Truncated = true
					break
				}
				event.Points = append(event.Points, livePoint{p.ResourceID, p.Key, p.TimestampMs, p.Value})
			}
			payload, err := json.Marshal(event)
			if err != nil || !send("event: snapshot\ndata: "+string(payload)+"\n\n") {
				return
			}
		}
	}
}
