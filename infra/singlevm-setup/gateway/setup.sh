#!/usr/bin/env bash
# Install and configure Caddy as a reverse proxy with HTTPS + basic auth.
# Run as root: sudo ./setup.sh
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "Error: run as root (sudo ./setup.sh)"
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# --- Install Caddy ---
if ! command -v caddy &>/dev/null; then
  echo "Installing Caddy..."
  apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -qq
  apt-get install -y -qq caddy
fi

# --- Resolve domain from VM external IP ---
EXTERNAL_IP=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip)
CADDY_DOMAIN="${EXTERNAL_IP}.sslip.io"
echo "Domain: ${CADDY_DOMAIN}"

# --- Deploy Caddyfile ---
cp "$SCRIPT_DIR/Caddyfile" /etc/caddy/Caddyfile

# Set the domain via environment override for systemd
mkdir -p /etc/systemd/system/caddy.service.d
cat > /etc/systemd/system/caddy.service.d/override.conf <<EOF
[Service]
Environment=CADDY_DOMAIN=${CADDY_DOMAIN}
EOF

# --- Start ---
systemctl daemon-reload
systemctl enable caddy
systemctl restart caddy

echo ""
echo "Caddy is running at https://${CADDY_DOMAIN}"
echo "Login: Testuser / password123"
