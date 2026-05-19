#!/usr/bin/env bash
# Build the universal base image for all Pi nodes (router + worker).
#
# This image has ALL packages pre-installed so cloud-init never runs apt.
# The playbooks only do configuration, no package installation.
#
# Requires: qemu-user-static (for ARM64 chroot on AMD64 host)
#
# Usage:
#   ./build-base-image.sh
#
# Output: base-arm64-raspi.img (used by router-node/ and basic-node/ build scripts)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUTPUT="${SCRIPT_DIR}/base-arm64-raspi.img"
UBUNTU_URL="https://cdimage.ubuntu.com/releases/noble/release/ubuntu-24.04.4-preinstalled-server-arm64+raspi.img.xz"
UBUNTU_IMG="${SCRIPT_DIR}/ubuntu-arm64-raspi.img"

# Check QEMU is available
if [[ ! -f /usr/bin/qemu-aarch64-static ]]; then
  echo "ERROR: qemu-user-static is required. Install with: sudo apt install qemu-user-static"
  exit 1
fi

echo "PROGRESS:5:Starting base image build"

# Download Ubuntu if not cached
if [[ ! -f "$UBUNTU_IMG" ]]; then
  echo "PROGRESS:10:Downloading Ubuntu Server 24.04 ARM64"
  curl -L -o "${UBUNTU_IMG}.xz" "$UBUNTU_URL"
  echo "PROGRESS:20:Decompressing base image"
  xz -d "${UBUNTU_IMG}.xz"
fi

echo "PROGRESS:25:Copying base image"
cp "$UBUNTU_IMG" "$OUTPUT"

# Mount root partition
echo "PROGRESS:30:Mounting image"
LOOP=$(sudo losetup --find --show --partscan "$OUTPUT")
MOUNT=$(mktemp -d)
sudo mount "${LOOP}p2" "$MOUNT"

# Set up chroot
echo "PROGRESS:35:Setting up ARM64 chroot"
sudo cp /usr/bin/qemu-aarch64-static "${MOUNT}/usr/bin/"
sudo mount -t proc proc "${MOUNT}/proc"
sudo mount -t sysfs sys "${MOUNT}/sys"
sudo mount -o bind /dev "${MOUNT}/dev"
sudo mount -o bind /dev/pts "${MOUNT}/dev/pts"

# Fix DNS (Ubuntu uses a symlink to systemd-resolved)
sudo rm -f "${MOUNT}/etc/resolv.conf"
echo "nameserver 8.8.8.8" | sudo tee "${MOUNT}/etc/resolv.conf" > /dev/null

# Install all packages
echo "PROGRESS:40:Installing packages (this is slow under QEMU)"
sudo chroot "$MOUNT" /usr/bin/qemu-aarch64-static /bin/bash -c '
set -e
export DEBIAN_FRONTEND=noninteractive

apt-get update -qq

# ── Shared packages (both router and worker) ────────────────────
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
  avahi-daemon

# ── Router packages ─────────────────────────────────────────────
apt-get install -y -qq \
  modemmanager \
  mobile-broadband-provider-info \
  network-manager \
  dnsmasq \
  iptables-persistent \
  netfilter-persistent \
  iw

# ── Worker packages (Kubernetes) ────────────────────────────────
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
echo "All packages installed successfully"
'

# Clean up chroot
echo "PROGRESS:90:Cleaning up chroot"
sudo umount "${MOUNT}/dev/pts" 2>/dev/null || true
sudo umount "${MOUNT}/dev" 2>/dev/null || true
sudo umount "${MOUNT}/sys" 2>/dev/null || true
sudo umount "${MOUNT}/proc" 2>/dev/null || true
sudo rm -f "${MOUNT}/usr/bin/qemu-aarch64-static"

# Restore systemd-resolved symlink
sudo rm -f "${MOUNT}/etc/resolv.conf"
sudo ln -s ../run/systemd/resolve/stub-resolv.conf "${MOUNT}/etc/resolv.conf"

echo "PROGRESS:95:Finalizing"
sudo umount "$MOUNT"
sudo losetup -d "$LOOP"
rmdir "$MOUNT"

echo "PROGRESS:100:Base image ready"
echo ""
echo "==> Base image: $OUTPUT ($(du -h "$OUTPUT" | cut -f1))"
echo ""
echo "Copy to router-node/ and basic-node/:"
echo "  cp $OUTPUT ../router-node/base-arm64-raspi.img"
echo "  cp $OUTPUT ../basic-node/base-arm64-raspi.img"
