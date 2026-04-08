# base-setup

First-SSH setup script. Run once on a fresh VM to install packages, dotfiles, and the Caddy gateway.

## What It Installs

1. Base packages: zsh, tmux, python3, curl, git, build-essential, jq
2. Node.js 24 (via NodeSource)
3. Dotfiles from `TC-AVNP/dotfiels.git` (cloned via PAT from Secret Manager)
4. Zsh as default shell
5. Claude Code (`@anthropic-ai/claude-code`)
6. Caddy web server with the base Caddyfile

## Caddy Ownership

This folder owns the **base Caddyfile** — it configures the domain, TLS, basic auth, and the "I am alive!" root response.

**Do NOT add service routes to the base Caddyfile.** Services add their own routes by dropping `.caddy` snippet files into `/etc/caddy/conf.d/`. The base Caddyfile imports them via `import /etc/caddy/conf.d/*.caddy`.

## IMPORTANT: Exposing Services

Any new service that needs to be reachable from the internet MUST have a `.caddy` route snippet in `/etc/caddy/conf.d/`. See `services/CLAUDE.md` for the pattern.

## Usage

```bash
~/attlas/base-setup/setup.sh
```

At the end, it prompts whether to also install services (ttyd, code-server).
