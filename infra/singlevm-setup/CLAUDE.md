# singlevm-setup

Terraform config for a single GCP VM with static IP, firewall rules, and Secret Manager IAM bindings.

## Resources Managed

- `google_compute_address.vm_static_ip` — static external IP
- `google_compute_instance.vm` — the VM (simple-zombie, e2-standard-4, Ubuntu 24.04)
- `google_compute_firewall.allow_ssh_iap` — SSH via IAP only (port 22, source 35.235.240.0/20)
- `google_compute_firewall.allow_https` — HTTP/HTTPS from anywhere (ports 80, 443)
- `google_secret_manager_secret_iam_member.vm_access` — IAM bindings for all 4 secrets (github-pat, cloudflare-dns-token, attlas-server-config, openclaw-config)

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
1. `gcloud auth login` and `gcloud auth application-default login`
2. Secrets must exist in GCP Secret Manager: `github-pat`, `cloudflare-dns-token`, `attlas-server-config`, `openclaw-config`

## State

Terraform state is committed to git. See README.md for how to use on a new machine.
