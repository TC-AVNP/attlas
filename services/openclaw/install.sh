#!/usr/bin/env bash
# OpenClaw — AI agent daemon with Telegram, Brave search, Anthropic
#
# Must be invoked as root. The daemon runs as SERVICE_USER (default
# openclaw-svc — a nologin system user) with SERVICE_STATE_DIR used as the
# service's HOME. openclaw reads its state from $HOME/.openclaw, so the
# effective state path is ${SERVICE_STATE_DIR}/.openclaw/.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install-openclaw.sh must run as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_USER="${SERVICE_USER:-openclaw-svc}"
SERVICE_STATE_DIR="${SERVICE_STATE_DIR:-/var/lib/openclaw}"
OPENCLAW_HOME="${SERVICE_STATE_DIR}/.openclaw"

# 1. Install OpenClaw (global npm package)
if ! command -v openclaw &>/dev/null; then
  npm install -g openclaw@latest
fi
echo "openclaw: $(openclaw --version 2>&1 | head -1 || echo 'installed')"

# 2. Provision the state directory
install -d -o "${SERVICE_USER}" -g "${SERVICE_USER}" -m 700 "${SERVICE_STATE_DIR}"
install -d -o "${SERVICE_USER}" -g "${SERVICE_USER}" -m 700 "${OPENCLAW_HOME}"

# 3. Fetch secrets from GCP Secret Manager
echo "Fetching OpenClaw secrets from Secret Manager..."
SECRETS_FILE=$(mktemp)
trap 'rm -f "$SECRETS_FILE"' EXIT
gcloud secrets versions access latest --secret=openclaw-config --quiet > "$SECRETS_FILE"

# 4. Build config from template + secrets (runs as root, written files
#    are then chowned to the service user).
echo "Building OpenClaw config..."
mkdir -p \
  "$OPENCLAW_HOME/identity" \
  "$OPENCLAW_HOME/credentials" \
  "$OPENCLAW_HOME/agents/main/agent" \
  "$OPENCLAW_HOME/devices" \
  "$OPENCLAW_HOME/workspace"

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

# 5. Fix ownership and permissions
chown -R "${SERVICE_USER}:${SERVICE_USER}" "${SERVICE_STATE_DIR}"
chmod 700 "$OPENCLAW_HOME"
find "$OPENCLAW_HOME/identity" "$OPENCLAW_HOME/credentials" "$OPENCLAW_HOME/agents" -type d -exec chmod 700 {} \;
find "$OPENCLAW_HOME/identity" "$OPENCLAW_HOME/credentials" "$OPENCLAW_HOME/agents" -type f -exec chmod 600 {} \;

# 6. Install the daemon as a system-level systemd service
OPENCLAW_VERSION=$(openclaw --version 2>&1 | grep -oP '[\d.]+' | head -1)
NODE_BIN=$(which node)
OPENCLAW_PKG=$(dirname "$(readlink -f "$(which openclaw)")")
OPENCLAW_JS="${OPENCLAW_PKG}/dist/index.js"

cat > /etc/systemd/system/openclaw-gateway.service <<UNIT
[Unit]
Description=OpenClaw Gateway (v${OPENCLAW_VERSION})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
ExecStart=${NODE_BIN} ${OPENCLAW_JS} gateway --port 18789
Restart=always
RestartSec=5
StateDirectory=openclaw
StateDirectoryMode=0700
Environment=HOME=${SERVICE_STATE_DIR}
Environment=OPENCLAW_GATEWAY_PORT=18789

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable openclaw-gateway
systemctl restart openclaw-gateway

# 7. Expose gateway via Caddy
cp "$SCRIPT_DIR/openclaw.caddy" /etc/caddy/conf.d/

echo "openclaw installed and configured (running as ${SERVICE_USER})"
echo "State dir: ${OPENCLAW_HOME}"
