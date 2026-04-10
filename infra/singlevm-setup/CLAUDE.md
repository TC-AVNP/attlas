# singlevm-setup

Terraform config for a single GCP VM with static IP, firewall rules, and Secret Manager IAM bindings.

## Resources Managed

- `google_compute_address.vm_static_ip` — static external IP
- `google_compute_instance.vm` — the VM (simple-zombie, e2-standard-4, Ubuntu 24.04)
- `google_compute_firewall.allow_ssh_iap` — SSH via IAP only (port 22, source 35.235.240.0/20)
- `google_compute_firewall.allow_https` — HTTP/HTTPS from anywhere (ports 80, 443)
- `google_secret_manager_secret_iam_member.vm_access` — IAM bindings for all 4 secrets (github-pat, cloudflare-dns-token, attlas-server-config, openclaw-config)

## Variables

- `attlas_repo` — GitHub HTTPS URL for this repo (used by startup script to clone)

## Instance Metadata

- `enable-oslogin = "TRUE"` — SSH access is tied to IAM identity, so
  `gcloud compute ssh` no longer auto-creates per-laptop phantom users
  matching each operator's local username. Everyone lands in a stable
  IAM-derived account and `sudo -iu agnostic-user` to become the
  service owner.

## Startup Script

The VM runs `startup.sh` on every boot (via `metadata_startup_script`). Responsibilities:
1. Installs `git` (and only `git`) if missing. Everything else is deferred to `base-setup/setup.sh`.
2. Creates the three non-root accounts:
   - `agnostic-user` — login user with NOPASSWD sudo, owns `/home/agnostic-user/iapetus/{attlas,dotfiels}`, backs ttyd/code-server.
   - `alive-svc` — `nologin` system user running `alive-server.service` with state under `/var/lib/alive-server/`.
   - `openclaw-svc` — `nologin` system user running `openclaw-gateway.service` with state under `/var/lib/openclaw/`.
3. Clones this repo to `/home/agnostic-user/iapetus/attlas` using a PAT from GCP Secret Manager (`github-pat`). `iapetus` (Atlas's father) is the parent directory convention used across all machines.

Everything else (packages, dotfiles, alive-server build, Caddy, services) is handled by `base-setup/setup.sh`, which an operator runs once after the first boot via `sudo bash ...`.

## Prerequisites

Before first `terraform apply`:
1. `gcloud auth login` and `gcloud auth application-default login`
2. Secrets must exist in GCP Secret Manager: `github-pat`, `cloudflare-dns-token`, `attlas-server-config`, `openclaw-config`

## State

Terraform state is committed to git. See README.md for how to use on a new machine.
