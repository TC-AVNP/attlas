#!/usr/bin/env bash
# Uninstall observability stack (preserves data)
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: uninstall.sh must run as root." >&2
  exit 1
fi

echo "==> Uninstalling Observability Stack"

# Stop services
for svc in otelcol victoria-metrics grafana-server; do
  if systemctl is-active "$svc" &>/dev/null; then
    systemctl stop "$svc"
    systemctl disable "$svc"
    echo "    Stopped ${svc}"
  fi
done

# Remove units (except grafana which is managed by dpkg)
rm -f /etc/systemd/system/victoria-metrics.service
rm -f /etc/systemd/system/otelcol.service
systemctl daemon-reload

# Remove binaries (except grafana which is managed by dpkg)
rm -f /usr/local/bin/victoria-metrics
rm -f /usr/local/bin/otelcol-contrib

# Remove Caddy config
rm -f /etc/caddy/sites.d/grafana.caddy
systemctl reload caddy 2>/dev/null || true

# Remove OTel config
rm -rf /etc/otelcol

echo ""
echo "==> Observability stack uninstalled"
echo "    Data preserved at /var/lib/victoria-metrics/ and /var/lib/observability/"
echo "    To remove data: rm -rf /var/lib/victoria-metrics /var/lib/observability"
echo "    To remove Grafana: apt remove grafana"
echo "    Reload Caddy: systemctl reload caddy"
