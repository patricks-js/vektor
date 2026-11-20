CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at_ms INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS resources (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  external_key TEXT NOT NULL,
  display_name TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
  first_seen_ms INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  UNIQUE(kind, external_key)
) STRICT;

CREATE TABLE IF NOT EXISTS metric_buckets (
  resource_id TEXT NOT NULL REFERENCES resources(id),
  metric_key TEXT NOT NULL,
  resolution_s INTEGER NOT NULL CHECK (resolution_s IN (30, 300, 3600)),
  bucket_start_ms INTEGER NOT NULL,
  sample_count INTEGER NOT NULL CHECK (sample_count > 0),
  value_sum REAL NOT NULL,
  value_min REAL NOT NULL,
  value_max REAL NOT NULL,
  value_last REAL NOT NULL,
  last_sample_ms INTEGER NOT NULL,
  PRIMARY KEY (resource_id, metric_key, resolution_s, bucket_start_ms)
) STRICT, WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS idx_metric_retention
  ON metric_buckets(resolution_s, bucket_start_ms);

CREATE TABLE IF NOT EXISTS rollup_state (
  resolution_s INTEGER PRIMARY KEY,
  closed_through_ms INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS system_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;
