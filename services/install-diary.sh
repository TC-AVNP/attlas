#!/usr/bin/env bash
# Diary — Hugo-powered project diary served at /diary/
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DIARY_DIR="$HOME/iapetus/attlas/diary"

# Install Hugo
if ! command -v hugo &>/dev/null; then
  echo "Installing Hugo..."
  HUGO_VERSION="0.147.6"
  curl -fsSL "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_${HUGO_VERSION}_linux-amd64.tar.gz" \
    -o /tmp/hugo.tar.gz
  sudo tar -C /usr/local/bin -xzf /tmp/hugo.tar.gz hugo
  rm -f /tmp/hugo.tar.gz
fi
echo "hugo: $(hugo version 2>&1 | head -1)"

# Build the site
echo "Building diary site..."
cd "$DIARY_DIR"
hugo --baseURL /diary/ --destination public
echo "Diary built to $DIARY_DIR/public"

# Deploy Caddy route snippet
sudo cp "$SCRIPT_DIR/diary.caddy" /etc/caddy/conf.d/

echo "diary installed -> /diary/"
