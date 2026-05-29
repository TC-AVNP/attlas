#!/usr/bin/env bash
# Uninstall Watchtower
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall.sh must run as root." >&2
  exit 1
fi

SERVICE_NAME="watchtower"

echo "==> Uninstalling ${SERVICE_NAME}"

systemctl disable --now "${SERVICE_NAME}" 2>/dev/null || true
rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload

rm -f "/usr/local/bin/${SERVICE_NAME}"
rm -f "/etc/caddy/conf.d/${SERVICE_NAME}.caddy"
rm -f "/etc/attlas-public-paths.d/watchtower.conf"

systemctl kill --signal=SIGHUP alive-server 2>/dev/null || true
systemctl reload caddy 2>/dev/null || true

echo ""
echo "NOTE: /var/lib/${SERVICE_NAME}/ and user ${SERVICE_NAME}-svc NOT removed."
echo "      Remove manually: rm -rf /var/lib/${SERVICE_NAME} && userdel ${SERVICE_NAME}-svc"
echo "${SERVICE_NAME} uninstalled."
