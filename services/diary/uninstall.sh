#!/usr/bin/env bash
# Uninstall diary — remove Hugo site and Caddy route
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall-diary.sh must run as root." >&2
  exit 1
fi

SERVICE_USER="${SERVICE_USER:-agnostic-user}"
SERVICE_HOME="$(getent passwd "${SERVICE_USER}" | cut -d: -f6)"

rm -rf "${SERVICE_HOME}/iapetus/attlas/services/diary/public"
rm -f /etc/caddy/conf.d/diary.caddy

echo "diary uninstalled"
