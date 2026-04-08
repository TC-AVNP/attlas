#!/usr/bin/env bash
# Cloud terminal (ttyd) — web-based terminal with zsh + dotfiles
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Install ttyd
if ! command -v ttyd &>/dev/null; then
  TTYD_VERSION="1.7.7"
  sudo curl -fsSL "https://github.com/tsl0922/ttyd/releases/download/${TTYD_VERSION}/ttyd.x86_64" \
    -o /usr/local/bin/ttyd
  sudo chmod +x /usr/local/bin/ttyd
fi
echo "ttyd: $(ttyd --version 2>&1 | head -1)"

# Create systemd unit
sudo tee /etc/systemd/system/ttyd.service > /dev/null <<EOF
[Unit]
Description=ttyd - Web terminal
After=network.target

[Service]
Type=simple
User=$(whoami)
ExecStart=/usr/local/bin/ttyd --base-path /terminal --port 7681 --writable /usr/bin/zsh
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Deploy Caddy route snippet
sudo cp "$SCRIPT_DIR/terminal.caddy" /etc/caddy/conf.d/

# Start
sudo systemctl daemon-reload
sudo systemctl enable --now ttyd

echo "terminal installed -> /terminal (port 7681)"
