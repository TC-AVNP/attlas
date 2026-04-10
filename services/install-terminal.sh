#!/usr/bin/env bash
# Cloud terminal (ttyd) — web-based terminal with zsh + dotfiles
#
# Must be invoked as root (typically via services/install.sh from
# base-setup/setup.sh). The ttyd service runs as SERVICE_USER (default
# agnostic-user) so browser shells land in the login user's account.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install-terminal.sh must run as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_USER="${SERVICE_USER:-agnostic-user}"

# Install ttyd
if ! command -v ttyd &>/dev/null; then
  TTYD_VERSION="1.7.7"
  curl -fsSL "https://github.com/tsl0922/ttyd/releases/download/${TTYD_VERSION}/ttyd.x86_64" \
    -o /usr/local/bin/ttyd
  chmod +x /usr/local/bin/ttyd
fi
echo "ttyd: $(ttyd --version 2>&1 | head -1)"

# Create systemd unit
cat > /etc/systemd/system/ttyd.service <<UNIT
[Unit]
Description=ttyd - Web terminal
After=network.target

[Service]
Type=simple
User=${SERVICE_USER}
ExecStart=/usr/local/bin/ttyd --base-path /terminal --port 7681 --writable /usr/bin/zsh
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

# Deploy Caddy route snippet
cp "$SCRIPT_DIR/terminal.caddy" /etc/caddy/conf.d/

# Start
systemctl daemon-reload
systemctl enable --now ttyd

echo "terminal installed -> /terminal (port 7681), running as ${SERVICE_USER}"
