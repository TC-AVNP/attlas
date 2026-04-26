CREATE TABLE IF NOT EXISTS users (
    email      TEXT PRIMARY KEY,
    is_admin   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
