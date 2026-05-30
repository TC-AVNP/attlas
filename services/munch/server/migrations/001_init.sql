-- Munch: meal-prep companion — dishes, ingredients, shopping, ratings.

CREATE TABLE IF NOT EXISTS dishes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    servings   INTEGER NOT NULL DEFAULT 2,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS ingredients (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    dish_id INTEGER NOT NULL REFERENCES dishes(id) ON DELETE CASCADE,
    name    TEXT NOT NULL,
    qty     TEXT NOT NULL DEFAULT '',
    unit    TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS shopping_sessions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    dish_id      INTEGER NOT NULL REFERENCES dishes(id) ON DELETE CASCADE,
    created_by   TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT
);

CREATE TABLE IF NOT EXISTS shopping_checks (
    session_id    INTEGER NOT NULL REFERENCES shopping_sessions(id) ON DELETE CASCADE,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients(id) ON DELETE CASCADE,
    checked_at    TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (session_id, ingredient_id)
);

CREATE TABLE IF NOT EXISTS ratings (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    dish_id    INTEGER NOT NULL REFERENCES dishes(id) ON DELETE CASCADE,
    rater      TEXT NOT NULL,
    score      INTEGER NOT NULL CHECK(score >= 0 AND score <= 10),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
