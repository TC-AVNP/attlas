#!/usr/bin/env bash
# Spin up an ephemeral ARM64 VM, build base images, tear it down.
#
# Usage:
#   ./launch-build-vm.sh              # build all (universal + legacy)
#   ./launch-build-vm.sh universal    # universal golden image only
#   ./launch-build-vm.sh legacy       # legacy router + worker only
#
# Cost: ~$1 per run (16 vCPU ARM64 SPOT, ~5-15 min)
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

MODE="${1:-all}"

echo "=== Step 1: Ensure GCS bucket exists ==="
gsutil ls "$GCS_BUCKET" 2>/dev/null || \
  gsutil mb -l europe-west4 -p "$PROJECT" "$GCS_BUCKET"

# Clean stale signals
gsutil rm "${GCS_BUCKET}/build-done-universal.signal" 2>/dev/null || true
gsutil rm "${GCS_BUCKET}/build-done-router.signal" 2>/dev/null || true
gsutil rm "${GCS_BUCKET}/build-done-worker.signal" 2>/dev/null || true

echo "=== Step 2: Create ARM64 build VM ==="
if gcloud compute instances describe "$VM_NAME" --zone="$ZONE" --project="$PROJECT" &>/dev/null; then
  echo "Deleting stale build VM..."
  gcloud compute instances delete "$VM_NAME" --zone="$ZONE" --project="$PROJECT" --quiet
fi

# Assemble startup script based on mode
COMBINED="/tmp/combined-build.sh"
: > "$COMBINED"
if [[ "$MODE" == "all" || "$MODE" == "universal" ]]; then
  cat "${SCRIPT_DIR}/build-universal.sh" >> "$COMBINED"
fi
if [[ "$MODE" == "all" || "$MODE" == "legacy" ]]; then
  cat "${SCRIPT_DIR}/build-router.sh" >> "$COMBINED"
  cat "${SCRIPT_DIR}/build-worker.sh" >> "$COMBINED"
fi

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
  --metadata-from-file=startup-script="$COMBINED" \
  --quiet

rm -f "$COMBINED"
echo "VM created. Building images (mode: ${MODE})..."

# ── Poll helper ──────────────────────────────────────────────────
wait_for_signal() {
  local name="$1" signal_path="$2"
  local poll=15 timeout=900 elapsed=0
  echo "=== Waiting for ${name} ==="
  while true; do
    if gsutil -q stat "$signal_path" 2>/dev/null; then
      local sig
      sig=$(gsutil cat "$signal_path" 2>/dev/null)
      if [[ "$sig" == "SUCCESS" ]]; then
        echo ""
        echo "${name} built!"
        return 0
      else
        echo ""
        echo "ERROR: ${name} build failed."
        gcloud compute instances delete "$VM_NAME" --zone="$ZONE" --project="$PROJECT" --quiet 2>/dev/null || true
        exit 1
      fi
    fi
    if [[ $elapsed -ge $timeout ]]; then
      echo ""
      echo "ERROR: ${name} build timed out after ${timeout}s"
      gcloud compute instances delete "$VM_NAME" --zone="$ZONE" --project="$PROJECT" --quiet 2>/dev/null || true
      exit 1
    fi
    printf "."
    sleep $poll
    elapsed=$((elapsed + poll))
  done
}

# ── Wait for each image ─────────────────────────────────────────
if [[ "$MODE" == "all" || "$MODE" == "universal" ]]; then
  wait_for_signal "Universal image" "${GCS_BUCKET}/build-done-universal.signal"
fi
if [[ "$MODE" == "all" || "$MODE" == "legacy" ]]; then
  wait_for_signal "Router image" "${GCS_BUCKET}/build-done-router.signal"
  wait_for_signal "Worker image" "${GCS_BUCKET}/build-done-worker.signal"
fi

echo "=== Download artifacts from GCS ==="
if [[ "$MODE" == "all" || "$MODE" == "universal" ]]; then
  gsutil cp "${GCS_BUCKET}/bfm-universal-arm64.img.zst" "${SCRIPT_DIR}/bfm-universal-arm64.img.zst"
fi
if [[ "$MODE" == "all" || "$MODE" == "legacy" ]]; then
  gsutil cp "${GCS_BUCKET}/base-router-arm64.img.zst" "${SCRIPT_DIR}/base-router-arm64.img.zst"
  gsutil cp "${GCS_BUCKET}/base-worker-arm64.img.zst" "${SCRIPT_DIR}/base-worker-arm64.img.zst"
fi
echo "Downloaded to ${SCRIPT_DIR}/"

echo "=== Install to provision directory ==="
sudo mkdir -p "$PROVISION_DIR"
if [[ "$MODE" == "all" || "$MODE" == "universal" ]]; then
  sudo cp "${SCRIPT_DIR}/bfm-universal-arm64.img.zst" "${PROVISION_DIR}/bfm-universal-arm64.img.zst"
  sudo zstd -d -f -o "${PROVISION_DIR}/bfm-universal-arm64.img" "${PROVISION_DIR}/bfm-universal-arm64.img.zst"
  sudo chmod 644 "${PROVISION_DIR}/bfm-universal-arm64.img"
fi
if [[ "$MODE" == "all" || "$MODE" == "legacy" ]]; then
  sudo cp "${SCRIPT_DIR}/base-router-arm64.img.zst" "${PROVISION_DIR}/base-router-arm64.img.zst"
  sudo cp "${SCRIPT_DIR}/base-worker-arm64.img.zst" "${PROVISION_DIR}/base-worker-arm64.img.zst"
  sudo zstd -d -f -o "${PROVISION_DIR}/base-router-arm64.img" "${PROVISION_DIR}/base-router-arm64.img.zst"
  sudo zstd -d -f -o "${PROVISION_DIR}/base-worker-arm64.img" "${PROVISION_DIR}/base-worker-arm64.img.zst"
  sudo chmod 644 "${PROVISION_DIR}/base-router-arm64.img" "${PROVISION_DIR}/base-worker-arm64.img"
fi
echo "Installed compressed + uncompressed versions"

echo "=== Tear down build VM ==="
gcloud compute instances delete "$VM_NAME" \
  --zone="$ZONE" --project="$PROJECT" --quiet

# Clean up signal files
gsutil rm "${GCS_BUCKET}/build-done-universal.signal" 2>/dev/null || true
gsutil rm "${GCS_BUCKET}/build-done-router.signal" 2>/dev/null || true
gsutil rm "${GCS_BUCKET}/build-done-worker.signal" 2>/dev/null || true

echo ""
echo "=== BUILD COMPLETE (mode: ${MODE}) ==="
if [[ "$MODE" == "all" || "$MODE" == "universal" ]]; then
  echo "Universal: ${PROVISION_DIR}/bfm-universal-arm64.img ($(sudo du -h "${PROVISION_DIR}/bfm-universal-arm64.img" | cut -f1))"
fi
if [[ "$MODE" == "all" || "$MODE" == "legacy" ]]; then
  echo "Router:    ${PROVISION_DIR}/base-router-arm64.img ($(sudo du -h "${PROVISION_DIR}/base-router-arm64.img" | cut -f1))"
  echo "Worker:    ${PROVISION_DIR}/base-worker-arm64.img ($(sudo du -h "${PROVISION_DIR}/base-worker-arm64.img" | cut -f1))"
fi
echo "GCS:       ${GCS_BUCKET}/"
