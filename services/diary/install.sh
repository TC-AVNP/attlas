#!/usr/bin/env bash
# Diary — Hugo-powered project diary served at /diary/
#
# Must be invoked as root. The diary build output lives inside the attlas
# repo under agnostic-user's iapetus workspace; Hugo is invoked as
# SERVICE_USER so the `public/` dir is owned by that user.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install-diary.sh must run as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_USER="${SERVICE_USER:-agnostic-user}"
SERVICE_HOME="$(getent passwd "${SERVICE_USER}" | cut -d: -f6)"
DIARY_DIR="${SERVICE_HOME}/iapetus/attlas/services/diary"

# Install Hugo
if ! command -v hugo &>/dev/null; then
  echo "Installing Hugo..."
  HUGO_VERSION="0.147.6"
  curl -fsSL "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_${HUGO_VERSION}_linux-amd64.tar.gz" \
    -o /tmp/hugo.tar.gz
  tar -C /usr/local/bin -xzf /tmp/hugo.tar.gz hugo
  rm -f /tmp/hugo.tar.gz
fi
echo "hugo: $(hugo version 2>&1 | head -1)"

# Build the site as SERVICE_USER so the output is owned correctly
echo "Building diary site..."
sudo -u "${SERVICE_USER}" bash -c "cd '${DIARY_DIR}' && hugo --baseURL /diary/ --destination public"
echo "Diary built to ${DIARY_DIR}/public"

# Deploy Caddy route snippet
cp "$SCRIPT_DIR/diary.caddy" /etc/caddy/conf.d/

echo "diary installed -> /diary/"
