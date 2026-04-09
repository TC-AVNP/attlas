#!/usr/bin/env bash
# Cloud VS Code (code-server) — VS Code in the browser
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Install code-server
if ! command -v code-server &>/dev/null; then
  curl -fsSL https://code-server.dev/install.sh | sh
fi
echo "code-server: $(code-server --version 2>&1 | head -1)"

# Create systemd unit
sudo tee /etc/systemd/system/code-server.service > /dev/null <<EOF
[Unit]
Description=code-server - VS Code in browser
After=network.target

[Service]
Type=simple
User=$(whoami)
ExecStart=/usr/bin/code-server --bind-addr 127.0.0.1:8080 --auth none --disable-telemetry
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Ship default settings (dark theme, disable welcome)
CS_USER_DIR="$HOME/.local/share/code-server/User"
mkdir -p "$CS_USER_DIR"
cat > "$CS_USER_DIR/settings.json" <<'SETTINGS'
{
  "workbench.colorTheme": "Default Dark Modern",
  "workbench.startupEditor": "none"
}
SETTINGS

# Pre-install extensions
code-server --install-extension golang.go 2>/dev/null || true
code-server --install-extension dart-code.flutter 2>/dev/null || true

# Deploy Caddy route snippet
sudo cp "$SCRIPT_DIR/code.caddy" /etc/caddy/conf.d/

# Start
sudo systemctl daemon-reload
sudo systemctl enable --now code-server

echo "code-server installed -> /code (port 8080)"
