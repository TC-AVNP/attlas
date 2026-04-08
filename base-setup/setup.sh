#!/usr/bin/env bash
# Base setup — run once after first SSH into a fresh VM.
# Installs packages, dotfiles, Node.js, Claude Code, and Caddy gateway.
set -euo pipefail

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

# 5. Set zsh as default shell + enable lingering for user services
sudo chsh -s /usr/bin/zsh "$(whoami)"
sudo loginctl enable-linger "$(whoami)"
echo "Default shell set to zsh, user linger enabled"

# 6. Install Claude Code
if ! command -v claude &>/dev/null; then
  echo "Installing Claude Code..."
  sudo npm install -g @anthropic-ai/claude-code
fi

# 7. Install Caddy
if ! command -v caddy &>/dev/null; then
  echo "Installing Caddy..."
  sudo apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
  sudo apt-get update -qq
  sudo apt-get install -y -qq caddy
fi

# 8. Start alive-server (VM dashboard)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
sudo tee /etc/systemd/system/alive-server.service > /dev/null <<'UNIT_EOF'
[Unit]
Description=Attlas VM Dashboard
After=network.target

[Service]
Type=simple
User=PLACEHOLDER_USER
WorkingDirectory=PLACEHOLDER_WORKDIR
ExecStart=/usr/bin/python3 PLACEHOLDER_WORKDIR/server.py
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT_EOF
sudo sed -i "s|PLACEHOLDER_USER|$(whoami)|g" /etc/systemd/system/alive-server.service
sudo sed -i "s|PLACEHOLDER_WORKDIR|$SCRIPT_DIR/alive-server|g" /etc/systemd/system/alive-server.service

sudo systemctl daemon-reload
sudo systemctl enable --now alive-server

# 9. Deploy base Caddyfile
EXTERNAL_IP=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip)
CADDY_DOMAIN="${EXTERNAL_IP}.sslip.io"

sudo cp "$SCRIPT_DIR/Caddyfile" /etc/caddy/Caddyfile
sudo mkdir -p /etc/caddy/conf.d
sudo mkdir -p /etc/systemd/system/caddy.service.d
echo "[Service]
Environment=CADDY_DOMAIN=${CADDY_DOMAIN}" | sudo tee /etc/systemd/system/caddy.service.d/override.conf > /dev/null

sudo systemctl daemon-reload
sudo systemctl enable --now caddy
sudo systemctl restart caddy

# 10. Verify dashboard is reachable
echo ""
echo "Verifying dashboard is reachable..."
sleep 5  # give Caddy time to obtain TLS cert
if curl -sf -u Testuser:password123 "https://${CADDY_DOMAIN}/" -o /dev/null; then
  echo "OK: https://${CADDY_DOMAIN}/ is live"
else
  echo "FAILED: Could not reach https://${CADDY_DOMAIN}/"
  echo "Check logs: sudo journalctl -u caddy --no-pager -n 30"
  echo "Also check: sudo journalctl -u alive-server --no-pager -n 30"
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
