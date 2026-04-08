#!/usr/bin/env bash
# Installs services (ttyd, code-server) and registers their Caddy route snippets.
# Run after base-setup/setup.sh has completed.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== Installing services ==="

# 1. Install ttyd
if ! command -v ttyd &>/dev/null; then
  echo "Installing ttyd..."
  TTYD_VERSION="1.7.7"
  sudo curl -fsSL "https://github.com/tsl0922/ttyd/releases/download/${TTYD_VERSION}/ttyd.x86_64" \
    -o /usr/local/bin/ttyd
  sudo chmod +x /usr/local/bin/ttyd
fi
echo "ttyd: $(ttyd --version 2>&1 | head -1)"

# 2. Install code-server
if ! command -v code-server &>/dev/null; then
  echo "Installing code-server..."
  curl -fsSL https://code-server.dev/install.sh | sh
fi
echo "code-server: $(code-server --version 2>&1 | head -1)"

# 3. Create systemd units
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

sudo tee /etc/systemd/system/code-server.service > /dev/null <<EOF
[Unit]
Description=code-server - VS Code in browser
After=network.target

[Service]
Type=simple
User=$(whoami)
ExecStart=/usr/bin/code-server --bind-addr 127.0.0.1:8080 --base-path /code --auth none --disable-telemetry
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# 4. Deploy Caddy route snippets
sudo cp "$SCRIPT_DIR/terminal.caddy" /etc/caddy/conf.d/
sudo cp "$SCRIPT_DIR/code.caddy" /etc/caddy/conf.d/

# 5. Start everything
sudo systemctl daemon-reload
sudo systemctl enable --now ttyd code-server
sudo systemctl reload caddy

echo ""
echo "=== Services installed ==="
echo "  /terminal  -> ttyd (port 7681)"
echo "  /code      -> code-server (port 8080)"
