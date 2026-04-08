# Attlas

Infrastructure and services mono-repo.

## Repo structure

```
attlas/
├── infra/                    # Terraform setups (one folder per environment)
│   └── singlevm-setup/      # Single GCP VM: openclaw-vm, europe-west1-b
│       ├── *.tf              # Terraform config
│       └── gateway/          # Caddy reverse proxy (Caddyfile + setup.sh)
└── services/                 # Web services (cloud vscode, web terminal, etc.)
```

## Current infrastructure

- **GCP project**: petprojects-488115
- **VM**: openclaw-vm, e2-standard-4, Ubuntu 24.04, europe-west1-b
- **Gateway**: Caddy with auto-HTTPS via sslip.io, basic auth
- **Domain**: {static-ip}.sslip.io (no custom domain yet)

## How to deploy from scratch

1. Infra: `cd infra/singlevm-setup && terraform init && terraform apply`
2. Gateway: `sudo infra/singlevm-setup/gateway/setup.sh`
3. Services will each have their own setup instructions in their folders.

## Auth

- Currently using basic auth (username/password) on all public endpoints
- Plan: replace with Google federation (OAuth2) later

## Git identity

- Name: commonlisp6
- Email: gcp.vm.clawde@me.com
