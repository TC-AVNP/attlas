# Base Setup

Run once after first SSH into a fresh VM. Installs everything needed for a working development environment.

## What It Does

- Installs base packages (zsh, tmux, python3, curl, git, etc.)
- Installs Node.js 24, Go
- Clones and installs [dotfiles](https://github.com/TC-AVNP/dotfiels) (zsh config, tmux, Claude settings)
- Sets zsh as default shell
- Installs Claude Code
- Builds the Go dashboard server (`alive-server/main.go`)
- Fetches OAuth2 config from GCP Secret Manager
- Installs Caddy and deploys the base gateway config
- Auto-updates Cloudflare DNS (`attlas.uk` → VM external IP)
- Verifies the dashboard is reachable
- Prompts to install services (ttyd, code-server, OpenClaw)

## Usage

Run **as root** (setup.sh creates system users, writes systemd units, and
provisions `/var/lib/<svc>/` state dirs):

```bash
sudo bash /home/agnostic-user/iapetus/attlas/base-setup/setup.sh
```

## Service topology after setup

| Service | User | State dir |
|---|---|---|
| `alive-server.service` | `alive-svc` (system, nologin) | `/var/lib/alive-server/` |
| `openclaw-gateway.service` | `openclaw-svc` (system, nologin) | `/var/lib/openclaw/` |
| `ttyd.service` | `agnostic-user` (login) | `/home/agnostic-user/` |
| `code-server.service` | `agnostic-user` (login) | `/home/agnostic-user/.local/share/code-server/` |
| `dotfiles-sync.timer` | `agnostic-user` | `/home/agnostic-user/iapetus/dotfiels/` |
| `caddy.service` | `caddy` (package default) | `/etc/caddy/` |

## Caddy Gateway

The `Caddyfile` in this directory is the base gateway config. It handles:
- TLS via Let's Encrypt (automatic with attlas.uk)
- Google OAuth2 auth (via `forward_auth` to the Go server)
- Dashboard at `/`
- Importing service route snippets from `/etc/caddy/conf.d/*.caddy`

**To expose a new service, add a `.caddy` snippet to `/etc/caddy/conf.d/` — do NOT edit this Caddyfile.**
