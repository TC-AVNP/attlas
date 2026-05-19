#!/usr/bin/env bash
# Observability — Victoria Metrics + OTel Collector + Grafana
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: install.sh must run as root." >&2
  exit 1
fi

DIR="$(cd "$(dirname "$0")" && pwd)"
ARCH=$(dpkg --print-architecture)
STATE_DIR="/var/lib/observability"
CERTS_DIR="${STATE_DIR}/certs"

# Versions
VM_VERSION="v1.118.0"
OTEL_VERSION="0.120.0"
GRAFANA_VERSION="11.6.0"

echo "==> Installing Observability Stack"

mkdir -p "${STATE_DIR}" "${CERTS_DIR}"

# ── 1. Victoria Metrics ──────────────────────────────────────────

VM_USER="victoriametrics-svc"
VM_DATA="/var/lib/victoria-metrics"
VM_PORT=8428

echo "==> Installing Victoria Metrics ${VM_VERSION}"

if ! id "${VM_USER}" &>/dev/null; then
  useradd --system --shell /usr/sbin/nologin --home-dir "${VM_DATA}" "${VM_USER}"
  echo "    Created user ${VM_USER}"
fi

mkdir -p "${VM_DATA}"
chown "${VM_USER}:${VM_USER}" "${VM_DATA}"

VM_ARCH="${ARCH}"
if [[ "$ARCH" == "amd64" ]]; then VM_ARCH="amd64"; fi

if [[ ! -f /usr/local/bin/victoria-metrics ]] || ! /usr/local/bin/victoria-metrics --version 2>&1 | grep -q "${VM_VERSION}"; then
  echo "    Downloading victoria-metrics-prod..."
  curl -fsSL "https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/${VM_VERSION}/victoria-metrics-linux-${VM_ARCH}-${VM_VERSION}.tar.gz" \
    -o /tmp/vm.tar.gz
  tar -xzf /tmp/vm.tar.gz -C /tmp/
  mv /tmp/victoria-metrics-prod /usr/local/bin/victoria-metrics
  chmod 755 /usr/local/bin/victoria-metrics
  rm -f /tmp/vm.tar.gz
  echo "    Installed /usr/local/bin/victoria-metrics"
fi

cat > /etc/systemd/system/victoria-metrics.service <<UNIT
[Unit]
Description=Victoria Metrics — time series database
After=network.target

[Service]
Type=simple
User=${VM_USER}
ExecStart=/usr/local/bin/victoria-metrics \\
  -storageDataPath=${VM_DATA} \\
  -retentionPeriod=3 \\
  -httpListenAddr=127.0.0.1:${VM_PORT}
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable victoria-metrics
systemctl restart victoria-metrics
echo "    Victoria Metrics running on 127.0.0.1:${VM_PORT}"

# ── 2. OpenTelemetry Collector ───────────────────────────────────

OTEL_USER="otelcol-svc"
OTEL_PORT=4318

echo "==> Installing OpenTelemetry Collector ${OTEL_VERSION}"

if ! id "${OTEL_USER}" &>/dev/null; then
  useradd --system --shell /usr/sbin/nologin --home-dir "${STATE_DIR}" "${OTEL_USER}"
  echo "    Created user ${OTEL_USER}"
fi

OTEL_ARCH="${ARCH}"
if [[ "$ARCH" == "amd64" ]]; then OTEL_ARCH="amd64"; fi
if [[ "$ARCH" == "arm64" ]]; then OTEL_ARCH="arm64"; fi

if [[ ! -f /usr/local/bin/otelcol-contrib ]] || ! /usr/local/bin/otelcol-contrib --version 2>&1 | grep -q "${OTEL_VERSION}"; then
  echo "    Downloading otelcol-contrib..."
  curl -fsSL "https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v${OTEL_VERSION}/otelcol-contrib_${OTEL_VERSION}_linux_${OTEL_ARCH}.tar.gz" \
    -o /tmp/otelcol.tar.gz
  tar -xzf /tmp/otelcol.tar.gz -C /tmp/ otelcol-contrib
  mv /tmp/otelcol-contrib /usr/local/bin/otelcol-contrib
  chmod 755 /usr/local/bin/otelcol-contrib
  rm -f /tmp/otelcol.tar.gz
  echo "    Installed /usr/local/bin/otelcol-contrib"
