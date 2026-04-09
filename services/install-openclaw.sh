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
SECRETS_FILE=$(mktemp)
gcloud secrets versions access latest --secret=openclaw-config --quiet > "$SECRETS_FILE"

# 3. Build config from template + secrets
echo "Building OpenClaw config..."
mkdir -p "$OPENCLAW_HOME/identity" "$OPENCLAW_HOME/credentials" "$OPENCLAW_HOME/agents/main/agent" "$OPENCLAW_HOME/devices" "$OPENCLAW_HOME/workspace"

python3 - "$SECRETS_FILE" "$SCRIPT_DIR" "$OPENCLAW_HOME" <<'PYEOF'
import json, os, sys

with open(sys.argv[1]) as f:
    secrets = json.load(f)
script_dir = sys.argv[2]
oc_home = sys.argv[3]
# Read template and substitute secrets
with open(os.path.join(script_dir, "openclaw", "config-template.json")) as f:
    config = json.load(f)

config["tools"]["web"]["search"]["apiKey"] = secrets["brave_search_api_key"]
config["channels"]["telegram"]["botToken"] = secrets["telegram_bot_token"]
config["gateway"]["auth"]["token"] = secrets["gateway_auth_token"]
config["agents"]["defaults"]["workspace"] = os.path.join(oc_home, "workspace")

# Resolve external origin for Control UI (Caddy reverse proxy)
config["gateway"]["controlUi"]["allowedOrigins"] = ["https://attlas.uk"]

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

rm -f "$SECRETS_FILE"

# 4. Set permissions
chmod 700 "$OPENCLAW_HOME"
find "$OPENCLAW_HOME/identity" "$OPENCLAW_HOME/credentials" "$OPENCLAW_HOME/agents" -type d -exec chmod 700 {} \;
find "$OPENCLAW_HOME/identity" "$OPENCLAW_HOME/credentials" "$OPENCLAW_HOME/agents" -type f -exec chmod 600 {} \;

# 5. Install and start daemon as system-level service
# Stop user-level service if it exists (migration from old setup)
systemctl --user stop openclaw-gateway 2>/dev/null || true
systemctl --user disable openclaw-gateway 2>/dev/null || true

OPENCLAW_VERSION=$(openclaw --version 2>&1 | grep -oP '[\d.]+' | head -1)
NODE_BIN=$(which node)
OPENCLAW_PKG=$(dirname "$(readlink -f "$(which openclaw)")")
OPENCLAW_JS="${OPENCLAW_PKG}/dist/index.js"

sudo tee /etc/systemd/system/openclaw-gateway.service > /dev/null <<UNIT_EOF
[Unit]
Description=OpenClaw Gateway (v${OPENCLAW_VERSION})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$(whoami)
ExecStart=${NODE_BIN} ${OPENCLAW_JS} gateway --port 18789
Restart=always
RestartSec=5
Environment=HOME=/home/$(whoami)
Environment=OPENCLAW_GATEWAY_PORT=18789

[Install]
WantedBy=multi-user.target
UNIT_EOF

sudo systemctl daemon-reload
sudo systemctl enable --now openclaw-gateway

# 6. Expose dashboard via Caddy
sudo cp "$SCRIPT_DIR/openclaw.caddy" /etc/caddy/conf.d/

echo "openclaw installed and configured"
echo "Check status: openclaw daemon status"
