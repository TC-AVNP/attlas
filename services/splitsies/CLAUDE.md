# Splitsies

Splitwise-like expense splitting app at `splitsies.attlas.uk`. Invite-only
access via Google OAuth with email whitelist (separate from attlas.uk auth).

## Problem

When splitting expenses with friends — trips, dinners, shared bills —
there's no lightweight, self-hosted way to track who owes whom. Splitsies
is a self-hosted alternative to Splitwise.

## Architecture

```
Internet
   │
   ▼
 Caddy  (terminates TLS for splitsies.attlas.uk via /etc/caddy/sites.d/splitsies-gateway.caddy)
   │
   ▼
 splitsies-gateway  (Go, 127.0.0.1:7700)
   │  reads routes from /etc/splitsies-gateway.d/*.conf, longest-prefix wins
   ▼
 splitsies backend  (Go, 127.0.0.1:7691)
   │
   ▼
 SQLite (/var/lib/splitsies/splitsies.db)
```

The gateway is a generic subdomain-level reverse proxy so future services
can register their own path prefixes under splitsies.attlas.uk without
touching Caddy. Splitsies is the only registered route today (`/` →
splitsies backend).

## Layout

```
services/splitsies/
├── server/                  # Go backend — REST + SSE + Google OAuth
│   ├── cmd/splitsies/       # main entry point
│   ├── api/                 # HTTP handlers
│   ├── service/             # business logic
│   ├── db/                  # SQLite migrations
│   └── events/              # SSE pub/sub broker
├── web/                     # React + Vite + Tailwind frontend
├── splitsies.route          # registers /→:7691 with splitsies-gateway
└── CLAUDE.md                # this file
```

## Development

Build and run locally (no Google OAuth, no gateway, direct access):

```bash
# Backend
cd server && PATH="/usr/local/go/bin:$PATH" go build -o /tmp/splitsies ./cmd/splitsies

# Frontend
cd ../web && npm install && npm run build

# Run with local bypass (auto-logs in as dev admin)
SPLITSIES_LOCAL_BYPASS=1 \
  SPLITSIES_DB=/tmp/splitsies.db \
  SPLITSIES_STATIC_DIR=web/dist \
  /tmp/splitsies serve
```

Then visit http://localhost:7691/

## Environment Variables

| Variable                   | Default                             | Purpose |
|----------------------------|-------------------------------------|---------|
| `SPLITSIES_DB`             | `/var/lib/splitsies/splitsies.db`   | SQLite path |
| `SPLITSIES_PORT`           | `7691`                              | HTTP listen port (bound to 127.0.0.1) |
| `SPLITSIES_STATIC_DIR`     | auto-discovered                     | React `dist/` directory |
| `SPLITSIES_GOOGLE_CLIENT_ID` | (empty)                           | Google OAuth client ID |
| `SPLITSIES_GOOGLE_SECRET`  | (empty)                             | Google OAuth client secret |
| `SPLITSIES_BASE_URL`       | `http://localhost:7691`             | Canonical base URL for OAuth redirects |
| `SPLITSIES_INITIAL_ADMIN`  | (empty)                             | Email seeded as first admin if no admin exists |
| `SPLITSIES_LOCAL_BYPASS`   | (empty)                             | Set to `1` to auto-login as dev user |

## Deployment (splitsies.attlas.uk)

Prerequisites and manual steps are documented in `DEPLOY.md` in this
directory.

## Features

1. User auth & invite-only registration (Google OAuth)
2. User management dashboard (admin: add/remove by email)
3. Group management (create groups, add members, permanent membership)
4. Add expenses (even/custom/percentage splits, categories)
5. Balance tracking (net positions, per-person, per-group breakdowns)
6. Settle up & payment suggestions (minimum payments algorithm)
7. Expense timeline (chronological view, filter by category/search)
8. Monthly spending overview (by group and category)
