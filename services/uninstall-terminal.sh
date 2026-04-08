#!/usr/bin/env bash
# Uninstall Cloud Terminal (ttyd)
set -euo pipefail

sudo systemctl disable --now ttyd 2>/dev/null || true
sudo rm -f /etc/systemd/system/ttyd.service
sudo rm -f /usr/local/bin/ttyd
sudo rm -f /etc/caddy/conf.d/terminal.caddy
sudo systemctl daemon-reload

echo "ttyd uninstalled"
