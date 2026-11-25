package host

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vektor/internal/database"
	"vektor/internal/metrics"
)

const (
	KindHost          = "host"
	KindCPUCore       = "cpu_core"
	KindDisk          = "disk"
	KindFilesystem    = "filesystem"
	KindNIC           = "nic"
	KindSensor        = "sensor"
	KindIncusInstance = "incus_instance"

	HostResourceID  = metrics.HostResourceID
	HostExternalKey = "local"

	maxMetadataJSONBytes = 64 * 1024
)

const resourceColumns = "id, kind, external_key, display_name, metadata_json, active, first_seen_ms, last_seen_ms"

// Resource models a persistent monitored entity stored in SQLite `resources`.
type Resource struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	ExternalKey  string `json:"external_key"`
	DisplayName  string `json:"display_name"`
	MetadataJSON string `json:"metadata_json"`
	Active       bool   `json:"active"`
	FirstSeenMs  int64  `json:"first_seen_ms"`
	LastSeenMs   int64  `json:"last_seen_ms"`
}

// DiscoveredResource is the input unit submitted to Registry.Reconcile.
type DiscoveredResource struct {
	ExternalKey  string
	DisplayName  string
	MetadataJSON string
}

// Registry reconciles discovered Linux resources against persistent SQLite identities.
type Registry struct {
	db *database.DB
}

// NewRegistry creates a Registry backed by db.
func NewRegistry(db *database.DB) *Registry {
	return &Registry{db: db}
}

func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// EnsureHost registers or updates the local host resource and boot_id metadata.
func (r *Registry) EnsureHost(ctx context.Context, inv Inventory, nowMs int64) error {
	meta, err := FormatMetadata(inv, maxMetadataJSONBytes)
	if err != nil {
		return fmt.Errorf("encode host inventory: %w", err)
	}
	display := inv.Hostname
	if strings.TrimSpace(display) == "" {
		display = "localhost"
	}

	tx, err := r.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO resources (id, kind, external_key, display_name, metadata_json, active, first_seen_ms, last_seen_ms)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(kind, external_key) DO UPDATE SET
			display_name = excluded.display_name,
			metadata_json = excluded.metadata_json,
			active = 1,
			last_seen_ms = excluded.last_seen_ms
	`, HostResourceID, KindHost, HostExternalKey, display, meta, nowMs, nowMs); err != nil {
		return fmt.Errorf("upsert host resource: %w", err)
	}

	if inv.BootID != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO system_meta(key, value) VALUES ('boot_id', ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value
		`, inv.BootID); err != nil {
			return fmt.Errorf("upsert boot_id: %w", err)
		}
	}

	return tx.Commit()
}

// EnsureCPUCores reconciles the logical CPU IDs reported by Linux.
func (r *Registry) EnsureCPUCores(ctx context.Context, cores []string, nowMs int64) ([]Resource, error) {
	items := make([]DiscoveredResource, 0, len(cores))
	for _, core := range cores {
		ext := "cpu" + core
		items = append(items, DiscoveredResource{
			ExternalKey:  ext,
			DisplayName:  ext,
			MetadataJSON: fmt.Sprintf(`{"core_index":%s}`, core),
		})
	}
	return r.Reconcile(ctx, KindCPUCore, items, nowMs)
}

