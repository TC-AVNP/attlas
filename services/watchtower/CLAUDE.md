# Watchtower

User session analytics service for the attlas ecosystem. Tracks who is spending time where across all applications.

## Problem

No centralized visibility into user behavior across attlas services. Each service logs independently (if at all), with no way to see who is accessing which applications, for how long, or how they navigate between services.

## Architecture

Three components:

1. **JS beacon** (`server/static/beacon.js`) — injected into all service pages, captures pageview/visibility/unload events. Identifies users by reading session cookies. Sends events via `navigator.sendBeacon()`.

2. **Go backend** (`server/main.go`) — receives beacon events via POST, stores in SQLite. Handles its own Google OAuth. Serves the analytics dashboard. Uses control.attlas.uk for access control.

3. **Caddy config** (`watchtower.caddy`) — subdomain `watchtower.attlas.uk` -> localhost:7702. No forward_auth (service handles its own OAuth).

## Directory Layout

```
watchtower/
├── CLAUDE.md
├── install.sh              # idempotent install (root)
├── uninstall.sh            # cleanup script
├── watchtower.caddy        # Caddy route snippet -> /etc/caddy/conf.d/
└── server/
    ├── go.mod
    ├── main.go             # all server code
    ├── migrations/
    │   └── 001_init.sql    # events table + indexes
    ├── static/
    │   └── beacon.js       # client-side beacon script
    └── templates/
        └── index.html      # dashboard UI
```

## Endpoints

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | /watchtower/api/beacon | Public | Receive beacon events |
| GET | /watchtower/beacon.js | Public | Serve beacon script |
| GET | /watchtower/api/health | Public | Health check |
| GET | /watchtower/ | Protected | Dashboard UI |
| GET | /watchtower/api/live | Protected | Active users (5min window) |
| GET | /watchtower/api/apps | Protected | Per-app breakdown |
| GET | /watchtower/api/users | Protected | Per-user summary |
| GET | /watchtower/api/heatmap | Protected | Hour-of-day x app matrix |
| GET | /watchtower/api/user/{email} | Protected | User session timeline |
| GET | /watchtower/api/stats | Protected | Overall event counts |

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| WATCHTOWER_PORT | 7702 | Listen port |
| WATCHTOWER_DB | watchtower.db | SQLite database path |
| WATCHTOWER_ADMIN_EMAIL | condecopedro@gmail.com | Admin email (always allowed) |
| WATCHTOWER_GOOGLE_CLIENT_ID | | Google OAuth client ID |
| WATCHTOWER_GOOGLE_SECRET | | Google OAuth client secret |
| WATCHTOWER_BASE_URL | https://watchtower.attlas.uk | Base URL for OAuth callbacks |
| WATCHTOWER_LOCAL_BYPASS | | Set to "1" to skip auth on loopback |

## Development

```bash
cd server
go mod tidy
WATCHTOWER_LOCAL_BYPASS=1 go run .
# Listens on 127.0.0.1:7702
```

## Deploy

```bash
sudo bash install.sh
```
