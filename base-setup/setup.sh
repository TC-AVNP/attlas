#!/usr/bin/env bash
# Base setup — run ONCE after the first boot of a fresh VM.
# Must be invoked as root (e.g. `sudo bash setup.sh`).
#
# This script assumes startup.sh has already:
#   - Created the login user `agnostic-user` with NOPASSWD sudo
#   - Created the system users `alive-svc` and `openclaw-svc`
#   - Cloned attlas to /home/agnostic-user/iapetus/attlas
#
# Responsibilities of this script:
#   1. Install system packages (zsh, tmux, node, caddy, go, hugo, etc.)
#   2. Clone the dotfiels repo into /home/agnostic-user/iapetus/dotfiels
#      and run its install.sh as agnostic-user (wires home symlinks and
#      installs the system-level dotfiles-sync service).
#   3. Build the alive-server Go binary as agnostic-user.
#   4. Provision /var/lib/alive-server/ and write the OAuth config there.
#   5. Install alive-server.service running as `alive-svc` with the state
#      directory, HOME, ATTLAS_DIR and CLAUDE_JSON_PATH set correctly.
#   6. Install Caddy and drop the base Caddyfile.
#   7. Point Cloudflare DNS at the current VM IP.
#   8. Prompt to install browser services (ttyd, code-server, openclaw,
#      diary), all of which run as `agnostic-user` or `openclaw-svc`.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: setup.sh must run as root. Use 'sudo bash setup.sh'." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

AGNOSTIC_USER="agnostic-user"
AGNOSTIC_HOME="/home/${AGNOSTIC_USER}"
WORKSPACE_DIR="${AGNOSTIC_HOME}/iapetus"
ATTLAS_DIR="${WORKSPACE_DIR}/attlas"
DOTFIELS_DIR="${WORKSPACE_DIR}/dotfiels"
ALIVE_STATE_DIR="/var/lib/alive-server"

echo "=== Base setup starting (running as root) ==="

# Guard: startup.sh must have run.
if ! id "${AGNOSTIC_USER}" &>/dev/null; then
  echo "ERROR: ${AGNOSTIC_USER} does not exist. Did startup.sh run?" >&2
  exit 1
fi
if [[ ! -d "${ATTLAS_DIR}" ]]; then
  echo "ERROR: ${ATTLAS_DIR} does not exist. Did startup.sh run?" >&2
  exit 1
fi

# 1. Base packages
echo "Installing base packages..."
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq zsh tmux python3 curl git build-essential jq whois

# 2. Node.js 24
if ! command -v node &>/dev/null || [[ "$(node -v)" != v24* ]]; then
  echo "Installing Node.js 24..."
  curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
  apt-get install -y -qq nodejs
fi
echo "Node.js: $(node -v)"

# 3. Clone dotfiels (as agnostic-user) into the iapetus workspace
if [[ ! -d "${DOTFIELS_DIR}" ]]; then
  echo "Cloning dotfiels..."
  PAT=$(gcloud secrets versions access latest --secret=github-pat --quiet)
  sudo -u "${AGNOSTIC_USER}" git clone \
    "https://${PAT}@github.com/TC-AVNP/dotfiels.git" "${DOTFIELS_DIR}"
fi

# 4. Run dotfiles installer AS agnostic-user (populates ${AGNOSTIC_HOME}
#    symlinks, installs the system-level dotfiles-sync.timer via sudo).
echo "Running dotfiles installer as ${AGNOSTIC_USER}..."
sudo -u "${AGNOSTIC_USER}" bash "${DOTFIELS_DIR}/install.sh"

# 5. Set zsh as default shell for agnostic-user
chsh -s /usr/bin/zsh "${AGNOSTIC_USER}"
echo "Default shell set to zsh for ${AGNOSTIC_USER}"

# 5b. Mark the iapetus workspace as safe for every user. Without this,
#     git refuses to query a repo from a process whose euid doesn't
#     match the on-disk owner (notably alive-svc reading the dotfiels
#     checkout for the dashboard's F1 panel).
echo "Adding iapetus repos to system-wide git safe.directory..."
git config --system --replace-all safe.directory "${DOTFIELS_DIR}"
git config --system --add          safe.directory "${ATTLAS_DIR}"

# 6. Install Claude Code (global npm, available to any user)
if ! command -v claude &>/dev/null; then
  echo "Installing Claude Code..."
  npm install -g @anthropic-ai/claude-code
fi

# 7. Install Go
if ! command -v go &>/dev/null; then
  echo "Installing Go..."
  curl -fsSL "https://go.dev/dl/go1.22.5.linux-amd64.tar.gz" | tar -C /usr/local -xzf -
  echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
