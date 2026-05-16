# homelab-planner

Wiki-style documentation site for the homelab project. Reachable at
`https://attlas.uk/homelab-planner/`. Documents the homelab's hardware,
networking, architecture, and build progress.

## Layout

```
services/homelab-planner/
├── server/              # Go backend — REST API + SQLite
│   ├── cmd/homelab-planner/  # main entry point
│   ├── api/             # REST handlers (wiki.go + api.go legacy)
│   ├── service/         # Business logic (wiki.go + service.go legacy)
│   └── db/              # SQLite migrations + seeds
└── web/                 # React + Vite + Tailwind frontend
    └── src/
        ├── layouts/     # WikiLayout (sidebar + outlet)
        ├── pages/       # WikiPage, JournalList, JournalEntryPage
        └── components/  # Markdown, Schematic3DEmbed, SchematicEmbed
```

## Architecture

- **Go backend** with pure-Go SQLite (modernc.org/sqlite, no CGO)
- **React + Vite + Tailwind** frontend with react-query + react-router
- **No auth** — piggybacks on Caddy's Google OAuth2 gate
- **Port 7691**

## Data model

- **Pages**: wiki articles with slug, title, markdown body, position ordering
- **Journal entries**: dated blog posts with title and markdown body
- Legacy tables (steps, checklist_items, item_options, build_log_entries) still exist

## Wiki pages (seeded)

| Slug | Title | Special |
|------|-------|---------|
| home | Homelab | Landing page with section links |
| standard-cluster | The Standard Cluster | 3D model embed at top |
| networking | Networking | 2D SVG schematic embed at top |
| architecture | Architecture | Kubernetes, Metal³, etcd |
| future-plans | Future Plans | Second house, Nebula |

## API routes

All under `/homelab-planner/api/`:

| Method | Path | Description |
|--------|------|-------------|
| GET | /pages | List all pages (no body) |
| GET | /pages/:slug | Get page with body |
| PATCH | /pages/:slug | Update page title/body |
| GET | /journal | List journal entries (no body) |
| POST | /journal | Create journal entry |
| GET | /journal/:id | Get journal entry |
| PATCH | /journal/:id | Update journal entry |
| DELETE | /journal/:id | Delete journal entry |

## Install

```
sudo bash services/homelab-planner/install.sh
```

Drops a route snippet in `/etc/caddy/conf.d/homelab-planner.caddy`.
