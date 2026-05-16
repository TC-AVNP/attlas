-- Seed: initial wiki pages for the homelab documentation site.

INSERT INTO pages (slug, title, body, position) VALUES (
    'home',
    'Homelab',
    '# Welcome to the Homelab

This wiki documents the design, build, and operation of a personal homelab — a small Kubernetes cluster made from Raspberry Pi 5 nodes, running at home.

## Why build a homelab?

When you want to run personal infrastructure — services, experiments, ML models — the choice is usually between expensive always-on cloud resources or having nothing at all. A homelab changes that: dedicated compute at home, running on your own terms, for a one-time hardware cost and pennies in electricity.

## What we are building

A three-node Kubernetes cluster using Raspberry Pi 5s, each booting from 1TB NVMe SSDs. The cluster runs full upstream Kubernetes (not k3s), with all nodes serving as both control plane members and workers. An AWS-based Metal³ control plane handles declarative provisioning, and etcd snapshots go to S3 every five minutes for disaster recovery.

The local network is self-contained: a Raspberry Pi 3 Model B+ with a 4G HAT provides the internet uplink, NAT, and DHCP. Everything connects through a gigabit switch.

## Sections

- **[The Standard Cluster](/homelab-planner/wiki/standard-cluster)** — hardware, costs, and what we ordered
- **[Networking](/homelab-planner/wiki/networking)** — the home network setup
- **[Architecture](/homelab-planner/wiki/architecture)** — Kubernetes, Metal³, etcd, disaster recovery
- **[Future Plans](/homelab-planner/wiki/future-plans)** — second house, Nebula mesh, what comes next
- **[Journal](/homelab-planner/journal)** — day-by-day build log',
    0
);

INSERT INTO pages (slug, title, body, position) VALUES (
    'standard-cluster',
    'The Standard Cluster',
    '## Hardware

The cluster is built from three Raspberry Pi 5 nodes. Two are general-purpose worker nodes with NVMe storage; one is an AI-focused node with a Hailo AI HAT+ for local ML inference. All three run as both Kubernetes control plane members and workers.

### What we ordered

| Component | Qty | Unit Price | Total | Notes |
|-----------|-----|-----------|-------|-------|
| Raspberry Pi 5 16GB | 2 | €260.00 | €520.00 | 10 cores, 16GB RAM each |
| AI HAT+ 2 (Hailo-10H, 40 TOPS) | 1 | €201.00 | €201.00 | 8GB dedicated RAM, stacks on Pi 5 |
| GeeekPi N05 NVMe HAT | 1 | €14.00 | €14.00 | Supports 2230 and 2242 M.2 |
| Crucial P310 1TB NVMe 2230 | 1 | €132.00 | €132.00 | PCIe Gen4, 7100 MB/s read |
| SanDisk Extreme 128GB microSD | 1 | €25.00 | €25.00 | A2, for AI node boot |
| Official Pi 5 27W PSU | 2 | €15.00 | €30.00 | 5V/5A USB-C |
| Raspberry Pi 3 Model B+ | 1 | €50.00 | €50.00 | Router node |
| Waveshare SIM7600G-H 4G HAT | 1 | €112.00 | €112.00 | 4G LTE Cat-4, global bands |
| SanDisk Ultra 64GB microSD | 1 | €18.00 | €18.00 | Router boot media |
| TP-Link LS1005G 5-port switch | 1 | €12.00 | €12.00 | Gigabit, unmanaged, fanless |
| Cable Matters Cat6 0.3m (5-pack) | 1 | €10.00 | €10.00 | 5 colours |
| **Total** | | | **€1,124.00** | |

All hardware was ordered from Amazon.es and Kubii.com. Everything has arrived.

### Node roles

**Worker Node (x1)**
- Raspberry Pi 5 16GB
- GeeekPi N05 NVMe HAT + Crucial P310 1TB NVMe SSD
- Boots directly from NVMe (EEPROM updated, no SD card needed)
- General-purpose Kubernetes workloads

**AI Node (x1)**
- Raspberry Pi 5 16GB
- AI HAT+ 2 with Hailo-10H NPU (40 TOPS, 8GB dedicated RAM)
- Boots from 128GB microSD
- ML inference workloads, edge AI

**Router Node (x1)**
- Raspberry Pi 3 Model B+
- Waveshare SIM7600G-H 4G HAT with antenna
- Provides internet uplink via 4G LTE
- NAT, DHCP, and WiFi hotspot for the cluster LAN

### Why the Pi 5?

The Raspberry Pi 5 with 16GB RAM and a quad-core Arm Cortex-A76 at 2.4GHz has enough headroom to run full upstream Kubernetes (not k3s). With 16GB per node, there is room for the control plane components (etcd, API server, scheduler, controller manager) alongside actual workloads. The NVMe boot support (after EEPROM update) gives fast, reliable storage without depending on fragile SD cards.',
    1
);

