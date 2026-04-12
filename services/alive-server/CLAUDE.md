# alive-server

Go dashboard that serves `attlas.uk/` and acts as the Caddy
`forward_auth` backend for every other service. Bootstrapped from
`base-setup/setup.sh` (not from `services/install.sh`) because the
dashboard must exist before the service menu does.

## Layout

```
services/alive-server/
├── cmd/
│   └── attlas-server/           # thin main package (flags, mux wiring, ListenAndServe)
│       ├── main.go
│       └── splitsies_detail.go  # proxies to splitsies for super-admin operations
├── internal/                    # split-out packages (no external imports)
│   ├── auth/                    # session cookie, Google OAuth2, forward_auth, public-path registry
│   ├── config/                  # OAuthConfig loader + session secret
│   ├── gcp/                     # metadata server reader + OAuth access token fetcher
│   ├── status/                  # VM info, system load, claude/dotfiles/domain helpers
│   └── util/                    # runCmd, humanDuration, sendJSON, cache TTL
├── frontend/                    # Vite + React SPA served as static assets
├── go.mod
└── attlas-server                # built binary (gitignored)
```

**In progress:** `cmd/attlas-server/main.go` still contains the
`costs`, `openclaw`, `infra`, `services`, and `static` handlers. The
plan to move them into `internal/` packages is in `attlas/refactor.md`
(step 2.6–2.10). The completed splits (auth/status/util/etc.) show the
pattern — each split is a new `internal/<pkg>/*.go` file plus a rename
of call sites in `main.go`, committed separately.

## Port

3000, bound to 127.0.0.1. Caddy's base site block uses it for
`forward_auth`, favicon rewrites, and the catch-all at the end.

## Environment

| Variable | Purpose |
|---|---|
| `ATTLAS_DIR` | Path to `iapetus/attlas` checkout |
| `HOME` | State dir (defaults to `/var/lib/alive-server/`) |
| `CLAUDE_JSON_PATH` | Optional — path to `~/.claude.json` for the logged-in indicator |

Secrets live in `$HOME/.attlas-server-config.json` (seeded from the
`attlas-server-config` GCP secret during setup) and
`$HOME/.attlas-session-secret` (auto-generated on first run).

## Dev

```bash
cd services/alive-server
go build -o attlas-server ./cmd/attlas-server
./attlas-server  # listens on 127.0.0.1:3000
```

For frontend work: `cd frontend && npm install && npm run dev` or
`npm run build` (output goes to `frontend/dist/` which the binary
serves at `/`).
