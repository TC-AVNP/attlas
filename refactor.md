# Attlas Restructure Plan

Not a feature plan — a **structural refactor**. Nothing the user sees
on `attlas.uk/` changes. What changes is how the code is organized so
it's no longer embarrassing to open.

## Why

- `base-setup/alive-server/main.go` is **~2000 lines** in one file
  because handlers were appended without ever being split out. Reading
  it to find any one handler is O(scroll).
- `services/` is a flat pile of `install-X.sh` and `X.caddy` files
  instead of one folder per service. No separation of concerns, no
  obvious "this is where everything for X lives".
- `diary/` lives at `attlas/diary/` (top-level) even though it's a
  Hugo-served, Caddy-routed service exactly like ttyd and code-server.
  Historical accident from Day 2 when there was no `services/` folder.
- Claude Code PTY login helper scripts live inside the alive-server
  directory. They should live with the service that owns Claude Code
  authentication, not with the dashboard binary that happens to
  shell out to them.

## Target structure

```
attlas/
├── CLAUDE.md
├── infra/                               # (unchanged)
│   └── singlevm-setup/
├── base-setup/                          # OS-level setup only
│   ├── setup.sh                         # apt + dotfiles + go + caddy install
│   ├── Caddyfile                        # base caddy config (imports conf.d/*)
│   └── CLAUDE.md
└── services/
    ├── alive-server/                    # the Go dashboard, MOVED from base-setup
    │   ├── cmd/
    │   │   └── attlas-server/
    │   │       └── main.go              # thin entry: flags, mux wiring, ListenAndServe
    │   ├── internal/
    │   │   ├── auth/
    │   │   │   ├── oauth.go             # google oauth2 + callback
    │   │   │   ├── session.go           # HMAC session cookie + state store
    │   │   │   └── forward_auth.go      # /api/auth/verify for caddy forward_auth
    │   │   ├── config/
    │   │   │   └── config.go            # OAuthConfig struct + secret loader
    │   │   ├── status/
    │   │   │   ├── handler.go           # GET /api/status
    │   │   │   ├── vm.go                # getVMInfo() metadata reads
    │   │   │   ├── system_load.go       # /proc/stat, /proc/loadavg, /proc/meminfo
    │   │   │   ├── claude.go            # isClaudeInstalled + isClaudeLoggedIn
    │   │   │   ├── dotfiles.go          # getDotfilesStatus + POST /api/dotfiles/sync
    │   │   │   └── domain.go            # whois attlas.uk + cache
    │   │   ├── openclaw/
    │   │   │   └── handler.go           # GET /api/services/openclaw
    │   │   ├── infra/
    │   │   │   ├── handler.go           # GET /api/services/infrastructure
    │   │   │   ├── uptime.go            # fetchInstanceUptime (Cloud Monitoring)
    │   │   │   └── vm_stop.go           # POST /api/vm/stop
    │   │   ├── costs/
    │   │   │   ├── cloud_spend.go       # GET /api/cloud-spend
    │   │   │   ├── breakdown.go         # GET /api/costs/breakdown
    │   │   │   ├── anthropic.go         # fetchAnthropicSpend
    │   │   │   └── bigquery.go          # fetchGCPSpendBigQuery + fetchGCPCategorizedCosts
    │   │   ├── services/
    │   │   │   ├── registry.go          # knownServices list
    │   │   │   ├── install.go           # POST /api/install-service
    │   │   │   └── uninstall.go         # POST /api/uninstall-service
    │   │   ├── gcp/
    │   │   │   └── metadata.go          # gcpMeta + getMetadataToken (shared by everyone)
    │   │   ├── util/
    │   │   │   ├── cache.go             # externalAPICacheTTL + TTL cache wrapper
    │   │   │   ├── cmd.go               # runCmdCtx + runCmd
    │   │   │   ├── humanize.go          # humanDuration, hoursMinutes, bytesToGB
    │   │   │   └── json.go              # sendJSON helper
    │   │   └── static/
    │   │       └── serve.go             # frontend dist + diary passthrough
    │   ├── frontend/                    # unchanged (vite + react, already fine)
    │   ├── go.mod
    │   ├── go.sum
    │   ├── install.sh                   # builds binary, installs systemd unit, seeds config
    │   ├── alive-server.service         # moved from base-setup if it lives there
    │   ├── alive-server.caddy           # caddy route snippet (base route + /api/* auth)
    │   └── CLAUDE.md
    ├── terminal/                        # ttyd
    │   ├── install.sh                   # MOVED from services/install-terminal.sh
    │   ├── terminal.caddy               # MOVED from services/terminal.caddy
    │   ├── ttyd.service                 # (if present anywhere, consolidated here)
    │   └── CLAUDE.md
    ├── code-server/
    │   ├── install.sh                   # MOVED from services/install-code-server.sh
    │   ├── code-server.caddy
    │   └── CLAUDE.md
    ├── openclaw/
    │   ├── install.sh                   # MOVED from services/install-openclaw.sh
    │   ├── openclaw.caddy
    │   └── CLAUDE.md
    ├── diary/                           # MOVED from attlas/diary/
    │   ├── install.sh                   # hugo install + timer to rebuild on pull
    │   ├── diary.caddy                  # routes /diary/ → static files
    │   ├── hugo.toml                    # MOVED from attlas/diary/config.toml (or similar)
    │   ├── content/                     # MOVED from attlas/diary/content
    │   ├── layouts/                     # MOVED from attlas/diary/layouts
    │   ├── static/                      # MOVED from attlas/diary/static
    │   └── CLAUDE.md
    └── claude-login/                    # PTY helper for claude cli authentication
        ├── claude-login-helper.sh       # MOVED from base-setup/alive-server/
        ├── install.sh                   # installs the helper + sudoers drop-in
        └── CLAUDE.md
```

