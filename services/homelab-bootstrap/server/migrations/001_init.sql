CREATE TABLE IF NOT EXISTS nodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mac_address TEXT NOT NULL UNIQUE,
    nvme_serial TEXT NOT NULL DEFAULT '',
    hostname TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    cpu_cores INTEGER NOT NULL DEFAULT 0,
    memory_mb INTEGER NOT NULL DEFAULT 0,
    lan_ip TEXT NOT NULL DEFAULT '',
    cert_fingerprint TEXT NOT NULL,
    registered_at TEXT NOT NULL DEFAULT (datetime('now'))
);