// EnsureIncusInstances upserts Incus instances with deterministic resource IDs ("incus:<name>")
// and marks absent instances inactive without deleting historical metrics.
func (r *Registry) EnsureIncusInstances(ctx context.Context, items []DiscoveredResource, nowMs int64) error {
	tx, err := r.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	seen := make(map[string]struct{}, len(items))
	for _, it := range items {
		ext := strings.TrimSpace(it.ExternalKey)
		if ext == "" {
			continue
		}
		if _, dup := seen[ext]; dup {
			continue
		}
		seen[ext] = struct{}{}
		id := "incus:" + ext
		display := strings.TrimSpace(it.DisplayName)
		if display == "" {
			display = ext
		}
		meta := strings.TrimSpace(it.MetadataJSON)
		if meta == "" || !json.Valid([]byte(meta)) {
			meta = "{}"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO resources (id, kind, external_key, display_name, metadata_json, active, first_seen_ms, last_seen_ms)
			VALUES (?, ?, ?, ?, ?, 1, ?, ?)
			ON CONFLICT(kind, external_key) DO UPDATE SET
				display_name = excluded.display_name,
				metadata_json = excluded.metadata_json,
				active = 1,
				last_seen_ms = excluded.last_seen_ms
		`, id, KindIncusInstance, ext, display, meta, nowMs, nowMs); err != nil {
			return fmt.Errorf("upsert incus instance %s: %w", ext, err)
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT external_key FROM resources WHERE kind = ? AND active = 1`, KindIncusInstance)
	if err != nil {
		return err
	}
	var toDeactivate []string
	for rows.Next() {
		var ext string
		if err := rows.Scan(&ext); err != nil {
			rows.Close()
			return err
		}
		if _, ok := seen[ext]; !ok {
			toDeactivate = append(toDeactivate, ext)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, ext := range toDeactivate {
		if _, err := tx.ExecContext(ctx, `UPDATE resources SET active = 0 WHERE kind = ? AND external_key = ?`, KindIncusInstance, ext); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Reconcile matches discovered items of kind against SQLite by (kind, external_key),
// preserving IDs and history of absent devices by setting active=0 instead of deleting rows.
func (r *Registry) Reconcile(ctx context.Context, kind string, items []DiscoveredResource, nowMs int64) ([]Resource, error) {
	if strings.TrimSpace(kind) == "" {
		return nil, errors.New("resource kind must not be empty")
	}

	seenKeys := make(map[string]struct{}, len(items))
	cleanItems := make([]DiscoveredResource, 0, len(items))
	for _, it := range items {
		ext := strings.TrimSpace(it.ExternalKey)
		if ext == "" {
			return nil, fmt.Errorf("resource of kind %q has empty external_key", kind)
		}
		if _, dup := seenKeys[ext]; dup {
			continue
		}
		seenKeys[ext] = struct{}{}

		meta := strings.TrimSpace(it.MetadataJSON)
		if meta == "" {
			meta = "{}"
		}
		if len(meta) > maxMetadataJSONBytes {
			return nil, fmt.Errorf("metadata for %s/%s exceeds %d bytes", kind, ext, maxMetadataJSONBytes)
		}
		if !json.Valid([]byte(meta)) {
			return nil, fmt.Errorf("invalid metadata_json for %s/%s", kind, ext)
		}
		display := strings.TrimSpace(it.DisplayName)
		if display == "" {
			display = ext
		}
		cleanItems = append(cleanItems, DiscoveredResource{
			ExternalKey:  ext,
			DisplayName:  display,
			MetadataJSON: meta,
		})
	}

	tx, err := r.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	existingByExt := make(map[string]Resource)
	rows, err := tx.QueryContext(ctx, `
		SELECT id, kind, external_key, display_name, metadata_json, active, first_seen_ms, last_seen_ms
		FROM resources
		WHERE kind = ?
	`, kind)
	if err != nil {
		return nil, fmt.Errorf("query existing resources of kind %q: %w", kind, err)
	}
	for rows.Next() {
		var res Resource
		var activeInt int
		if err := rows.Scan(&res.ID, &res.Kind, &res.ExternalKey, &res.DisplayName, &res.MetadataJSON, &activeInt, &res.FirstSeenMs, &res.LastSeenMs); err != nil {
			_ = rows.Close()
			return nil, err
		}
		res.Active = activeInt == 1
		existingByExt[res.ExternalKey] = res
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	activeOut := make([]Resource, 0, len(cleanItems))
	for _, item := range cleanItems {
		if ex, ok := existingByExt[item.ExternalKey]; ok {
			if _, err := tx.ExecContext(ctx, `
				UPDATE resources
				SET display_name = ?, metadata_json = ?, active = 1, last_seen_ms = ?
				WHERE id = ?
			`, item.DisplayName, item.MetadataJSON, nowMs, ex.ID); err != nil {
				return nil, fmt.Errorf("reactivate resource %s: %w", ex.ID, err)
			}
			ex.DisplayName = item.DisplayName
			ex.MetadataJSON = item.MetadataJSON
			ex.Active = true
			ex.LastSeenMs = nowMs
			activeOut = append(activeOut, ex)
		} else {
			newID := buildResourceID(kind, item.ExternalKey)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO resources (id, kind, external_key, display_name, metadata_json, active, first_seen_ms, last_seen_ms)
				VALUES (?, ?, ?, ?, ?, 1, ?, ?)
			`, newID, kind, item.ExternalKey, item.DisplayName, item.MetadataJSON, nowMs, nowMs); err != nil {
				return nil, fmt.Errorf("insert resource %s (%s): %w", newID, item.ExternalKey, err)
			}
			activeOut = append(activeOut, Resource{
				ID:           newID,
				Kind:         kind,
				ExternalKey:  item.ExternalKey,
				DisplayName:  item.DisplayName,
				MetadataJSON: item.MetadataJSON,
				Active:       true,
				FirstSeenMs:  nowMs,
				LastSeenMs:   nowMs,
			})
		}
	}

	for ext, ex := range existingByExt {
		if _, stillPresent := seenKeys[ext]; stillPresent || !ex.Active {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE resources SET active = 0 WHERE id = ?`, ex.ID); err != nil {
			return nil, fmt.Errorf("deactivate resource %s: %w", ex.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return activeOut, nil
}

// List returns resources filtered optionally by kind and active status.
func (r *Registry) List(ctx context.Context, kind string, includeInactive bool) ([]Resource, error) {
	query := `SELECT ` + resourceColumns + ` FROM resources WHERE 1=1`
	var args []any
	if strings.TrimSpace(kind) != "" {
		query += ` AND kind = ?`
		args = append(args, strings.TrimSpace(kind))
	}
	if !includeInactive {
		query += ` AND active = 1`
	}
	query += ` ORDER BY kind ASC, display_name ASC, id ASC`
	return r.list(ctx, query, args...)
}

// ListPage uses the immutable resource ID as a cursor, including across renames.
func (r *Registry) ListPage(ctx context.Context, kind string, active *bool, after string, limit int) ([]Resource, error) {
	query := `SELECT ` + resourceColumns + ` FROM resources WHERE id > ?`
	args := []any{after}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	if active != nil {
		query += ` AND active = ?`
		args = append(args, *active)
	}
	query += ` ORDER BY id ASC LIMIT ?`
	return r.list(ctx, query, append(args, limit)...)
}

func (r *Registry) list(ctx context.Context, query string, args ...any) ([]Resource, error) {
	rows, err := r.db.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Resource
	for rows.Next() {
		res, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

// Get fetches a resource by persistent ID regardless of active state.
func (r *Registry) Get(ctx context.Context, id string) (Resource, error) {
	return scanResource(r.db.Reader().QueryRowContext(ctx, `SELECT `+resourceColumns+` FROM resources WHERE id = ?`, id))
}

func scanResource(row interface{ Scan(...any) error }) (Resource, error) {
	var res Resource
	var active int
	err := row.Scan(&res.ID, &res.Kind, &res.ExternalKey, &res.DisplayName, &res.MetadataJSON, &active, &res.FirstSeenMs, &res.LastSeenMs)
	res.Active = active == 1
	return res, err
}

// GetHost reads the host resource and decodes its cached Inventory metadata.
func (r *Registry) GetHost(ctx context.Context) (Resource, Inventory, error) {
	res, err := r.Get(ctx, HostResourceID)
	if err != nil {
		return Resource{}, Inventory{}, err
	}
	var inv Inventory
	if res.MetadataJSON != "" && res.MetadataJSON != "{}" {
		if err := json.Unmarshal([]byte(res.MetadataJSON), &inv); err != nil {
			return res, Inventory{}, fmt.Errorf("decode host inventory metadata: %w", err)
		}
	}
	return res, inv, nil
}

// GetMeta retrieves a key from system_meta.
func (r *Registry) GetMeta(ctx context.Context, key string) (string, bool, error) {
	var val string
	err := r.db.Reader().QueryRowContext(ctx, `SELECT value FROM system_meta WHERE key = ?`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// SetMeta inserts or updates a key in system_meta.
func (r *Registry) SetMeta(ctx context.Context, key, value string) error {
	_, err := r.db.Writer().ExecContext(ctx, `
		INSERT INTO system_meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

func buildResourceID(kind, externalKey string) string {
	switch kind {
	case KindHost:
		return HostResourceID
	case KindCPUCore:
		if suffix, ok := strings.CutPrefix(externalKey, "cpu"); ok && suffix != "" {
			return "cpu:" + suffix
		}
	}
	sum := sha256.Sum256([]byte(kind + "\x00" + externalKey))
	return kind + ":" + hex.EncodeToString(sum[:8])
}