## Locked decisions

| # | Decision | Why |
|---|---|---|
| D1 | Go package layout uses `cmd/` + `internal/` | Standard Go project layout. `internal/` prevents external imports and keeps everything cohesive in one binary. |
| D2 | No microservices | Still ONE binary (`attlas-server`). Packages are for reading, not deployment. |
| D3 | Per-service folders under `services/` | Every service owns its install script, caddy snippet, systemd unit, and readme. No more hunting across flat files. |
| D4 | `diary/` becomes `services/diary/` | It's a Caddy-routed service, treat it like one. Hugo content lives with the Hugo config. |
| D5 | `alive-server/` moves from `base-setup/` to `services/` | alive-server IS a service. It only lived under base-setup because the first draft bootstrapped it during initial VM setup. |
| D6 | `base-setup/` keeps only OS-level work | packages, users, dotfiles, go install, caddy install. The alive-server `install.sh` is called as the last step of setup, same as every other service. |
| D7 | Each package has a public entrypoint file named for the package | e.g. `internal/openclaw/handler.go` exports `RegisterRoutes(mux *http.ServeMux)` or similar. Keeps the wiring in one place per feature. |
| D8 | CLAUDE.md per service folder | So future sessions can read the one CLAUDE.md for the folder they're editing without spelunking. |
| D9 | No new tests in this refactor | Tests are a separate task. Don't mix scope. |
| D10 | Git history stays | No filter-repo, no force-push on main. The refactor is a sequence of normal commits on main. |

## Cross-cutting concerns

### Caddyfile paths

Base `Caddyfile` currently has `import /etc/caddy/conf.d/*.caddy`. That
stays. What changes is where each `.caddy` file LIVES in the repo —
every service's `install.sh` is responsible for copying its own snippet
to `/etc/caddy/conf.d/` and reloading caddy.

The alive-server snippet is special: it owns the dashboard root route
AND the `/api/auth/verify` forward_auth endpoint that every other
service imports. It stays the same functionally; only the source path
changes (from `base-setup/alive-server/something.caddy` or equivalent
to `services/alive-server/alive-server.caddy`).

### systemd units

Every `services/<name>/install.sh` is responsible for:
1. Writing its `.service` (and `.timer` if any) unit to
   `/etc/systemd/system/`.
2. `systemctl daemon-reload`.
3. `systemctl enable --now <name>.service` (or `.timer`).

This is already true for some services, but not uniformly. The
refactor normalizes it.

### ATTLAS_DIR environment variable

`alive-server` reads `ATTLAS_DIR` to find `services/install-*.sh`
scripts (for the install/uninstall endpoints) and `diary/public/` (for
the Hugo static site passthrough). After the refactor:
- Install scripts move to `services/<name>/install.sh` — the install/
  uninstall endpoints need to update their path pattern from
  `services/install-<id>.sh` to `services/<id>/install.sh`.
