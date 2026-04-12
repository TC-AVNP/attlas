#!/usr/bin/env bash
# Uninstall petboard. Does NOT delete /var/lib/petboard — project data
# is precious; remove it by hand if you're sure.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall-petboard.sh must run as root." >&2
  exit 1
fi

# Stop and disable the service.
if systemctl list-unit-files petboard.service &>/dev/null; then
  systemctl stop petboard 2>/dev/null || true
  systemctl disable petboard 2>/dev/null || true
  rm -f /etc/systemd/system/petboard.service
  systemctl daemon-reload
  echo "Stopped and removed petboard.service"
fi

# Remove binary + static assets.
rm -f /usr/local/bin/petboard
rm -rf /usr/local/share/petboard
echo "Removed binary and static assets"

# Remove Caddy route snippet and reload Caddy.
rm -f /etc/caddy/conf.d/petboard.caddy
if command -v caddy &>/dev/null; then
  systemctl reload caddy 2>/dev/null || true
  echo "Removed Caddy snippet and reloaded"
fi

# Remove alive-server public-path registration and reload it.
rm -f /etc/attlas-public-paths.d/petboard.conf
if systemctl is-active --quiet alive-server; then
  systemctl kill --signal=SIGHUP alive-server 2>/dev/null || true
fi

# Leave /var/lib/petboard and the petboard-svc user alone — removing
# the user would orphan the state directory, and the state directory
# contains the project history which we never want to delete on an
# uninstall. The operator can rm -rf and userdel manually if really
# desired.
echo "petboard uninstalled — state preserved at /var/lib/petboard"
