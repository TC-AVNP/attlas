-- 0001_init: initial schema for petboard.
--
-- Every timestamp column is a unix second (INTEGER) rather than a
-- TEXT/DATETIME because every consumer — the canvas time axis, effort
-- rollups, SSE event payloads, and MCP tool responses — wants seconds
-- anyway. Storing them as integers keeps the code free of timezone
-- footguns.

CREATE TABLE projects (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    slug        TEXT    NOT NULL UNIQUE,
    name        TEXT    NOT NULL,
    problem     TEXT    NOT NULL CHECK (length(trim(problem)) > 0),
    description TEXT,
    priority    TEXT    NOT NULL CHECK (priority IN ('high', 'medium', 'low')),
    color       TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    archived_at INTEGER,
    canvas_x    REAL,
    canvas_y    REAL
);

CREATE INDEX projects_archived_at ON projects(archived_at);

CREATE TABLE features (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title        TEXT    NOT NULL,
    description  TEXT,
    status       TEXT    NOT NULL CHECK (status IN ('backlog', 'in_progress', 'done', 'dropped')),
    created_at   INTEGER NOT NULL,
    started_at   INTEGER,
    completed_at INTEGER,
    dropped_at   INTEGER
);

CREATE INDEX features_project_id ON features(project_id);
CREATE INDEX features_status ON features(status);

CREATE TABLE effort_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    feature_id INTEGER          REFERENCES features(id) ON DELETE SET NULL,
    minutes    INTEGER NOT NULL CHECK (minutes > 0),
    note       TEXT,
    logged_at  INTEGER NOT NULL
);

CREATE INDEX effort_logs_project_id ON effort_logs(project_id);
CREATE INDEX effort_logs_logged_at ON effort_logs(logged_at);