fi

# Generate mTLS certs for the OTLP endpoint (reuse homelab CA if available)
HOMELAB_CA="/var/lib/homelab-bootstrap/ca.crt"
HOMELAB_CA_KEY="/var/lib/homelab-bootstrap/ca.key"

if [[ -f "$HOMELAB_CA" && -f "$HOMELAB_CA_KEY" ]]; then
  echo "    Using homelab CA for OTel mTLS"
  cp "$HOMELAB_CA" "${CERTS_DIR}/ca.crt"

  # Generate a server cert for the OTel Collector if not present
  if [[ ! -f "${CERTS_DIR}/server.key" ]]; then
    openssl genrsa -out "${CERTS_DIR}/server.key" 2048
    openssl req -new -key "${CERTS_DIR}/server.key" \
      -out "${CERTS_DIR}/server.csr" \
      -subj "/CN=otel.attlas.uk/O=attlas"

    # SAN for the domain
    cat > "${CERTS_DIR}/server-ext.cnf" <<EOF
subjectAltName=DNS:otel.attlas.uk
EOF
    openssl x509 -req -days 3650 -in "${CERTS_DIR}/server.csr" \
      -CA "$HOMELAB_CA" -CAkey "$HOMELAB_CA_KEY" -CAcreateserial \
      -extfile "${CERTS_DIR}/server-ext.cnf" \
      -out "${CERTS_DIR}/server.crt"
    rm -f "${CERTS_DIR}/server.csr" "${CERTS_DIR}/server-ext.cnf"
    echo "    Generated server certificate for otel.attlas.uk"
  fi
else
  echo "    WARNING: homelab CA not found — generating self-signed CA for OTel"
  if [[ ! -f "${CERTS_DIR}/ca.key" ]]; then
    openssl genrsa -out "${CERTS_DIR}/ca.key" 4096
    openssl req -new -x509 -days 3650 -key "${CERTS_DIR}/ca.key" \
      -out "${CERTS_DIR}/ca.crt" \
      -subj "/CN=OTel CA/O=attlas"
  fi
  if [[ ! -f "${CERTS_DIR}/server.key" ]]; then
    openssl genrsa -out "${CERTS_DIR}/server.key" 2048
    openssl req -new -key "${CERTS_DIR}/server.key" \
      -out "${CERTS_DIR}/server.csr" \
      -subj "/CN=otel.attlas.uk/O=attlas"
    cat > "${CERTS_DIR}/server-ext.cnf" <<EOF
subjectAltName=DNS:otel.attlas.uk
EOF
    openssl x509 -req -days 3650 -in "${CERTS_DIR}/server.csr" \
      -CA "${CERTS_DIR}/ca.crt" -CAkey "${CERTS_DIR}/ca.key" -CAcreateserial \
      -extfile "${CERTS_DIR}/server-ext.cnf" \
      -out "${CERTS_DIR}/server.crt"
    rm -f "${CERTS_DIR}/server.csr" "${CERTS_DIR}/server-ext.cnf"
    echo "    Generated self-signed CA and server certificate"
  fi
fi

