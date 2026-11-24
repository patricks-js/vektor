// Package audit records administrative host management actions in SQLite.
package audit

import (
	"context"
	"time"

	"vektor/internal/database"
)

const MaxEntries = 500

type Entry struct {
	ID          int64  `json:"id"`
	Actor       string `json:"actor"`
	Action      string `json:"action"`
	Target      string `json:"target"`
	Status      string `json:"status"`
	Detail      string `json:"detail,omitempty"`
	CreatedAtMs int64  `json:"created_at_ms"`
}

type Store struct {
	db  *database.DB
	now func() time.Time
}

func NewStore(db *database.DB) *Store {
	return &Store{db: db, now: time.Now}
}

func (s *Store) Record(ctx context.Context, actor, action, target, status, detail string) (Entry, error) {
	now := s.now().UnixMilli()
	if status != "ok" {
		status = "error"
	}
	res, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO audit_log (actor, action, target, status, detail, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?)
	`, actor, action, target, status, detail, now)
	if err != nil {
		return Entry{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Entry{}, err
	}
	_, _ = s.db.Writer().ExecContext(ctx, `
		DELETE FROM audit_log
		WHERE id NOT IN (SELECT id FROM audit_log ORDER BY created_at_ms DESC, id DESC LIMIT ?)
	`, MaxEntries)
	return Entry{
		ID: id, Actor: actor, Action: action, Target: target,
		Status: status, Detail: detail, CreatedAtMs: now,
	}, nil
}

func (s *Store) List(ctx context.Context, limit int) ([]Entry, error) {
	if limit <= 0 || limit > MaxEntries {
		limit = 50
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, actor, action, target, status, detail, created_at_ms
		FROM audit_log
		ORDER BY created_at_ms DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &e.Status, &e.Detail, &e.CreatedAtMs); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
