CREATE TABLE admin_credentials (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    salt BLOB NOT NULL CHECK (length(salt) = 16),
    password_hash BLOB NOT NULL CHECK (length(password_hash) = 32)
) STRICT;

CREATE TABLE admin_sessions (
    token_hash BLOB PRIMARY KEY CHECK (length(token_hash) = 32),
    expires_at_ms INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

CREATE INDEX admin_sessions_expiry ON admin_sessions (expires_at_ms);
