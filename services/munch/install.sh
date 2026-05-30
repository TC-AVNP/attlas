#!/usr/bin/env bash
# Munch — meal-prep companion: pick a dish, shop, cook, rate
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install.sh must run as root." >&2
  exit 1
fi

DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_NAME="munch"
SERVICE_USER="${SERVICE_USER:-munch-svc}"
BUILD_USER="${BUILD_USER:-agnostic-user}"
STATE_DIR="/var/lib/${SERVICE_NAME}"
PORT=7703

echo "==> Installing ${SERVICE_NAME}"

# 1. System user.
if ! id "${SERVICE_USER}" &>/dev/null; then
  useradd --system --shell /usr/sbin/nologin --home-dir "${STATE_DIR}" "${SERVICE_USER}"
  echo "    Created user ${SERVICE_USER}"
fi

# 2. State directory.
mkdir -p "${STATE_DIR}"
chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}"
chmod 700 "${STATE_DIR}"

# 3. Build Go binary.
echo "    Building Go binary..."
sudo -u "${BUILD_USER}" -H env PATH="/usr/local/go/bin:$PATH" bash -c \
  "cd '${DIR}/server' && go build -o /tmp/${SERVICE_NAME}-build ."
mv "/tmp/${SERVICE_NAME}-build" "/usr/local/bin/${SERVICE_NAME}"
echo "    Installed /usr/local/bin/${SERVICE_NAME}"

# 4. Fetch Anthropic API key from openclaw-config secret.
ANTHROPIC_KEY=$(gcloud secrets versions access latest --secret=openclaw-config --quiet 2>/dev/null \
  | python3 -c "import sys,json; print(json.load(sys.stdin).get('anthropic_api_key',''))" || true)

# 5. Systemd unit.
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Munch — meal-prep companion
After=network.target

[Service]
Type=simple
User=${SERVICE_USER}
ExecStart=/usr/local/bin/${SERVICE_NAME}
Restart=always
RestartSec=5

Environment=MUNCH_PORT=${PORT}
Environment=MUNCH_DB=${STATE_DIR}/munch.db
Environment=MUNCH_ANTHROPIC_KEY=${ANTHROPIC_KEY}

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"
echo "    Service ${SERVICE_NAME} started on port ${PORT}"

# 6. Caddy site block.
install -d -m 755 /etc/caddy/sites.d
cp "${DIR}/${SERVICE_NAME}.caddy" /etc/caddy/sites.d/
echo "    Installed Caddy site block"

# Ensure Caddyfile imports sites.d.
if ! grep -q '^import /etc/caddy/sites.d' /etc/caddy/Caddyfile; then
  echo 'import /etc/caddy/sites.d/*.caddy' >> /etc/caddy/Caddyfile
fi

# 7. Cloudflare DNS.
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
EXTERNAL_IP=$(curl -sf -H "Metadata-Flavor: Google" \
  http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip 2>/dev/null || true)
CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"

if [[ -n "$CF_TOKEN" && -n "$EXTERNAL_IP" ]]; then
  RECORD_NAME="munch.attlas.uk"
  RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=${RECORD_NAME}" \
    -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')")
  if [[ -n "$RECORD_ID" ]]; then
    curl -sf -X PUT "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      -d "{\"type\":\"A\",\"name\":\"${RECORD_NAME}\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
    echo "    DNS updated: ${RECORD_NAME} -> ${EXTERNAL_IP}"
  else
    curl -sf -X POST "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      -d "{\"type\":\"A\",\"name\":\"${RECORD_NAME}\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
    echo "    DNS created: ${RECORD_NAME} -> ${EXTERNAL_IP}"
  fi
fi

# 8. Reload Caddy.
systemctl reload caddy
echo "    Caddy reloaded"

echo "==> ${SERVICE_NAME} installed -> https://munch.attlas.uk/"
