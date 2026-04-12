# homelab-planner

Personal homelab build tracker for attlas. Reachable at
`https://attlas.uk/homelab-planner/` once installed. Tracks the homelab
build as a series of weekend-sized steps, each with a hardware shopping
checklist (compare options, select, track procurement status) and a
build log (journal entries for blog posts later).

## Layout

```
services/homelab-planner/
├── server/              # Go backend — REST API + SQLite
│   ├── cmd/homelab-planner/  # main entry point
│   ├── api/             # REST handlers
│   ├── service/         # Business logic
│   └── db/              # SQLite migrations + bootstrap seed
└── web/                 # React + Vite + Tailwind frontend
```

## Architecture

- **Go backend** with pure-Go SQLite (modernc.org/sqlite, no CGO)
- **React + Vite + Tailwind** frontend with react-query + react-router
- **No auth** — piggybacks on Caddy's Google OAuth2 gate
- **No SSE** — personal use, single user, polling via react-query staleTime
- **Port 7691** (petboard is 7690)

## Data model

- **Steps**: independent milestones (e.g. "Buy and assemble the rigs")
- **Checklist items**: things to buy per step, with budget/actual cost tracking
  - Status flow: researching -> ordered -> arrived
- **Item options**: alternatives to compare per checklist item (name, URL, price, notes)
  - One option can be "selected" on the parent item
- **Build log entries**: timestamped journal notes per step

## API routes

All under `/homelab-planner/api/`:

| Method | Path | Description |
|--------|------|-------------|
| GET | /steps | List all steps |
| POST | /steps | Create step |
| GET | /steps/:id | Get step detail (items + log) |
| PATCH | /steps/:id | Update step |
| DELETE | /steps/:id | Delete step |
| POST | /steps/:id/items | Add checklist item |
| PATCH | /items/:id | Update item |
| DELETE | /items/:id | Delete item |
| POST | /items/:id/options | Add option |
| PATCH | /options/:id | Update option |
| DELETE | /options/:id | Delete option |
| POST | /steps/:id/log | Add log entry |
| PATCH | /log/:id | Update log entry |
| DELETE | /log/:id | Delete log entry |

## Install

```
sudo bash services/install-homelab-planner.sh
```

Drops a route snippet in `/etc/caddy/conf.d/homelab-planner.caddy`.
