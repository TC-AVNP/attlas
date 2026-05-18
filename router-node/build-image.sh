#!/usr/bin/env bash
# Build the golden image for the Pi 3B+ router node.
#
# On first boot, the Pi registers itself with homelab-bootstrap at
# homelab.attlas.uk, which creates a Cloudflare tunnel and returns the
# connector token. The Pi then starts cloudflared automatically.
#
# Usage:
#   ./build-image.sh [output.img]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUTPUT="${1:-golden-image-router.img}"
UBUNTU_URL="https://cdimage.ubuntu.com/releases/noble/release/ubuntu-24.04.4-preinstalled-server-arm64+raspi.img.xz"
UBUNTU_IMG="ubuntu-arm64-raspi.img"

echo "==> Building router golden image"

if [[ ! -f "$UBUNTU_IMG" ]]; then
  echo "==> Downloading Ubuntu Server 24.04 ARM64..."
  curl -L -o "${UBUNTU_IMG}.xz" "$UBUNTU_URL"
  xz -d "${UBUNTU_IMG}.xz"
fi
echo "    Base image: $UBUNTU_IMG"

echo "==> Creating image"
cp "$UBUNTU_IMG" "$OUTPUT"

echo "==> Injecting cloud-init"
LOOP=$(sudo losetup --find --show --partscan "$OUTPUT")
MOUNT=$(mktemp -d)
sudo mount "${LOOP}p1" "$MOUNT"

sudo cp "${SCRIPT_DIR}/user-data" "${MOUNT}/user-data"
echo "instance-id: pi-router-$(date +%Y%m%d)" | sudo tee "${MOUNT}/meta-data" > /dev/null

# Network config for WiFi
sudo tee "${MOUNT}/network-config" > /dev/null <<'NETCFG'
version: 2
wifis:
  wlan0:
    dhcp4: true
    access-points:
      "Iphone 4E":
        password: "xadrez12"
ethernets:
  eth0:
    dhcp4: true
    optional: true
NETCFG

sudo umount "$MOUNT"
sudo losetup -d "$LOOP"
rmdir "$MOUNT"

echo ""
echo "==> Router image ready: $OUTPUT"
echo "Flash with Pi Imager → Use custom → select this file"
