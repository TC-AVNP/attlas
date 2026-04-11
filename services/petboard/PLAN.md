# Petboard — Plan

Personal project tracker for commonlisp6's pet projects. Installed as a new
service under attlas, reachable at `https://attlas.uk/petboard/`. Managed
interactively by Claude Code via an MCP endpoint served by the same Go
binary. The main view is an infinite-zoom 2D canvas showing projects as
glowing threads across a time axis; a per-project detail page handles the
backlog / in-progress / done / dropped columns.

## Stack

- **Backend**: Go single binary, `net/http`, `modernc.org/sqlite` (pure Go,
  no CGO).
- **Frontend**: React + Vite + TypeScript, Tailwind, `react-konva` for the
  canvas, `@tanstack/react-query` for server state.
- **State**: SQLite at `/var/lib/petboard/petboard.db`, owned by a new
  `petboard-svc` nologin system user (mirrors `openclaw-svc`).
- **Auth**: browser session reuses the existing Caddy + alive-server Google
  OAuth. MCP clients use petboard's own OAuth 2.1 flow for access tokens.
  No new infrastructure secrets.
- **Deploy**: systemd unit + Caddy route snippet + install script under
  `attlas/services/petboard/`. One small touch to `base-setup/alive-server/`
  for the generic public-path registry (see Auth section).

## Service layout

```
attlas/services/petboard/
├── install-petboard.sh
├── uninstall-petboard.sh
├── petboard.caddy
├── README.md
├── PLAN.md                         # this file
├── server/
│   ├── go.mod
│   ├── cmd/petboard/main.go        # serve | migrate subcommands
│   ├── db/
│   │   ├── db.go
│   │   └── migrations/
│   │       ├── 0001_init.sql
│   │       └── 0002_oauth.sql
│   ├── api/
│   │   ├── projects.go
│   │   ├── features.go
│   │   ├── effort.go
│   │   └── events.go               # SSE
│   ├── oauth/
│   │   ├── wellknown.go            # /.well-known/* metadata
│   │   ├── register.go             # RFC 7591 dynamic client registration
│   │   ├── authorize.go            # /authorize — reuses Caddy session
│   │   ├── token.go                # /token — PKCE verification
│   │   ├── middleware.go           # Bearer token auth middleware
│   │   └── store.go                # SQLite-backed clients/codes/tokens
│   ├── mcp/
│   │   └── handler.go              # /mcp streamable HTTP + tool dispatch
│   ├── service/                    # internal business layer — shared by REST + MCP
│   │   ├── projects.go
│   │   ├── features.go
│   │   └── effort.go
│   └── static/
│       └── embed.go                # //go:embed dist/*
└── web/
    ├── index.html
    ├── vite.config.ts
    ├── tsconfig.json
    ├── package.json
    ├── tailwind.config.ts
    └── src/
        ├── main.tsx
        ├── App.tsx
        ├── api/client.ts
        ├── pages/
        │   ├── Universe.tsx
        │   └── ProjectDetail.tsx
        └── canvas/
            ├── Stage.tsx
            ├── ProjectThread.tsx
            ├── FeatureOrb.tsx
            ├── NowLine.tsx
            ├── ProblemCard.tsx
            └── zoom.ts              # semantic-zoom level helpers
```

Plus `dotfiels/claude/skills/petboard.md` for the Claude Code skill.

Changes outside `services/petboard/`:
- `base-setup/alive-server/main.go` gains a generic public-path registry (V2)
  that reads `/etc/attlas-public-paths.d/*.conf`.

## Data model

### `projects`

| column       | type    | notes                                                |
| ------------ | ------- | ---------------------------------------------------- |
| id           | INTEGER | PK                                                   |
| slug         | TEXT    | UNIQUE, URL-safe                                     |
| name         | TEXT    | NOT NULL                                             |
| problem      | TEXT    | **NOT NULL**, CHECK `length(trim(problem)) > 0`      |
| description  | TEXT    | nullable — implementation notes, distinct from problem |
| priority     | TEXT    | CHECK IN ('high','medium','low')                     |
| color        | TEXT    | hex, derived from slug hash on create, persisted     |
| created_at   | INTEGER | unix seconds                                         |
| archived_at  | INTEGER | nullable                                             |
| canvas_x     | REAL    | persisted canvas position (nullable → auto)          |
| canvas_y     | REAL    | persisted canvas position (nullable → auto)          |

