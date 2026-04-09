# base-setup

First-SSH setup script. Run once on a fresh VM to install packages, dotfiles, and the Caddy gateway.

## What It Installs

1. Base packages: zsh, tmux, python3, curl, git, build-essential, jq
2. Node.js 24 (via NodeSource)
3. Dotfiles from `TC-AVNP/dotfiels.git` (cloned via PAT from Secret Manager)
4. Zsh as default shell
5. Claude Code (`@anthropic-ai/claude-code`)
6. Go (for building the dashboard server)
7. Dashboard server — Go binary (`alive-server/main.go`) with Google OAuth2 auth
8. Caddy web server with the base Caddyfile
9. Auto-updates Cloudflare DNS to point `attlas.uk` to the VM's external IP

## Dashboard Server (alive-server)

The dashboard is a Go binary at `alive-server/main.go`. It handles:
- Google OAuth2 login (`/oauth2/login`, `/oauth2/callback`)
- Session verification for Caddy `forward_auth` (`/api/auth/verify`)
- VM status API (`/api/status`)
- Claude Code login helper (`/api/claude-login`)
- Service install/uninstall (`/api/install-service`, `/api/uninstall-service`)
- Static file serving for the React frontend (`frontend/dist/`)

OAuth2 config is read from `~/.attlas-server-config.json` (fetched from GCP Secret Manager during setup). Session secret is auto-generated at `~/.attlas-session-secret`.

## Caddy Ownership

This folder owns the **base Caddyfile** — it configures the domain, TLS, OAuth2 auth, and the dashboard.

**Do NOT add service routes to the base Caddyfile.** Services add their own routes by dropping `.caddy` snippet files into `/etc/caddy/conf.d/`. The base Caddyfile imports them via `import /etc/caddy/conf.d/*.caddy`.

## IMPORTANT: Exposing Services

Any new service that needs to be reachable from the internet MUST have a `.caddy` route snippet in `/etc/caddy/conf.d/`. See `services/CLAUDE.md` for the pattern.

## Usage

```bash
~/attlas/base-setup/setup.sh
```

At the end, it prompts whether to also install services (ttyd, code-server).
