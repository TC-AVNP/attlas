#!/usr/bin/env bash
# Build the base image for ROUTER Pi nodes.
#
# This runs INSIDE the ARM64 build VM (t2a-standard-16).
# Router image includes: networking (ModemManager, NM, dnsmasq, iptables, iw)
# but NO Kubernetes packages.
#
# Output: base-router-arm64.img.zst (uploaded to GCS)
set -uo pipefail

GCS_BUCKET="gs://attlas-base-images"
SIGNAL_FILE="${GCS_BUCKET}/build-done-router.signal"

trap 'echo "FAILED" | gsutil cp - "$SIGNAL_FILE" 2>/dev/null' ERR

set -e

export PATH="/snap/bin:/snap/google-cloud-cli/current/bin:$PATH"
cloud-init status --wait 2>/dev/null || true
apt-get update -qq && apt-get install -y -qq zstd cloud-guest-utils

UBUNTU_URL="https://cdimage.ubuntu.com/releases/noble/release/ubuntu-24.04.4-preinstalled-server-arm64+raspi.img.xz"
WORK="/tmp/base-image-build"
OUTPUT="${WORK}/base-router-arm64.img"

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

echo "=== Phase 4: Install ROUTER packages (native ARM64) ==="
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

# ── Router-only packages ────────────────────────────────────────
apt-get install -y -qq \
  modemmanager \
  mobile-broadband-provider-info \
  network-manager \
  dnsmasq \
  iptables-persistent \
  netfilter-persistent \
  iw

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
echo "Router packages installed successfully"
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
echo "Compressed router image: $FINAL ($SIZE)"

echo "=== Phase 7: Upload to GCS ==="
gsutil cp "$FINAL" "${GCS_BUCKET}/base-router-arm64.img.zst"
echo "Uploaded to ${GCS_BUCKET}/base-router-arm64.img.zst"

echo "SUCCESS" | gsutil cp - "$SIGNAL_FILE"
echo "=== ROUTER IMAGE DONE ==="
