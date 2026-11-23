// Package alerts stores alert rules, notification channels, and event history,
// evaluates active rules against recent SQLite metrics, and delivers webhooks.
package alerts

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/url"
	"strings"
	"time"

	"vektor/internal/database"
	"vektor/internal/metrics"
)

const (
	MaxRules    = 100
	MaxChannels = 20
	MaxEvents   = 200
)

var (
	ErrInvalid  = errors.New("invalid alert configuration")
	ErrNotFound = errors.New("alert item not found")
	ErrLimit    = errors.New("alert limit reached")
)

type Rule struct {
	ID              int64   `json:"id"`
	Name            string  `json:"name"`
	ResourceID      string  `json:"resource_id"`
	MetricKey       string  `json:"metric_key"`
	Operator        string  `json:"operator"`
	Threshold       float64 `json:"threshold"`
	DurationSeconds int     `json:"duration_seconds"`
	Enabled         bool    `json:"enabled"`
	CreatedAtMs     int64   `json:"created_at_ms"`
	UpdatedAtMs     int64   `json:"updated_at_ms"`
}

type RuleInput struct {
	Name            string  `json:"name"`
	ResourceID      string  `json:"resource_id"`
	MetricKey       string  `json:"metric_key"`
	Operator        string  `json:"operator"`
	Threshold       float64 `json:"threshold"`
	DurationSeconds int     `json:"duration_seconds"`
	Enabled         bool    `json:"enabled"`
}

type Channel struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	URL         string `json:"url"`
	Enabled     bool   `json:"enabled"`
	CreatedAtMs int64  `json:"created_at_ms"`
}

type ChannelInput struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

type Event struct {
	ID            int64   `json:"id"`
	RuleID        int64   `json:"rule_id"`
	RuleName      string  `json:"rule_name"`
	ResourceID    string  `json:"resource_id"`
	MetricKey     string  `json:"metric_key"`
	Status        string  `json:"status"`
	Value         float64 `json:"value"`
	Threshold     float64 `json:"threshold"`
	TriggeredAtMs int64   `json:"triggered_at_ms"`
	ResolvedAtMs  *int64  `json:"resolved_at_ms,omitempty"`
	NotifyError   string  `json:"notify_error,omitempty"`
}

type Store struct {
	db  *database.DB
	now func() time.Time
}

func NewStore(db *database.DB) *Store {
	return &Store{db: db, now: time.Now}
}

func ValidateRuleInput(in RuleInput) error {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 120 {
		return ErrInvalid
	}
	if len(in.ResourceID) == 0 || len(in.ResourceID) > 128 {
		return ErrInvalid
	}
	for _, ch := range in.ResourceID {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == ':' || ch == '_' || ch == '-') {
			return ErrInvalid
		}
	}
	if _, ok := metrics.LookupMetric(in.MetricKey); !ok {
		return ErrInvalid
	}
	if in.Operator != "gt" && in.Operator != "lt" {
		return ErrInvalid
	}
	if math.IsNaN(in.Threshold) || math.IsInf(in.Threshold, 0) {
		return ErrInvalid
	}
	if in.DurationSeconds < 0 || in.DurationSeconds > 86400 {
		return ErrInvalid
	}
	return nil
}

func ValidateWebhookURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return ErrInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrInvalid
	}
	return nil
}

func ValidateChannelInput(in ChannelInput) error {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 120 {
		return ErrInvalid
	}
	if in.Kind != "" && in.Kind != "webhook" {
		return ErrInvalid
	}
	return ValidateWebhookURL(in.URL)
}