fi
export PATH=$PATH:/usr/local/go/bin
echo "Go: $(go version)"

# 8. Build alive-server (Go binary) as agnostic-user so the on-disk
#    permissions match the rest of the repo.
echo "Building alive-server..."
sudo -u "${AGNOSTIC_USER}" bash -c "export PATH=\$PATH:/usr/local/go/bin && cd '${ATTLAS_DIR}/services/alive-server' && go build -o attlas-server ./cmd/attlas-server"
echo "alive-server built"

# 9. Provision /var/lib/alive-server/ and seed the OAuth config from
#    Secret Manager. The service itself will have StateDirectory= set so
#    systemd also enforces ownership, but we pre-populate here because
#    the config file must exist before the first service start.
echo "Provisioning ${ALIVE_STATE_DIR}..."
install -d -o alive-svc -g alive-svc -m 700 "${ALIVE_STATE_DIR}"
gcloud secrets versions access latest --secret=attlas-server-config --quiet \
  > "${ALIVE_STATE_DIR}/.attlas-server-config.json"
chown alive-svc:alive-svc "${ALIVE_STATE_DIR}/.attlas-server-config.json"
chmod 600 "${ALIVE_STATE_DIR}/.attlas-server-config.json"

# 10. Install alive-server.service
#     - Runs as the alive-svc system user
#     - HOME=${ALIVE_STATE_DIR} so os.UserHomeDir() resolves config/secret paths
#     - ATTLAS_DIR overrides the baked-in $HOME/attlas assumption
echo "Installing alive-server.service..."
cat > /etc/systemd/system/alive-server.service <<UNIT
[Unit]
Description=Attlas VM Dashboard
After=network.target

[Service]
Type=simple
User=alive-svc
Group=alive-svc
WorkingDirectory=${ATTLAS_DIR}/services/alive-server
ExecStart=${ATTLAS_DIR}/services/alive-server/attlas-server
Restart=always
RestartSec=5
StateDirectory=alive-server
StateDirectoryMode=0700
Environment=HOME=${ALIVE_STATE_DIR}
Environment=ATTLAS_DIR=${ATTLAS_DIR}

[Install]
WantedBy=multi-user.target
UNIT

# Sudoers drop-in: allow alive-svc to invoke `claude` as agnostic-user.
# This is what makes the dashboard's "Login to Claude Code" button write
# to /home/agnostic-user/.claude.json instead of alive-svc's home, so
# the same login is visible from the /terminal browser shell.
CLAUDE_BIN="$(command -v claude || true)"
if [[ -n "${CLAUDE_BIN}" ]]; then
  echo "Installing sudoers drop-in for alive-svc → claude (path: ${CLAUDE_BIN})..."
  cat > /etc/sudoers.d/alive-svc-claude <<SUDOERS
# Auto-generated by base-setup/setup.sh — do not edit.
# Allows the dashboard (running as alive-svc) to invoke the Claude Code CLI
# as agnostic-user, both for the login flow and for the "is logged in"
# status check. Tightly scoped to the claude binary.
Defaults:alive-svc env_keep += "BROWSER TERM COLUMNS LINES"
alive-svc ALL=(agnostic-user) NOPASSWD: ${CLAUDE_BIN}
SUDOERS
  chmod 440 /etc/sudoers.d/alive-svc-claude
  visudo -c -f /etc/sudoers.d/alive-svc-claude
else
  echo "WARNING: claude binary not found on PATH, skipping sudoers drop-in"
  echo "         (dashboard Claude login will be unavailable)"
fi

# Sudoers drop-in: allow alive-svc to install / uninstall services and
# reload caddy. Required because services/*/install.sh and
# services/*/uninstall.sh scripts write to /etc/systemd/system/ and
# /etc/caddy/conf.d/, and need to reload caddy after dropping new route
# snippets. Without this drop-in the dashboard's Install / Uninstall
# buttons fail with a sudo password prompt.
#
# The wildcard scope (services/*/install.sh, services/*/uninstall.sh)
# is intentional: it's automatically extended whenever a new service
# folder is added under services/, with no sudoers churn. agnostic-user
# already has full NOPASSWD sudo, so this doesn't widen the trust
# boundary in practice.
echo "Installing sudoers drop-in for alive-svc → service install/uninstall..."
cat > /etc/sudoers.d/alive-svc-services <<SUDOERS
# Auto-generated by base-setup/setup.sh — do not edit.
alive-svc ALL=(root) NOPASSWD: /bin/bash ${ATTLAS_DIR}/services/*/install.sh
alive-svc ALL=(root) NOPASSWD: /bin/bash ${ATTLAS_DIR}/services/*/uninstall.sh
alive-svc ALL=(root) NOPASSWD: /usr/bin/systemctl reload caddy, /bin/systemctl reload caddy
SUDOERS
chmod 440 /etc/sudoers.d/alive-svc-services
visudo -c -f /etc/sudoers.d/alive-svc-services

