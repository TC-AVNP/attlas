#!/usr/bin/env bash
# Base setup — run once after first SSH into a fresh VM.
# Installs packages, dotfiles, Node.js, Claude Code, Go, alive-server, and Caddy gateway.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== Base setup starting ==="

# 1. Base packages
echo "Installing base packages..."
sudo apt-get update -qq
sudo apt-get install -y -qq zsh tmux python3 curl git build-essential jq

# 2. Node.js 24
if ! command -v node &>/dev/null || [[ "$(node -v)" != v24* ]]; then
  echo "Installing Node.js 24..."
  curl -fsSL https://deb.nodesource.com/setup_24.x | sudo bash -
  sudo apt-get install -y -qq nodejs
fi
echo "Node.js: $(node -v)"

# 3. Clone dotfiles
if [ ! -d ~/dotfiles ]; then
  echo "Cloning dotfiles..."
  PAT=$(gcloud secrets versions access latest --secret=github-pat --quiet)
  git clone "https://${PAT}@github.com/TC-AVNP/dotfiels.git" ~/dotfiles
fi

# 4. Run dotfiles installer
echo "Running dotfiles installer..."
cd ~/dotfiles && bash install.sh

# 5. Set zsh as default shell
sudo chsh -s /usr/bin/zsh "$(whoami)"
echo "Default shell set to zsh"

# 6. Install Claude Code
if ! command -v claude &>/dev/null; then
  echo "Installing Claude Code..."
  sudo npm install -g @anthropic-ai/claude-code
fi

# 7. Install Go
if ! command -v go &>/dev/null; then
  echo "Installing Go..."
  curl -fsSL "https://go.dev/dl/go1.22.5.linux-amd64.tar.gz" | sudo tar -C /usr/local -xzf -
  echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh > /dev/null
  export PATH=$PATH:/usr/local/go/bin
fi
echo "Go: $(go version)"

# 8. Build alive-server (Go binary)
echo "Building alive-server..."
cd "$SCRIPT_DIR/alive-server"
go build -o attlas-server .
echo "alive-server built"

# 9. Install Caddy
if ! command -v caddy &>/dev/null; then
  echo "Installing Caddy..."
  sudo apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
  sudo apt-get update -qq
  sudo apt-get install -y -qq caddy
fi

# 10. Start alive-server (VM dashboard)
ALIVE_DIR="$SCRIPT_DIR/alive-server"
sudo tee /etc/systemd/system/alive-server.service > /dev/null <<UNIT_EOF
[Unit]
Description=Attlas VM Dashboard
After=network.target

[Service]
Type=simple
User=$(whoami)
WorkingDirectory=${ALIVE_DIR}
ExecStart=${ALIVE_DIR}/attlas-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT_EOF

sudo systemctl daemon-reload
sudo systemctl enable --now alive-server

# 11. Deploy base Caddyfile
CADDY_DOMAIN="attlas.uk"

sudo cp "$SCRIPT_DIR/Caddyfile" /etc/caddy/Caddyfile
sudo mkdir -p /etc/caddy/conf.d
sudo mkdir -p /etc/systemd/system/caddy.service.d
echo "[Service]
Environment=CADDY_DOMAIN=${CADDY_DOMAIN}" | sudo tee /etc/systemd/system/caddy.service.d/override.conf > /dev/null

sudo systemctl daemon-reload
sudo systemctl enable --now caddy
sudo systemctl restart caddy

# 12. Verify dashboard is reachable
echo ""
echo "Verifying dashboard is reachable..."
sleep 5  # give Caddy time to obtain TLS cert
if curl -sf "http://localhost:3000/api/status" -o /dev/null; then
  echo "OK: alive-server responding on localhost:3000"
else
  echo "FAILED: alive-server not responding"
  echo "Check logs: sudo journalctl -u alive-server --no-pager -n 30"
fi

echo ""
echo "=== Base setup complete ==="
echo ""
echo "NOTE: Run 'claude' to log in to Claude Code (requires interactive auth)."
echo ""
if [ -t 0 ]; then
  read -p "Install services now? (y/n) " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    bash "$SCRIPT_DIR/../services/install.sh"
  fi
else
  echo "Non-interactive mode — skipping services prompt."
  echo "Run ~/attlas/services/install.sh to install services."
fi
