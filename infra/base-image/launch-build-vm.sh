#!/usr/bin/env bash
# Spin up an ephemeral ARM64 VM, build BOTH base images, tear it down.
#
# Usage:
#   ./launch-build-vm.sh
#
# Builds:
#   1. base-router-arm64.img.zst (router packages, no k8s)
#   2. base-worker-arm64.img.zst (k8s packages, no router networking)
#
# Cost: ~$1 per run (16 vCPU ARM64 SPOT, ~10-15 min for both)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT="petprojects-488115"
ZONE="europe-west4-a"
VM_NAME="pi-image-builder"
MACHINE_TYPE="t2a-standard-16"
DISK_SIZE="50"
IMAGE_FAMILY="ubuntu-2404-lts-arm64"
IMAGE_PROJECT="ubuntu-os-cloud"
GCS_BUCKET="gs://attlas-base-images"
PROVISION_DIR="/var/lib/homelab-bootstrap"

echo "=== Step 1: Ensure GCS bucket exists ==="
gsutil ls "$GCS_BUCKET" 2>/dev/null || \
  gsutil mb -l europe-west4 -p "$PROJECT" "$GCS_BUCKET"

# Clean stale signals
gsutil rm "${GCS_BUCKET}/build-done-router.signal" 2>/dev/null || true
gsutil rm "${GCS_BUCKET}/build-done-worker.signal" 2>/dev/null || true

echo "=== Step 2: Create ARM64 build VM ==="
if gcloud compute instances describe "$VM_NAME" --zone="$ZONE" --project="$PROJECT" &>/dev/null; then
  echo "Deleting stale build VM..."
  gcloud compute instances delete "$VM_NAME" --zone="$ZONE" --project="$PROJECT" --quiet
fi

# Create VM with a startup script that runs both builds sequentially
cat "${SCRIPT_DIR}/build-router.sh" "${SCRIPT_DIR}/build-worker.sh" > /tmp/combined-build.sh

gcloud compute instances create "$VM_NAME" \
  --project="$PROJECT" \
  --zone="$ZONE" \
  --machine-type="$MACHINE_TYPE" \
  --image-family="$IMAGE_FAMILY" \
  --image-project="$IMAGE_PROJECT" \
  --boot-disk-size="${DISK_SIZE}GB" \
  --boot-disk-type=pd-ssd \
  --provisioning-model=SPOT \
  --instance-termination-action=DELETE \
  --scopes=storage-rw \
  --metadata-from-file=startup-script=/tmp/combined-build.sh \
  --quiet

rm -f /tmp/combined-build.sh
echo "VM created. Building both images autonomously..."

echo "=== Step 3: Wait for ROUTER image ==="
POLL_INTERVAL=15
TIMEOUT=900
ELAPSED=0
while true; do
  if gsutil -q stat "${GCS_BUCKET}/build-done-router.signal" 2>/dev/null; then
    SIGNAL=$(gsutil cat "${GCS_BUCKET}/build-done-router.signal" 2>/dev/null)
    if [[ "$SIGNAL" == "SUCCESS" ]]; then
      echo ""
      echo "Router image built!"
      break
    else
      echo ""
      echo "ERROR: Router build failed."
      gcloud compute instances delete "$VM_NAME" --zone="$ZONE" --project="$PROJECT" --quiet 2>/dev/null || true
      exit 1
    fi
  fi
  if [[ $ELAPSED -ge $TIMEOUT ]]; then
    echo ""
    echo "ERROR: Router build timed out after ${TIMEOUT}s"
    gcloud compute instances delete "$VM_NAME" --zone="$ZONE" --project="$PROJECT" --quiet 2>/dev/null || true
    exit 1
  fi
  printf "."
  sleep $POLL_INTERVAL
  ELAPSED=$((ELAPSED + POLL_INTERVAL))
done

echo "=== Step 4: Wait for WORKER image ==="
ELAPSED=0
while true; do
  if gsutil -q stat "${GCS_BUCKET}/build-done-worker.signal" 2>/dev/null; then
    SIGNAL=$(gsutil cat "${GCS_BUCKET}/build-done-worker.signal" 2>/dev/null)
    if [[ "$SIGNAL" == "SUCCESS" ]]; then
      echo ""
      echo "Worker image built!"
      break
    else
      echo ""
      echo "ERROR: Worker build failed."
      gcloud compute instances delete "$VM_NAME" --zone="$ZONE" --project="$PROJECT" --quiet 2>/dev/null || true
      exit 1
    fi
  fi
  if [[ $ELAPSED -ge $TIMEOUT ]]; then
    echo ""
    echo "ERROR: Worker build timed out after ${TIMEOUT}s"
    gcloud compute instances delete "$VM_NAME" --zone="$ZONE" --project="$PROJECT" --quiet 2>/dev/null || true
    exit 1
  fi
  printf "."
  sleep $POLL_INTERVAL
  ELAPSED=$((ELAPSED + POLL_INTERVAL))
done

echo "=== Step 5: Download artifacts from GCS ==="
gsutil cp "${GCS_BUCKET}/base-router-arm64.img.zst" "${SCRIPT_DIR}/base-router-arm64.img.zst"
gsutil cp "${GCS_BUCKET}/base-worker-arm64.img.zst" "${SCRIPT_DIR}/base-worker-arm64.img.zst"
echo "Downloaded both to ${SCRIPT_DIR}/"

echo "=== Step 6: Install to provision directory ==="
# Compressed (backup)
sudo cp "${SCRIPT_DIR}/base-router-arm64.img.zst" "${PROVISION_DIR}/base-router-arm64.img.zst"
sudo cp "${SCRIPT_DIR}/base-worker-arm64.img.zst" "${PROVISION_DIR}/base-worker-arm64.img.zst"
# Uncompressed (fast provisioning — avoids zstd -d on every image build)
sudo zstd -d -f -o "${PROVISION_DIR}/base-router-arm64.img" "${PROVISION_DIR}/base-router-arm64.img.zst"
sudo zstd -d -f -o "${PROVISION_DIR}/base-worker-arm64.img" "${PROVISION_DIR}/base-worker-arm64.img.zst"
sudo chmod 644 "${PROVISION_DIR}/base-router-arm64.img" "${PROVISION_DIR}/base-worker-arm64.img"
echo "Installed compressed + uncompressed versions"

echo "=== Step 7: Tear down build VM ==="
gcloud compute instances delete "$VM_NAME" \
  --zone="$ZONE" --project="$PROJECT" --quiet

# Clean up signal files
gsutil rm "${GCS_BUCKET}/build-done-router.signal" 2>/dev/null || true
gsutil rm "${GCS_BUCKET}/build-done-worker.signal" 2>/dev/null || true

echo ""
echo "=== BUILD COMPLETE ==="
echo "Router: ${PROVISION_DIR}/base-router-arm64.img ($(sudo du -h "${PROVISION_DIR}/base-router-arm64.img" | cut -f1))"
echo "Worker: ${PROVISION_DIR}/base-worker-arm64.img ($(sudo du -h "${PROVISION_DIR}/base-worker-arm64.img" | cut -f1))"
echo "GCS:    ${GCS_BUCKET}/base-{router,worker}-arm64.img.zst"
