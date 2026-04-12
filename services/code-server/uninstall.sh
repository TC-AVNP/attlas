#!/usr/bin/env bash
# Uninstall Cloud VS Code (code-server)
set -euo pipefail

sudo systemctl disable --now code-server 2>/dev/null || true
sudo rm -f /etc/systemd/system/code-server.service
sudo rm -f /etc/caddy/conf.d/code.caddy
sudo systemctl daemon-reload

# Remove code-server binary and config
if command -v code-server &>/dev/null; then
  sudo rm -f /usr/bin/code-server
  sudo rm -rf /usr/lib/code-server
fi

echo "code-server uninstalled"
