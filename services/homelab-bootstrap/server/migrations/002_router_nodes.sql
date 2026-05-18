CREATE TABLE IF NOT EXISTS router_nodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mac_address TEXT NOT NULL UNIQUE,
    hostname TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    cpu_cores INTEGER NOT NULL DEFAULT 0,
    memory_mb INTEGER NOT NULL DEFAULT 0,
    lan_ip TEXT NOT NULL DEFAULT '',
    subdomain TEXT NOT NULL,
    tunnel_id TEXT NOT NULL,
    tunnel_token TEXT NOT NULL,
    dns_record_id TEXT NOT NULL DEFAULT '',
    registered_at TEXT NOT NULL DEFAULT (datetime('now'))
);
