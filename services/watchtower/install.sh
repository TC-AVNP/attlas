#!/usr/bin/env bash
# Watchtower — user session analytics for the attlas ecosystem
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install.sh must run as root." >&2
  exit 1
fi

DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_NAME="watchtower"
SERVICE_USER="${SERVICE_USER:-watchtower-svc}"
BUILD_USER="${BUILD_USER:-agnostic-user}"
STATE_DIR="/var/lib/${SERVICE_NAME}"
PORT=7702
SUBDOMAIN="watchtower.attlas.uk"

echo "==> Installing ${SERVICE_NAME}"

# 1. System user
if ! id "${SERVICE_USER}" &>/dev/null; then
  useradd --system --shell /usr/sbin/nologin --home-dir "${STATE_DIR}" "${SERVICE_USER}"
  echo "    Created user ${SERVICE_USER}"
fi

# 2. State directory
mkdir -p "${STATE_DIR}"
chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}"
chmod 700 "${STATE_DIR}"

# 3. Build Go binary
echo "    Building Go binary..."
sudo -u "${BUILD_USER}" -H env PATH="/usr/local/go/bin:$PATH" bash -c \
  "cd '${DIR}/server' && go mod tidy && go build -o /tmp/${SERVICE_NAME}-build ."
mv "/tmp/${SERVICE_NAME}-build" "/usr/local/bin/${SERVICE_NAME}"
echo "    Binary installed to /usr/local/bin/${SERVICE_NAME}"

# 4. Systemd unit (no OAuth — auth handled by Caddy forward_auth to alive-server)
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Watchtower — user session analytics
After=network.target

[Service]
Type=simple
User=${SERVICE_USER}
ExecStart=/usr/local/bin/${SERVICE_NAME}
Restart=always
RestartSec=5

Environment=WATCHTOWER_PORT=${PORT}
Environment=WATCHTOWER_DB=${STATE_DIR}/watchtower.db

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"
echo "    Systemd unit installed and started"

# 5. Caddy route snippet (subdomain with forward_auth -> sites.d/)
install -d -m 755 /etc/caddy/sites.d
cp "${DIR}/${SERVICE_NAME}.caddy" /etc/caddy/sites.d/

# Ensure Caddyfile imports sites.d
if ! grep -q 'import /etc/caddy/sites.d/\*.caddy' /etc/caddy/Caddyfile 2>/dev/null; then
  echo 'import /etc/caddy/sites.d/*.caddy' >> /etc/caddy/Caddyfile
fi
echo "    Caddy snippet installed to /etc/caddy/sites.d/${SERVICE_NAME}.caddy"

# 6. Remove old path-based config if it exists
rm -f /etc/caddy/conf.d/${SERVICE_NAME}.caddy
rm -f /etc/attlas-public-paths.d/watchtower.conf

# 7. Upsert Cloudflare DNS A record
CF_TOKEN=""
if command -v gcloud &>/dev/null; then
  CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
fi
if [ -n "${CF_TOKEN}" ]; then
  ZONE_ID="813c7bfa1c9f2b1a02a60c97f3171fa6"
  VM_IP=$(curl -sf -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip 2>/dev/null || true)
  if [ -n "${VM_IP}" ]; then
    EXISTING=$(curl -sf -X GET "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records?type=A&name=${SUBDOMAIN}" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')" 2>/dev/null || true)
    if [ -n "${EXISTING}" ]; then
      curl -sf -X PUT "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records/${EXISTING}" \
        -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
        --data "{\"type\":\"A\",\"name\":\"${SUBDOMAIN}\",\"content\":\"${VM_IP}\",\"proxied\":false}" >/dev/null
      echo "    DNS A record updated for ${SUBDOMAIN} -> ${VM_IP}"
    else
      curl -sf -X POST "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records" \
        -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
        --data "{\"type\":\"A\",\"name\":\"${SUBDOMAIN}\",\"content\":\"${VM_IP}\",\"proxied\":false}" >/dev/null
      echo "    DNS A record created for ${SUBDOMAIN} -> ${VM_IP}"
    fi
  fi
fi

# 8. Reload Caddy
systemctl reload caddy
echo "    Caddy reloaded"

echo ""
echo "${SERVICE_NAME} installed -> https://${SUBDOMAIN} (port ${PORT})"