- `diary/public/` moves to `services/diary/public/` — the static file
  mux in alive-server needs to follow.

Both are in the Go code and will be updated as part of the
`services/` package and `static/` package moves.

### GOPATH / go.mod

`base-setup/alive-server/go.mod` moves with the code to
`services/alive-server/go.mod`. The module path inside `go.mod` gets
renamed (currently it's something like `attlas/alive-server` or the
default). Needs to be something stable like
`github.com/TC-AVNP/attlas/services/alive-server` so the `internal/`
imports resolve cleanly.

This rename cascades to every import statement in every `.go` file.
The refactor's first commit after the move should be "update import
paths to new module name" and pass `go build`.

### Frontend dist path

Alive-server serves `frontend/dist/` as static files. After the move,
this is now at `services/alive-server/frontend/dist/` relative to
`ATTLAS_DIR`. Update the path lookup in the static handler.

## Order of moves

Each step is one (or two) commits, one deploy, one verification. If
any step breaks the running dashboard, STOP and fix before moving on.

### Step 0 — Prep

- Read the full `main.go` once more and sketch out which line ranges
  belong to which future package. Pin the mapping in a comment at the
  top of main.go so the splits are mechanical.
- Confirm `git status` is clean and there are no stray untracked files.

### Step 1 — Move alive-server to services/, rename module, fix imports

1. `git mv base-setup/alive-server services/alive-server`
2. Edit `services/alive-server/go.mod` to `module github.com/TC-AVNP/attlas/services/alive-server`.
3. Currently `main.go` is in the package root. Move it to
   `cmd/attlas-server/main.go` AS-IS — don't refactor yet, just move.
4. `go build` from `services/alive-server/`. Fix any path breakage.
5. Update anything that references the old path:
   - `base-setup/setup.sh` (if it builds alive-server)
   - `services/alive-server/install.sh` (if moved)
   - `alive-server.service` unit `ExecStart=` and `WorkingDirectory=`
   - Any CLAUDE.md that mentions the path
6. Deploy: VM pulls, builds at the new path, restarts alive-server,
   confirms dashboard still loads.
7. **Checkpoint:** dashboard works exactly as before. No behavior
   change, only a file move.

### Step 2 — Split main.go into internal/ packages

One sub-step per package. After each split, `go build` must pass and
the dashboard behavior must be unchanged.

