#!/usr/bin/env bash
# Cloud VS Code (code-server) — VS Code in the browser
#
# Must be invoked as root. code-server runs as SERVICE_USER (default
# agnostic-user). User-specific setup (settings, extensions) is performed
# via `sudo -u ${SERVICE_USER}` so the files land in the right home.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install-code-server.sh must run as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_USER="${SERVICE_USER:-agnostic-user}"
SERVICE_HOME="$(getent passwd "${SERVICE_USER}" | cut -d: -f6)"

if [[ -z "${SERVICE_HOME}" || ! -d "${SERVICE_HOME}" ]]; then
  echo "ERROR: home directory for ${SERVICE_USER} not found." >&2
  exit 1
fi

# Install code-server
if ! command -v code-server &>/dev/null; then
  curl -fsSL https://code-server.dev/install.sh | sh
fi
echo "code-server: $(code-server --version 2>&1 | head -1)"

# Create systemd unit
# WorkingDirectory opens the iapetus workspace as the default folder in
# code-server. The path is hardcoded (not %h) because for system units
# %h resolves to /root regardless of User=.
cat > /etc/systemd/system/code-server.service <<UNIT
[Unit]
Description=code-server - VS Code in browser
After=network.target

[Service]
Type=simple
User=${SERVICE_USER}
WorkingDirectory=${SERVICE_HOME}/iapetus
ExecStart=/usr/bin/code-server --bind-addr 127.0.0.1:8080 --auth none --disable-telemetry
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

# Ship default settings (dark theme, disable welcome) as SERVICE_USER
CS_USER_DIR="${SERVICE_HOME}/.local/share/code-server/User"
sudo -u "${SERVICE_USER}" mkdir -p "${CS_USER_DIR}"
sudo -u "${SERVICE_USER}" tee "${CS_USER_DIR}/settings.json" > /dev/null <<'SETTINGS'
{
  "workbench.colorTheme": "Default Dark Modern",
  "workbench.startupEditor": "none"
}
SETTINGS

# Pre-install extensions as SERVICE_USER
sudo -u "${SERVICE_USER}" code-server --install-extension golang.go 2>/dev/null || true
sudo -u "${SERVICE_USER}" code-server --install-extension dart-code.flutter 2>/dev/null || true

# Deploy Caddy route snippet
cp "$SCRIPT_DIR/code.caddy" /etc/caddy/conf.d/

# Start. Use enable + restart (not enable --now) so re-installs pick up
# unit-file changes — enable --now is a no-op when the service is already
# running and would leave the old process behind.
systemctl daemon-reload
systemctl enable code-server
systemctl restart code-server

echo "code-server installed -> /code (port 8080), running as ${SERVICE_USER}"
