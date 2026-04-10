#!/usr/bin/env bash
# Uninstall diary — remove Hugo site and Caddy route
set -euo pipefail

rm -rf ~/iapetus/attlas/diary/public
sudo rm -f /etc/caddy/conf.d/diary.caddy

echo "diary uninstalled"
