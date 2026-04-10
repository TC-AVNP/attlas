#!/usr/bin/env bash
# Startup script — runs as root on every boot (via metadata_startup_script).
# Creates the service accounts and clones the attlas repo. All idempotent.
#
# User model:
#   - agnostic-user   login user, NOPASSWD sudo, backs ttyd/code-server and
#                     owns /home/agnostic-user/iapetus/{attlas,dotfiels}.
#   - alive-svc       system user, nologin, runs alive-server.service with
#                     state under /var/lib/alive-server/.
#   - openclaw-svc    system user, nologin, runs openclaw-gateway.service
#                     with state under /var/lib/openclaw/.
#
# Anything that needs packages beyond git (node, caddy, go, hugo, etc.) or
# needs to build and wire up services is deferred to base-setup/setup.sh,
# which an operator runs once after the first boot.
set -euo pipefail
exec > >(tee -a /var/log/startup-script.log) 2>&1
echo "=== startup.sh running at $(date) ==="

ATTLAS_REPO="${attlas_repo}"
AGNOSTIC_USER="agnostic-user"
AGNOSTIC_HOME="/home/$${AGNOSTIC_USER}"
WORKSPACE_DIR="$${AGNOSTIC_HOME}/iapetus"
ATTLAS_DIR="$${WORKSPACE_DIR}/attlas"

# 1. Install git if missing (everything else is base-setup's job)
if ! command -v git &>/dev/null; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq git curl
fi

# 2. Create the agnostic login user
if ! id "$${AGNOSTIC_USER}" &>/dev/null; then
  echo "Creating login user $${AGNOSTIC_USER}..."
  useradd -m -s /bin/bash "$${AGNOSTIC_USER}"
  usermod -aG sudo "$${AGNOSTIC_USER}"
  echo "$${AGNOSTIC_USER} ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/$${AGNOSTIC_USER}
  chmod 440 /etc/sudoers.d/$${AGNOSTIC_USER}
fi

# Ensure agnostic-user's home is traversable by services that need to read
# repo files under /home/agnostic-user/iapetus/attlas/.
chmod 755 "$${AGNOSTIC_HOME}"

# 3. Create system users for long-running services (nologin, no home).
#    systemd's StateDirectory= will provision /var/lib/<svc>/ on first start.
for svc_user in alive-svc openclaw-svc; do
  if ! id "$${svc_user}" &>/dev/null; then
    echo "Creating system user $${svc_user}..."
    useradd --system --no-create-home --shell /usr/sbin/nologin "$${svc_user}"
  fi
done

# 4. Clone the attlas repo into the iapetus workspace (as agnostic-user).
sudo -u "$${AGNOSTIC_USER}" mkdir -p "$${WORKSPACE_DIR}"

if [ ! -d "$${ATTLAS_DIR}" ]; then
  echo "Cloning attlas repo..."
  PAT=$(gcloud secrets versions access latest --secret=github-pat --quiet 2>/dev/null || true)
  if [ -z "$${PAT}" ]; then
    echo "ERROR: Could not fetch github-pat from Secret Manager. Skipping clone."
  else
    REPO_WITH_PAT=$(echo "$${ATTLAS_REPO}" | sed "s|https://|https://$${PAT}@|")
    sudo -u "$${AGNOSTIC_USER}" git clone "$${REPO_WITH_PAT}" "$${ATTLAS_DIR}"
    echo "attlas repo cloned to $${ATTLAS_DIR}"
  fi
else
  echo "attlas repo already exists at $${ATTLAS_DIR}"
fi

echo "=== startup.sh finished at $(date) ==="
echo ""
echo "Next step: SSH in and run sudo bash $${ATTLAS_DIR}/base-setup/setup.sh"
