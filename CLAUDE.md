# Attlas

Infrastructure and services monorepo for a single GCP VM environment.

## Directory Structure

```
attlas/
├── infra/              # Terraform — provisions VM, network, static IP
│   └── singlevm-setup/
├── base-setup/         # First-SSH setup — packages, dotfiles, Caddy gateway
└── services/           # Browser-accessible services — ttyd, code-server
```

## How It Works

1. **`infra/`** provisions a bare Ubuntu VM with `terraform apply`. Startup script clones this repo. Terraform also manages Secret Manager IAM bindings for all secrets.
2. **`base-setup/`** is run manually after first SSH. It installs packages, dotfiles, Node.js, Go, Claude Code, builds the Go dashboard server, installs Caddy, and auto-updates Cloudflare DNS. After completion, `https://attlas.uk/` serves the dashboard behind Google OAuth2.
3. **`services/`** installs browser-accessible services (cloud terminal, cloud VS Code, OpenClaw) and registers them with the Caddy gateway.

## IMPORTANT: Exposing Services to the Internet

Caddy is the single entry point for all HTTPS traffic. The base Caddyfile lives in `base-setup/Caddyfile` and imports route snippets from `/etc/caddy/conf.d/*.caddy`.

**To expose a new service to the internet, you MUST:**
1. Add a `.caddy` route snippet file to the `services/` directory
2. Copy it to `/etc/caddy/conf.d/` on the VM
3. Run `sudo systemctl reload caddy`

**Without a Caddy snippet, a service is only accessible on localhost and NOT reachable from the browser.**

## Current Infrastructure

- **GCP project**: petprojects-488115
- **VM**: simple-zombie, e2-standard-4, Ubuntu 24.04, europe-west1-b
- **Gateway**: Caddy with auto-HTTPS
- **Auth**: Google OAuth2 (allowed emails configured in `attlas-server-config` secret)
- **Domain**: attlas.uk (Cloudflare DNS, auto-updated on provision)
- **Dashboard**: Go binary (`base-setup/alive-server/main.go`)

## GCP Secret Manager secrets

| Secret | Purpose |
|--------|---------|
| `github-pat` | GitHub PAT for cloning repos |
| `cloudflare-dns-token` | Cloudflare API token for DNS updates |
| `attlas-server-config` | OAuth2 client ID/secret + allowed emails |
| `openclaw-config` | OpenClaw channel tokens and API keys |

## Diary

The `diary/` folder contains one entry per working session. Each entry logs what was done, lessons learned, and the Claude session ID. **Always update the diary at the end of each session.**

## Git Identity

- Name: commonlisp6
- Email: gcp.vm.clawde@me.com
