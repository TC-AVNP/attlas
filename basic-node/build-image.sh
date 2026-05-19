#!/usr/bin/env bash
# Build the golden image for homelab Pi worker nodes.
#
# Usage:
#   TOKEN="abc123..." ./build-image.sh [output.img]
#
# Emits PROGRESS:<pct>:<message> lines for the provision API to stream.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUTPUT="${1:-golden-image.img}"
UBUNTU_URL="https://cdimage.ubuntu.com/releases/noble/release/ubuntu-24.04.4-preinstalled-server-arm64+raspi.img.xz"
UBUNTU_IMG="${SCRIPT_DIR}/ubuntu-arm64-raspi.img"

if [[ -z "${TOKEN:-}" ]]; then
  echo "ERROR: TOKEN env var is required." >&2
  exit 1
fi

echo "PROGRESS:5:Starting worker image build"

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
MOUNT_DIR=$(mktemp -d)
sudo mount "${LOOP}p1" "$MOUNT_DIR"

sudo cp "${SCRIPT_DIR}/user-data" "${MOUNT_DIR}/user-data"
echo "instance-id: homelab-$(date +%Y%m%d)" | sudo tee "${MOUNT_DIR}/meta-data" > /dev/null

echo "PROGRESS:85:Injecting registration token"
sudo umount "$MOUNT_DIR"
sudo mount "${LOOP}p2" "$MOUNT_DIR"
echo -n "$TOKEN" | sudo tee "${MOUNT_DIR}/etc/image-token" > /dev/null
sudo chmod 600 "${MOUNT_DIR}/etc/image-token"

echo "PROGRESS:95:Finalizing image"
sudo umount "$MOUNT_DIR"
sudo losetup -d "$LOOP"
rmdir "$MOUNT_DIR"

echo "PROGRESS:100:Image ready"
