# Builder's Knowledge Base

Knowledge graph for the iapetus ecosystem at `knowledge.attlas.uk`.

## Problem

When commonlisp6 or a Claude agent needs to do something in the iapetus
ecosystem, the knowledge of how things are done lives in scattered diary
entries, old transcripts, and tribal memory. This service is the single
authoritative source for "this is how we do X here."

## Architecture

```
Internet
   |
   v
 Caddy  (terminates TLS for knowledge.attlas.uk via /etc/caddy/sites.d/)
   |
   v
 knowledge  (Go, 127.0.0.1:7694)
   |
   v
 SQLite (/var/lib/knowledge/knowledge.db)
```

The service handles its own Google OAuth (same pattern as david-s-checklist).

## Concepts

- **Entry**: a self-contained knowledge document (node in the graph).
  Has a slug, title, markdown content, and a placeholder flag.
- **Link**: a directed edge between two entries. Has an optional label.
- **Ark**: the root entry that acts as the starting point for navigating
  the knowledge graph.

## Layout

```
services/knowledge/
├── CLAUDE.md                     # this file
├── install.sh                    # idempotent install script
├── uninstall.sh                  # cleanup script
├── knowledge.caddy               # Caddy site block
├── mcp/
│   ├── main.go                   # stdio MCP server for Claude Code
│   ├── knowledge-mcp             # compiled binary
│   ├── go.mod / go.sum
└── server/
    ├── main.go                   # all server code
    ├── go.mod / go.sum
    ├── migrations/
    │   └── 001_init.sql          # entries, links, sessions tables
    └── templates/
        ├── index.html            # graph view + entry list
        ├── entry.html            # single entry view
        ├── login.html            # Google sign-in page
        └── denied.html           # access denied page
```

## API

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | / | any | Graph visualization + entry list |
| GET | /entry/{slug} | any | View a single entry |
| GET | /api/graph | any | JSON graph data (nodes + edges) |
| POST | /api/entries | admin | Create an entry |
| PUT | /api/entries/{id} | admin | Update an entry |
| DELETE | /api/entries/{id} | admin | Delete an entry |
| POST | /api/links | admin | Create a link between entries |
| DELETE | /api/links/{id} | admin | Delete a link |

## Development

```bash
cd server
PATH="/usr/local/go/bin:$PATH" go build -o /tmp/knowledge .

KNOWLEDGE_LOCAL_BYPASS=1 \
  KNOWLEDGE_DB=/tmp/knowledge-dev.db \
  /tmp/knowledge
```

Then visit http://localhost:7694/

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `KNOWLEDGE_PORT` | `7694` | HTTP listen port |
| `KNOWLEDGE_DB` | `/var/lib/knowledge/knowledge.db` | SQLite path |
| `KNOWLEDGE_ADMIN_EMAIL` | `condecopedro@gmail.com` | Admin email |
| `KNOWLEDGE_GOOGLE_CLIENT_ID` | (empty) | Google OAuth client ID |
| `KNOWLEDGE_GOOGLE_SECRET` | (empty) | Google OAuth client secret |
| `KNOWLEDGE_BASE_URL` | `http://localhost:<port>` | Canonical base URL |
| `KNOWLEDGE_LOCAL_BYPASS` | (empty) | Set `1` to skip auth on loopback |

## MCP Server

`mcp/` contains a stdio-based MCP server (Go) that lets Claude Code
query knowledge base entries without opening the browser UI. Configured
in `~/.claude.json` under `mcpServers.knowledge`.

Tools: `search_entries`, `get_entry`, `list_entries`.

All content-returning tools accept a `view` parameter (`"llm"` or
`"human"`) to select which content field to return. Default: `llm`.

Rebuild after changes:
```bash
cd services/knowledge/mcp
PATH="/usr/local/go/bin:$PATH" go build -o knowledge-mcp .
```

## Deployment

```bash
sudo bash install.sh    # builds, installs, sets up DNS + Caddy + systemd
```

Prerequisite: `https://knowledge.attlas.uk/auth/callback` must be an
authorized redirect URI in the Google OAuth client.
