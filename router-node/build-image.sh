#!/usr/bin/env bash
# Build the golden image for the Pi 3B+ router node.
#
# Usage:
#   TOKEN="abc123..." ./build-image.sh [output.img]
#
# Emits PROGRESS:<pct>:<message> lines for the provision API to stream.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUTPUT="${1:-golden-image-router.img}"
UBUNTU_URL="https://cdimage.ubuntu.com/releases/noble/release/ubuntu-24.04.4-preinstalled-server-arm64+raspi.img.xz"
UBUNTU_IMG="${SCRIPT_DIR}/ubuntu-arm64-raspi.img"

if [[ -z "${TOKEN:-}" ]]; then
  echo "ERROR: TOKEN env var is required." >&2
  exit 1
fi

echo "PROGRESS:5:Starting router image build"

if [[ ! -f "$UBUNTU_IMG" ]]; then
  echo "PROGRESS:10:Downloading Ubuntu Server 24.04 ARM64"
  curl -L -o "${UBUNTU_IMG}.xz" "$UBUNTU_URL"
  echo "PROGRESS:30:Decompressing base image"
  xz -d "${UBUNTU_IMG}.xz"
fi

echo "PROGRESS:40:Copying base image"
cp "$UBUNTU_IMG" "$OUTPUT"
chmod 644 "$OUTPUT"

echo "PROGRESS:70:Injecting cloud-init"
LOOP=$(sudo losetup --find --show --partscan "$OUTPUT")
MOUNT=$(mktemp -d)
sudo mount "${LOOP}p1" "$MOUNT"

sudo cp "${SCRIPT_DIR}/user-data" "${MOUNT}/user-data"
echo "instance-id: pi-router-$(date +%Y%m%d)" | sudo tee "${MOUNT}/meta-data" > /dev/null

sudo tee "${MOUNT}/network-config" > /dev/null <<'NETCFG'
version: 2
ethernets:
  eth0:
    dhcp4: true
    optional: true
NETCFG

echo "PROGRESS:85:Injecting registration token"
sudo umount "$MOUNT"
sudo mount "${LOOP}p2" "$MOUNT"
echo -n "$TOKEN" | sudo tee "${MOUNT}/etc/image-token" > /dev/null
sudo chmod 600 "${MOUNT}/etc/image-token"

echo "PROGRESS:95:Finalizing image"
sudo umount "$MOUNT"
sudo losetup -d "$LOOP"
rmdir "$MOUNT"

echo "PROGRESS:100:Image ready"
