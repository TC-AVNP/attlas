#!/usr/bin/env bash
# Revista Maria Tennis — tournament bracket manager at rm.attlas.uk
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install.sh must run as root." >&2
  exit 1
fi

DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_NAME="revista-maria"
SERVICE_USER="${SERVICE_USER:-rm-svc}"
BUILD_USER="${BUILD_USER:-agnostic-user}"
STATE_DIR="/var/lib/${SERVICE_NAME}"
PORT=7696

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

# 4. Systemd unit.
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Revista Maria Tennis
After=network.target

[Service]
Type=simple
User=${SERVICE_USER}
ExecStart=/usr/local/bin/${SERVICE_NAME}
Restart=always
RestartSec=5

Environment=RM_PORT=${PORT}
Environment=RM_DB=${STATE_DIR}/rm.db
Environment=RM_ADMIN_PASSPHRASE=rm2026

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"
echo "    Service ${SERVICE_NAME} started on port ${PORT}"

# 5. Caddy site block for rm.attlas.uk.
install -d -m 755 /etc/caddy/sites.d
cp "${DIR}/${SERVICE_NAME}.caddy" /etc/caddy/sites.d/

# 5b. Ensure /etc/caddy/Caddyfile imports sites.d at the top level.
if ! grep -q '^import /etc/caddy/sites.d' /etc/caddy/Caddyfile; then
  echo "    Patching /etc/caddy/Caddyfile to import /etc/caddy/sites.d/*.caddy"
  cp /etc/caddy/Caddyfile /etc/caddy/Caddyfile.bak.$(date +%Y%m%d-%H%M%S)
  TMP_CADDYFILE=$(mktemp)
  {
    echo "# Subdomain site blocks."
    echo "import /etc/caddy/sites.d/*.caddy"
    echo ""
    cat /etc/caddy/Caddyfile
  } > "$TMP_CADDYFILE"
  install -m 644 "$TMP_CADDYFILE" /etc/caddy/Caddyfile
  rm -f "$TMP_CADDYFILE"
fi

# 6. Ensure Cloudflare A record for rm.attlas.uk points to this VM.
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
EXTERNAL_IP=$(curl -sf -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip 2>/dev/null || true)
if [[ -n "$CF_TOKEN" && -n "$EXTERNAL_IP" ]]; then
  CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"
  RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=rm.attlas.uk" \
    -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')")
  if [[ -n "$RECORD_ID" ]]; then
    curl -sf -X PUT "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      -d "{\"type\":\"A\",\"name\":\"rm.attlas.uk\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
    echo "    Cloudflare DNS updated: rm.attlas.uk -> ${EXTERNAL_IP}"
  else
    curl -sf -X POST "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      -d "{\"type\":\"A\",\"name\":\"rm.attlas.uk\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
    echo "    Cloudflare DNS created: rm.attlas.uk -> ${EXTERNAL_IP}"
  fi
else
  echo "    WARNING: skipping Cloudflare DNS (token or IP unavailable)"
  echo "    Create manually: rm.attlas.uk -> <VM IP>"
fi

# 7. Reload Caddy to pick up the new site block.
systemctl reload caddy
echo "    Caddy reloaded"

echo "${SERVICE_NAME} installed -> https://rm.attlas.uk/"
