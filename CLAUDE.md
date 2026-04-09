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

1. **`infra/`** provisions a bare Ubuntu VM with `terraform apply`. A minimal startup script clones this repo onto the VM.
2. **`base-setup/`** is run manually after first SSH. It installs packages, dotfiles, Node.js, Claude Code, and the Caddy gateway. After completion, `https://attlas.uk/` serves the dashboard behind cookie auth.
3. **`services/`** installs browser-accessible services (cloud terminal, cloud VS Code) and registers them with the Caddy gateway.

## IMPORTANT: Exposing Services to the Internet

Caddy is the single entry point for all HTTPS traffic. The base Caddyfile lives in `base-setup/Caddyfile` and imports route snippets from `/etc/caddy/conf.d/*.caddy`.

**To expose a new service to the internet, you MUST:**
1. Add a `.caddy` route snippet file to the `services/` directory
2. Copy it to `/etc/caddy/conf.d/` on the VM
3. Run `sudo systemctl reload caddy`

**Without a Caddy snippet, a service is only accessible on localhost and NOT reachable from the browser.**

## Current Infrastructure

- **GCP project**: petprojects-488115
- **VM**: openclaw-vm, e2-standard-4, Ubuntu 24.04, europe-west1-b
- **Gateway**: Caddy with auto-HTTPS, cookie auth (Testuser/password123)
- **Domain**: attlas.uk (Cloudflare DNS → static IP)

## Git Identity

- Name: commonlisp6
- Email: gcp.vm.clawde@me.com
