#!/usr/bin/env bash
# Splitsies — expense splitting app at splitsies.attlas.uk
#
# Depends on install-splitsies-gateway.sh being run first (creates the
# gateway that routes splitsies.attlas.uk traffic to this backend).
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install-splitsies.sh must run as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SPLITSIES_DIR="${SCRIPT_DIR}/splitsies"
BUILD_USER="${BUILD_USER:-agnostic-user}"
SERVICE_USER="${SERVICE_USER:-splitsies-svc}"
STATE_DIR="/var/lib/splitsies"
STATIC_DEST="/usr/local/share/splitsies/dist"
BIN_DEST="/usr/local/bin/splitsies"
SPLITSIES_PORT="${SPLITSIES_PORT:-7692}"
GATEWAY_ROUTES_DIR="/etc/splitsies-gateway.d"
BASE_URL="${SPLITSIES_BASE_URL:-https://splitsies.attlas.uk}"

if [[ ! -d "${SPLITSIES_DIR}" ]]; then
  echo "ERROR: splitsies directory not found at ${SPLITSIES_DIR}" >&2
  exit 1
fi

# 1. Service user.
if ! getent passwd "${SERVICE_USER}" >/dev/null; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
  echo "Created system user ${SERVICE_USER}"
fi

# 2. State directory.
install -d -o "${SERVICE_USER}" -g "${SERVICE_USER}" -m 700 "${STATE_DIR}"

# 3. Build the React frontend.
BUILD_PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
echo "Building splitsies web frontend..."
sudo -u "${BUILD_USER}" -H env PATH="${BUILD_PATH}" bash -c "
  cd '${SPLITSIES_DIR}/web' && \
  (npm ci --no-audit --no-fund --prefer-offline 2>/dev/null || npm install --no-audit --no-fund)
"
sudo -u "${BUILD_USER}" -H env PATH="${BUILD_PATH}" bash -c "cd '${SPLITSIES_DIR}/web' && npm run build"

# 4. Build the Go binary.
echo "Building splitsies Go binary..."
sudo -u "${BUILD_USER}" -H env PATH="${BUILD_PATH}" bash -c "
  cd '${SPLITSIES_DIR}/server' && \
  go mod tidy && \
  go build -o /tmp/splitsies-build ./cmd/splitsies
"

# 5. Install artifacts.
install -m 755 /tmp/splitsies-build "${BIN_DEST}"
rm -f /tmp/splitsies-build
rm -rf "${STATIC_DEST}"
install -d -m 755 "$(dirname "${STATIC_DEST}")"
cp -r "${SPLITSIES_DIR}/web/dist" "${STATIC_DEST}"
echo "Installed binary -> ${BIN_DEST}"
echo "Installed static files -> ${STATIC_DEST}"

# 6. Fetch config from Secret Manager.
#    Secret is a JSON blob: {
#      "client_id":     "<Google OAuth client ID>",
#      "client_secret": "<Google OAuth client secret>",
#      "initial_admin": "<email to seed as the first admin, only used when users table is empty>"
#    }
GOOGLE_CLIENT_ID=""
GOOGLE_SECRET=""
INITIAL_ADMIN=""
if command -v gcloud >/dev/null 2>&1; then
  SPLITSIES_CONFIG="$(gcloud secrets versions access latest --secret=splitsies-config --quiet 2>/dev/null || true)"
  if [[ -n "${SPLITSIES_CONFIG}" ]]; then
    GOOGLE_CLIENT_ID=$(echo "${SPLITSIES_CONFIG}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('client_id',''))")
    GOOGLE_SECRET=$(echo "${SPLITSIES_CONFIG}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('client_secret',''))")
    INITIAL_ADMIN=$(echo "${SPLITSIES_CONFIG}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('initial_admin',''))")
    echo "Loaded splitsies config from Secret Manager"
  else
    echo "WARNING: splitsies-config secret not found; Google OAuth will not work"
    echo "  Create the secret with: gcloud secrets create splitsies-config --data-file=- <<< '{\"client_id\":\"...\",\"client_secret\":\"...\",\"initial_admin\":\"you@example.com\"}'"
  fi
fi

# 7. Systemd unit.
cat > /etc/systemd/system/splitsies.service <<UNIT
[Unit]
Description=Splitsies — expense splitting app
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
Environment=SPLITSIES_DB=${STATE_DIR}/splitsies.db
Environment=SPLITSIES_PORT=${SPLITSIES_PORT}
Environment=SPLITSIES_STATIC_DIR=${STATIC_DEST}
Environment=SPLITSIES_BASE_URL=${BASE_URL}
Environment=SPLITSIES_GOOGLE_CLIENT_ID=${GOOGLE_CLIENT_ID}
Environment=SPLITSIES_GOOGLE_SECRET=${GOOGLE_SECRET}
Environment=SPLITSIES_INITIAL_ADMIN=${INITIAL_ADMIN}
# Loopback requests without X-Forwarded-For are trusted as system
# super-admin so alive-server can manage splitsies users/admins from
# the attlas dashboard. Caddy + splitsies-gateway both set
# X-Forwarded-For on public traffic, so this can never weaken real auth.
Environment=SPLITSIES_LOCAL_BYPASS=1
ExecStart=${BIN_DEST} serve
Restart=always
RestartSec=5
StateDirectory=splitsies
StateDirectoryMode=0700

[Install]
WantedBy=multi-user.target
UNIT

# 8. Register this service's route with splitsies-gateway.
install -d -m 755 "${GATEWAY_ROUTES_DIR}"
cp "${SPLITSIES_DIR}/splitsies.route" "${GATEWAY_ROUTES_DIR}/splitsies.conf"

# 9. Signal gateway to reload routes (if it's running).
if systemctl is-active --quiet splitsies-gateway; then
  systemctl reload splitsies-gateway || systemctl restart splitsies-gateway
  echo "Reloaded splitsies-gateway routes"
fi

# 10. Start splitsies.
systemctl daemon-reload
systemctl enable splitsies
systemctl restart splitsies

echo "splitsies installed -> port ${SPLITSIES_PORT}, base_url ${BASE_URL}"
echo "Gateway route registered at ${GATEWAY_ROUTES_DIR}/splitsies.conf"
