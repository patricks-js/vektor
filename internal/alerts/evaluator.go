package alerts

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultEvalInterval = 30 * time.Second
	metricLookback      = 5 * time.Minute
	notifyQueueSize     = 64
	notifyTimeout       = 15 * time.Second
)

type ruleState struct {
	pendingSince time.Time
	firing       bool
	triggeredAt  int64
}

type notifyJob struct {
	event    Event
	channels []Channel
}

type Evaluator struct {
	store    *Store
	notifier *Notifier
	logger   *slog.Logger
	interval time.Duration
	now      func() time.Time
	mu       sync.Mutex
	states   map[int64]*ruleState

	notifyCh     chan notifyJob
	workerActive atomic.Bool
}

func NewEvaluator(store *Store, notifier *Notifier, logger *slog.Logger) *Evaluator {
	return &Evaluator{
		store:    store,
		notifier: notifier,
		logger:   logger,
		interval: defaultEvalInterval,
		now:      time.Now,
		states:   make(map[int64]*ruleState),
		notifyCh: make(chan notifyJob, notifyQueueSize),
	}
}

func (e *Evaluator) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	e.workerActive.Store(true)
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.runNotifyWorker(ctx)
	}()
	defer func() {
		e.workerActive.Store(false)
		wg.Wait()
	}()

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := e.EvaluateOnce(ctx); err != nil && ctx.Err() == nil && e.logger != nil {
				e.logger.Warn("alert evaluation failed", slog.String("error", err.Error()))
			}
		}
	}
}

func (e *Evaluator) runNotifyWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-e.notifyCh:
			if e.notifier == nil {
				continue
			}
			sendCtx, cancel := context.WithTimeout(ctx, notifyTimeout)
			notifyErr := e.notifier.NotifyAll(sendCtx, job.channels, job.event)
			cancel()
			if notifyErr != "" && job.event.ID > 0 {
				updateCtx, uCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				if err := e.store.UpdateEventNotifyError(updateCtx, job.event.ID, notifyErr); err != nil && e.logger != nil {
					e.logger.Warn("update alert notify_error failed", slog.String("error", err.Error()))
				}
				uCancel()
			}
		}
	}
}

func (e *Evaluator) dispatchEvent(ctx context.Context, channels []Channel, ev Event) {
	if e.notifier == nil || len(channels) == 0 {
		if _, err := e.store.RecordEvent(ctx, ev); err != nil && e.logger != nil {
			e.logger.Warn("record alert event failed", slog.String("error", err.Error()))
		}
		return
	}

	if e.workerActive.Load() {
		recorded, err := e.store.RecordEvent(ctx, ev)
		if err != nil {
			if e.logger != nil {
				e.logger.Warn("record alert event failed", slog.String("error", err.Error()))
			}
			return
		}
		chCopy := make([]Channel, len(channels))
		copy(chCopy, channels)
		select {
		case e.notifyCh <- notifyJob{event: recorded, channels: chCopy}:
		default:
			if e.logger != nil {
				e.logger.Warn("alert webhook queue full; dropping notification", slog.Int64("event_id", recorded.ID))
			}
			_ = e.store.UpdateEventNotifyError(ctx, recorded.ID, "notification queue full")
		}
		return
	}

	// Synchronous path for direct EvaluateOnce calls in tests/CLI.
	ev.NotifyError = e.notifier.NotifyAll(ctx, channels, ev)
	if _, err := e.store.RecordEvent(ctx, ev); err != nil && e.logger != nil {
		e.logger.Warn("record alert event failed", slog.String("error", err.Error()))
	}
}

func breached(op string, value, threshold float64) bool {
	if op == "lt" {
		return value < threshold
	}
	return value > threshold
}

func (e *Evaluator) EvaluateOnce(ctx context.Context) error {
	rules, err := e.store.ListRules(ctx)
	if err != nil {
		return err
	}
	channels, err := e.store.ListChannels(ctx)
	if err != nil {
		return err
	}

	now := e.now()
	minBucket := now.Add(-metricLookback).UnixMilli()
	seen := make(map[int64]bool, len(rules))

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, rule := range rules {
		seen[rule.ID] = true
		if !rule.Enabled {
			delete(e.states, rule.ID)
			continue
		}
		val, ok, err := e.store.LatestMetric(ctx, rule.ResourceID, rule.MetricKey, minBucket)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}

		st := e.states[rule.ID]
		if st == nil {
			st = &ruleState{}
			e.states[rule.ID] = st
		}

		if breached(rule.Operator, val, rule.Threshold) {
			if st.pendingSince.IsZero() {
				st.pendingSince = now
			}
			if !st.firing && now.Sub(st.pendingSince) >= time.Duration(rule.DurationSeconds)*time.Second {
				st.firing = true
				st.triggeredAt = now.UnixMilli()
				ev := Event{
					RuleID:        rule.ID,
					RuleName:      rule.Name,
					ResourceID:    rule.ResourceID,
					MetricKey:     rule.MetricKey,
					Status:        "firing",
					Value:         val,
					Threshold:     rule.Threshold,
					TriggeredAtMs: st.triggeredAt,
				}
				e.dispatchEvent(ctx, channels, ev)
			}
		} else {
			st.pendingSince = time.Time{}
			if st.firing {
				st.firing = false
				resolvedAt := now.UnixMilli()
				ev := Event{
					RuleID:        rule.ID,
					RuleName:      rule.Name,
					ResourceID:    rule.ResourceID,
					MetricKey:     rule.MetricKey,
					Status:        "resolved",
					Value:         val,
					Threshold:     rule.Threshold,
					TriggeredAtMs: st.triggeredAt,
					ResolvedAtMs:  &resolvedAt,
				}
				e.dispatchEvent(ctx, channels, ev)
			}
		}
	}

	for id := range e.states {
		if !seen[id] {
			delete(e.states, id)
		}
	}
	return nil
}
