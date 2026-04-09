# Attlas TODO

## What was done (2026-04-08)

### Infrastructure
- Terraform provisions VM (simple-zombie), static IP, firewall rules (IAP SSH + HTTPS)
- Startup script clones attlas repo via GitHub PAT from GCP Secret Manager
- base-setup/setup.sh installs: packages, Node.js 24, dotfiles, Claude Code, Caddy, alive-server
- services/install.sh provides interactive menu to install ttyd, code-server, OpenClaw

### Services running
- **Dashboard** (`/`) — React app showing VM info, service status, Claude login
- **Cloud Terminal** (`/terminal/`) — ttyd web terminal with zsh + dotfiles
- **Cloud VS Code** (`/code/`) — code-server IDE
- **OpenClaw** (`/openclaw/`) — AI agent dashboard (Telegram, Brave search, Anthropic)
- **Claude Code** — authenticated (max subscription)

### Claude Code login via dashboard
- PTY helper navigates the full claude login wizard (theme → login method → URL → code → security → trust)
- Code input from browser, auto-submitted to PTY

## What was done (2026-04-09)

### Custom domain
- [x] Bought `attlas.uk` on Cloudflare
- [x] Replaced all sslip.io references with attlas.uk
- [x] Caddy auto-obtains TLS cert via HTTP-01 ACME

### Go server rewrite
- [x] Replaced single-threaded Python `http.server` with concurrent Go binary
- [x] Auto-generated session secret (`~/.attlas-session-secret`)
- [x] CSRF tokens on login form (removed with OAuth2)
- [x] Rate limiting on login (removed with OAuth2)

### Google OAuth2
- [x] Replaced username/password auth with Google OAuth2
- [x] Only allowed emails can access (configured in `attlas-server-config` secret)
- [x] Scary "KEEP AWAY" page for unauthorized users
- [x] Profile banner shows logged-in email + logout link
- [x] Allowed emails list shown in dashboard

### Reliability
- [x] OpenClaw moved to system-level systemd (no more `loginctl enable-linger` hack)
- [x] Standard Caddy from apt (no custom builds)

### Code-server config
- [x] Dark theme (`Default Dark Modern`)
- [x] Welcome page disabled
- [x] Go and Flutter extensions pre-installed

### Infrastructure automation
- [x] Cloudflare DNS auto-updates on provision (setup.sh calls Cloudflare API)
- [x] Terraform manages Secret Manager IAM bindings for all secrets (`secrets.tf`)
- [x] Terraform state committed to git (not gitignored)
- [x] OpenClaw heartbeat set to 24h, health check to 60min

### Phase C — Reproducibility proof
- [x] `terraform destroy` → `terraform apply` → zero manual interventions
- [x] All services come up, dashboard accessible, OAuth2 works

## Current VM
- **Name**: simple-zombie
- **IP**: 34.62.185.156
- **Domain**: attlas.uk
- **Zone**: europe-west1-b
- **Project**: petprojects-488115
- **Auth**: Google OAuth2 (condecopedro@gmail.com)
