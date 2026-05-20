#!/usr/bin/env bash
# Build the base image for WORKER Pi nodes.
#
# This runs INSIDE the ARM64 build VM (t2a-standard-16).
# Worker image includes: Kubernetes (kubelet, kubeadm, kubectl, containerd)
# but NO router networking packages (no modemmanager, dnsmasq, iptables, iw).
#
# Output: base-worker-arm64.img.zst (uploaded to GCS)
set -uo pipefail

GCS_BUCKET="gs://attlas-base-images"
SIGNAL_FILE="${GCS_BUCKET}/build-done-worker.signal"

trap 'echo "FAILED" | gsutil cp - "$SIGNAL_FILE" 2>/dev/null' ERR

set -e

export PATH="/snap/bin:/snap/google-cloud-cli/current/bin:$PATH"
cloud-init status --wait 2>/dev/null || true
apt-get update -qq && apt-get install -y -qq zstd cloud-guest-utils

UBUNTU_URL="https://cdimage.ubuntu.com/releases/noble/release/ubuntu-24.04.4-preinstalled-server-arm64+raspi.img.xz"
WORK="/tmp/base-image-build"
OUTPUT="${WORK}/base-worker-arm64.img"

mkdir -p "$WORK"
cd "$WORK"

echo "=== Phase 1: Download Ubuntu Server ARM64 ==="
if [[ ! -f ubuntu-arm64-raspi.img ]]; then
  curl -fSL -o ubuntu-arm64-raspi.img.xz "$UBUNTU_URL"
  echo "Decompressing..."
  xz -d ubuntu-arm64-raspi.img.xz
fi
cp ubuntu-arm64-raspi.img "$OUTPUT"

echo "=== Phase 2: Expand image to 6GB ==="
truncate -s 6G "$OUTPUT"
LOOP=$(sudo losetup --find --show --partscan "$OUTPUT")
sudo growpart "$LOOP" 2
sudo e2fsck -fy "${LOOP}p2" || true
sudo resize2fs "${LOOP}p2"

echo "=== Phase 3: Mount and install packages ==="
MOUNT=$(mktemp -d)
sudo mount "${LOOP}p2" "$MOUNT"

sudo mount -t proc proc "${MOUNT}/proc"
sudo mount -t sysfs sys "${MOUNT}/sys"
sudo mount -o bind /dev "${MOUNT}/dev"
sudo mount -o bind /dev/pts "${MOUNT}/dev/pts"

sudo rm -f "${MOUNT}/etc/resolv.conf"
echo "nameserver 8.8.8.8" | sudo tee "${MOUNT}/etc/resolv.conf" > /dev/null

echo "=== Phase 4: Install WORKER packages (native ARM64) ==="
sudo chroot "$MOUNT" /bin/bash -c '
set -e
export DEBIAN_FRONTEND=noninteractive

apt-get update -qq

# ── Shared packages ─────────────────────────────────────────────
apt-get install -y -qq \
  ansible \
  curl \
  jq \
  bc \
  netcat-openbsd \
  python3 \
  git \
  zsh \
  tmux \
  fzf \
  avahi-daemon \
  zstd

# ── Worker-only packages (Kubernetes) ───────────────────────────
apt-get install -y -qq \
  containerd \
  conntrack \
  socat \
  open-iscsi \
  nfs-common

# Kubernetes repo + packages
mkdir -p /etc/apt/keyrings
curl -fsSL "https://pkgs.k8s.io/core:/stable:/v1.31/deb/Release.key" | \
  gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.31/deb/ /" > \
  /etc/apt/sources.list.d/kubernetes.list
apt-get update -qq
apt-get install -y -qq kubelet kubeadm kubectl
apt-mark hold kubelet kubeadm kubectl

# ── Node.js + Claude Code ───────────────────────────────────────
curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
apt-get install -y -qq nodejs
npm install -g @anthropic-ai/claude-code

# ── OTel Collector ──────────────────────────────────────────────
ARCH=$(dpkg --print-architecture)
OTEL_VERSION="0.120.0"
curl -fsSL "https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v${OTEL_VERSION}/otelcol-contrib_${OTEL_VERSION}_linux_${ARCH}.tar.gz" \
  -o /tmp/otelcol.tar.gz
tar -xzf /tmp/otelcol.tar.gz -C /tmp/ otelcol-contrib
mv /tmp/otelcol-contrib /usr/local/bin/otelcol-contrib
chmod 755 /usr/local/bin/otelcol-contrib
rm -f /tmp/otelcol.tar.gz

# ── Cleanup ─────────────────────────────────────────────────────
apt-get clean
rm -rf /var/lib/apt/lists/* /tmp/*
echo "Worker packages installed successfully"
'

echo "=== Phase 5: Cleanup chroot ==="
sudo umount "${MOUNT}/dev/pts" 2>/dev/null || true
sudo umount "${MOUNT}/dev" 2>/dev/null || true
sudo umount "${MOUNT}/sys" 2>/dev/null || true
sudo umount "${MOUNT}/proc" 2>/dev/null || true

sudo rm -f "${MOUNT}/etc/resolv.conf"
sudo ln -s ../run/systemd/resolve/stub-resolv.conf "${MOUNT}/etc/resolv.conf"

sudo umount "$MOUNT"
sudo losetup -d "$LOOP"
rmdir "$MOUNT"

echo "=== Phase 6: Compress with zstd ==="
zstd -9 --rm -T0 "$OUTPUT"
FINAL="${OUTPUT}.zst"
SIZE=$(du -h "$FINAL" | cut -f1)
echo "Compressed worker image: $FINAL ($SIZE)"

echo "=== Phase 7: Upload to GCS ==="
gsutil cp "$FINAL" "${GCS_BUCKET}/base-worker-arm64.img.zst"
echo "Uploaded to ${GCS_BUCKET}/base-worker-arm64.img.zst"

echo "SUCCESS" | gsutil cp - "$SIGNAL_FILE"
echo "=== WORKER IMAGE DONE ==="
