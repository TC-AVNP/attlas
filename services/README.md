# Services

Browser-accessible services that run behind the Caddy gateway.

## Current Services

- **Cloud Terminal** (`/terminal`) — [ttyd](https://github.com/tsl0922/ttyd) on port 7681
- **Cloud VS Code** (`/code`) — [code-server](https://github.com/coder/code-server) on port 8080
- **OpenClaw** — AI agent daemon (no web UI)

## Usage

Run **as root**; each `install-*.sh` writes a systemd unit under
`/etc/systemd/system/` and drops a `.caddy` snippet into
`/etc/caddy/conf.d/`:

```bash
sudo bash /home/agnostic-user/iapetus/attlas/services/install.sh
```

This presents a menu of available services. Select which ones to install, or `a` for all.

## Service user contract

Each `install-*.sh` hard-defaults the service user, but respects these env var overrides:

- `SERVICE_USER` — the system user the service runs as. Defaults:
  - ttyd, code-server, diary → `agnostic-user` (login user, backs browser shells / owns the repo)
  - openclaw → `openclaw-svc` (system nologin user with state under `/var/lib/openclaw/`)
- `SERVICE_STATE_DIR` — openclaw only. Defaults to `/var/lib/openclaw/`.

`alive-server.service` is installed by `base-setup/setup.sh`, not by any `services/install-*.sh`, and runs as its own dedicated `alive-svc` user.

## Adding a New Service

1. Create `install-myservice.sh` (second line comment = menu description). Must refuse to run unless invoked as root.
2. Install binary + create systemd unit (with explicit `User=${SERVICE_USER}`) + copy `.caddy` snippet to `/etc/caddy/conf.d/`.
3. Create `myservice.caddy` with the reverse proxy route.
4. It will auto-appear in the install menu.

**Without a `.caddy` snippet in `/etc/caddy/conf.d/`, a web service will NOT be reachable from the browser.**
