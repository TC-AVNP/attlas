# Observability

Unified metrics collection and dashboards for the entire attlas ecosystem.

## Architecture

```
Nodes (Pi-routers, Pi5 workers)          GCP VM
   │                                  ┌──────────────────────┐
   │ OTLP/HTTP + mTLS                │ OTel Collector        │
   │ port 4318                       │ 0.0.0.0:4318 (mTLS)  │
   └────────────────────────────────▶│         │             │
                                     │         ▼             │
                                     │ Victoria Metrics      │
                                     │ 127.0.0.1:8428       │
                                     │         │             │
                                     │         ▼             │
                                     │ Grafana               │
                                     │ grafana.attlas.uk     │
                                     └──────────────────────┘
```

## Components

| Component | Port | User | Binary |
|-----------|------|------|--------|
| Victoria Metrics | 8428 (localhost) | victoriametrics-svc | /usr/local/bin/victoria-metrics |
| OTel Collector | 4318 (public, mTLS) | otelcol-svc | /usr/local/bin/otelcol-contrib |
| Grafana | 3001 (localhost) | grafana | /usr/sbin/grafana-server |

## Authentication

- **OTel Collector**: mTLS using the homelab CA. Nodes must present a client cert signed by the same CA used for homelab-bootstrap.
- **Grafana**: Caddy reverse proxy at grafana.attlas.uk. Google OAuth (restricted to allowed email from attlas-server-config). Falls back to password auth (commonlisp / xadrez12) if OAuth credentials unavailable.

## Data

- **Victoria Metrics data**: /var/lib/victoria-metrics/
- **Retention**: 3 months
- **OTel config**: /etc/otelcol/config.yaml
- **Grafana config**: /etc/grafana/grafana.ini
- **mTLS certs**: /var/lib/observability/certs/

## Deployment

```bash
sudo bash services/observability/install.sh
```

## Firewall

Port 4318 is opened via Terraform in `infra/singlevm-setup/network.tf` (allow-otel rule).
