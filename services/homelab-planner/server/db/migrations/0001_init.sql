-- Steps: independent weekend-sized milestones
CREATE TABLE steps (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    title       TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL,
    completed_at INTEGER
);

-- Checklist items: things to buy/do per step
CREATE TABLE checklist_items (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    step_id            INTEGER NOT NULL REFERENCES steps(id) ON DELETE CASCADE,
    name               TEXT    NOT NULL,
    budget_cents       INTEGER,
    actual_cost_cents  INTEGER,
    status             TEXT    NOT NULL DEFAULT 'researching'
                       CHECK(status IN ('researching', 'ordered', 'arrived')),
    selected_option_id INTEGER,
    created_at         INTEGER NOT NULL
);

-- Options to compare for each checklist item
CREATE TABLE item_options (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id    INTEGER NOT NULL REFERENCES checklist_items(id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    url        TEXT    NOT NULL DEFAULT '',
    price_cents INTEGER,
    notes      TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

-- Build log entries per step
CREATE TABLE build_log_entries (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    step_id    INTEGER NOT NULL REFERENCES steps(id) ON DELETE CASCADE,
    body       TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);