# Sudoers drop-in: allow alive-svc to trigger the dotfiles-sync oneshot
# via the dashboard's [sync now] button. Tightly scoped to the systemd
# start verb on exactly that unit.
echo "Installing sudoers drop-in for alive-svc → dotfiles-sync..."
cat > /etc/sudoers.d/alive-svc-dotfiles-sync <<SUDOERS
# Auto-generated by base-setup/setup.sh — do not edit.
alive-svc ALL=(root) NOPASSWD: /usr/bin/systemctl start dotfiles-sync.service, /bin/systemctl start dotfiles-sync.service
SUDOERS
chmod 440 /etc/sudoers.d/alive-svc-dotfiles-sync
visudo -c -f /etc/sudoers.d/alive-svc-dotfiles-sync

# Sudoers drop-in: allow alive-svc to invoke the openclaw CLI as the
# openclaw-svc user. Used by the /api/services/openclaw detail endpoint
# to fetch runtime status + usage counters via
# `openclaw status --all --json`. HOME is preserved so openclaw finds
# its state under /var/lib/openclaw/.openclaw/.
echo "Installing sudoers drop-in for alive-svc → openclaw status..."
cat > /etc/sudoers.d/alive-svc-openclaw <<SUDOERS
# Auto-generated by base-setup/setup.sh — do not edit.
Defaults:alive-svc env_keep += "HOME"
alive-svc ALL=(openclaw-svc) NOPASSWD: /usr/bin/openclaw
SUDOERS
chmod 440 /etc/sudoers.d/alive-svc-openclaw
visudo -c -f /etc/sudoers.d/alive-svc-openclaw

systemctl daemon-reload
systemctl enable --now alive-server

# 11. Install Caddy
if ! command -v caddy &>/dev/null; then
  echo "Installing Caddy..."
  apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -qq
  apt-get install -y -qq caddy
fi

# 12. Update Cloudflare DNS to point attlas.uk at the current VM IP
EXTERNAL_IP=$(curl -sf -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip)
echo "External IP: ${EXTERNAL_IP}"
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
if [[ -n "$CF_TOKEN" ]]; then
  CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"
  RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=attlas.uk" \
    -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; print(json.load(sys.stdin)['result'][0]['id'])")
  curl -sf -X PUT "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
    -H "Authorization: Bearer ${CF_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"type\":\"A\",\"name\":\"attlas.uk\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
  echo "Cloudflare DNS updated: attlas.uk -> ${EXTERNAL_IP}"
else
  echo "WARNING: cloudflare-dns-token not found in Secret Manager, skipping DNS update"
fi

# 13. Deploy base Caddyfile
CADDY_DOMAIN="attlas.uk"
cp "${SCRIPT_DIR}/Caddyfile" /etc/caddy/Caddyfile
mkdir -p /etc/caddy/conf.d
mkdir -p /etc/systemd/system/caddy.service.d
cat > /etc/systemd/system/caddy.service.d/override.conf <<OVERRIDE
[Service]
Environment=CADDY_DOMAIN=${CADDY_DOMAIN}
OVERRIDE

systemctl daemon-reload
systemctl enable --now caddy
systemctl restart caddy

# 14. Verify dashboard is reachable
echo ""
echo "Verifying dashboard is reachable..."
sleep 5
if curl -sf "http://localhost:3000/api/status" -o /dev/null; then
  echo "OK: alive-server responding on localhost:3000"
else
  echo "FAILED: alive-server not responding"
  echo "Check logs: journalctl -u alive-server --no-pager -n 30"
fi

echo ""
echo "=== Base setup complete ==="
echo ""
echo "NOTE: Run 'sudo -iu ${AGNOSTIC_USER}' then 'claude' to log in to Claude Code."
echo ""

# 15. Offer to install browser services.
if [ -t 0 ]; then
  read -p "Install services now? (y/n) " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    bash "${SCRIPT_DIR}/../services/install.sh"
  fi
else
  echo "Non-interactive mode — skipping services prompt."
  echo "Run sudo bash ${ATTLAS_DIR}/services/install.sh to install services."
fi
