#!/usr/bin/env bash
# Uninstall AFM — reverses install.sh
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall.sh must run as root." >&2
  exit 1
fi

SERVICE_NAME="afm"

echo "=== Uninstalling AFM ==="

# Stop and disable service.
systemctl stop "$SERVICE_NAME" 2>/dev/null || true
systemctl disable "$SERVICE_NAME" 2>/dev/null || true
rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload
echo "Systemd unit removed"

# Remove binary.
rm -f "/usr/local/bin/$SERVICE_NAME"
echo "Binary removed"

# Remove Caddy snippet.
rm -f /etc/caddy/conf.d/afm.caddy
echo "Caddy route removed (reload caddy to take effect)"

# NOTE: /var/lib/afm/ and the afm-svc user are NOT removed (data is precious).
echo "=== AFM uninstalled (data preserved at /var/lib/afm/) ==="
