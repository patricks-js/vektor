CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('ok', 'error')),
    detail TEXT NOT NULL DEFAULT '',
    created_at_ms INTEGER NOT NULL
) STRICT;

CREATE INDEX audit_log_created ON audit_log (created_at_ms DESC);