chown -R "${OTEL_USER}:${OTEL_USER}" "${CERTS_DIR}"
chmod 600 "${CERTS_DIR}"/*.key
chmod 644 "${CERTS_DIR}"/*.crt

# OTel Collector config
mkdir -p /etc/otelcol
cat > /etc/otelcol/config.yaml <<OTELCFG
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:${OTEL_PORT}
        tls:
          cert_file: ${CERTS_DIR}/server.crt
          key_file: ${CERTS_DIR}/server.key
          client_ca_file: ${CERTS_DIR}/ca.crt

  hostmetrics:
    collection_interval: 30s
    scrapers:
      cpu:
      memory:
      disk:
      network:
      load:
      filesystem:

processors:
  batch:
    timeout: 10s
    send_batch_size: 1000

exporters:
  prometheusremotewrite:
    endpoint: http://127.0.0.1:${VM_PORT}/api/v1/write
    tls:
      insecure: true

service:
  pipelines:
    metrics:
      receivers: [otlp, hostmetrics]
      processors: [batch]
      exporters: [prometheusremotewrite]
OTELCFG

chown "${OTEL_USER}:${OTEL_USER}" /etc/otelcol/config.yaml

cat > /etc/systemd/system/otelcol.service <<UNIT
[Unit]
Description=OpenTelemetry Collector
After=network.target victoria-metrics.service

[Service]
Type=simple
User=${OTEL_USER}
ExecStart=/usr/local/bin/otelcol-contrib --config=/etc/otelcol/config.yaml
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable otelcol
systemctl restart otelcol
echo "    OTel Collector running on 0.0.0.0:${OTEL_PORT} (mTLS)"

# ── 3. Grafana ───────────────────────────────────────────────────

GRAFANA_PORT=3001

echo "==> Installing Grafana ${GRAFANA_VERSION}"

if ! command -v grafana-server &>/dev/null || ! grafana-server -v 2>&1 | grep -q "${GRAFANA_VERSION}"; then
  echo "    Downloading Grafana..."
  apt-get install -y -qq musl 2>/dev/null || true
  curl -fsSL "https://dl.grafana.com/oss/release/grafana_${GRAFANA_VERSION}_${ARCH}.deb" \
    -o /tmp/grafana.deb
  dpkg -i /tmp/grafana.deb
  rm -f /tmp/grafana.deb
  echo "    Installed Grafana"
fi

# ── Grafana config ──────────────────────────────────────────────
# Pull Google OAuth credentials from the same attlas-server-config
# secret used by alive-server so Grafana can do Google login.

GRAFANA_OAUTH_CLIENT_ID=""
GRAFANA_OAUTH_CLIENT_SECRET=""
GRAFANA_ALLOWED_EMAIL=""

if command -v gcloud &>/dev/null; then
  OAUTH_JSON=$(gcloud secrets versions access latest --secret=attlas-server-config --quiet 2>/dev/null || true)
  if [[ -n "${OAUTH_JSON}" ]]; then
    GRAFANA_OAUTH_CLIENT_ID=$(echo "${OAUTH_JSON}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('google_oauth_client_id',''))" 2>/dev/null || true)
    GRAFANA_OAUTH_CLIENT_SECRET=$(echo "${OAUTH_JSON}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('google_oauth_client_secret',''))" 2>/dev/null || true)
    GRAFANA_ALLOWED_EMAIL=$(echo "${OAUTH_JSON}" | python3 -c "import sys,json; emails=json.load(sys.stdin).get('allowed_emails',[]); print(emails[0] if emails else '')" 2>/dev/null || true)
    echo "    Loaded Google OAuth credentials from attlas-server-config"
  fi
fi

# Override Grafana port (default 3000 conflicts with alive-server)
mkdir -p /etc/grafana
cat > /etc/grafana/grafana.ini <<GRAFCFG
[server]
http_port = ${GRAFANA_PORT}
root_url = https://grafana.attlas.uk/
serve_from_sub_path = false

[security]
admin_user = commonlisp
admin_password = xadrez12

[auth.anonymous]
enabled = false

[users]
allow_sign_up = false
allow_org_create = false
auto_assign_org = true
auto_assign_org_role = Admin
GRAFCFG

# Append Google OAuth section only if credentials were loaded
if [[ -n "${GRAFANA_OAUTH_CLIENT_ID}" && -n "${GRAFANA_OAUTH_CLIENT_SECRET}" ]]; then
  cat >> /etc/grafana/grafana.ini <<OAUTHCFG

[auth.google]
enabled = true
allow_sign_up = true
auto_login = true
client_id = ${GRAFANA_OAUTH_CLIENT_ID}
client_secret = ${GRAFANA_OAUTH_CLIENT_SECRET}
scopes = openid email profile
auth_url = https://accounts.google.com/o/oauth2/v2/auth
token_url = https://oauth2.googleapis.com/token
api_url = https://openidconnect.googleapis.com/v1/userinfo
allowed_emails = ${GRAFANA_ALLOWED_EMAIL}
OAUTHCFG
  echo "    Google OAuth enabled — only ${GRAFANA_ALLOWED_EMAIL} can log in"
else
  echo "    WARNING: Google OAuth credentials not found — falling back to password auth"
fi

# Provision Victoria Metrics as default datasource
mkdir -p /etc/grafana/provisioning/datasources
cat > /etc/grafana/provisioning/datasources/victoriametrics.yaml <<DSCFG
apiVersion: 1
datasources:
  - name: Victoria Metrics
    type: prometheus
    access: proxy
    url: http://127.0.0.1:${VM_PORT}
    isDefault: true
    editable: false
DSCFG

systemctl daemon-reload
systemctl enable grafana-server
systemctl restart grafana-server
echo "    Grafana running on 127.0.0.1:${GRAFANA_PORT}"

# ── 4. Caddy site block for Grafana ──────────────────────────────

install -d -m 755 /etc/caddy/sites.d
cp "${DIR}/grafana.caddy" /etc/caddy/sites.d/

# Ensure Caddyfile imports sites.d
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

# ── 5. Cloudflare DNS ────────────────────────────────────────────

CF_TOKEN=$(gcloud secrets versions access latest --secret=cloudflare-dns-token --quiet 2>/dev/null || true)
EXTERNAL_IP=$(curl -sf -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip 2>/dev/null || true)

if [[ -n "$CF_TOKEN" && -n "$EXTERNAL_IP" ]]; then
  CF_ZONE="813c7bfa1c9f2b1a02a60c97f3171fa6"

  for SUBDOMAIN in grafana otel; do
    FQDN="${SUBDOMAIN}.attlas.uk"
    RECORD_ID=$(curl -sf "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records?type=A&name=${FQDN}" \
      -H "Authorization: Bearer ${CF_TOKEN}" | python3 -c "import sys,json; r=json.load(sys.stdin)['result']; print(r[0]['id'] if r else '')")

    if [[ -n "$RECORD_ID" ]]; then
      curl -sf -X PUT "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records/${RECORD_ID}" \
        -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
        -d "{\"type\":\"A\",\"name\":\"${FQDN}\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
      echo "    Cloudflare DNS updated: ${FQDN} -> ${EXTERNAL_IP}"
    else
      curl -sf -X POST "https://api.cloudflare.com/client/v4/zones/${CF_ZONE}/dns_records" \
        -H "Authorization: Bearer ${CF_TOKEN}" -H "Content-Type: application/json" \
        -d "{\"type\":\"A\",\"name\":\"${FQDN}\",\"content\":\"${EXTERNAL_IP}\",\"proxied\":false}" > /dev/null
      echo "    Cloudflare DNS created: ${FQDN} -> ${EXTERNAL_IP}"
    fi
  done
else
  echo "    WARNING: skipping Cloudflare DNS (token or IP unavailable)"
fi

# ── 6. Reload Caddy ──────────────────────────────────────────────

systemctl reload caddy
echo "    Caddy reloaded"

echo ""
echo "==> Observability stack installed"
echo "    Victoria Metrics: http://127.0.0.1:${VM_PORT}"
echo "    OTel Collector:   https://otel.attlas.uk:${OTEL_PORT} (mTLS)"
echo "    Grafana:          https://grafana.attlas.uk/"
echo ""
echo "    Grafana login: commonlisp / xadrez12"
