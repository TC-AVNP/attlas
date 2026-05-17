#!/usr/bin/env bash
# Set up a single-node Kubernetes control plane on the GCP VM.
# This becomes the initial control plane that Pis will join later.
# Uses kubeadm with stacked etcd (replicated when Pis join as control-plane members).
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: setup-k8s.sh must run as root." >&2
  exit 1
fi

echo "==> Setting up Kubernetes control plane"

# Get the VM's external IP for the control-plane endpoint
EXTERNAL_IP=$(curl -sf -H "Metadata-Flavor: Google" \
  http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip)
echo "    External IP: ${EXTERNAL_IP}"

# ── 1. Kernel prerequisites ───────────────────────────────────────────

echo "==> Configuring kernel modules and sysctl"

cat > /etc/modules-load.d/k8s.conf <<EOF
overlay
br_netfilter
EOF

modprobe overlay
modprobe br_netfilter

cat > /etc/sysctl.d/k8s.conf <<EOF
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOF

sysctl --system > /dev/null 2>&1
echo "    Kernel modules and sysctl configured"

# ── 2. Disable swap ──────────────────────────────────────────────────

echo "==> Disabling swap"
swapoff -a || true
# Remove swap entries from fstab permanently
sed -i '/\sswap\s/d' /etc/fstab
echo "    Swap disabled"

# ── 3. Install containerd ────────────────────────────────────────────

if ! command -v containerd &>/dev/null; then
  echo "==> Installing containerd"
  apt-get update -qq
  apt-get install -y -qq containerd conntrack > /dev/null

  # Generate default config and enable systemd cgroup driver
  mkdir -p /etc/containerd
  containerd config default > /etc/containerd/config.toml
  sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml

  systemctl restart containerd
  systemctl enable containerd
  echo "    containerd installed and configured"
else
  echo "    containerd already installed"
fi

# ── 4. Install kubeadm, kubelet, kubectl ─────────────────────────────

if ! command -v kubeadm &>/dev/null; then
  echo "==> Installing kubeadm, kubelet, kubectl"

  apt-get install -y -qq apt-transport-https ca-certificates curl gpg > /dev/null

  # Add Kubernetes apt repo (v1.31 — latest stable)
  K8S_VERSION="v1.31"
  mkdir -p /etc/apt/keyrings
  curl -fsSL "https://pkgs.k8s.io/core:/stable:/${K8S_VERSION}/deb/Release.key" | \
    gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg

  echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/${K8S_VERSION}/deb/ /" | \
    tee /etc/apt/sources.list.d/kubernetes.list > /dev/null

  apt-get update -qq
  apt-get install -y -qq kubelet kubeadm kubectl > /dev/null

  # Pin versions to prevent accidental upgrades
  apt-mark hold kubelet kubeadm kubectl > /dev/null

  systemctl enable kubelet
  echo "    kubeadm, kubelet, kubectl installed (${K8S_VERSION})"
else
  echo "    kubeadm already installed: $(kubeadm version -o short)"
fi

# ── 5. Initialize the cluster ────────────────────────────────────────

if [[ -f /etc/kubernetes/admin.conf ]]; then
  echo "    Cluster already initialized, skipping kubeadm init"
else
  echo "==> Initializing Kubernetes cluster"
  echo "    Control plane endpoint: ${EXTERNAL_IP}:6443"
  echo "    Pod network CIDR: 10.244.0.0/16 (Flannel/Calico default)"

  # Use internal IP as control-plane-endpoint because GCP doesn't allow
  # hairpin NAT (VM can't reach its own external IP). External IP is in
  # cert SANs so Pis can connect via it. When Pis join, their kubeconfig
  # will point at the external IP.
  INTERNAL_IP=$(curl -sf -H "Metadata-Flavor: Google" \
    http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/ip)
  echo "    Internal IP: ${INTERNAL_IP}"

  kubeadm init \
    --control-plane-endpoint "${INTERNAL_IP}:6443" \
    --upload-certs \
    --pod-network-cidr "10.244.0.0/16" \
    --apiserver-cert-extra-sans "${EXTERNAL_IP},homelab.attlas.uk" \
    --apiserver-advertise-address "${INTERNAL_IP}"

  echo "    Cluster initialized"
