#!/usr/bin/env bash
# Uninstall Munch service (preserves data)
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall.sh must run as root." >&2
  exit 1
fi

SERVICE_NAME="munch"
CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"

echo "==> Uninstalling ${SERVICE_NAME}"

# 1. Stop and disable systemd unit.
systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload
echo "    Systemd unit removed"

# 2. Remove binary.
rm -f "/usr/local/bin/${SERVICE_NAME}"
echo "    Binary removed"

# 3. Remove Caddy snippet.
rm -f "/etc/caddy/sites.d/${SERVICE_NAME}.caddy"
echo "    Caddy snippet removed"

# 4. Delete Cloudflare DNS record.
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
if [[ -n "$CF_TOKEN" ]]; then
  RECORD_NAME="munch.attlas.uk"
  RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=${RECORD_NAME}" \
    -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')" 2>/dev/null || true)
  if [[ -n "$RECORD_ID" ]]; then
    curl -sf -X DELETE "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" > /dev/null
    echo "    DNS record deleted"
  fi
fi

echo "    NOTE: State directory /var/lib/${SERVICE_NAME} and user munch-svc preserved."
echo "    Run: sudo systemctl reload caddy"
echo "==> ${SERVICE_NAME} uninstalled"
