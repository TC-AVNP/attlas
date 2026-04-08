#!/usr/bin/env bash
# OpenClaw — AI agent daemon with Telegram, Brave search, Anthropic
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OPENCLAW_HOME="$HOME/.openclaw"

# 1. Install OpenClaw
if ! command -v openclaw &>/dev/null; then
  sudo npm install -g openclaw@latest
fi
echo "openclaw: $(openclaw --version 2>&1 | head -1 || echo 'installed')"

# 2. Fetch secrets from GCP Secret Manager
echo "Fetching OpenClaw secrets from Secret Manager..."
SECRETS=$(gcloud secrets versions access latest --secret=openclaw-config --quiet)

# 3. Build config from template + secrets
echo "Building OpenClaw config..."
mkdir -p "$OPENCLAW_HOME/identity" "$OPENCLAW_HOME/credentials" "$OPENCLAW_HOME/agents/main/agent" "$OPENCLAW_HOME/devices" "$OPENCLAW_HOME/workspace"

python3 <<PYEOF
import json, os

secrets = json.loads('''$SECRETS''')
home = os.path.expanduser("~")
oc_home = os.path.join(home, ".openclaw")
script_dir = "$SCRIPT_DIR"

# Read template and substitute secrets
with open(os.path.join(script_dir, "openclaw", "config-template.json")) as f:
    config = json.load(f)

config["tools"]["web"]["search"]["apiKey"] = secrets["brave_search_api_key"]
config["channels"]["telegram"]["botToken"] = secrets["telegram_bot_token"]
config["gateway"]["auth"]["token"] = secrets["gateway_auth_token"]
config["agents"]["defaults"]["workspace"] = os.path.join(oc_home, "workspace")

with open(os.path.join(oc_home, "openclaw.json"), "w") as f:
    json.dump(config, f, indent=2)

# Write identity files
with open(os.path.join(oc_home, "identity", "device.json"), "w") as f:
    json.dump({
        "version": 1,
        "deviceId": secrets["device_id"],
        "publicKeyPem": secrets["device_public_key"],
        "privateKeyPem": secrets["device_private_key"],
        "createdAtMs": 1774199430254
    }, f, indent=2)

with open(os.path.join(oc_home, "identity", "device-auth.json"), "w") as f:
    json.dump({
        "version": 1,
        "deviceId": secrets["device_id"],
        "tokens": {
            "operator": {
                "token": secrets["operator_token"],
                "role": "operator",
                "scopes": ["operator.admin"],
                "updatedAtMs": 1774201373737
            }
        }
    }, f, indent=2)

# Write auth profiles (Anthropic API key)
with open(os.path.join(oc_home, "agents", "main", "agent", "auth-profiles.json"), "w") as f:
    json.dump({
        "version": 1,
        "profiles": {
            "anthropic:default": {
                "type": "api_key",
                "provider": "anthropic",
                "key": secrets["anthropic_api_key"]
            }
        },
        "lastGood": {"anthropic": "anthropic:default"}
    }, f, indent=2)

# Write telegram credentials
with open(os.path.join(oc_home, "credentials", "telegram-pairing.json"), "w") as f:
    json.dump({"version": 1, "requests": []}, f, indent=2)

with open(os.path.join(oc_home, "credentials", "telegram-default-allowFrom.json"), "w") as f:
    json.dump({"version": 1, "allowFrom": secrets["telegram_allow_from"]}, f, indent=2)

# Write paired devices
with open(os.path.join(oc_home, "devices", "paired.json"), "w") as f:
    did = secrets["device_id"]
    json.dump({
        did: {
            "deviceId": did,
            "publicKey": secrets["device_public_key"].split("\\n")[1] if "\\n" in secrets["device_public_key"] else "",
            "platform": "linux",
            "clientId": "gateway-client",
            "clientMode": "ui",
            "role": "operator",
            "roles": ["operator"],
            "scopes": ["operator.read", "operator.admin"],
            "approvedScopes": ["operator.read", "operator.admin"],
            "tokens": {
                "operator": {
                    "token": secrets["operator_token"],
                    "role": "operator",
                    "scopes": ["operator.admin"]
                }
            },
            "displayName": "openclaw-tui"
        }
    }, f, indent=2)

print("Config files written to " + oc_home)
PYEOF

# 4. Set permissions
chmod 700 "$OPENCLAW_HOME"
chmod -R 600 "$OPENCLAW_HOME/identity" "$OPENCLAW_HOME/credentials" "$OPENCLAW_HOME/agents"
chmod -R u+X "$OPENCLAW_HOME/identity" "$OPENCLAW_HOME/credentials" "$OPENCLAW_HOME/agents"

# 5. Install and start daemon (non-interactive)
# Uses the config we just wrote — no wizard needed
openclaw daemon install
openclaw daemon start

echo "openclaw installed and configured"
echo "Check status: openclaw daemon status"