fi

# ── 6. Set up kubectl for agnostic-user ──────────────────────────────

echo "==> Configuring kubectl for agnostic-user"
USER_HOME="/home/agnostic-user"
mkdir -p "${USER_HOME}/.kube"
cp /etc/kubernetes/admin.conf "${USER_HOME}/.kube/config"
chown -R agnostic-user:agnostic-user "${USER_HOME}/.kube"
echo "    kubectl configured at ${USER_HOME}/.kube/config"

# ── 7. Set up kubectl for homelab-svc (for token generation) ─────────

echo "==> Configuring kubectl for homelab-svc"
SVC_HOME="/var/lib/homelab-bootstrap"
if [[ -d "${SVC_HOME}" ]]; then
  mkdir -p "${SVC_HOME}/.kube"
  cp /etc/kubernetes/admin.conf "${SVC_HOME}/.kube/config"
  chown -R homelab-svc:homelab-svc "${SVC_HOME}/.kube"
  echo "    kubectl configured at ${SVC_HOME}/.kube/config"
  # Update systemd unit with KUBECONFIG
  if [[ -f /etc/systemd/system/homelab-bootstrap.service ]]; then
    if ! grep -q KUBECONFIG /etc/systemd/system/homelab-bootstrap.service; then
      sed -i '/Environment=HOMELAB_DB/a Environment=KUBECONFIG='"${SVC_HOME}"'/.kube/config' \
        /etc/systemd/system/homelab-bootstrap.service
      systemctl daemon-reload
      systemctl restart homelab-bootstrap
      echo "    homelab-bootstrap restarted with KUBECONFIG"
    fi
  fi
else
  echo "    WARNING: ${SVC_HOME} not found — run homelab-bootstrap install.sh first"
fi

# ── 8. Install a CNI (Flannel — simplest for multi-arch) ─────────────

echo "==> Installing Flannel CNI"
export KUBECONFIG=/etc/kubernetes/admin.conf

# Wait for API server to be ready
echo "    Waiting for API server..."
for i in $(seq 1 30); do
  if kubectl get nodes &>/dev/null; then break; fi
  sleep 2
done

kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml
echo "    Flannel CNI installed"

# ── 9. Remove the control-plane taint (allow scheduling pods here) ───

echo "==> Removing control-plane NoSchedule taint"
NODE_NAME=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
kubectl taint nodes "${NODE_NAME}" node-role.kubernetes.io/control-plane:NoSchedule- 2>/dev/null || true
echo "    Taint removed — pods can schedule on this node"

# ── 10. Open firewall port 6443 for Pi access ────────────────────────

echo "==> Checking GCP firewall for port 6443"
if command -v gcloud &>/dev/null; then
  # Check if rule already exists
  if ! gcloud compute firewall-rules describe allow-k8s-api --quiet 2>/dev/null; then
    gcloud compute firewall-rules create allow-k8s-api \
      --allow tcp:6443 \
      --source-ranges 0.0.0.0/0 \
      --target-tags "$(curl -sf -H 'Metadata-Flavor: Google' http://metadata.google.internal/computeMetadata/v1/instance/tags | python3 -c 'import sys,json; print(",".join(json.load(sys.stdin)))' 2>/dev/null || echo '')" \
      --description "Allow Kubernetes API access for homelab Pis" \
      --quiet 2>/dev/null || echo "    WARNING: could not create firewall rule (may need manual setup)"
    echo "    Firewall rule created: allow-k8s-api (tcp:6443)"
  else
    echo "    Firewall rule already exists"
  fi
else
  echo "    WARNING: gcloud not found, ensure port 6443 is open in GCP firewall"
fi

# ── Done ─────────────────────────────────────────────────────────────

echo ""
echo "==> Kubernetes cluster ready!"
echo ""
kubectl get nodes -o wide
echo ""
echo "Next steps:"
echo "  1. Deploy homelab-bootstrap service (sudo bash install.sh)"
echo "  2. Flash golden image to Pis"
echo "  3. Pis register and receive join tokens automatically"
echo "  4. Once all Pis are control-plane members, drain this node"
