#!/usr/bin/env bash
# petboard-login — device-grant auth for headless environments.
#
# Requests a device code from petboard, shows you a URL + code to open
# on any browser, polls until you approve, then configures the Claude
# Code MCP server with the resulting bearer token.
#
# Usage:
#   bash petboard-login.sh              # default: attlas.uk
#   bash petboard-login.sh localhost    # use local petboard (no auth)
#
set -euo pipefail

BASE="${1:-https://attlas.uk/petboard}"
BASE="${BASE%/}"

echo "Requesting device code from $BASE ..."
RESP=$(curl -sS -X POST -d "scope=petboard:read+petboard:write" "$BASE/oauth/device/code")

DEVICE_CODE=$(echo "$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['device_code'])")
USER_CODE=$(echo "$RESP"   | python3 -c "import json,sys; print(json.load(sys.stdin)['user_code'])")
VERIFY_URI=$(echo "$RESP"  | python3 -c "import json,sys; print(json.load(sys.stdin)['verification_uri'])")
INTERVAL=$(echo "$RESP"    | python3 -c "import json,sys; print(json.load(sys.stdin).get('interval',5))")
EXPIRES=$(echo "$RESP"     | python3 -c "import json,sys; print(json.load(sys.stdin).get('expires_in',300))")

echo ""
echo "┌─────────────────────────────────────────────┐"
echo "│                                             │"
echo "│   Open this URL on any device:              │"
echo "│                                             │"
echo "│   $VERIFY_URI"
echo "│                                             │"
echo "│   Enter code:  $USER_CODE                   │"
echo "│                                             │"
echo "│   Expires in ${EXPIRES}s                            │"
echo "└─────────────────────────────────────────────┘"
echo ""
echo "Waiting for approval (polling every ${INTERVAL}s) ..."

DEADLINE=$((SECONDS + EXPIRES))
TOKEN=""
while [ $SECONDS -lt $DEADLINE ]; do
  sleep "$INTERVAL"
  TRESP=$(curl -sS -X POST \
    -d "grant_type=urn:ietf:params:oauth:grant-type:device_code&device_code=$DEVICE_CODE" \
    "$BASE/oauth/token" 2>&1)

  ERROR=$(echo "$TRESP" | python3 -c "import json,sys; print(json.load(sys.stdin).get('error',''))" 2>/dev/null || echo "parse_error")

  case "$ERROR" in
    authorization_pending)
      printf "."
      ;;
    "")
      TOKEN=$(echo "$TRESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")
      break
      ;;
    slow_down)
      INTERVAL=$((INTERVAL + 5))
      printf "s"
      ;;
    *)
      echo ""
      echo "Error: $TRESP"
      exit 1
      ;;
  esac
done

if [ -z "$TOKEN" ]; then
  echo ""
  echo "Timed out waiting for approval."
  exit 1
fi

echo ""
echo "Approved! Token received."
echo ""

# Update Claude Code MCP config with the new token.
if command -v claude &>/dev/null; then
  echo "Configuring Claude Code MCP server ..."
  claude mcp remove petboard 2>/dev/null || true
  claude mcp add --transport http \
    -H "Authorization: Bearer $TOKEN" \
    -s user \
    petboard "$BASE/mcp"
  echo "Done. Petboard MCP is ready — token valid for 30 days."
else
  echo "Claude Code not found. Set the token manually:"
  echo ""
  echo "  claude mcp add --transport http \\"
  echo "    -H \"Authorization: Bearer $TOKEN\" \\"
  echo "    -s user petboard $BASE/mcp"
  echo ""
  echo "Or use it directly:"
  echo "  curl -H \"Authorization: Bearer $TOKEN\" $BASE/mcp"
fi
