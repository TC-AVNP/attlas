# Homelab Bootstrap

mTLS-protected node registration service at `homelab.attlas.uk`.

## Problem

When a new Raspberry Pi 5 is added to the homelab cluster, it needs a
way to register itself with the control plane — announcing its hardware
specs, getting tracked in an inventory, and establishing identity. This
service handles that registration over mutual TLS so only Pis with the
golden image client cert can register.

## Architecture

```
Pi 5 (golden image, client cert)
   |
   | HTTPS + client cert
   v
 Caddy (homelab.attlas.uk, verifies client cert against CA)
   |
   | X-Client-Cert-Fingerprint header
   v
 homelab-bootstrap (Go, 127.0.0.1:7695)
   |
   v
 SQLite (/var/lib/homelab-bootstrap/homelab.db)
```

Caddy enforces mTLS — only clients presenting a certificate signed by
the homelab CA (`/var/lib/homelab-bootstrap/ca.crt`) can connect. The
Go service receives pre-authenticated requests.

## API

| Method | Path | Purpose |
|--------|------|---------|
| POST | /api/register | Register a node (MAC, NVMe serial, hostname, model, CPU, RAM, LAN IP) |
| GET | /api/nodes | List all registered nodes as JSON |
| DELETE | /api/nodes/{mac} | Deregister a node by MAC address |

The node inventory UI lives on the main attlas dashboard (`attlas.uk`),
which proxies to this service over localhost via `GET /api/homelab/nodes`.

## Registration payload

```json
{
  "mac_address": "dc:a6:32:xx:xx:xx",
  "nvme_serial": "CT1000P310SSD8_...",
  "hostname": "pi-1",
  "model": "Raspberry Pi 5 Model B Rev 1.0",
  "cpu_cores": 4,
  "memory_mb": 16384,
  "lan_ip": "10.0.0.11"
}
```

Only `mac_address` and `hostname` are required. All other fields are
optional — Pis without NVMe (booting from SD) simply omit `nvme_serial`.

The Pi collects this on boot via:
- `cat /proc/device-tree/model` — model string
- `nproc` — CPU cores
- `free -m | awk '/Mem:/{print $2}'` — total RAM in MB
- `ip -4 addr show eth0 | grep inet | awk '{print $2}' | cut -d/ -f1` — LAN IP
- `cat /sys/class/net/eth0/address` — MAC address
- `cat /sys/block/nvme0n1/serial` — NVMe serial (optional, only if NVMe present)

## Pi-side healthz

Each Pi runs a tiny healthz daemon as a systemd service (auto-starts on
boot, restarts on failure) on port 9100 that responds to `GET /healthz`
with node status. This is reachable from within the home LAN only
(10.0.0.0/24). Use it to check if Pis are alive when you have LAN
access or a tunnel. Port 8080 is reserved for application workloads.

## Certificates

Generated on first `install.sh` run:
- `ca.key` / `ca.crt` — the CA (stays on the VM, never leaves)
- `client.key` / `client.crt` — baked into the golden image

To issue additional client certs (e.g. for your laptop):
```bash
openssl genrsa -out laptop.key 2048
openssl req -new -key laptop.key -out laptop.csr -subj "/CN=laptop/O=attlas"
openssl x509 -req -days 3650 -in laptop.csr \
  -CA /var/lib/homelab-bootstrap/ca.crt \
  -CAkey /var/lib/homelab-bootstrap/ca.key \
  -CAcreateserial -out laptop.crt
```

## Layout

```
services/homelab-bootstrap/
├── CLAUDE.md
├── install.sh
├── uninstall.sh
├── homelab-bootstrap.caddy
└── server/
    ├── go.mod / go.sum
    ├── main.go
    └── migrations/
        └── 001_init.sql
```

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `HOMELAB_PORT` | `7695` | HTTP listen port |
| `HOMELAB_DB` | `/var/lib/homelab-bootstrap/homelab.db` | SQLite path |

## Development

```bash
cd server
PATH="/usr/local/go/bin:$PATH" go build -o /tmp/homelab-bootstrap .
HOMELAB_DB=/tmp/homelab-test.db /tmp/homelab-bootstrap
```

Test registration (without mTLS, localhost):
```bash
curl -X POST http://localhost:7695/api/register \
  -H "Content-Type: application/json" \
  -d '{"mac_address":"dc:a6:32:aa:bb:cc","nvme_serial":"CT1000_TEST","hostname":"pi-test","model":"Raspberry Pi 5 Model B Rev 1.0","cpu_cores":4,"memory_mb":16384,"lan_ip":"10.0.0.11"}'
```

## Deployment

```bash
sudo bash install.sh
```
