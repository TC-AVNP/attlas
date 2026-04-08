#!/usr/bin/env bash
# Minimal startup script — runs as root on every boot.
# Only purpose: ensure the attlas repo is cloned for the VM user.
set -euo pipefail
exec > >(tee -a /var/log/startup-script.log) 2>&1
echo "=== startup.sh running at $(date) ==="

VM_USER="${vm_user}"
ATTLAS_REPO="${attlas_repo}"
HOME_DIR="/home/$${VM_USER}"

# 1. Install git if missing
if ! command -v git &>/dev/null; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq git curl
fi

# 2. Create user if missing
if ! id "$${VM_USER}" &>/dev/null; then
  useradd -m -s /bin/bash "$${VM_USER}"
  usermod -aG sudo "$${VM_USER}"
  echo "$${VM_USER} ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/$${VM_USER}
  chmod 440 /etc/sudoers.d/$${VM_USER}
fi

# 3. Clone attlas repo if not already cloned
if [ ! -d "$${HOME_DIR}/attlas" ]; then
  echo "Cloning attlas repo..."
  # Fetch PAT from Secret Manager
  PAT=$(gcloud secrets versions access latest --secret=github-pat --quiet 2>/dev/null || true)
  if [ -z "$${PAT}" ]; then
    echo "ERROR: Could not fetch github-pat from Secret Manager. Skipping clone."
  else
    REPO_WITH_PAT=$(echo "$${ATTLAS_REPO}" | sed "s|https://|https://$${PAT}@|")
    sudo -u "$${VM_USER}" git clone "$${REPO_WITH_PAT}" "$${HOME_DIR}/attlas"
    echo "attlas repo cloned to $${HOME_DIR}/attlas"
  fi
else
  echo "attlas repo already exists at $${HOME_DIR}/attlas"
fi

echo "=== startup.sh finished at $(date) ==="