`problem` is the *why*, not the *what*. See the skill for how Claude Code
writes it.

### `features`

| column       | type    | notes                                                 |
| ------------ | ------- | ----------------------------------------------------- |
| id           | INTEGER | PK                                                    |
| project_id   | INTEGER | FK projects(id) ON DELETE CASCADE                     |
| title        | TEXT    |                                                       |
| description  | TEXT    | nullable                                              |
| status       | TEXT    | CHECK IN ('backlog','in_progress','done','dropped')   |
| created_at   | INTEGER | unix seconds                                          |
| started_at   | INTEGER | nullable, set when status first → in_progress         |
| completed_at | INTEGER | nullable, set when status → done                      |
| dropped_at   | INTEGER | nullable, set when status → dropped                   |

### `effort_logs`

| column     | type    | notes                              |
| ---------- | ------- | ---------------------------------- |
| id         | INTEGER | PK                                 |
| project_id | INTEGER | FK projects(id)                    |
| feature_id | INTEGER | FK features(id), nullable          |
| minutes    | INTEGER | stored as minutes for precision    |
| note       | TEXT    | nullable                           |
| logged_at  | INTEGER | unix seconds                       |

### `schema_version`

One-row table tracking the highest applied migration number. Migrations are
embedded SQL files executed in order on startup.

### Bootstrap seed

After all schema migrations apply, the binary runs `server/db/seed.sql`
**iff** the `projects` table is empty. That file bootstraps petboard's
very first project: petboard itself. Origin story. Re-materializes on a
fresh DB and is a no-op on every subsequent start.

### OAuth tables (`0002_oauth.sql`)

- `oauth_clients(id, client_id, client_name, redirect_uris, created_at)`
- `oauth_auth_codes(code_hash, client_id, redirect_uri, code_challenge, code_challenge_method, created_at, expires_at, used)`
- `oauth_access_tokens(token_hash, client_id, created_at, last_used_at, expires_at)`

All tokens stored as SHA-256 hashes; the raw token only exists in the
response to `/token` and in the client's memory.

## REST API

All routes under `/petboard/api/`. JSON in/out. Auth is the Caddy session
cookie (browser clients) — transparent to the Go server because alive-server
already validated it via `forward_auth`.

| method | path                                | body / returns                                            |
| ------ | ----------------------------------- | --------------------------------------------------------- |
| GET    | `/api/projects`                     | list with aggregates (feature counts by status, total minutes) |
| POST   | `/api/projects`                     | `{ name, problem, priority, description? }` → project     |
| GET    | `/api/projects/:slug`               | project + features + recent effort log                    |
| PATCH  | `/api/projects/:slug`               | `{ name?, problem?, priority?, description?, color?, canvas_x?, canvas_y?, archived? }` |
| DELETE | `/api/projects/:slug`               | soft-delete (sets archived_at); hard-delete via `?hard=1` |
| POST   | `/api/projects/:slug/features`      | `{ title, description? }` → feature (status=backlog)      |
| PATCH  | `/api/features/:id`                 | `{ title?, description?, status? }` — status transitions auto-set timestamps |
| DELETE | `/api/features/:id`                 |                                                           |
| POST   | `/api/projects/:slug/effort`        | `{ minutes, note?, feature_id? }`                         |
| GET    | `/api/events`                       | SSE stream of mutation events                             |

SSE event shape: `{ type: 'project.created' | 'project.updated' | 'feature.status_changed' | 'effort.logged' | ..., payload: {...} }`.

Server rejects `POST /api/projects` and `PATCH /api/projects/:slug` with a
400 if `problem` is present but empty after trimming. Rejects `POST` with
400 if `problem` is missing entirely.

## MCP tools

Exposed at `/petboard/mcp` via streamable HTTP transport. Bearer-token
auth. Tools wrap the same `service/` package the REST handlers use — no
duplicated business logic.

