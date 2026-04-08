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

### Auth
- Cookie-based login via alive-server (`/login` page)
- Caddy `forward_auth` checks session cookie on every request
- All services protected behind single login

### Claude Code login via dashboard
- PTY helper navigates the full claude login wizard (theme → login method → URL → code → security → trust)
- Code input from browser, auto-submitted to PTY

## What is NOT done

### Phase C: Reproducibility proof
- [ ] `terraform destroy` the current VM
- [ ] `terraform apply` fresh — verify everything works from scratch
- [ ] All services come up, dashboard accessible, Claude login works

### Reliability fixes
- [ ] **Replace alive-server with Flask+Gunicorn** — current `http.server.HTTPServer` is single-threaded. One slow request blocks ALL services (forward_auth depends on it). Use Flask + Gunicorn with 2+ workers.
- [ ] **Revert to standard Caddy** — custom build with `replace-response` plugin is installed but not used. Reinstall from apt.
- [ ] **Test loginctl linger on fresh provision** — needed for OpenClaw user-level systemd to survive SSH disconnect. Added to setup.sh but untested.

### Security fixes
- [ ] **Random session secret** — currently hardcoded as `"attlas-session-secret-change-me"` in server.py. Anyone with repo access can forge session cookies. Generate random secret at install time, store in `~/.attlas-session-secret`.
- [ ] **Rate limiting on login** — no brute-force protection. Add failed-attempt tracking (5 failures in 5 min → 15 min block).
- [ ] **CSRF token on login form** — login form has no CSRF protection. Add per-render random token in hidden field.
- [ ] **Stronger credentials** — `Testuser/password123` hardcoded in git. Move to GCP Secret Manager or generate at install time.

### Future improvements
- [ ] **Google OAuth2** — replace cookie auth with Google federation. Single sign-on, no passwords. Use caddy-security or external auth provider.
- [ ] **Tailscale** — private networking, no public ports needed, identity-based auth, encrypted by default.
- [ ] **Custom domain + DNS-01 certs** — drop port 80 (ACME HTTP challenge), use proper domain with DNS-01 via Cloudflare.
- [ ] **Move Terraform state to GCS bucket** — local state is fragile, can't collaborate.

## Current VM
- **Name**: simple-zombie
- **IP**: 35.195.105.231
- **Domain**: 35-195-105-231.sslip.io
- **Zone**: europe-west1-b
- **Project**: petprojects-488115
- **Login**: Testuser / password123
