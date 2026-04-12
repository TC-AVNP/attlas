#!/usr/bin/env bash
# Claude Login helper — PTY automation for `claude login` flow
#
# The alive-server dashboard's "log in to Claude" button shells out to
# claude-login-helper.py to drive the interactive `claude login` wizard
# through a PTY, reading the magic URL out of /tmp/claude-login-url and
# accepting the paste-back code via /tmp/claude-login-code.
#
# There is no systemd unit or Caddy snippet for this service — the
# helper is a Python script invoked ad-hoc by alive-server. This
# install.sh is a marker for the services/install.sh menu so it can
# show claude-login as an installable dependency of the dashboard.
#
# Requires: python3, sudoers.d/alive-svc-claude drop-in that lets
# alive-svc run `claude auth status` and `python3 .../claude-login-helper.py`
# as agnostic-user.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install.sh must run as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Sanity check: the helper exists and is executable-ish.
HELPER="${SCRIPT_DIR}/claude-login-helper.py"
if [[ ! -f "${HELPER}" ]]; then
  echo "ERROR: helper not found at ${HELPER}" >&2
  exit 1
fi

# Ensure python3 is installed (noop if already there).
if ! command -v python3 >/dev/null 2>&1; then
  apt-get update -qq
  apt-get install -y -qq python3
fi

echo "claude-login helper installed at ${HELPER}"
echo "alive-server resolves this path via \${ATTLAS_DIR}/services/claude-login/claude-login-helper.py"
