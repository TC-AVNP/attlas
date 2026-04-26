# David's Checklist

Task assignment and handover tool hosted at `david.attlas.uk`.

## Problem

When commonlisp6 needs to coordinate a set of tasks for friends or
housemates, there's no shared place where both can see what's left to do
and track progress. Today he'd have to resort to sending a list over chat
or email, which gets buried, can't be ticked off, and requires
back-and-forth to confirm what's done.

## Architecture

```
Internet
   |
   v
 Caddy  (terminates TLS for david.attlas.uk via /etc/caddy/sites.d/)
   |
   v
 david-s-checklist  (Go, 127.0.0.1:7693)
   |
   v
 SQLite (/var/lib/david-s-checklist/david.db)
```

The service handles its own Google OAuth (same pattern as splitsies).

## Roles

- **Admin** (`condeco.pedro@gmail.com`): can create handovers, add/edit/delete
  tasks, assign tasks to people by email.
- **Users** (anyone with tasks assigned): can view and tick off their tasks.
  Auth is dynamic — any email with assigned tasks can log in.

## Concepts

- **Handover**: a named group of tasks for a situation (e.g. "Summer vacation
  \- Mariana"). Has a title, description, and assignee. All tasks within
  inherit the assignee.
- **Standalone task**: a task not in any handover, assigned to a specific
  person by email.

## Layout

```
services/david-s-checklist/
├── CLAUDE.md                          # this file
├── install.sh                         # idempotent install script
├── uninstall.sh                       # cleanup script
├── david-s-checklist.caddy            # Caddy site block
├── todos.json                         # initial seed data (used once)
└── server/
    ├── main.go                        # all server code
    ├── go.mod / go.sum
    ├── migrations/
    │   ├── 001_init.sql               # sessions + completions tables
    │   ├── 002_todos_table.sql        # todos table
    │   └── 003_handovers.sql          # handovers table
    └── templates/
        ├── index.html                 # main checklist page
        ├── login.html                 # Google sign-in page
        └── denied.html               # access denied page
```

## API

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | / | any | Main page (admin sees all, users see their tasks) |
| POST | /api/toggle/{id} | any | Toggle task completion |
| POST | /api/todos | admin | Create a task |
| PUT | /api/todos/{id} | admin | Update a task |
| DELETE | /api/todos/{id} | admin | Delete a task |
| POST | /api/handovers | admin | Create a handover |
| DELETE | /api/handovers/{id} | admin | Delete handover + its tasks |
| GET | /api/info | admin | JSON with admin, assignees, sessions |

## Development

```bash
cd server
PATH="/usr/local/go/bin:$PATH" go build -o /tmp/david-s-checklist .

DAVID_LOCAL_BYPASS=1 \
  DAVID_DB=/tmp/david-test.db \
  DAVID_TODOS=../todos.json \
  /tmp/david-s-checklist
```

Then visit http://localhost:7693/

## Environment Variables

| Variable                | Default                                     | Purpose |
|-------------------------|---------------------------------------------|---------|
| `DAVID_PORT`            | `7693`                                      | HTTP listen port |
| `DAVID_DB`              | `/var/lib/david-s-checklist/david.db`       | SQLite path |
| `DAVID_TODOS`           | `./todos.json`                              | Seed file (used once if DB empty) |
| `DAVID_ADMIN_EMAIL`     | `condeco.pedro@gmail.com`                   | Admin email |
| `DAVID_GOOGLE_CLIENT_ID`| (empty)                                     | Google OAuth client ID |
| `DAVID_GOOGLE_SECRET`   | (empty)                                     | Google OAuth client secret |
| `DAVID_BASE_URL`        | `http://localhost:<port>`                   | Canonical base URL |
| `DAVID_LOCAL_BYPASS`    | (empty)                                     | Set `1` to skip auth on loopback |

## Deployment

```bash
sudo bash install.sh    # builds, installs, sets up DNS + Caddy + systemd
sudo systemctl reload caddy
```

Prerequisite: `https://david.attlas.uk/auth/callback` must be an
authorized redirect URI in the Google OAuth client.
