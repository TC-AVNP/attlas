# Control

Centralized access management for attlas services at `control.attlas.uk`.

## Problem

When commonlisp6 wants to give someone access to attlas services, he has
to update GCP Secret Manager, edit systemd units, insert into SQLite
databases, modify grafana.ini, and restart multiple services. Control
replaces all of that with a single UI and API.

## Architecture

```
Internet
   |
   v
 Caddy  (forward_auth → alive-server, then reverse_proxy)
   |
   v
 control  (Go, 127.0.0.1:7701)
   |
   v
 SQLite (/var/lib/control/control.db)
```

The web UI is admin-only, protected by alive-server's forward_auth.
The localhost API (`/api/check`, `/api/allowed`) is called directly by
other services for authorization checks — no auth needed since it's
localhost-only.

## Layout

```
services/control/
├── CLAUDE.md
├── install.sh
├── control.caddy
└── server/
    ├── go.mod / go.sum
    ├── main.go
    ├── migrations/
    │   └── 001_init.sql
    └── templates/
        └── index.html
```

## API

### Localhost API (no auth, for service-to-service calls)

| Method | Path | Purpose |
|--------|------|---------|
| GET | /api/check?email=X&service=Y | Check if email has access → `{"allowed": bool}` |
| GET | /api/allowed?service=Y | List all allowed emails → `{"emails": [...]}` |

### Admin API (behind forward_auth, X-Auth-User must be admin)

| Method | Path | Purpose |
|--------|------|---------|
| GET | / | Admin UI |
| GET | /api/data | Full user + service matrix as JSON |
| POST | /api/users | Add a user `{email, services[]}` |
| DELETE | /api/users/{email} | Remove a user and all grants |
| PUT | /api/grants/{email} | Set grants for a user `{services[]}` |
| POST | /api/services | Add a service `{id, name, url}` |
| DELETE | /api/services/{id} | Remove a service and all grants |

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `CONTROL_PORT` | `7701` | HTTP listen port |
| `CONTROL_DB` | `/var/lib/control/control.db` | SQLite path |
| `CONTROL_ADMIN_EMAIL` | `condecopedro@gmail.com` | Bootstrap admin |

## Development

```bash
cd server
PATH="/usr/local/go/bin:$PATH" go build -o /tmp/control .
CONTROL_DB=/tmp/control-dev.db /tmp/control
```

## Deployment

```bash
sudo bash install.sh
```

## Service Config

| Field | Value |
|-------|-------|
| Port | 7701 |
| Domain | control.attlas.uk |
| DB | /var/lib/control/control.db |
| Binary | /usr/local/bin/control |
| Systemd | control.service |
| User | control-svc |
