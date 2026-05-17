#!/usr/bin/env bash
# Build the golden image for homelab Pi nodes.
#
# Usage:
#   ./build-image.sh /path/to/output.img
#
# The output is a raw disk image ready to flash to SD/NVMe.
# Auth to the bootstrap service is via basic auth (credentials in the user-data).
# No certificates needed in the image.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUTPUT="${1:-golden-image.img}"
UBUNTU_URL="https://cdimage.ubuntu.com/releases/noble/release/ubuntu-24.04.4-preinstalled-server-arm64+raspi.img.xz"
UBUNTU_IMG="ubuntu-arm64-raspi.img"

echo "==> Building homelab golden image"

# ── 1. Download Ubuntu if not cached ────────────────────────────────
if [[ ! -f "$UBUNTU_IMG" ]]; then
  echo "==> Downloading Ubuntu Server 24.04 ARM64 for Raspberry Pi..."
  curl -L -o "${UBUNTU_IMG}.xz" "$UBUNTU_URL"
  echo "    Decompressing..."
  xz -d "${UBUNTU_IMG}.xz"
fi
echo "    Base image: $UBUNTU_IMG"

# ── 2. Copy the base image as our golden image ──────────────────────
echo "==> Creating golden image from base"
cp "$UBUNTU_IMG" "$OUTPUT"

# ── 3. Mount boot partition and inject cloud-init ────────────────────
echo "==> Mounting boot partition to inject cloud-init"
LOOP=$(sudo losetup --find --show --partscan "$OUTPUT")
MOUNT_DIR=$(mktemp -d)
sudo mount "${LOOP}p1" "$MOUNT_DIR"

# ── 4. Write our user-data (replaces Ubuntu default) ─────────────────
echo "==> Injecting cloud-init user-data"
sudo cp "${SCRIPT_DIR}/user-data" "${MOUNT_DIR}/user-data"
echo "instance-id: homelab-$(date +%Y%m%d)" | sudo tee "${MOUNT_DIR}/meta-data" > /dev/null
echo "    user-data and meta-data written"

# ── 5. Unmount ──────────────────────────────────────────────────────
echo "==> Unmounting"
sudo umount "$MOUNT_DIR"
sudo losetup -d "$LOOP"
rmdir "$MOUNT_DIR"

echo ""
echo "==> Golden image ready: $OUTPUT"
echo ""
echo "Flash to SD card:"
echo "  sudo dd if=$OUTPUT of=/dev/<device> bs=4M status=progress"
echo ""
echo "Or use balenaEtcher / Raspberry Pi Imager."