- `list_projects(include_archived=false)` — id, slug, name, priority, problem (truncated), feature counts, total effort hours
- `get_project(slug)` — full project with features and effort log
- `create_project(name, problem, priority, description?)` — **`problem` is required**; returns slug
- `update_project(slug, priority?|name?|problem?|description?|archive?)`
- `add_feature(project_slug, title, description?)` — returns feature id
- `set_feature_status(feature_id, status)` — backlog|in_progress|done|dropped
- `update_feature(feature_id, title?|description?)`
- `log_effort(project_slug, hours, note?, feature_id?)` — hours → minutes at the boundary
- `search(query)` — fuzzy match over project names, problems, and feature titles

## Auth architecture

### Overview

Two kinds of clients hit `/petboard/*`:

1. **Browser clients** (you, in a web browser, looking at the canvas) —
   reuse the existing Caddy + alive-server + Google OAuth session.
2. **MCP clients** (Claude Code) — use petboard's own OAuth 2.1 flow with
   PKCE, storing access tokens in petboard's SQLite.

Both end up trusted by the same Go server. The browser path reuses what
attlas already has; the MCP path adds real OAuth 2.1 that Claude Code drives
end-to-end.

### Alive-server changes

Two related edits in `base-setup/alive-server/main.go`:

**(A) Public-path registry (V2)** — `handleAuthVerify` currently checks only
whether the request has a valid session. It gains a public-path exemption
check that runs first.

**(B) Return-URL preservation across OAuth round-trip** — discovered during
the probe: alive-server today always redirects to `/` after a successful
Google login, losing the original URL. The MCP `/authorize` flow needs the
user's browser to land back on `/petboard/oauth/authorize?client_id=...` so
petboard can generate the auth code and 302 to Claude Code's callback. Fix:
`handleAuthVerify` passes `X-Forwarded-Uri` as `return_to` when redirecting
to `/oauth2/login`, the state store holds the return URL alongside the
state token, and `handleOAuth2Callback` redirects to the (validated)
return URL instead of `/`. Validation: must be a same-origin relative path
(starts with `/`, not `//`, parseable with empty scheme/host). Falls back
to `/` if missing or unsafe. This is a generic improvement; every attlas
service benefits.

- Alive-server on startup reads `/etc/attlas-public-paths.d/*.conf` into
  an in-memory prefix list. Watches the directory with `fsnotify` for
  reloads.
- Each config file has one path prefix per line. `#` comments and blank
  lines ignored.
- `handleAuthVerify` reads `X-Forwarded-Uri` (which Caddy's `forward_auth`
  sets automatically) and returns 200 immediately if any public prefix
  matches. Otherwise falls through to the existing session check.
- Result: the base Caddyfile stays untouched. Any service needing
  unauthenticated paths drops a `.conf` file during install and removes
  it during uninstall.

Petboard's file `/etc/attlas-public-paths.d/petboard.conf`:

```
/petboard/mcp
/petboard/.well-known/
/petboard/oauth/register
/petboard/oauth/token
```

Note that `/petboard/oauth/authorize` is **not** in this list —
`/authorize` deliberately goes through Caddy's auth so the browser session
is validated before petboard issues an auth code.

### Paths and their auth posture

| path                                                   | Caddy auth | who handles auth                                     |
| ------------------------------------------------------ | ---------- | ---------------------------------------------------- |
| `/petboard/` (UI, React SPA)                           | yes        | Caddy → alive-server → Google session                |
| `/petboard/api/*`                                      | yes        | same                                                 |
| `/petboard/events` (SSE)                               | yes        | same                                                 |
| `/petboard/oauth/authorize`                            | **yes**    | same — we want Caddy auth here to reuse the session  |
| `/petboard/.well-known/oauth-authorization-server`     | no         | public metadata                                      |
| `/petboard/.well-known/oauth-protected-resource`       | no         | public metadata                                      |
| `/petboard/oauth/register`                             | no         | RFC 7591 dynamic client registration                 |
| `/petboard/oauth/token`                                | no         | petboard validates PKCE + auth code                  |
| `/petboard/mcp`                                        | no         | petboard validates Bearer access token               |

