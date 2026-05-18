#!/usr/bin/env bash
# Homelab Bootstrap — mTLS node registration service at homelab.attlas.uk
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install.sh must run as root." >&2
  exit 1
fi

DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICE_NAME="homelab-bootstrap"
SERVICE_USER="${SERVICE_USER:-homelab-svc}"
BUILD_USER="${BUILD_USER:-agnostic-user}"
STATE_DIR="/var/lib/${SERVICE_NAME}"
PORT=7697

echo "==> Installing ${SERVICE_NAME}"

# 1. System user.
if ! id "${SERVICE_USER}" &>/dev/null; then
  useradd --system --shell /usr/sbin/nologin --home-dir "${STATE_DIR}" "${SERVICE_USER}"
  echo "    Created user ${SERVICE_USER}"
fi

# 2. State directory.
mkdir -p "${STATE_DIR}"
chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}"
chmod 700 "${STATE_DIR}"

# 3. Generate CA and client certificate (only on first install).
if [[ ! -f "${STATE_DIR}/ca.key" ]]; then
  echo "    Generating CA certificate..."
  openssl genrsa -out "${STATE_DIR}/ca.key" 4096
  openssl req -new -x509 -days 3650 -key "${STATE_DIR}/ca.key" \
    -out "${STATE_DIR}/ca.crt" \
    -subj "/CN=Homelab Bootstrap CA/O=attlas"

  echo "    Generating client certificate for golden image..."
  openssl genrsa -out "${STATE_DIR}/client.key" 2048
  openssl req -new -key "${STATE_DIR}/client.key" \
    -out "${STATE_DIR}/client.csr" \
    -subj "/CN=homelab-node/O=attlas"
  openssl x509 -req -days 3650 -in "${STATE_DIR}/client.csr" \
    -CA "${STATE_DIR}/ca.crt" -CAkey "${STATE_DIR}/ca.key" -CAcreateserial \
    -out "${STATE_DIR}/client.crt"
  rm -f "${STATE_DIR}/client.csr"

  chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}"/*.key "${STATE_DIR}"/*.crt
  chmod 600 "${STATE_DIR}/ca.key" "${STATE_DIR}/client.key"
  chmod 644 "${STATE_DIR}/ca.crt" "${STATE_DIR}/client.crt"

  echo "    CA cert:     ${STATE_DIR}/ca.crt"
  echo "    Client cert: ${STATE_DIR}/client.crt"
  echo "    Client key:  ${STATE_DIR}/client.key"
  echo ""
  echo "    >>> Copy client.crt and client.key into the golden image <<<"
else
  echo "    CA and client certs already exist, skipping generation"
fi

# 4. SSH authorized keys for Pi nodes.
if [[ ! -f "${STATE_DIR}/authorized_keys" ]]; then
  # Seed from the current user's keys if available
  if [[ -f "/home/agnostic-user/.ssh/authorized_keys" ]]; then
    cp "/home/agnostic-user/.ssh/authorized_keys" "${STATE_DIR}/authorized_keys"
    echo "    SSH keys seeded from agnostic-user's authorized_keys"
  else
    touch "${STATE_DIR}/authorized_keys"
    echo "    WARNING: No SSH keys found. Add keys to ${STATE_DIR}/authorized_keys"
    echo "    Pis won't be accessible via SSH until you add at least one key."
  fi
  chown "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}/authorized_keys"
  chmod 644 "${STATE_DIR}/authorized_keys"
fi

# 5. GitHub PAT (for Pi dotfiles clone).
GITHUB_PAT=""
if command -v gcloud &>/dev/null; then
  GITHUB_PAT=$(gcloud secrets versions access latest --secret=github-pat --quiet 2>/dev/null || true)
  if [[ -n "$GITHUB_PAT" ]]; then
    echo "    GitHub PAT loaded from Secret Manager"
  fi
fi

# 5b. Cloudflare credentials (for router tunnel management).
# The API token needs: Account:Cloudflare Tunnel:Edit + Zone:DNS:Edit permissions.
CF_API_TOKEN=""
CF_ACCOUNT_ID=""
CF_ZONE_ID="813c7bfa1c9f2b1a02a60c97f3171fa6"
if command -v gcloud &>/dev/null; then
  CF_API_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
  if [[ -n "$CF_API_TOKEN" ]]; then
    # Look up the account ID from the zone
    CF_ACCOUNT_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE_ID}" \
      -H "Authorization: Bearer ${CF_API_TOKEN}" | python3 -c "import sys,json; print(json.load(sys.stdin)['result']['account']['id'])" 2>/dev/null || true)
    if [[ -n "$CF_ACCOUNT_ID" ]]; then
      echo "    Cloudflare tunnel management configured (account: ${CF_ACCOUNT_ID:0:8}...)"
    else
      echo "    WARNING: Could not resolve Cloudflare account ID — router registration will be disabled"
    fi
  else
    echo "    WARNING: cloudflare-dns-token not found — router registration will be disabled"
  fi
fi

