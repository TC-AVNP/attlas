#!/usr/bin/env bash
# Uninstall Revista Maria Tennis
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall.sh must run as root." >&2
  exit 1
fi

SERVICE_NAME="revista-maria"

# 1. Stop and disable systemd unit.
if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
  systemctl stop "${SERVICE_NAME}"
fi
if systemctl is-enabled --quiet "${SERVICE_NAME}" 2>/dev/null; then
  systemctl disable "${SERVICE_NAME}"
fi
rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload
echo "Removed systemd unit"

# 2. Remove binary.
rm -f "/usr/local/bin/${SERVICE_NAME}"
echo "Removed binary"

# 3. Remove Caddy site block.
rm -f "/etc/caddy/sites.d/${SERVICE_NAME}.caddy"
echo "Removed Caddy site block"

# 4. Remove Cloudflare DNS record.
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
if [[ -n "$CF_TOKEN" ]]; then
  CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"
  RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=rm.attlas.uk" \
    -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')" 2>/dev/null || true)
  if [[ -n "$RECORD_ID" ]]; then
    curl -sf -X DELETE "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" > /dev/null
    echo "Removed Cloudflare DNS record"
  fi
fi

echo ""
echo "NOTE: State directory /var/lib/${SERVICE_NAME}/ and user rm-svc were NOT removed."
echo "      Remove manually if desired: rm -rf /var/lib/${SERVICE_NAME} && userdel rm-svc"
echo ""
echo "${SERVICE_NAME} uninstalled. Run 'sudo systemctl reload caddy' to apply."
