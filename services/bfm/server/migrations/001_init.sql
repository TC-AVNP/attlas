-- BFM schema: brain fleet management

CREATE TABLE IF NOT EXISTS brains (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT DEFAULT '',
    status TEXT DEFAULT 'provisioning',
    ip TEXT,
    lan TEXT,
    provisioned_at TEXT DEFAULT (datetime('now')),
    last_seen TEXT,
    pxe_dhcp_start TEXT,
    pxe_dhcp_end TEXT,
    pxe_dns TEXT,
    pxe_gateway TEXT,
    pxe_lease_hours INTEGER DEFAULT 24,
    pxe_max_slaves INTEGER DEFAULT 16,
    pxe_paused INTEGER DEFAULT 0,
    pxe_assigned_image TEXT,
    pxe_serving_image TEXT,
    pxe_config_sync TEXT DEFAULT 'pending',
    cert_pem TEXT,
    key_pem TEXT,
    cert_serial TEXT,
    cert_expires_at TEXT
);

CREATE TABLE IF NOT EXISTS slaves (
    id TEXT PRIMARY KEY,
    brain_id TEXT NOT NULL REFERENCES brains(id),
    name TEXT NOT NULL,
    status TEXT DEFAULT 'provisioning',
    ip TEXT,
    mac TEXT,
    model TEXT,
    playbook_id TEXT,
    playbook_version TEXT,
    image_version TEXT,
    k8s_status TEXT,
    joined_at TEXT,
    last_seen TEXT,
    cert_pem TEXT,
    key_pem TEXT
);

CREATE TABLE IF NOT EXISTS vouchers (
    id TEXT PRIMARY KEY,
    brain_id TEXT NOT NULL REFERENCES brains(id),
    kind TEXT NOT NULL,
    state TEXT DEFAULT 'pending',
    token_hash TEXT UNIQUE,
    playbook_id TEXT,
    playbook_version TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    redeemed_at TEXT,
    redeemed_by TEXT,
    revoked_at TEXT
);

CREATE TABLE IF NOT EXISTS playbooks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS playbook_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    playbook_id TEXT NOT NULL REFERENCES playbooks(id),
    version TEXT NOT NULL,
    yaml TEXT NOT NULL,
    notes TEXT DEFAULT '',
    lines INTEGER,
    sha TEXT,
    uploaded_by TEXT DEFAULT 'you@attlas.uk',
    uploaded_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS slave_images (
    version TEXT PRIMARY KEY,
    filename TEXT,
    file_size TEXT,
    sha TEXT,
    notes TEXT DEFAULT '',
    uploaded_by TEXT DEFAULT 'you@attlas.uk',
    uploaded_at TEXT DEFAULT (datetime('now')),
    is_current INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS brain_images (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    brain_id TEXT NOT NULL REFERENCES brains(id),
    filename TEXT,
    file_size TEXT,
    sha TEXT,
    built_at TEXT DEFAULT (datetime('now')),
    downloaded_at TEXT
);

CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    brain_id TEXT NOT NULL REFERENCES brains(id),
    type TEXT NOT NULL,
    msg TEXT NOT NULL,
    actor TEXT DEFAULT 'system',
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS boot_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    brain_id TEXT NOT NULL REFERENCES brains(id),
    mac TEXT,
    model TEXT,
    result TEXT NOT NULL,
    error TEXT,
    slave_id TEXT,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS pxe_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    brain_id TEXT NOT NULL REFERENCES brains(id),
    level TEXT NOT NULL,
    src TEXT NOT NULL,
    msg TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS brain_playbook_map (
    brain_id TEXT NOT NULL REFERENCES brains(id),
    pi_model TEXT NOT NULL,
    playbook_id TEXT REFERENCES playbooks(id),
    PRIMARY KEY (brain_id, pi_model)
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL
);