### End-to-end MCP OAuth flow

1. Claude Code connects to `https://attlas.uk/petboard/mcp` → 401 with
   `WWW-Authenticate: Bearer resource_metadata="/petboard/.well-known/oauth-protected-resource"`
2. Claude fetches the well-known docs → learns petboard's auth endpoints
3. Claude POSTs `/petboard/oauth/register` → gets a `client_id`
4. Claude builds an `/authorize?response_type=code&client_id=...&code_challenge=...&redirect_uri=http://127.0.0.1:<port>/callback&...`
   URL and opens the user's default browser
5. Caddy sees no session on `/petboard/oauth/authorize` → forward_auth
   returns 302 → alive-server redirects to `/oauth2/login` → Google → alive-server
   sets session → redirects back to `/petboard/oauth/authorize?...`
6. Re-request now passes forward_auth → reaches petboard's authorize handler →
   petboard stores `(code_hash, code_challenge, client_id, redirect_uri)` in
   SQLite, 302s to `http://127.0.0.1:<port>/callback?code=<raw>`
7. Claude's local listener captures the code, POSTs `/petboard/oauth/token`
   with `code` + PKCE `code_verifier` → petboard validates, marks code used,
   mints an opaque 32-byte access token, stores its hash, returns the raw token
8. Claude stores the token and uses `Authorization: Bearer <token>` on all
   subsequent `/petboard/mcp` calls
9. Petboard's Bearer middleware hashes the presented token, looks it up,
   updates `last_used_at`, forwards to the MCP handler

No `petboard auth login` CLI. No cookie copying. Zero manual steps once the
service is installed.

### Token lifetime

Access tokens: 30 days from creation. Sliding window semantics — each use
extends `expires_at` by another 30 days from `last_used_at`. No refresh
tokens. When a token expires or is revoked, Claude Code re-runs the OAuth
flow (one browser click if the Caddy session is still fresh).

Admin endpoint: `DELETE /api/oauth/tokens/:id` to revoke manually.

### PKCE + security notes

- `code_challenge_method=S256` required (reject plain)
- Auth codes single-use, 60 second TTL, stored as SHA-256 hashes
- `redirect_uri` for registered clients must match on `/token` exchange
- Registered `redirect_uri` values restricted to `http://127.0.0.1:*` and
  `http://localhost:*` to prevent open-redirect abuse
- Tokens stored as SHA-256 hashes so a DB read doesn't leak usable tokens

## Canvas spec

- **Background**: dark navy (`#0a0e1a`). Subtle dot grid that scales with zoom.
- **Coordinate system**: x = time (unix seconds mapped linearly to canvas
  pixels), y = persisted `canvas_y` per project. New projects get an
  auto-assigned y on creation based on insertion order; dragging the project
  label persists the new y. An "auto-arrange" button in the toolbar resets
  all y's by priority.
- **Viewport**: react-konva `Stage` with pan (drag empty space) and zoom
  (mouse wheel, centered on cursor). Min zoom ≈ 1-year span in view, max zoom
  ≈ 1-day span.
- **Project thread**: a horizontal line from `created_at` to `now` in the
  project's color. Line width + glow intensity derived from priority:
  - `high` → width 6, strong outer glow, subtle pulse animation
  - `medium` → width 4, moderate glow
  - `low` → width 2, faint glow, desaturated
- **Feature orbs** positioned on the thread at their relevant timestamps:
  - `created_at` → outlined ring
  - `started_at` → pulsing ring
  - `completed_at` → filled glowing orb
  - `dropped_at` → faint desaturated ring
  - thin connector line from `created_at` to the terminal orb visualizes cycle time
- **Now line**: thin vertical line with subtle particle animation, advances
  in real time.
- **Semantic zoom layers** (driven by `Stage.scaleX()`):
  - `< 0.3` → threads only, no text, no orbs — overview of your life
  - `0.3 – 0.8` → project name label at thread start, orbs render as small dots
  - `0.8 – 2.0` → orbs render with full status styling, progress ring ("7/12") next to project label, **problem statement shown as tooltip on label hover**
  - `≥ 2.0` → feature titles render as small cards next to orbs, effort log ticks visible under the thread, **problem statement rendered as a pull-quote card floating above the project label**
