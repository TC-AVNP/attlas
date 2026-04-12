# petboard

Personal project tracker at `attlas.uk/petboard/`. Go backend + SQLite
with a React + react-konva frontend showing all projects as draggable
nodes on an infinite canvas. Also exposes a full OAuth 2.1 MCP server
so Claude Code can plan/track projects directly from a terminal
session.

## Layout

```
services/petboard/
├── server/                    # Go backend: REST + SSE + MCP + OAuth 2.1
│   ├── cmd/petboard/
│   ├── api/                   # HTTP handlers
│   ├── service/               # business layer
│   ├── db/                    # migrations + seed
│   ├── events/                # in-process pub/sub for SSE
│   ├── mcp/                   # MCP server implementation
│   └── oauth/                 # OAuth 2.1 (DCR + PKCE)
├── web/                       # Vite + React + Tailwind + react-konva SPA
├── install.sh
├── uninstall.sh
├── petboard.caddy             # route snippet for /petboard*
└── PLAN.md                    # full architecture notes
```

## Port

7690.

## Persistence

SQLite at `/var/lib/petboard/petboard.db`. Schema migrations are
embedded in the binary and applied on startup.

## Claude integration

Claude Code logs in via petboard's MCP OAuth 2.1 flow. The login URL
path (`/petboard/oauth/authorize`) reuses the attlas Google OAuth
session, so there's no second password prompt — the user only sees
Claude's "authorize attlas" dialog.

## Dev

```bash
# Backend
cd server && go build -o petboard ./cmd/petboard
./petboard serve --port=7690 --db=/tmp/petboard-dev.db

# Frontend
cd ../web && npm install && npm run dev
```
