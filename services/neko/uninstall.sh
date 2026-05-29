#!/usr/bin/env bash
# Uninstall Neko — remove service, binary, Caddy route, and DNS record
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall.sh must run as root." >&2
  exit 1
fi

SERVICE_NAME="neko"

echo "==> Uninstalling ${SERVICE_NAME}"

# 1. Stop and remove systemd unit.
if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
  systemctl stop "${SERVICE_NAME}"
  echo "    Stopped ${SERVICE_NAME}"
fi
systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload
echo "    Removed systemd unit"

# 2. Remove binary.
rm -f "/usr/local/bin/${SERVICE_NAME}"
echo "    Removed binary"

# 3. Remove Caddy site block.
rm -f "/etc/caddy/sites.d/${SERVICE_NAME}.caddy"
echo "    Removed Caddy site block"

# 4. Remove Cloudflare DNS record.
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
if [[ -n "$CF_TOKEN" ]]; then
  CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"
  RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=neko.attlas.uk" \
    -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')")
  if [[ -n "$RECORD_ID" ]]; then
    curl -sf -X DELETE "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" > /dev/null
    echo "    Cloudflare DNS record removed"
  fi
fi

echo "    NOTE: system user (neko-svc) not removed — delete manually if desired"
echo "    Reload Caddy: systemctl reload caddy"
echo "${SERVICE_NAME} uninstalled"