- **Interactions**:
  - Click feature orb → right-side drawer with feature details
  - Click project label → navigate to `/petboard/p/:slug`
  - Drag project label vertically → updates `canvas_y`, PATCH on drop
  - Toolbar top-right: zoom in / out / fit-all / reset view / auto-arrange
  - Toolbar top-left: priority filter chips, status filter chips, time-window slider
- **Time-window slider**: drags a range over the x-axis. Features whose
  relevant timestamps fall outside the window fade to low opacity (they
  don't disappear — you can still see the thread). Lets you "forget"
  completed features from years past without losing the data.
- **Live updates**: SSE stream pushes mutation events. Canvas animates to
  the new state with Konva tweens (200ms ease-out).

## Project detail page

Route: `/petboard/p/:slug`. Standard React page, not on the canvas.

- **Header**: name, priority pill, color chip, created date, total effort (hours), "Back to universe" link
- **Problem block**: rendered prominently right under the header as a pull
  quote. Editable inline (click to edit, save on blur, PATCH on save).
- **Four-column board** (Backlog / In Progress / Done / Dropped) — plain
  lists, no drag-and-drop in v1
- **Feature card**: title, created date, status, "Log effort" button,
  status-change buttons
- **Effort log section**: sparkline of daily minutes over the last 30 days,
  list below
- **Buttons**: "Add feature" at the top of Backlog, "Log effort" in the
  header for project-level effort without a feature

## Install / systemd / Caddy

### `install-petboard.sh` (run as root)

1. `useradd --system --no-create-home --shell /usr/sbin/nologin petboard-svc`
2. `install -d -o petboard-svc -g petboard-svc -m 700 /var/lib/petboard`
3. As `agnostic-user`: `cd web && npm ci && npm run build`
4. As `agnostic-user`: `cd server && go build -o /tmp/petboard ./cmd/petboard`
5. `install -m 755 /tmp/petboard /usr/local/bin/petboard`
6. Write systemd unit `/etc/systemd/system/petboard.service`:

   ```
   [Unit]
   Description=Petboard
   After=network-online.target
   Wants=network-online.target

   [Service]
   Type=simple
   User=petboard-svc
   Group=petboard-svc
   Environment=PETBOARD_DB=/var/lib/petboard/petboard.db
   Environment=PETBOARD_PORT=7690
   Environment=PETBOARD_ISSUER=https://attlas.uk/petboard
   ExecStart=/usr/local/bin/petboard serve
   Restart=always
   RestartSec=5
   StateDirectory=petboard
   StateDirectoryMode=0700

   [Install]
   WantedBy=multi-user.target
   ```

7. `systemctl daemon-reload && systemctl enable --now petboard`
8. `install -d -m 755 /etc/attlas-public-paths.d`
9. `install -m 644 petboard-public-paths.conf /etc/attlas-public-paths.d/petboard.conf`
10. `systemctl reload alive-server` (picks up the new public-path file)
11. `cp petboard.caddy /etc/caddy/conf.d/`

### `petboard.caddy`

```caddyfile
handle /petboard* {
    reverse_proxy localhost:7690
}
```

### `uninstall-petboard.sh`

- Stops/disables `petboard.service`, removes the binary
- Removes `/etc/caddy/conf.d/petboard.caddy`, reloads Caddy
- Removes `/etc/attlas-public-paths.d/petboard.conf`, reloads alive-server
- Removes the systemd unit
- **Does not delete `/var/lib/petboard`** — data is precious

## Claude Code skill

`dotfiels/claude/skills/petboard.md` encodes policy, not mechanism. The MCP
tools give Claude the verbs; the skill tells it the taste.

### Trigger

When the user talks about planning a new pet project, adding work to one,
logging effort, changing priorities, or asking about status.

### Writing good problem statements

The `problem` field is the *why*, never the *what*. A good problem
statement:

- Names the person or situation feeling the pain
- Describes the current workaround or lack thereof
- Explains why the pain is worth solving **now**
- Avoids naming the implementation — solutions belong in `description`

**Template**: "When [situation], [person] needs [outcome] because
[reason/cost of not having it]. Today they [current workaround or
nothing]."

**Good**: "When I'm debugging a flaky service on the VM, I don't have a
fast way to correlate logs across the three systemd units. Today I
`journalctl -u ...` in three panes and eyeball timestamps, which is slow
and error-prone."

**Bad**: "Build a log aggregator with OpenSearch and Grafana." (That's the
solution.)

**Bad**: "Logs are annoying." (No situation, no person, no cost.)

Before calling `create_project`, Claude drafts the problem statement, reads
it back to the user, and only calls the tool once the user approves. If
the user changes their mind mid-framing, Claude drafts again.

### Other policy

- Feature titles: imperative, present tense, short
- Priority guidance: `high` = actively working this cycle; `medium` = next up; `low` = parked / aspirational
- At session start, if the user resumes work on a known project, `set_feature_status(id, in_progress)` for whichever feature is in focus
- At session end, `log_effort(project_slug, hours, note)` with a short note. Round to the nearest half hour.
- When the user says "done with X", `set_feature_status(id, done)` then `log_effort` for the session
- Before `create_project`, `search` first to avoid duplicates
- Never silently delete projects or features — ask first

## Implementation order

1. **Cookie/auth probe** — verify on the live VM that `X-Forwarded-Uri`
   is being set by Caddy's forward_auth (spec says yes, but confirm on
   the actual version). If not, the alive-server change needs a tweak.
