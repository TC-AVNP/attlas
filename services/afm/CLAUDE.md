# AFM — Attlas File Manager

Web-based file manager for the homelab NAS, served at `attlas.uk/afm/`.

## Problem

When commonlisp6 wants to store, browse, or play back files on the
homelab, there's no unified interface that works from both desktop and
phone. Today he either SSHs in or relies on default file-sharing
protocols — neither offers a clean way to upload, search, or stream.

## Architecture

```
Internet
   |
   v
 Caddy (forward_auth → alive-server)
   |
   v
 afm (Go, 127.0.0.1:7695)
   |
   v
 SQLite (/var/lib/afm/afm.db)  +  Filesystem (/home/agnostic-user/afm/)
```

Auth is handled by alive-server's forward_auth. User identity comes
from the `X-Auth-Email` header set by Caddy after successful auth.

## Layout

```
services/afm/
├── CLAUDE.md
├── install.sh
├── uninstall.sh
├── afm.caddy
└── server/
    ├── go.mod / go.sum
    ├── main.go
    ├── migrations/
    │   └── 001_init.sql
    └── templates/
        └── index.html
```

## API

| Method | Path | Purpose |
|--------|------|---------|
| GET | /afm/ | Main file browser UI |
| GET | /afm/api/list?path= | List directory contents (JSON) |
| POST | /afm/api/upload | Upload file(s) to current path |
| GET | /afm/api/preview?path= | Get file content for preview |
| GET | /afm/api/download?path= | Download a file |

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `AFM_PORT` | `7695` | HTTP listen port |
| `AFM_DB` | `/var/lib/afm/afm.db` | SQLite database path |
| `AFM_FILES` | `/home/agnostic-user/afm` | File storage root |

## Development

```bash
cd server
PATH="/usr/local/go/bin:$PATH" go build -o /tmp/afm .

AFM_DB=/tmp/afm-test.db \
  AFM_FILES=/tmp/afm-files \
  /tmp/afm
```

Then visit http://localhost:7695/afm/

## Deployment

```bash
sudo bash install.sh
```
