#!/usr/bin/env bash
# Uninstall public download page
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall.sh must run as root." >&2
  exit 1
fi

rm -f /etc/caddy/sites.d/public.caddy
rm -rf /var/www/public
systemctl reload caddy
echo "public uninstalled"