# 6. Set up kubeconfig and sudoers for kubeadm token generation.
if [[ -f /etc/kubernetes/admin.conf ]]; then
  mkdir -p "${STATE_DIR}/.kube"
  cp /etc/kubernetes/admin.conf "${STATE_DIR}/.kube/config"
  chown -R "${SERVICE_USER}:${SERVICE_USER}" "${STATE_DIR}/.kube"
  echo "    kubectl configured for ${SERVICE_USER}"

  # Allow the service user to run specific kubeadm commands without password
  cat > "/etc/sudoers.d/${SERVICE_NAME}" <<SUDOERS
${SERVICE_USER} ALL=(root) NOPASSWD: /usr/bin/kubeadm token create *
${SERVICE_USER} ALL=(root) NOPASSWD: /usr/bin/kubeadm init phase upload-certs --upload-certs
SUDOERS
  chmod 440 "/etc/sudoers.d/${SERVICE_NAME}"
  echo "    sudoers configured for kubeadm commands"
else
  echo "    WARNING: /etc/kubernetes/admin.conf not found — run setup-k8s.sh first"
  echo "    The service will start but token generation will fail until k8s is set up"
fi

# 5. Build Go binary.
echo "    Building Go binary..."
sudo -u "${BUILD_USER}" -H env PATH="/usr/local/go/bin:$PATH" bash -c \
  "cd '${DIR}/server' && go build -o /tmp/${SERVICE_NAME}-build ."
mv "/tmp/${SERVICE_NAME}-build" "/usr/local/bin/${SERVICE_NAME}"
echo "    Installed /usr/local/bin/${SERVICE_NAME}"

# 5. Systemd unit.
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=Homelab Bootstrap — mTLS node registration
After=network.target

[Service]
Type=simple
User=${SERVICE_USER}
ExecStart=/usr/local/bin/${SERVICE_NAME}
Restart=always
RestartSec=5

Environment=HOMELAB_PORT=${PORT}
Environment=HOMELAB_DB=${STATE_DIR}/homelab.db
Environment=KUBECONFIG=${STATE_DIR}/.kube/config
Environment=HOMELAB_API_ENDPOINT=https://34.62.185.156:6443
Environment=HOMELAB_SSH_KEYS_FILE=${STATE_DIR}/authorized_keys
Environment=HOMELAB_GITHUB_PAT=${GITHUB_PAT}
Environment=CLOUDFLARE_API_TOKEN=${CF_API_TOKEN}
Environment=CLOUDFLARE_ACCOUNT_ID=${CF_ACCOUNT_ID}
Environment=CLOUDFLARE_ZONE_ID=${CF_ZONE_ID}

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"
echo "    Service ${SERVICE_NAME} started on port ${PORT}"

# 6. Caddy site block for homelab.attlas.uk (mTLS).
install -d -m 755 /etc/caddy/sites.d
cp "${DIR}/${SERVICE_NAME}.caddy" /etc/caddy/sites.d/
echo "    Caddy config installed (mTLS required)"

# 6b. Ensure /etc/caddy/Caddyfile imports sites.d at the top level.
if ! grep -q '^import /etc/caddy/sites.d' /etc/caddy/Caddyfile; then
  echo "    Patching /etc/caddy/Caddyfile to import /etc/caddy/sites.d/*.caddy"
  cp /etc/caddy/Caddyfile /etc/caddy/Caddyfile.bak.$(date +%Y%m%d-%H%M%S)
  TMP_CADDYFILE=$(mktemp)
  {
    echo "# Subdomain site blocks."
    echo "import /etc/caddy/sites.d/*.caddy"
    echo ""
    cat /etc/caddy/Caddyfile
  } > "$TMP_CADDYFILE"
  install -m 644 "$TMP_CADDYFILE" /etc/caddy/Caddyfile
  rm -f "$TMP_CADDYFILE"
fi

# 7. Ensure Cloudflare A record for homelab.attlas.uk points to this VM.
CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
EXTERNAL_IP=$(curl -sf -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip 2>/dev/null || true)
if [[ -n "$CF_TOKEN" && -n "$EXTERNAL_IP" ]]; then
  CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"
  RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=homelab.attlas.uk" \
    -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')")
  if [[ -n "$RECORD_ID" ]]; then
    curl -sf -X PUT "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      -d "{\"type\":\"A\",\"name\":\"homelab.attlas.uk\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
    echo "    Cloudflare DNS updated: homelab.attlas.uk -> ${EXTERNAL_IP}"
  else
    curl -sf -X POST "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records" \
      -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
      -d "{\"type\":\"A\",\"name\":\"homelab.attlas.uk\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
    echo "    Cloudflare DNS created: homelab.attlas.uk -> ${EXTERNAL_IP}"
  fi
else
  echo "    WARNING: skipping Cloudflare DNS (token or IP unavailable)"
  echo "    Create manually: homelab.attlas.uk -> <VM IP>"
fi

# 8. Reload Caddy to pick up the new site block.
systemctl reload caddy
echo "    Caddy reloaded"

echo ""
echo "${SERVICE_NAME} installed -> https://homelab.attlas.uk/"
echo ""
echo "To test with the client cert:"
echo "  curl --cert ${STATE_DIR}/client.crt --key ${STATE_DIR}/client.key https://homelab.attlas.uk/api/nodes"
