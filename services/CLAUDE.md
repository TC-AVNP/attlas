# services

Browser-accessible services running behind the Caddy gateway.

## Current Services

| Service | Script | Port | URL Path | Description |
|---------|--------|------|----------|-------------|
| terminal | `install-terminal.sh` | 7681 | /terminal | Web terminal (ttyd, zsh with dotfiles) |
| code-server | `install-code-server.sh` | 8080 | /code | VS Code in the browser |
| openclaw | `install-openclaw.sh` | 18789 | /openclaw | AI agent daemon (Telegram, Brave, Anthropic) |
| diary | `install-diary.sh` | — | /diary | Hugo-powered project diary (static site) |

## How install.sh Works

`install.sh` is a menu-based installer. It discovers all `install-*.sh` scripts in this directory and presents them as options. You can install individual services or all at once.

## IMPORTANT: Adding a New Service

Every service that needs to be reachable from the internet **MUST** have a Caddy route snippet. Without it, the service is only accessible on localhost.

To add a new service:

1. Create `install-myservice.sh` — the second comment line becomes the menu description
2. Inside the script:
   - Install the binary (with idempotency guard: `if ! command -v ... &>/dev/null`)
   - Create a systemd unit
   - **Copy a `.caddy` route snippet to `/etc/caddy/conf.d/`**
3. Create `myservice.caddy` in this directory:
   ```caddyfile
   handle /myservice* {
       reverse_proxy localhost:PORT
   }
   ```
4. `install.sh` will auto-discover the new `install-myservice.sh` and show it in the menu
5. Caddy is reloaded automatically after all selected services are installed

## Usage

Run as root:

```bash
sudo bash /home/agnostic-user/iapetus/attlas/services/install.sh
```

## Service user contract

- `install-terminal.sh`, `install-code-server.sh`, `install-diary.sh` default to `SERVICE_USER=agnostic-user` and `User=agnostic-user` in the systemd unit. Browser shells / editors thus land in the login user's home.
- `install-openclaw.sh` defaults to `SERVICE_USER=openclaw-svc` (a nologin system user) with `SERVICE_STATE_DIR=/var/lib/openclaw` and sets `Environment=HOME=${SERVICE_STATE_DIR}` on the unit, so openclaw finds its state at `/var/lib/openclaw/.openclaw/`.
- `alive-server.service` itself is installed by `base-setup/setup.sh` (not here) and runs as the dedicated `alive-svc` user.
