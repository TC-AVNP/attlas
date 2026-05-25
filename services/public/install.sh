#!/usr/bin/env bash
# Public — password-protected download page at public.attlas.uk
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install.sh must run as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WWW_DIR="/var/www/public"

# 1. Install static files.
install -d -o root -g root -m 755 "${WWW_DIR}"
install -m 644 "${SCRIPT_DIR}/index.html" "${WWW_DIR}/index.html"
echo "Installed site -> ${WWW_DIR}"

# 2. Caddy site block for public.attlas.uk.
install -d -m 755 /etc/caddy/sites.d
cp "${SCRIPT_DIR}/public.caddy" /etc/caddy/sites.d/
echo "Caddy site block deployed"

# 3. Ensure Cloudflare A record for public.attlas.uk points to this VM.
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
EXTERNAL_IP=$(curl -sf -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip 2>/dev/null || true)
if [[ -n "$CF_TOKEN" && -n "$EXTERNAL_IP" ]]; then
  CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"
  RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=public.attlas.uk" \
    -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')")
  if [[ -n "$RECORD_ID" ]]; then
    curl -sf -X PUT "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      -d "{\"type\":\"A\",\"name\":\"public.attlas.uk\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
    echo "Cloudflare DNS updated: public.attlas.uk -> ${EXTERNAL_IP}"
  else
    curl -sf -X POST "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      -d "{\"type\":\"A\",\"name\":\"public.attlas.uk\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
    echo "Cloudflare DNS created: public.attlas.uk -> ${EXTERNAL_IP}"
  fi
else
  echo "WARNING: skipping Cloudflare DNS update (cloudflare-dns-token secret or VM IP unavailable)"
  echo "  Create the A record manually: public.attlas.uk -> <VM IP>"
fi

# 4. Reload Caddy.
systemctl reload caddy
echo "public installed -> https://public.attlas.uk/"
