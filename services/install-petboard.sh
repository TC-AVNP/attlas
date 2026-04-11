#!/usr/bin/env bash
# Petboard — personal project tracker with infinite-canvas UI and MCP server
#
# Must be invoked as root (typically via services/install.sh from
# base-setup/setup.sh). Runs the daemon as petboard-svc (a nologin
# system user) with state under /var/lib/petboard, mirroring the
# openclaw-svc pattern.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install-petboard.sh must run as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PETBOARD_DIR="${SCRIPT_DIR}/petboard"
BUILD_USER="${BUILD_USER:-agnostic-user}"
SERVICE_USER="${SERVICE_USER:-petboard-svc}"
STATE_DIR="/var/lib/petboard"
STATIC_DEST="/usr/local/share/petboard/dist"
BIN_DEST="/usr/local/bin/petboard"
PETBOARD_PORT="${PETBOARD_PORT:-7690}"

if [[ ! -d "${PETBOARD_DIR}" ]]; then
  echo "ERROR: petboard service directory not found at ${PETBOARD_DIR}" >&2
  exit 1
fi

# 1. Create the service user if missing. No home directory, no shell —
#    state lives in /var/lib/petboard and the binary never needs to
#    spawn anything.
if ! getent passwd "${SERVICE_USER}" >/dev/null; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
  echo "Created system user ${SERVICE_USER}"
fi

# 2. Provision state directory.
install -d -o "${SERVICE_USER}" -g "${SERVICE_USER}" -m 700 "${STATE_DIR}"

# 3. Build the React frontend as BUILD_USER so node_modules and dist
#    end up owned by the same user that owns the source tree. node + npm
#    come from the base-setup install.
#
# PATH note: sudo -u ... bash -c runs a non-login shell so /etc/profile.d
# files (where go and node land on the attlas VM) are not sourced. We
# prepend the known install locations explicitly so the build never
# depends on the caller's environment.
BUILD_PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"

echo "Building petboard web frontend..."
sudo -u "${BUILD_USER}" -H env PATH="${BUILD_PATH}" bash -c "
  cd '${PETBOARD_DIR}/web' && \
  (npm ci --no-audit --no-fund --prefer-offline 2>/dev/null || npm install --no-audit --no-fund)
"
sudo -u "${BUILD_USER}" -H env PATH="${BUILD_PATH}" bash -c "cd '${PETBOARD_DIR}/web' && npm run build"

# 4. Build the Go binary as BUILD_USER. `go mod tidy` populates go.sum
#    from the single direct require in go.mod on first install (and is a
#    no-op on subsequent runs).
echo "Building petboard Go binary..."
sudo -u "${BUILD_USER}" -H env PATH="${BUILD_PATH}" bash -c "
  cd '${PETBOARD_DIR}/server' && \
  go mod tidy && \
  go build -o /tmp/petboard-build ./cmd/petboard
"

# 5. Install the built artifacts to their runtime locations. The static
#    dir is blown away and recreated on every install so stale files
#    from a previous build don't linger.
install -m 755 /tmp/petboard-build "${BIN_DEST}"
rm -f /tmp/petboard-build

rm -rf "${STATIC_DEST}"
install -d -m 755 "$(dirname "${STATIC_DEST}")"
cp -r "${PETBOARD_DIR}/web/dist" "${STATIC_DEST}"
echo "Installed binary -> ${BIN_DEST}"
echo "Installed static files -> ${STATIC_DEST}"

# 6. Write the systemd unit. Note StateDirectory= creates/chowns
#    /var/lib/petboard automatically, but we also do it above so a
#    manual systemctl start before enable still finds it.
cat > /etc/systemd/system/petboard.service <<UNIT
[Unit]
Description=Petboard — personal project tracker
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
Environment=PETBOARD_DB=${STATE_DIR}/petboard.db
Environment=PETBOARD_PORT=${PETBOARD_PORT}
Environment=PETBOARD_STATIC_DIR=${STATIC_DEST}
ExecStart=${BIN_DEST} serve
Restart=always
RestartSec=5
StateDirectory=petboard
StateDirectoryMode=0700

[Install]
WantedBy=multi-user.target
UNIT

# 7. Register public paths with alive-server. These are the endpoints
#    petboard needs to reach without going through Caddy's Google OAuth
#    — the MCP OAuth 2.1 well-known docs, DCR, token exchange, and the
#    MCP endpoint itself. The /oauth/authorize path is deliberately NOT
#    in this list: we want it to go through the existing session check
#    so we can reuse the user's browser login.
install -d -m 755 /etc/attlas-public-paths.d
cat > /etc/attlas-public-paths.d/petboard.conf <<PATHS
# Petboard MCP OAuth 2.1 public endpoints
/petboard/.well-known/
/petboard/oauth/register
/petboard/oauth/token
/petboard/mcp
PATHS

# 8. Signal alive-server to reload its public-path registry without
#    dropping existing sessions. Fall back to restart if the signal
#    path is unavailable for any reason.
if systemctl is-active --quiet alive-server; then
  if systemctl kill --signal=SIGHUP alive-server 2>/dev/null; then
    echo "Signalled alive-server SIGHUP to reload public-path registry"
  else
    echo "WARNING: SIGHUP to alive-server failed, restarting instead"
    systemctl restart alive-server
  fi
fi

# 9. Start the petboard service. enable + restart (not enable --now) so
#    re-installs pick up unit-file changes.
systemctl daemon-reload
systemctl enable petboard
systemctl restart petboard

# 10. Deploy the Caddy route snippet. Caddy reload is handled by
#     services/install.sh at the end so multiple service installs
#     batch into a single reload.
cp "${PETBOARD_DIR}/petboard.caddy" /etc/caddy/conf.d/

echo "petboard installed -> /petboard (port ${PETBOARD_PORT}), running as ${SERVICE_USER}"
