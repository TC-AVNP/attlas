CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY);

CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    email      TEXT    NOT NULL,
    app        TEXT    NOT NULL,
    origin     TEXT    NOT NULL DEFAULT '',
    path       TEXT    NOT NULL,
    session_id TEXT    NOT NULL,
    event_type TEXT    NOT NULL,
    meta       TEXT    NOT NULL DEFAULT '',
    client_ts  INTEGER NOT NULL,
    server_ts  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_email_ts   ON events(email, server_ts);
CREATE INDEX IF NOT EXISTS idx_events_app_ts     ON events(app, server_ts);
CREATE INDEX IF NOT EXISTS idx_events_session    ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_server_ts  ON events(server_ts);
CREATE INDEX IF NOT EXISTS idx_events_type       ON events(event_type, server_ts);
