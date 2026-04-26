CREATE TABLE IF NOT EXISTS handovers (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    assignee    TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Add assignee and handover_id to todos.
-- Using a trick: SQLite ignores ADD COLUMN if column already exists
-- when wrapped in a try-catch (we handle the error in Go).
