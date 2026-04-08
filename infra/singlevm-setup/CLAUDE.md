# singlevm-setup

Terraform config for a single GCP VM with static IP and firewall rules.

## Resources Managed

- `google_compute_address.vm_static_ip` — static external IP
- `google_compute_instance.vm` — the VM (openclaw-vm, e2-standard-4, Ubuntu 24.04)
- `google_compute_firewall.allow_ssh` — SSH from anywhere (port 22)
- `google_compute_firewall.allow_http_https` — HTTP/HTTPS from anywhere (ports 80, 443)

## Variables

- `vm_user` — non-root user created by the startup script (default: condecopedro)
- `attlas_repo` — GitHub HTTPS URL for this repo (used by startup script to clone)

## Startup Script

The VM runs `startup.sh` on every boot (via `metadata_startup_script`). It does three things:
1. Installs git if missing
2. Creates the `vm_user` account if missing
3. Clones this repo to `~/attlas` using a PAT from GCP Secret Manager (`github-pat`)

Everything else (packages, dotfiles, services) is handled by `base-setup/` and `services/`, which the user runs manually after SSH.

## Prerequisites

Before first `terraform apply`:
1. `gcloud auth login` and set project
2. Create the `github-pat` secret in GCP Secret Manager (see top-level CLAUDE.md)

## State

Terraform state is local. It is gitignored — if lost, re-import resources.
