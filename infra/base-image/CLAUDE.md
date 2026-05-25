# Base Image Builder

Builds ARM64 base images for Raspberry Pi nodes.

## Images

### Universal (BFM)
- **`bfm-universal-arm64.img.zst`** — shared packages only. No role-specific software. PXE-ready.
- Role differentiation (k8s worker, router, storage, etc.) is handled by Ansible playbooks at boot time.
- Works on Pi 3, 4, and 5 — the Pi firmware selects the right kernel/DTB.

### Legacy (role-specific)
- **`base-router-arm64.img.zst`** — networking packages (ModemManager, NM, dnsmasq, iptables, iw). NO Kubernetes.
- **`base-worker-arm64.img.zst`** — Kubernetes packages (kubelet, kubeadm, kubectl, containerd). NO router networking.

## When to rebuild

Rebuild when:
- A package is added or removed from the install list
- A package version needs to be bumped (e.g., OTel Collector)
- Ubuntu base version changes

You do NOT need to rebuild for:
- Cloud-init, playbook, or token changes (injected at provision time)

## How to build

**You MUST use the ARM64 build VM. Do NOT build on the zombie VM (AMD64) using QEMU.**

```bash
cd /home/agnostic-user/iapetus/attlas/infra/base-image
./launch-build-vm.sh              # build all (universal + legacy)
./launch-build-vm.sh universal    # universal golden image only
./launch-build-vm.sh legacy       # legacy router + worker only
```

This will:
1. Create a `t2a-standard-16` (16-core ARM64) SPOT VM in `europe-west4-a` (~$1/run)
2. Build the selected image(s) — native ARM64, no QEMU, ~5 min each
3. Upload to `gs://attlas-base-images/`
4. Download and install (compressed + uncompressed) to `/var/lib/homelab-bootstrap/`
5. Delete the build VM

## Files

| File | Purpose |
|------|---------|
| `launch-build-vm.sh` | Entry point — creates ARM64 VM, builds selected images, tears down |
| `build-universal.sh` | Universal (BFM) build — shared packages only, PXE-ready |
| `build-router.sh` | Legacy router build — runs inside the ARM64 VM |
| `build-worker.sh` | Legacy worker build — runs inside the ARM64 VM |
| `CLAUDE.md` | You are here |

## Package allocation

| Package | Universal | Router (legacy) | Worker (legacy) | Purpose |
|---------|-----------|-----------------|-----------------|---------|
| ansible/curl/jq/git/zsh/tmux/fzf | yes | yes | yes | Shared tooling |
| nodejs + claude-code | yes | yes | yes | AI assistant |
| otelcol-contrib | yes | yes | yes | Telemetry |
| avahi-daemon | yes | yes | yes | mDNS |
| nfs-common | yes | no | yes | NFS root boot |
| modemmanager | no | yes | no | SIM/4G modem |
| network-manager | no | yes | no | WiFi AP |
| dnsmasq | no | yes | no | DHCP server |
| iptables-persistent | no | yes | no | NAT/firewall |
| iw | no | yes | no | WiFi config |
| containerd | no | no | yes | Container runtime |
| kubelet/kubeadm/kubectl | no | no | yes | Kubernetes |
| conntrack/socat/open-iscsi | no | no | yes | K8s dependencies |
