#!/usr/bin/env bash
# Uninstall OpenClaw
set -euo pipefail

sudo systemctl disable --now openclaw 2>/dev/null || true
sudo rm -f /etc/systemd/system/openclaw.service
sudo systemctl daemon-reload

if command -v openclaw &>/dev/null; then
  sudo npm uninstall -g openclaw
fi

echo "openclaw uninstalled"
