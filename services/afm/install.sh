#!/usr/bin/env bash
# AFM — web-based file manager for the homelab NAS
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install.sh must run as root." >&2
  exit 1
fi

DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_NAME="afm"
SERVICE_USER="agnostic-user"
BUILD_USER="agnostic-user"
STATE_DIR="/var/lib/$SERVICE_NAME"
FILES_DIR="/home/agnostic-user/afm"
PORT=7695

echo "=== Installing AFM (Attlas File Manager) ==="

# Create state directory (DB) and files directory.
mkdir -p "$STATE_DIR"
chown "$SERVICE_USER:$SERVICE_USER" "$STATE_DIR"
mkdir -p "$FILES_DIR"
chown "$SERVICE_USER:$SERVICE_USER" "$FILES_DIR"

# Build Go binary.
echo "Building $SERVICE_NAME..."
sudo -u "$BUILD_USER" -H env PATH="/usr/local/go/bin:$PATH" bash -c \
  "cd '$DIR/server' && go build -o /tmp/${SERVICE_NAME}-build ."
mv "/tmp/${SERVICE_NAME}-build" "/usr/local/bin/$SERVICE_NAME"
chmod 755 "/usr/local/bin/$SERVICE_NAME"
echo "Binary installed at /usr/local/bin/$SERVICE_NAME"

# Write systemd unit.
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=AFM - Attlas File Manager
After=network.target

[Service]
Type=simple
User=$SERVICE_USER
ExecStart=/usr/local/bin/$SERVICE_NAME
Environment=AFM_PORT=$PORT
Environment=AFM_DB=$STATE_DIR/afm.db
Environment=AFM_FILES=$FILES_DIR
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"
echo "Systemd unit installed and started"

# Deploy Caddy route snippet.
install -d -m 755 /etc/caddy/conf.d
cp "$DIR/afm.caddy" /etc/caddy/conf.d/
systemctl reload caddy
echo "Caddy route deployed"

echo "=== AFM installed: attlas.uk/afm/ (port $PORT) ==="