Order (chosen so each split's dependencies are already in place):

1. **`internal/util/`** — humanize, sendJSON, runCmd, runCmdCtx, cache
   constants. No dependencies on other internal packages.
2. **`internal/gcp/`** — gcpMeta, getMetadataToken. Depends on nothing.
3. **`internal/config/`** — OAuthConfig + secret file loader.
4. **`internal/auth/`** — session store, oauth handlers, forward_auth.
   Depends on config.
5. **`internal/status/`** — status handler and all its sub-helpers
   (VM info, system_load, claude, dotfiles, domain expiry). The
   biggest single split, ~500 lines. Depends on gcp + util.
6. **`internal/openclaw/`** — openclaw detail handler. Depends on
   util + config (for anthropic key) + costs (for fetchAnthropicSpend
   — see step 7).
7. **`internal/costs/`** — cloud_spend + costs_breakdown handlers +
   anthropic + bigquery fetchers. Depends on util + gcp + config.
8. **`internal/infra/`** — infrastructure handler + fetchInstanceUptime +
   vm/stop handler. Depends on util + gcp.
9. **`internal/services/`** — install/uninstall endpoints + registry.
10. **`internal/static/`** — static file + diary passthrough serving.

After each sub-step: commit ("Split X out of main.go"), build, and for
the biggest two (status + costs) deploy and verify the dashboard is
happy. Smaller splits can batch their deploys.

Final `cmd/attlas-server/main.go` ends up ~100 lines of flag parsing,
config load, mux setup (calling `RegisterRoutes` for each package),
and `http.ListenAndServe`.

### Step 3 — Flatten services/ into per-service folders

1. `git mv services/install-terminal.sh services/terminal/install.sh`
2. `git mv services/terminal.caddy services/terminal/terminal.caddy`
3. Same pattern for code-server, openclaw.
4. For any service that had a `.service` systemd unit living under
   `services/`, move it into `services/<name>/`.
5. Update `services/alive-server/internal/services/registry.go` to
   look at `services/<id>/install.sh` instead of
   `services/install-<id>.sh`.
6. Deploy: confirm the "services" card on the dashboard still shows
   the four services correctly, and that install/uninstall endpoints
   still work (dry-run — don't actually uninstall ttyd to test).

### Step 4 — Move diary into services/

1. `git mv diary services/diary`
2. Move `attlas/diary/layouts/`, `content/`, `static/`, config files —
   already handled by step 1 above.
3. Find every reference to `attlas/diary` in the codebase:
   - `services/alive-server/internal/static/serve.go` (the passthrough
     handler) — path from `ATTLAS_DIR/diary/public` to
     `ATTLAS_DIR/services/diary/public`.
   - `feedback_diary.md` memory file on the laptop + VM — the
     "diary entry path" guidance currently says
     `attlas/diary/content/YYYY-MM-DD.md`, update to
     `attlas/services/diary/content/YYYY-MM-DD.md`.
   - The daily diary checklist's rebuild command:
     ```
     cd ~/iapetus/attlas && git pull && cd services/diary && hugo ...
     ```
   - Any existing diary `.md` internal links.
4. Create `services/diary/install.sh` that installs hugo (if missing),
   does a first build, and optionally installs a systemd timer to
   rebuild periodically.
5. Create `services/diary/diary.caddy` that serves the static output
   under `/diary/`. (Currently alive-server passes `/diary/` through
   to the static directory — keep that or switch to a dedicated caddy
   route, pick one.)
6. Deploy: VM pulls, diary rebuilds at new path, `attlas.uk/diary/`
   still renders Day 1-4.

### Step 5 — Extract claude-login helper

1. `git mv services/alive-server/cmd/attlas-server/claude-login-helper*`
   `services/claude-login/` (exact filename TBD — inspect what's
   actually there).
2. Update `services/alive-server/internal/status/claude.go` (or
   wherever the helper path is referenced) to the new location.
3. Create `services/claude-login/install.sh` that installs the helper
   script + the sudoers drop-in that lets `alive-svc` invoke it as
   `agnostic-user`.
4. Deploy: verify the Claude login flow on the dashboard still works
   end-to-end (get URL → open in browser → paste code → "Logged in").

### Step 6 — Tidy up base-setup

1. `base-setup/setup.sh` loses the alive-server build step — it now
   just installs packages and exits. The prompt at the end that asks
   "install services?" iterates `services/*/install.sh` instead of
   hardcoded install-X.sh paths.
2. Remove any dead files left behind in `base-setup/` that were only
   there because of alive-server.

### Step 7 — Add per-service CLAUDE.md

One short CLAUDE.md in every new folder, matching the tone of the
existing ones. Goal: a future session reading only the folder's
CLAUDE.md understands what the folder owns and how it integrates.

## Files that will be touched (rough)

- Everything under `base-setup/alive-server/` — moved + split into ~30
  files under `services/alive-server/`
- Every file under `services/` — moved into per-service subfolders
- `attlas/diary/*` — moved into `services/diary/`
- `base-setup/setup.sh` — lose alive-server build, gain per-service
  install loop
- `base-setup/Caddyfile` — unchanged (still imports `conf.d/*.caddy`)
- Every systemd unit file — `ExecStart=` and `WorkingDirectory=`
  updated to new paths
- `CLAUDE.md` files at every level — paths updated
- Claude memory: `feedback_diary.md` needs the diary path updated

## Risks and gotchas

- **Import path churn.** Renaming the Go module touches every `.go`
  file. Do this in step 1, in a single commit, and run `go build`
  before pushing. If there's a cyclic import after the split, undo
  and rethink the package boundaries.
- **Mixing moves and edits in one commit.** A `git mv` followed by
  `sed`-style edits in the same commit confuses git's rename detection
  and produces huge diffs that are impossible to review. Rule: each
  commit is either a MOVE (git mv + path updates only) or a
  REFACTOR (code changes, no file moves). Never both.
- **Systemd unit paths baked into already-installed services.** The
  running VM has `alive-server.service` pointing at the OLD path
  (`/home/agnostic-user/iapetus/attlas/base-setup/alive-server/...`).
  After the move, the path is wrong. The deploy step must reinstall
  the unit (rewrite `/etc/systemd/system/alive-server.service` with
  the new `ExecStart=`) before restarting. If I forget this, the
  service fails to restart and the dashboard goes dark.
- **Caddy snippet paths in `/etc/caddy/conf.d/`.** Caddy reads from
  `/etc/caddy/conf.d/`, not from the repo. The refactor doesn't change
  Caddy's runtime behavior — only the source location of the `.caddy`
  files. No Caddy reload is needed unless an `install.sh` changes the
  snippet content.
- **Diary content losing history.** Using `git mv` preserves history
  for each file. NOT using `git mv` and instead doing
  `mkdir + cp + rm` creates new files with no history and blames go
  to the refactor commit. Always `git mv`.
- **Laptop has work-in-progress files.** Before step 1, confirm no
  uncommitted files exist in `base-setup/alive-server/`. Running
  `git mv` over unstaged changes is fine (they move with the file)
  but `git status` should be clean before starting so the diff is
  readable.
- **The VM and the laptop get out of sync during a multi-commit
  deploy.** The refactor is done on the laptop, pushed, then the VM
  pulls. If the pull happens mid-refactor between two commits, the VM
  sees a half-moved state. Solution: push at step boundaries only
  (after a step is built AND verified locally), and deploy
  immediately after pushing. Don't leave a half-moved main on origin.
- **Dotfiles-sync on the VM.** If the dotfiles repo has any paths to
  attlas (it doesn't right now, but verify), those need updating too.

## Deployment

- Each step above pushes to `origin/main` and gets deployed to the VM
  before moving on.
- Deploy command is the same as the normal workflow:
  ```bash
  sudo -u agnostic-user bash -c "cd ~/iapetus/attlas && git pull"
  sudo -u agnostic-user bash -c "cd ~/iapetus/attlas/services/alive-server && PATH=\$PATH:/usr/local/go/bin go build -o attlas-server ."
  sudo systemctl restart alive-server
  ```
  (Path changes to `services/alive-server` after step 1.)
- The systemd unit's `ExecStart=` / `WorkingDirectory=` also need to
  be rewritten to the new path — either by reinstalling via
  `services/alive-server/install.sh` or by a one-shot `sed` in place.
  Either is fine; document whichever the refactor picks.
- Caddy: reload only if any `.caddy` file content changed. Pure moves
  don't require it.

## Out of scope (explicitly, do not add)

- **No new features.** If I notice a bug while splitting, file it
  mentally and fix it AFTER the refactor, not during. Mixed commits
  of "move + fix" are unreviewable.
- **No test suite.** Adding tests is a separate task. Adding tests
  AND splitting the code in one pass doubles the diff size for no
  benefit.
- **No CI pipeline.** Same reason.
- **No Hugo theme refactor on the diary side.** The move is pure
  `git mv`; content and layouts stay byte-identical.
- **No switch from Caddy to nginx/traefik.** Caddy stays.
- **No switch from systemd to anything else.** systemd stays.
- **No monorepo tools (Bazel, Turborepo, etc.).** Plain `go build`
  and `npm run build`.
- **No dependency bumps.** Refactoring AND upgrading React or Go
  versions in the same pass will break things twice as fast.
- **No new sudoers drop-ins** unless a move forces one (e.g. the
  claude-login helper extraction).
- **No cleanup of `costsavingplan.md`** — that's someone else's draft.

## Estimate

Not giving a time estimate (per the rules) — but scope-wise: ~7 steps,
each 1-3 commits, each with a deploy. Several are mechanical; the big
cost is the `main.go` split (step 2) which is ~10 sub-splits. Probably
a focused session.

## Success criteria

- `git grep -l '^' services/alive-server/cmd/attlas-server/main.go |
  xargs wc -l` shows main.go ≤ 200 lines.
- No file in `services/alive-server/internal/` exceeds ~400 lines.
- Every `services/*/install.sh` exists and is executable.
- `attlas/diary/` directory does not exist; `services/diary/` does.
- No file under `services/` has a name starting with `install-`.
- Dashboard at `attlas.uk/` loads. All four services (terminal,
  code-server, openclaw, diary) still work. Openclaw detail, infra
  detail, costs detail pages still render. `sync dotfiles`, `stop vm`,
  and claude login flow all still work. System load card still
  updates. Uptime chart still renders stacked bars from Cloud
  Monitoring. Nothing visually regressed.
