# Base Setup

Run once after first SSH into a fresh VM. Installs everything needed for a working development environment.

## What It Does

- Installs base packages (zsh, tmux, python3, curl, git, etc.)
- Installs Node.js 24
- Clones and installs [dotfiles](https://github.com/TC-AVNP/dotfiels) (zsh config, tmux, Claude settings)
- Sets zsh as default shell
- Installs Claude Code
- Installs Caddy and deploys the base gateway config
- Verifies "I am alive!" is reachable at `https://{ip}.sslip.io/`
- Prompts to install services (ttyd, code-server)

## Usage

```bash
~/attlas/base-setup/setup.sh
```

## Caddy Gateway

The `Caddyfile` in this directory is the base gateway config. It handles:
- TLS via Let's Encrypt (automatic with sslip.io)
- Basic auth (Testuser / password123)
- "I am alive!" response at `/`
- Importing service route snippets from `/etc/caddy/conf.d/*.caddy`

**To expose a new service, add a `.caddy` snippet to `/etc/caddy/conf.d/` — do NOT edit this Caddyfile.**
