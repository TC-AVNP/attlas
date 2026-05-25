# BFM — Brain Fleet Management

Cloud control plane at `bfm.attlas.uk` for managing fleets of Raspberry Pi clusters organized into brain/slave hierarchies.

## Architecture

- **Go server** with embedded SQLite, Google OAuth, React SPA frontend
- **Brain** = gateway Pi that manages a local network of slaves via PXE boot
- **Slave** = worker Pi that PXE boots from its brain, runs an assigned Ansible playbook
- **Voucher** = one-time token for provisioning. Brain vouchers baked into SD images, slave vouchers claimed by brains at runtime
- **Playbook** = uploaded Ansible YAML assigned to vouchers, delivered on redemption

## Directory Layout

```
bfm/
├── CLAUDE.md
├── install.sh          # idempotent deploy script
├── uninstall.sh        # cleanup (preserves state)
├── bfm.caddy           # Caddy site block
└── server/
    ├── go.mod
    ├── main.go          # all server code
    ├── migrations/      # SQLite schema
    └── templates/       # HTML/CSS/JSX (embedded)
        ├── index.html   # React SPA (served as raw bytes, not template)
        ├── login.html   # OAuth login page
        ├── denied.html  # access denied
        └── styles.css   # BFM design system (Geist font, light theme)
```

## Service Config

| Field | Value |
|-------|-------|
| Port | 7698 |
| Domain | bfm.attlas.uk |
| DB | /var/lib/bfm/bfm.db |
| Binary | /usr/local/bin/bfm |
| Systemd | bfm.service |
| User | bfm-svc |

## Development

```bash
# Build
cd server && go build -o /tmp/bfm .

# Run locally
BFM_PORT=7698 BFM_DB=/tmp/bfm.db BFM_STATE_DIR=/tmp/bfm BFM_LOCAL_BYPASS=1 /tmp/bfm

# Deploy
sudo bash install.sh
```

## Key Design Decisions

- **index.html served as raw bytes** (not Go template) because inline JSX contains `{{ }}` that clashes with `html/template`
- **BFM design system** uses Geist font family and light theme — intentionally different from the attlas dark theme
- **Brain SD image** uses the router Pi image from `router-node/` as its base
- **Single main.go** for the entire server (OAuth, all CRUD APIs, node registration, SSE build streaming)
