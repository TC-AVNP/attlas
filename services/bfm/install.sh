#!/usr/bin/env bash
# Brain Fleet Management — Pi cluster control plane at bfm.attlas.uk
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install.sh must run as root." >&2
  exit 1
fi

DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_NAME="bfm"
SERVICE_USER="${SERVICE_USER:-bfm-svc}"
BUILD_USER="${BUILD_USER:-agnostic-user}"
STATE_DIR="/var/lib/${SERVICE_NAME}"
PORT=7698

echo "==> Installing ${SERVICE_NAME}..."

# ── System user ──
if ! id "${SERVICE_USER}" &>/dev/null; then
  useradd --system --shell /usr/sbin/nologin --home-dir "${STATE_DIR}" "${SERVICE_USER}"
  echo "    Created user ${SERVICE_USER}"
fi

# ── State directory ──
mkdir -p "${STATE_DIR}/images" "${STATE_DIR}/playbooks"
chown -R "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}"
chmod 700 "${STATE_DIR}"

# ── Build Go binary ──
echo "    Building Go binary..."
sudo -u "${BUILD_USER}" -H env PATH="/usr/local/go/bin:$PATH" bash -c \
  "cd '${DIR}/server' && go build -o /tmp/${SERVICE_NAME}-build ."
mv "/tmp/${SERVICE_NAME}-build" "/usr/local/bin/${SERVICE_NAME}"
echo "    Binary installed at /usr/local/bin/${SERVICE_NAME}"

# ── OAuth credentials ──
GOOGLE_CLIENT_ID=""
GOOGLE_SECRET=""
if command -v gcloud &>/dev/null; then
  CONFIG=$(gcloud secrets versions access latest --secret=attlas-server-config --quiet 2>/dev/null || true)
  if [ -n "${CONFIG}" ]; then
    GOOGLE_CLIENT_ID=$(echo "${CONFIG}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('google_oauth_client_id',''))" 2>/dev/null || true)
    GOOGLE_SECRET=$(echo "${CONFIG}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('google_oauth_client_secret',''))" 2>/dev/null || true)
  fi
fi

# ── Systemd unit ──
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Brain Fleet Management
After=network.target

[Service]
Type=simple
User=${SERVICE_USER}
ExecStart=/usr/local/bin/${SERVICE_NAME}
Restart=always
RestartSec=5

Environment=BFM_PORT=${PORT}
Environment=BFM_DB=${STATE_DIR}/bfm.db
Environment=BFM_STATE_DIR=${STATE_DIR}
Environment=BFM_ATTLAS_DIR=/home/${BUILD_USER}/iapetus/attlas
Environment=BFM_BASE_URL=https://bfm.attlas.uk
Environment=BFM_ADMIN_EMAIL=condecopedro@gmail.com
Environment=BFM_GOOGLE_CLIENT_ID=${GOOGLE_CLIENT_ID}
Environment=BFM_GOOGLE_SECRET=${GOOGLE_SECRET}
Environment=BFM_LOCAL_BYPASS=1

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"
echo "    Systemd unit installed and started"

# ── Caddy site block ──
install -d -m 755 /etc/caddy/sites.d
cp "${DIR}/${SERVICE_NAME}.caddy" /etc/caddy/sites.d/

# Ensure Caddyfile imports sites.d
CADDYFILE="/etc/caddy/Caddyfile"
if [ -f "${CADDYFILE}" ] && ! grep -q '^import /etc/caddy/sites.d' "${CADDYFILE}"; then
  echo 'import /etc/caddy/sites.d/*.caddy' >> "${CADDYFILE}"
fi

# ── Cloudflare DNS ──
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
ZONE_ID="813c7bfa1c9f2b1a02a60c97f3171fa6"
EXTERNAL_IP=$(curl -sf -H "Metadata-Flavor: Google" \
  http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip 2>/dev/null || true)

if [ -n "${CF_TOKEN}" ] && [ -n "${EXTERNAL_IP}" ]; then
  RECORD_NAME="bfm.attlas.uk"
  EXISTING=$(curl -sf -X GET "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records?type=A&name=${RECORD_NAME}" \
    -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" 2>/dev/null || true)
  RECORD_ID=$(echo "${EXISTING}" | python3 -c "import sys,json; r=json.load(sys.stdin).get('result',[]); print(r[0]['id'] if r else '')" 2>/dev/null || true)

  if [ -n "${RECORD_ID}" ]; then
    curl -sf -X PUT "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      --data "{\"type\":\"A\",\"name\":\"${RECORD_NAME}\",\"content\":\"${EXTERNAL_IP}\",\"ttl\":1,\"proxied\":false}" >/dev/null
    echo "    DNS record updated: ${RECORD_NAME} → ${EXTERNAL_IP}"
  else
    curl -sf -X POST "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      --data "{\"type\":\"A\",\"name\":\"${RECORD_NAME}\",\"content\":\"${EXTERNAL_IP}\",\"ttl\":1,\"proxied\":false}" >/dev/null
    echo "    DNS record created: ${RECORD_NAME} → ${EXTERNAL_IP}"
  fi
fi

systemctl reload caddy
echo "==> ${SERVICE_NAME} installed → https://bfm.attlas.uk/"
