#!/usr/bin/env bash
# Splitsies gateway — subdomain-level reverse proxy for splitsies.attlas.uk
#
# This gateway is the Caddy-facing endpoint for everything under
# splitsies.attlas.uk. It routes requests to backend services registered
# in /etc/splitsies-gateway.d/. The splitsies service itself drops its
# route file here on install.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install-splitsies-gateway.sh must run as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GATEWAY_DIR="${SCRIPT_DIR}/splitsies-gateway"
BUILD_USER="${BUILD_USER:-agnostic-user}"
SERVICE_USER="${SERVICE_USER:-splitsies-gw}"
ROUTES_DIR="/etc/splitsies-gateway.d"
BIN_DEST="/usr/local/bin/splitsies-gateway"
GATEWAY_PORT="${SPLITSIES_GATEWAY_PORT:-7700}"

if [[ ! -d "${GATEWAY_DIR}" ]]; then
  echo "ERROR: splitsies-gateway directory not found at ${GATEWAY_DIR}" >&2
  exit 1
fi

# 1. Service user.
if ! getent passwd "${SERVICE_USER}" >/dev/null; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
  echo "Created system user ${SERVICE_USER}"
fi

# 2. Routes directory (world-readable so services can check their config,
#    writable only by root since it controls traffic routing).
install -d -o root -g root -m 755 "${ROUTES_DIR}"

# 3. Build the Go binary as BUILD_USER.
BUILD_PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
echo "Building splitsies-gateway..."
sudo -u "${BUILD_USER}" -H env PATH="${BUILD_PATH}" bash -c "
  cd '${GATEWAY_DIR}' && \
  go mod tidy && \
  go build -o /tmp/splitsies-gateway-build .
"
install -m 755 /tmp/splitsies-gateway-build "${BIN_DEST}"
rm -f /tmp/splitsies-gateway-build
echo "Installed binary -> ${BIN_DEST}"

# 4. Systemd unit.
cat > /etc/systemd/system/splitsies-gateway.service <<UNIT
[Unit]
Description=Splitsies gateway — subdomain reverse proxy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
Environment=SPLITSIES_GATEWAY_PORT=${GATEWAY_PORT}
Environment=SPLITSIES_GATEWAY_ROUTES=${ROUTES_DIR}
ExecStart=${BIN_DEST}
# Send SIGHUP via 'systemctl reload splitsies-gateway' to re-read routes.
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

# 5. Caddy site block for splitsies.attlas.uk.
install -d -m 755 /etc/caddy/sites.d
cp "${GATEWAY_DIR}/splitsies-gateway.caddy" /etc/caddy/sites.d/

# 6. Ensure Cloudflare A record for splitsies.attlas.uk points to this VM.
#    The gateway owns this subdomain's routing, so it also owns the DNS
#    record lifecycle. Idempotent: creates if missing, updates if stale.
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
EXTERNAL_IP=$(curl -sf -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip 2>/dev/null || true)
if [[ -n "$CF_TOKEN" && -n "$EXTERNAL_IP" ]]; then
  CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"
  RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=splitsies.attlas.uk" \
    -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')")
  if [[ -n "$RECORD_ID" ]]; then
    curl -sf -X PUT "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      -d "{\"type\":\"A\",\"name\":\"splitsies.attlas.uk\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
    echo "Cloudflare DNS updated: splitsies.attlas.uk -> ${EXTERNAL_IP}"
  else
    curl -sf -X POST "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      -d "{\"type\":\"A\",\"name\":\"splitsies.attlas.uk\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
    echo "Cloudflare DNS created: splitsies.attlas.uk -> ${EXTERNAL_IP}"
  fi
else
  echo "WARNING: skipping Cloudflare DNS update (cloudflare-dns-token secret or VM IP unavailable)"
  echo "  Create the A record manually: splitsies.attlas.uk -> <VM IP>"
fi

# 7. Start the gateway. Caddy reload is handled by services/install.sh at
#    the end of a batch install so multiple service installs collapse
#    into a single reload.
systemctl daemon-reload
systemctl enable splitsies-gateway
systemctl restart splitsies-gateway

echo "splitsies-gateway installed -> :${GATEWAY_PORT}, routes=${ROUTES_DIR}"
echo "Caddy site block deployed to /etc/caddy/sites.d/splitsies-gateway.caddy"
