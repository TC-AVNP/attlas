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
SERVICE_HOME="$(getent passwd "${SERVICE_USER}" | cut -d: -f6)"

if [[ -z "${SERVICE_HOME}" || ! -d "${SERVICE_HOME}" ]]; then
  echo "ERROR: home directory for ${SERVICE_USER} not found." >&2
  exit 1
fi

# Install ttyd
if ! command -v ttyd &>/dev/null; then
  TTYD_VERSION="1.7.7"
  curl -fsSL "https://github.com/tsl0922/ttyd/releases/download/${TTYD_VERSION}/ttyd.x86_64" \
    -o /usr/local/bin/ttyd
  chmod +x /usr/local/bin/ttyd
fi
echo "ttyd: $(ttyd --version 2>&1 | head -1)"

# Create systemd unit
# WorkingDirectory drops the user into the iapetus workspace by default
# when they open /terminal in the browser. The path is hardcoded (not %h)
# because for system units %h resolves to /root regardless of User=.
cat > /etc/systemd/system/ttyd.service <<UNIT
[Unit]
Description=ttyd - Web terminal
After=network.target

[Service]
Type=simple
User=${SERVICE_USER}
WorkingDirectory=${SERVICE_HOME}/iapetus
ExecStart=/usr/local/bin/ttyd --base-path /terminal --port 7681 --writable -t titleFixed=attlas /usr/bin/zsh
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

# Deploy Caddy route snippet
cp "$SCRIPT_DIR/terminal.caddy" /etc/caddy/conf.d/

# Start. Use enable + restart (not enable --now) so re-installs pick up
# unit-file changes — enable --now is a no-op when the service is already
# running and would leave the old process behind.
systemctl daemon-reload
systemctl enable ttyd
systemctl restart ttyd

echo "terminal installed -> /terminal (port 7681), running as ${SERVICE_USER}"
