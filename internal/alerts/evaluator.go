package alerts

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	defaultEvalInterval = 30 * time.Second
	metricLookback      = 5 * time.Minute
)

type ruleState struct {
	pendingSince time.Time
	firing       bool
	triggeredAt  int64
}

type Evaluator struct {
	store    *Store
	notifier *Notifier
	logger   *slog.Logger
	interval time.Duration
	now      func() time.Time
	mu       sync.Mutex
	states   map[int64]*ruleState
}

func NewEvaluator(store *Store, notifier *Notifier, logger *slog.Logger) *Evaluator {
	return &Evaluator{
		store:    store,
		notifier: notifier,
		logger:   logger,
		interval: defaultEvalInterval,
		now:      time.Now,
		states:   make(map[int64]*ruleState),
	}
}

func (e *Evaluator) Run(ctx context.Context) error {
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
				if e.notifier != nil {
					ev.NotifyError = e.notifier.NotifyAll(ctx, channels, ev)
				}
				if _, err := e.store.RecordEvent(ctx, ev); err != nil && e.logger != nil {
					e.logger.Warn("record firing alert failed", slog.String("error", err.Error()))
				}
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
				if e.notifier != nil {
					ev.NotifyError = e.notifier.NotifyAll(ctx, channels, ev)
				}
				if _, err := e.store.RecordEvent(ctx, ev); err != nil && e.logger != nil {
					e.logger.Warn("record resolved alert failed", slog.String("error", err.Error()))
				}
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
