#!/usr/bin/env bash
# Uninstall BFM — preserves state directory
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall.sh must run as root." >&2
  exit 1
fi

SERVICE_NAME="bfm"

echo "==> Uninstalling ${SERVICE_NAME}..."

# Stop and disable
systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload

# Remove binary
rm -f "/usr/local/bin/${SERVICE_NAME}"

# Remove Caddy site
rm -f "/etc/caddy/sites.d/${SERVICE_NAME}.caddy"

# Remove DNS record
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
ZONE_ID="813c7bfa1c9f2b1a02a60c97f3171fa6"
if [ -n "${CF_TOKEN}" ]; then
  RECORD_NAME="bfm.attlas.uk"
  EXISTING=$(curl -sf -X GET "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records?type=A&name=${RECORD_NAME}" \
    -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" 2>/dev/null || true)
  RECORD_ID=$(echo "${EXISTING}" | python3 -c "import sys,json; r=json.load(sys.stdin).get('result',[]); print(r[0]['id'] if r else '')" 2>/dev/null || true)
  if [ -n "${RECORD_ID}" ]; then
    curl -sf -X DELETE "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" >/dev/null
    echo "    DNS record removed"
  fi
fi

echo "    State directory /var/lib/${SERVICE_NAME}/ preserved (contains database)"
echo "    Remember to reload Caddy: systemctl reload caddy"
echo "==> ${SERVICE_NAME} uninstalled"
