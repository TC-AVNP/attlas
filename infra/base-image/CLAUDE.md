# Base Image Builder

Builds TWO separate ARM64 base images for Raspberry Pi nodes:
- **`base-router-arm64.img.zst`** — networking packages (ModemManager, NM, dnsmasq, iptables, iw). NO Kubernetes.
- **`base-worker-arm64.img.zst`** — Kubernetes packages (kubelet, kubeadm, kubectl, containerd). NO router networking.

## When to rebuild

Rebuild when:
- A package is added or removed from the install list
- A package version needs to be bumped (e.g., Kubernetes, OTel Collector)
- Ubuntu base version changes

You do NOT need to rebuild for:
- Cloud-init, playbook, or token changes (injected at provision time)

## How to build

**You MUST use the ARM64 build VM. Do NOT build on the zombie VM (AMD64) using QEMU.**

```bash
cd /home/agnostic-user/iapetus/attlas/infra/base-image
./launch-build-vm.sh
```

This will:
1. Create a `t2a-standard-16` (16-core ARM64) SPOT VM in `europe-west4-a` (~$1/run)
2. Build router image, then worker image — native ARM64, no QEMU, ~5 min each
3. Upload both to `gs://attlas-base-images/`
4. Download and install both (compressed + uncompressed) to `/var/lib/homelab-bootstrap/`
5. Delete the build VM

## Files

| File | Purpose |
|------|---------|
| `launch-build-vm.sh` | Entry point — creates ARM64 VM, builds both, tears down |
| `build-router.sh` | Router build recipe — runs inside the ARM64 VM |
| `build-worker.sh` | Worker build recipe — runs inside the ARM64 VM |
| `CLAUDE.md` | You are here |

## Package split

| Package | Router | Worker | Purpose |
|---------|--------|--------|---------|
| modemmanager | yes | no | SIM/4G modem |
| network-manager | yes | no | WiFi AP, NM connections |
| dnsmasq | yes | no | DHCP server |
| iptables-persistent | yes | no | NAT/firewall |
| iw | yes | no | WiFi config |
| containerd | no | yes | Container runtime |
| kubelet/kubeadm/kubectl | no | yes | Kubernetes |
| conntrack/socat/open-iscsi/nfs-common | no | yes | K8s dependencies |
| ansible/curl/jq/git/zsh/nodejs/otel | yes | yes | Shared |
