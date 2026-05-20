#!/usr/bin/env bash
# Build a provisioned golden image for a Pi router node.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUTPUT="${1:-golden-image-router.img}"
BASE_IMAGE="/var/lib/homelab-bootstrap/base-router-arm64.img"
BASE_IMAGE_ZST="/var/lib/homelab-bootstrap/base-router-arm64.img.zst"

if [[ -z "${TOKEN:-}" ]]; then
  echo "ERROR: TOKEN env var is required." >&2
  exit 1
fi

echo "PROGRESS:0:Starting router image build"

# Phase 1: Copy or decompress base image
if [[ -f "$BASE_IMAGE" ]]; then
  echo "PROGRESS:5:Copying base image"
  BASE_SIZE=$(stat -c%s "$BASE_IMAGE")
  # Copy with progress monitoring
  cp "$BASE_IMAGE" "$OUTPUT" &
  CP_PID=$!
  BASE_MB=$(( BASE_SIZE / 1048576 ))
  while kill -0 $CP_PID 2>/dev/null; do
    if [[ -f "$OUTPUT" ]]; then
      CUR=$(stat -c%s "$OUTPUT" 2>/dev/null || echo 0)
      CUR_MB=$(( CUR / 1048576 ))
      PCT=$(( 5 + CUR * 25 / BASE_SIZE ))
      echo "PROGRESS:${PCT}:Copying base image (${CUR_MB}MB / ${BASE_MB}MB)"
    fi
    sleep 2
  done
  wait $CP_PID
elif [[ -f "$BASE_IMAGE_ZST" ]]; then
  echo "PROGRESS:5:Decompressing base image"
  zstd -d -o "$OUTPUT" "$BASE_IMAGE_ZST"
else
  echo "ERROR: Base image not found" >&2
  exit 1
fi
chmod 644 "$OUTPUT"
echo "PROGRESS:30:Base image ready"

# Phase 2: Mount and inject
echo "PROGRESS:35:Mounting image"
LOOP=$(sudo losetup --find --show --partscan "$OUTPUT")
MOUNT=$(mktemp -d)

echo "PROGRESS:38:Injecting cloud-init"
sudo mount "${LOOP}p1" "$MOUNT"
sudo cp "${SCRIPT_DIR}/user-data" "${MOUNT}/user-data"
echo "instance-id: pi-router-$(date +%Y%m%d%H%M%S)" | sudo tee "${MOUNT}/meta-data" > /dev/null
sudo tee "${MOUNT}/network-config" > /dev/null <<'NETCFG'
version: 2
ethernets:
  eth0:
    dhcp4: true
    optional: true
NETCFG
sudo umount "$MOUNT"

echo "PROGRESS:40:Injecting registration token"
sudo mount "${LOOP}p2" "$MOUNT"
echo -n "$TOKEN" | sudo tee "${MOUNT}/etc/image-token" > /dev/null
sudo chmod 600 "${MOUNT}/etc/image-token"

echo "PROGRESS:42:Injecting bootstrap WiFi"
if [[ -n "${WIFI_SSID:-}" && -n "${WIFI_PASSWORD:-}" ]]; then
  sudo mkdir -p "${MOUNT}/etc/NetworkManager/system-connections"
  sudo tee "${MOUNT}/etc/NetworkManager/system-connections/bootstrap-wifi.nmconnection" > /dev/null <<WIFIEOF
[connection]
id=bootstrap-wifi
type=wifi
autoconnect=true
autoconnect-priority=-1

[wifi]
ssid=${WIFI_SSID}
mode=infrastructure

[wifi-security]
key-mgmt=wpa-psk
psk=${WIFI_PASSWORD}

[ipv4]
method=auto

[ipv6]
method=auto
WIFIEOF
  sudo chmod 600 "${MOUNT}/etc/NetworkManager/system-connections/bootstrap-wifi.nmconnection"
fi
sudo umount "$MOUNT"
sudo losetup -d "$LOOP"
rmdir "$MOUNT"

# Phase 3: Compress for download
echo "PROGRESS:45:Compressing for download"
IMG_SIZE=$(stat -c%s "$OUTPUT")
IMG_MB=$(( IMG_SIZE / 1048576 ))
zstd -3 --rm -T0 "$OUTPUT" &
ZST_PID=$!
while kill -0 $ZST_PID 2>/dev/null; do
  if [[ -f "${OUTPUT}.zst" ]]; then
    CUR=$(stat -c%s "${OUTPUT}.zst" 2>/dev/null || echo 0)
    CUR_MB=$(( CUR / 1048576 ))
    # Estimate: compressed is ~30% of original
    EST_FINAL=$(( IMG_SIZE * 30 / 100 ))
    EST_MB=$(( EST_FINAL / 1048576 ))
    if [[ $EST_FINAL -gt 0 ]]; then
      PCT=$(( 45 + CUR * 54 / EST_FINAL ))
      [[ $PCT -gt 99 ]] && PCT=99
      echo "PROGRESS:${PCT}:Compressing (${CUR_MB}MB / ~${EST_MB}MB)"
    fi
  fi
  sleep 2
done
wait $ZST_PID

echo "PROGRESS:100:Image ready"