INSERT INTO pages (slug, title, body, position) VALUES (
    'networking',
    'Networking',
    '## Network topology

The homelab has a self-contained local network with its own internet uplink — no dependency on a landlord''s ISP or existing home router.

### Components

- **Router**: Raspberry Pi 3 Model B+ with a Waveshare SIM7600G-H 4G HAT
- **Switch**: TP-Link LS1005G, 5-port gigabit, unmanaged
- **Cabling**: Cat6 ethernet, 0.3m short runs

### How it connects

The Pi 3B+ provides the internet uplink via its 4G HAT (USB connection to the SIM7600G-H modem). It runs NAT and DHCP on the LAN side, handing out static IP reservations to the two Pi 5 nodes by MAC address.

The TP-Link switch sits between the router and the two Pi 5s. Three of the five ports are used: one uplink from the Pi 3B+, and one each for the worker and AI nodes.

The Pi 3B+ also runs a WiFi hotspot (via hostapd) so laptops and phones can join the homelab network for management.

### IP addressing

The LAN uses a `10.0.0.0/24` range:
- `10.0.0.1` — Pi 3B+ router (gateway)
- `10.0.0.10` — Pi 5 worker node
- `10.0.0.11` — Pi 5 AI node

DNS resolution is handled by dnsmasq on the router, forwarding upstream to Cloudflare (1.1.1.1).

### Internet uplink

The 4G connection uses a SIM card in the Waveshare HAT. The modem appears as a USB network device and is managed via ModemManager. Bandwidth is shared across all cluster nodes — sufficient for pulling container images, API calls, and remote management, but not intended for high-throughput workloads.',
    2
);

INSERT INTO pages (slug, title, body, position) VALUES (
    'architecture',
    'Architecture',
    '## Kubernetes

The cluster runs **full upstream Kubernetes** installed via kubeadm — not k3s. With 16GB RAM per Pi 5, the lightweight argument for k3s does not apply. Running the full distribution avoids needing to bolt on missing pieces later (descheduler, additional CNI plugins, monitoring stacks).

All nodes are dual-role: each runs the full control plane stack (etcd, API server, scheduler, controller manager) **and** is schedulable as a worker. With only two Pi 5 nodes, dedicating either as "worker only" would waste capacity.

### etcd replication

etcd runs in stacked mode — colocated with the control plane on each Pi. With two nodes, the quorum requirement is 2 of 2, meaning both nodes must be healthy for writes. This is a known limitation of a two-node setup; a third Pi 5 in the future would improve this to quorum 2 of 3.

### Metal³ provisioning

A Metal³ control plane runs on a small AWS EC2 instance (t4g.small ARM64). Each Pi boots from a golden image (Ubuntu Server 24.04 LTS ARM64 + Metal³ agent), registers with the control plane using a bootstrap token, and receives a unique per-node certificate.

Metal³ lives on AWS (not on the Pis) to solve the bootstrap chicken-and-egg: it needs to exist before any Pi can join. It also survives Pi cluster outages and has a stable public endpoint.

### Disaster recovery

etcd snapshots upload to AWS S3 (`s3://homelab-etcd-snapshots/`) every five minutes from a systemd timer on each Pi. Only the etcd leader actually performs the snapshot. The uploader runs outside Kubernetes so it survives a broken cluster.

If all Pis fail simultaneously, a break-glass procedure spins up an AWS EC2 VM via Terraform, restores the latest snapshot, and runs as a temporary single-node control plane until the Pis recover.

### Golden image

A single disk image is flashed to every Pi''s NVMe (or microSD for the AI node). It contains:
- Ubuntu Server 24.04 LTS ARM64
- Metal³ agent (systemd service)
- Bootstrap token for initial registration
- SSH authorized keys for admin access

Per-node identity is established after first boot: the agent registers with Metal³, gets a unique cert keyed to the hardware (MAC address, NVMe serial), and the bootstrap token is revoked.',
    3
);

INSERT INTO pages (slug, title, body, position) VALUES (
    'future-plans',
    'Future Plans',
    '## Second house

The current cluster is a single-site setup. The long-term plan is to expand to a second house with another set of Pi 5 nodes, joined to the same Kubernetes cluster over an encrypted overlay network.

### Nebula mesh

The inter-site network would use **Nebula** — a self-hosted, NAT-traversing, peer-to-peer mesh built on the Noise Protocol Framework. Both houses are behind 4G NAT, so Nebula''s lighthouse + hole-punching design is a good fit.

A Nebula CA would issue per-node identity certs. The AWS Metal³ VM is the natural lighthouse node (stable public IP).

### Why defer this

- The single-house cluster is already complex enough (Metal³ + replicated etcd + AWS break-glass)
- Cross-site networking is the single biggest source of operational pain in multi-region clusters
- The hardware for the second house has not been purchased
- The second site''s 4G carrier, latency, and NAT behavior need to be understood empirically

### Other future work

- **Monitoring and alerting** — Prometheus + Grafana stack once the cluster is running
- **Distributed storage** — cross-site replication for persistent workloads
- **Solar/battery** — off-grid power at one or both sites
- **Third Pi 5** — adding a third node to the first house would improve etcd quorum from 2-of-2 to 2-of-3, tolerating one node failure without losing writes',
    4
);