2. **alive-server public-path registry** — implement the V2 registry in
   `base-setup/alive-server/main.go`, with `fsnotify` reload. Test with
   a synthetic `.conf` file and `curl`.
3. **Scaffold petboard** — create `services/petboard/` tree, empty Go
   module, empty Vite project, stub install script.
4. **Backend v1** — SQLite migrations (projects + features + effort
   logs), projects CRUD with mandatory `problem`, features CRUD, effort
   log, list-with-aggregates. Load `server/db/seed.sql` on first init
   (when projects table is empty) — this plants petboard as the very
   first pet project. No SSE, no OAuth, no MCP yet.
5. **Install / Caddy** — deploy backend with a placeholder static page,
   verify end-to-end browser auth on the VM.
6. **Frontend scaffold** — Vite + React + Tailwind + react-router +
   react-query, fetch `/api/projects`, render a plain list. Verify the
   `/petboard/` base path works.
7. **Detail page** — `/petboard/p/:slug` with the four-column board,
   editable problem block, and effort log. Functional, ugly.
8. **Canvas v1** — react-konva stage, threads and orbs, pan + zoom,
   semantic zoom layers. No drag-persist yet.
9. **Canvas v2** — drag to reposition, time-window slider, priority/status
   filters, now-line, particles, glow. Problem pull-quote at full zoom.
10. **SSE live updates** — push from server on mutations, canvas tweens
    on incoming events.
11. **OAuth 2.1 endpoints** — well-known metadata, DCR, authorize, token,
    Bearer middleware, tables. Test the full flow with a minimal MCP
    client stub.
12. **MCP endpoint** — `/mcp` handler wrapping the internal `service/`
    package, tool definitions, Bearer auth.
13. **Skill** — write `dotfiels/claude/skills/petboard.md`. Connect Claude
    Code to petboard's MCP server and test end-to-end.
14. **Polish** — errors, empty states, dark-mode consistency, keyboard
    shortcuts if we feel like it.

## Open risks

- Caddy's `forward_auth` not setting `X-Forwarded-Uri` in the exact form
  expected → resolvable by reading the right header name once we probe it.
- `react-konva` performance with hundreds of orbs + glow filters. Should
  be fine, but fallback: drop glow below `scale < 0.5`.
- MCP streamable HTTP transport support in the specific Claude Code
  version installed on the VM and the mac. If the version is too old, we
  may need to upgrade. Confirm at step 11.
- Alive-server may need a graceful-reload SIGHUP handler if `systemctl
  reload` isn't already wired up. Fallback is `systemctl restart` on
  install.
- If Claude Code's MCP OAuth client doesn't like 127.0.0.1-only redirect
  URIs (some implementations insist on localhost), we allow both.
