-- Control: centralized access control for attlas services.

CREATE TABLE IF NOT EXISTS services (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS users (
    email      TEXT PRIMARY KEY,
    is_admin   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS grants (
    email      TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
    service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (email, service_id)
);
