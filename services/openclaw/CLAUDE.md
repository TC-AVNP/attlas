# openclaw

Openclaw gateway (a long-running agent daemon) served at
`attlas.uk/openclaw/`. Runs under the `openclaw-svc` user with state
in `/var/lib/openclaw/`.

## Files

- `install.sh` — creates the `openclaw-svc` user, installs the
  openclaw binary, writes the systemd unit, drops the sudoers rule
  that lets alive-svc call `openclaw status --json`, and deploys the
  Caddy snippet.
- `uninstall.sh` — tears it down.
- `openclaw.caddy` — Caddy route for `/openclaw*` →
  `localhost:18789`.

## Ports

- 18789 — the HTTP API the dashboard hits.
- 18791, 46545 — internal ports (see openclaw's own docs).

## Secrets

`openclaw-config` in GCP Secret Manager holds channel tokens and
API keys. `install.sh` fetches it and writes it to
`/var/lib/openclaw/config.json` with 600 perms.

## Detail page

The dashboard shows runtime stats (active tasks, sessions, last-30-day
Anthropic spend) at `/services/details/openclaw`. Backend:
`GET /api/services/openclaw` in `alive-server/cmd/attlas-server/main.go`
(pending extraction into `internal/openclaw/`).
