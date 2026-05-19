CREATE TABLE IF NOT EXISTS image_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT NOT NULL UNIQUE,
    node_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    label TEXT NOT NULL DEFAULT '',
    cert_serial TEXT NOT NULL DEFAULT '',
    cert_pem TEXT NOT NULL DEFAULT '',
    key_pem TEXT NOT NULL DEFAULT '',
    ca_pem TEXT NOT NULL DEFAULT '',
    mac_address TEXT NOT NULL DEFAULT '',
    hostname TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    redeemed_at TEXT,
    revoked_at TEXT
);
