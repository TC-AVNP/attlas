CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Registration closed by default
INSERT OR IGNORE INTO settings (key, value) VALUES ('registration_open', '0');
