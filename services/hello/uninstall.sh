#!/usr/bin/env bash
# Uninstall the hello service.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall.sh must run as root." >&2
  exit 1
fi

WWW_DIR="/var/www/hello"

rm -f /etc/caddy/sites.d/hello.caddy
rm -rf "${WWW_DIR}"

# Best-effort Cloudflare DNS cleanup.
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
if [[ -n "$CF_TOKEN" ]]; then
  CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"
  RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=hello.attlas.uk" \
    -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')")
  if [[ -n "$RECORD_ID" ]]; then
    curl -sf -X DELETE "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" > /dev/null
    echo "Cloudflare DNS record deleted: hello.attlas.uk"
  fi
fi

echo "hello uninstalled."
