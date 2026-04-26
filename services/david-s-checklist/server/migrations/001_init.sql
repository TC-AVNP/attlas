CREATE TABLE IF NOT EXISTS completions (
    todo_id    INTEGER PRIMARY KEY,
    completed_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    email      TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