func (s *Store) ListRules(ctx context.Context) ([]Rule, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, name, resource_id, metric_key, operator, threshold, duration_seconds, enabled, created_at_ms, updated_at_ms
		FROM alert_rules
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []Rule{}
	for rows.Next() {
		var r Rule
		var enabled int
		if err := rows.Scan(&r.ID, &r.Name, &r.ResourceID, &r.MetricKey, &r.Operator, &r.Threshold, &r.DurationSeconds, &enabled, &r.CreatedAtMs, &r.UpdatedAtMs); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (s *Store) CreateRule(ctx context.Context, in RuleInput) (Rule, error) {
	if err := ValidateRuleInput(in); err != nil {
		return Rule{}, err
	}
	var count int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_rules`).Scan(&count); err != nil {
		return Rule{}, err
	}
	if count >= MaxRules {
		return Rule{}, ErrLimit
	}
	now := s.now().UnixMilli()
	name := strings.TrimSpace(in.Name)
	enabled := 0
	if in.Enabled {
		enabled = 1
	}
	res, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO alert_rules (name, resource_id, metric_key, operator, threshold, duration_seconds, enabled, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, name, in.ResourceID, in.MetricKey, in.Operator, in.Threshold, in.DurationSeconds, enabled, now, now)
	if err != nil {
		return Rule{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Rule{}, err
	}
	return Rule{
		ID: id, Name: name, ResourceID: in.ResourceID, MetricKey: in.MetricKey,
		Operator: in.Operator, Threshold: in.Threshold, DurationSeconds: in.DurationSeconds,
		Enabled: in.Enabled, CreatedAtMs: now, UpdatedAtMs: now,
	}, nil
}

func (s *Store) UpdateRule(ctx context.Context, id int64, in RuleInput) (Rule, error) {
	if id <= 0 {
		return Rule{}, ErrInvalid
	}
	if err := ValidateRuleInput(in); err != nil {
		return Rule{}, err
	}
	now := s.now().UnixMilli()
	name := strings.TrimSpace(in.Name)
	enabled := 0
	if in.Enabled {
		enabled = 1
	}
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE alert_rules
		SET name = ?, resource_id = ?, metric_key = ?, operator = ?, threshold = ?, duration_seconds = ?, enabled = ?, updated_at_ms = ?
		WHERE id = ?
	`, name, in.ResourceID, in.MetricKey, in.Operator, in.Threshold, in.DurationSeconds, enabled, now, id)
	if err != nil {
		return Rule{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Rule{}, err
	}
	if affected == 0 {
		return Rule{}, ErrNotFound
	}
	var created int64
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT created_at_ms FROM alert_rules WHERE id = ?`, id).Scan(&created); err != nil {
		return Rule{}, err
	}
	return Rule{
		ID: id, Name: name, ResourceID: in.ResourceID, MetricKey: in.MetricKey,
		Operator: in.Operator, Threshold: in.Threshold, DurationSeconds: in.DurationSeconds,
		Enabled: in.Enabled, CreatedAtMs: created, UpdatedAtMs: now,
	}, nil
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	res, err := s.db.Writer().ExecContext(ctx, `DELETE FROM alert_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, name, kind, url, enabled, created_at_ms
		FROM alert_channels
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	channels := []Channel{}
	for rows.Next() {
		var c Channel
		var enabled int
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.URL, &enabled, &c.CreatedAtMs); err != nil {
			return nil, err
		}
		c.Enabled = enabled != 0
		channels = append(channels, c)
	}
	return channels, rows.Err()
}

func (s *Store) CreateChannel(ctx context.Context, in ChannelInput) (Channel, error) {
	if err := ValidateChannelInput(in); err != nil {
		return Channel{}, err
	}
	var count int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_channels`).Scan(&count); err != nil {
		return Channel{}, err
	}
	if count >= MaxChannels {
		return Channel{}, ErrLimit
	}
	now := s.now().UnixMilli()
	name := strings.TrimSpace(in.Name)
	rawURL := strings.TrimSpace(in.URL)
	kind := "webhook"
	enabled := 0
	if in.Enabled {
		enabled = 1
	}
	res, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO alert_channels (name, kind, url, enabled, created_at_ms)
		VALUES (?, ?, ?, ?, ?)
	`, name, kind, rawURL, enabled, now)
	if err != nil {
		return Channel{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Channel{}, err
	}
	return Channel{ID: id, Name: name, Kind: kind, URL: rawURL, Enabled: in.Enabled, CreatedAtMs: now}, nil
}

func (s *Store) DeleteChannel(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	res, err := s.db.Writer().ExecContext(ctx, `DELETE FROM alert_channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RecordEvent(ctx context.Context, ev Event) (Event, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO alert_events (rule_id, rule_name, resource_id, metric_key, status, value, threshold, triggered_at_ms, resolved_at_ms, notify_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, ev.RuleID, ev.RuleName, ev.ResourceID, ev.MetricKey, ev.Status, ev.Value, ev.Threshold, ev.TriggeredAtMs, ev.ResolvedAtMs, ev.NotifyError)
	if err != nil {
		return Event{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Event{}, err
	}
	ev.ID = id
	// Keep event history bounded so SQLite stays lightweight.
	_, _ = s.db.Writer().ExecContext(ctx, `
		DELETE FROM alert_events
		WHERE id NOT IN (SELECT id FROM alert_events ORDER BY triggered_at_ms DESC, id DESC LIMIT ?)
	`, MaxEvents)
	return ev, nil
}

func (s *Store) ListEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > MaxEvents {
		limit = 50
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, rule_id, rule_name, resource_id, metric_key, status, value, threshold, triggered_at_ms, resolved_at_ms, notify_error
		FROM alert_events
		ORDER BY triggered_at_ms DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var ev Event
		var resolved sql.NullInt64
		if err := rows.Scan(&ev.ID, &ev.RuleID, &ev.RuleName, &ev.ResourceID, &ev.MetricKey, &ev.Status, &ev.Value, &ev.Threshold, &ev.TriggeredAtMs, &resolved, &ev.NotifyError); err != nil {
			return nil, err
		}
		if resolved.Valid {
			v := resolved.Int64
			ev.ResolvedAtMs = &v
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// LatestMetric returns the most recent persisted 30s raw bucket value within maxAge.
func (s *Store) LatestMetric(ctx context.Context, resourceID, metricKey string, minBucketMs int64) (float64, bool, error) {
	var val float64
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT value_last
		FROM metric_buckets
		WHERE resource_id = ? AND metric_key = ? AND resolution_s = 30 AND bucket_start_ms >= ?
		ORDER BY bucket_start_ms DESC
		LIMIT 1
	`, resourceID, metricKey, minBucketMs).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return val, true, nil
}
