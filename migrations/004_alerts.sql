CREATE TABLE alert_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    metric_key TEXT NOT NULL,
    operator TEXT NOT NULL CHECK (operator IN ('gt', 'lt')),
    threshold REAL NOT NULL,
    duration_seconds INTEGER NOT NULL CHECK (duration_seconds >= 0 AND duration_seconds <= 86400),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    created_at_ms INTEGER NOT NULL,
    updated_at_ms INTEGER NOT NULL
) STRICT;

CREATE TABLE alert_channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('webhook')),
    url TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    created_at_ms INTEGER NOT NULL
) STRICT;

CREATE TABLE alert_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL,
    rule_name TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    metric_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('firing', 'resolved')),
    value REAL NOT NULL,
    threshold REAL NOT NULL,
    triggered_at_ms INTEGER NOT NULL,
    resolved_at_ms INTEGER,
    notify_error TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX alert_events_triggered ON alert_events (triggered_at_ms DESC);
CREATE INDEX alert_events_rule ON alert_events (rule_id, triggered_at_ms DESC);
