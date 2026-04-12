-- Seed: all steps from the homelab planning sessions

-- Step 1: Buy and assemble the rigs
INSERT INTO steps (title, description, position, total_budget_cents, created_at) VALUES (
    'Buy and assemble the rigs',
    'Purchase all hardware for two homelab rigs (one per house). Each rig: 2x Raspberry Pi 5 8GB, 2x NVMe HAT, 2x 1TB NVMe SSD, 1x Pi Zero 2W (5G router), 1x 65-100W GaN USB-C charger, 1x gigabit ethernet switch, ethernet cables, 1x USB 5G dongle.',
    0, 110000, unixepoch()
);

-- Rig 1 (House 1)
INSERT INTO checklist_items (step_id, name, group_name, budget_cents, status, created_at) VALUES
    (1, 'Raspberry Pi 5 8GB (x2)',        'Rig 1 — House 1', 18000, 'researching', unixepoch()),
    (1, 'NVMe HAT for Pi 5 (x2)',         'Rig 1 — House 1',  5000, 'researching', unixepoch()),
    (1, '1TB NVMe SSD (x2)',              'Rig 1 — House 1', 14000, 'researching', unixepoch()),
    (1, 'Pi Zero 2W',                     'Rig 1 — House 1',  2000, 'researching', unixepoch()),
    (1, '65-100W GaN USB-C charger',      'Rig 1 — House 1',  4000, 'researching', unixepoch()),
    (1, 'Gigabit ethernet switch',        'Rig 1 — House 1',  2000, 'researching', unixepoch()),
    (1, 'Ethernet cables',               'Rig 1 — House 1',  1000, 'researching', unixepoch()),
    (1, 'USB 5G dongle with SIM slot',   'Rig 1 — House 1',  8000, 'researching', unixepoch());

-- Rig 2 (House 2)
INSERT INTO checklist_items (step_id, name, group_name, budget_cents, status, created_at) VALUES
    (1, 'Raspberry Pi 5 8GB (x2)',        'Rig 2 — House 2', 18000, 'researching', unixepoch()),
    (1, 'NVMe HAT for Pi 5 (x2)',         'Rig 2 — House 2',  5000, 'researching', unixepoch()),
    (1, '1TB NVMe SSD (x2)',              'Rig 2 — House 2', 14000, 'researching', unixepoch()),
    (1, 'Pi Zero 2W',                     'Rig 2 — House 2',  2000, 'researching', unixepoch()),
    (1, '65-100W GaN USB-C charger',      'Rig 2 — House 2',  4000, 'researching', unixepoch()),
    (1, 'Gigabit ethernet switch',        'Rig 2 — House 2',  2000, 'researching', unixepoch()),
    (1, 'Ethernet cables',               'Rig 2 — House 2',  1000, 'researching', unixepoch()),
    (1, 'USB 5G dongle with SIM slot',   'Rig 2 — House 2',  8000, 'researching', unixepoch());

-- Step 2: Decide on the router setup
INSERT INTO steps (title, description, position, created_at) VALUES (
    'Decide on the router setup',
    'Figure out the 5G router build: USB 5G dongle compatible with Pi Zero 2W (or Pi 3), antenna options, network sharing config. Research specific hardware models and order.',
    1, unixepoch()
);

-- Step 3: Assemble the routers
INSERT INTO steps (title, description, position, created_at) VALUES (
    'Assemble the routers',
    'Build the 5G routers from Pi Zero 2W + USB 5G dongle + USB-to-ethernet adapter. Configure modem drivers, NAT/routing, DHCP. Connect to the ethernet switch and verify internet connectivity for both worker Pis.',
    2, unixepoch()
);

-- Step 4: Assemble the rigs
INSERT INTO steps (title, description, position, created_at) VALUES (
    'Assemble the compute rigs',
    'Mount NVMe HATs on Pi 5s, install SSDs, flash OS images (Ubuntu or Raspberry Pi OS), verify each node boots and is reachable on the local network via the router.',
    3, unixepoch()
);

-- Step 5: Decide on control plane strategy
INSERT INTO steps (title, description, position, created_at) VALUES (
    'Decide on control plane strategy',
    'Decide whether the control plane runs on GCP (hibernatable to save costs) or self-hosted. Key questions: where does etcd run, what happens when GCP master is hibernated, how do worker nodes behave without a reachable control plane.',
    4, unixepoch()
);

-- Step 6: Decide on cluster orchestration
INSERT INTO steps (title, description, position, created_at) VALUES (
    'Decide on cluster orchestration platform',
    'Choose between Kubernetes, OpenStack, or another platform. Consider: multi-site support, ARM64 compatibility, resource overhead, ecosystem maturity, hybrid GCP model support.',
    5, unixepoch()
);

-- Step 7: Onboard rigs into the cluster
INSERT INTO steps (title, description, position, created_at) VALUES (
    'Onboard rigs into the cluster',
    'Join all 4 worker Pi 5 nodes to the chosen platform. Configure cross-site networking, set up node labels for geographic awareness (house-1 vs house-2), verify pods schedule and run across both locations.',
    6, unixepoch()
);

-- Step 8: Set up distributed storage
INSERT INTO steps (title, description, position, created_at) VALUES (
    'Set up distributed storage with RAID across sites',
    'Configure distributed storage replicated across both houses. Needs: S3-compatible object storage, persistent volumes for pods, Postgres persistent storage. 4TB raw across 4 nodes, ~2TB usable after cross-site replication.',
    7, unixepoch()
);
