#!/usr/bin/env bash
# Homelab Planner — personal homelab build tracker
#
# Must be invoked as root (typically via services/install.sh).
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install-homelab-planner.sh must run as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_DIR="${SCRIPT_DIR}/homelab-planner"
BUILD_USER="${BUILD_USER:-agnostic-user}"
SERVICE_USER="${SERVICE_USER:-homelab-planner-svc}"
STATE_DIR="/var/lib/homelab-planner"
STATIC_DEST="/usr/local/share/homelab-planner/dist"
BIN_DEST="/usr/local/bin/homelab-planner"
SERVICE_PORT="${HOMELAB_PLANNER_PORT:-7691}"

if [[ ! -d "${SERVICE_DIR}" ]]; then
  echo "ERROR: homelab-planner service directory not found at ${SERVICE_DIR}" >&2
  exit 1
fi

# 1. Create the service user if missing.
if ! getent passwd "${SERVICE_USER}" >/dev/null; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
  echo "Created system user ${SERVICE_USER}"
fi

# 2. Provision state directory.
install -d -o "${SERVICE_USER}" -g "${SERVICE_USER}" -m 700 "${STATE_DIR}"

# 3. Build the React frontend.
BUILD_PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"

echo "Building homelab-planner web frontend..."
sudo -u "${BUILD_USER}" -H env PATH="${BUILD_PATH}" bash -c "
  cd '${SERVICE_DIR}/web' && \
  (npm ci --no-audit --no-fund --prefer-offline 2>/dev/null || npm install --no-audit --no-fund)
"
sudo -u "${BUILD_USER}" -H env PATH="${BUILD_PATH}" bash -c "cd '${SERVICE_DIR}/web' && npm run build"

# 4. Build the Go binary.
echo "Building homelab-planner Go binary..."
sudo -u "${BUILD_USER}" -H env PATH="${BUILD_PATH}" bash -c "
  cd '${SERVICE_DIR}/server' && \
  go mod tidy && \
  go build -o /tmp/homelab-planner-build ./cmd/homelab-planner
"

# 5. Install built artifacts.
install -m 755 /tmp/homelab-planner-build "${BIN_DEST}"
rm -f /tmp/homelab-planner-build

rm -rf "${STATIC_DEST}"
install -d -m 755 "$(dirname "${STATIC_DEST}")"
cp -r "${SERVICE_DIR}/web/dist" "${STATIC_DEST}"
echo "Installed binary -> ${BIN_DEST}"
echo "Installed static files -> ${STATIC_DEST}"

# 6. Write the systemd unit.
cat > /etc/systemd/system/homelab-planner.service <<UNIT
[Unit]
Description=Homelab Planner — personal homelab build tracker
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
Environment=HOMELAB_PLANNER_DB=${STATE_DIR}/homelab-planner.db
Environment=HOMELAB_PLANNER_PORT=${SERVICE_PORT}
Environment=HOMELAB_PLANNER_STATIC_DIR=${STATIC_DEST}
ExecStart=${BIN_DEST} serve
Restart=always
RestartSec=5
StateDirectory=homelab-planner
StateDirectoryMode=0700

[Install]
WantedBy=multi-user.target
UNIT

# 7. Start the service.
systemctl daemon-reload
systemctl enable homelab-planner
systemctl restart homelab-planner

# 8. Deploy the Caddy route snippet.
cp "${SERVICE_DIR}/homelab-planner.caddy" /etc/caddy/conf.d/

echo "homelab-planner installed -> /homelab-planner (port ${SERVICE_PORT}), running as ${SERVICE_USER}"
