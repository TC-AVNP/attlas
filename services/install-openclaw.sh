#!/usr/bin/env bash
# OpenClaw — AI agent daemon
set -euo pipefail

# Install OpenClaw
if ! command -v openclaw &>/dev/null; then
  sudo npm install -g openclaw@latest
fi
echo "openclaw: $(openclaw --version 2>&1 | head -1 || echo 'installed')"

# Run onboarding + install daemon
openclaw onboard --install-daemon

echo "openclaw installed and daemon running"
