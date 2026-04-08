#!/usr/bin/env bash
# Interactive service installer — presents a menu of available services.
# Each service has its own install script in this directory.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Discover available services (any file matching install-*.sh)
SERVICES=()
DESCRIPTIONS=()
for f in "$SCRIPT_DIR"/install-*.sh; do
  [ -f "$f" ] || continue
  name=$(basename "$f" | sed 's/install-//;s/\.sh//')
  desc=$(head -2 "$f" | grep '^#' | tail -1 | sed 's/^# *//')
  SERVICES+=("$name")
  DESCRIPTIONS+=("$desc")
done

if [ ${#SERVICES[@]} -eq 0 ]; then
  echo "No services found (no install-*.sh files in $SCRIPT_DIR)"
  exit 0
fi

echo "=== Available services ==="
echo ""
for i in "${!SERVICES[@]}"; do
  echo "  $((i+1)). ${SERVICES[$i]} — ${DESCRIPTIONS[$i]}"
done
echo ""
echo "  a. Install ALL"
echo "  q. Quit"
echo ""
read -p "Select services to install (e.g. 1 3, or 'a' for all): " -r SELECTION

if [[ "$SELECTION" == "q" ]]; then
  echo "No services installed."
  exit 0
fi

SELECTED=()
if [[ "$SELECTION" == "a" ]]; then
  SELECTED=("${SERVICES[@]}")
else
  for num in $SELECTION; do
    idx=$((num - 1))
    if [ $idx -ge 0 ] && [ $idx -lt ${#SERVICES[@]} ]; then
      SELECTED+=("${SERVICES[$idx]}")
    else
      echo "Invalid selection: $num"
    fi
  done
fi

if [ ${#SELECTED[@]} -eq 0 ]; then
  echo "No valid services selected."
  exit 0
fi

echo ""
echo "Installing: ${SELECTED[*]}"
echo ""

for svc in "${SELECTED[@]}"; do
  echo "--- Installing $svc ---"
  bash "$SCRIPT_DIR/install-$svc.sh"
  echo ""
done

# Reload Caddy to pick up any new route snippets
if command -v caddy &>/dev/null; then
  sudo systemctl reload caddy
  echo "Caddy reloaded."
fi

echo ""
echo "=== Done ==="
