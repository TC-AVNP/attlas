-- Seed: all steps from the homelab planning sessions
-- Updated 2026-05-06: single-house setup with AI node

-- Refining steps (decide/research/order)

INSERT INTO steps (title, description, position, category, total_budget_cents, created_at) VALUES (
    'Decide on and order hardware',
    'Research, compare options, and order all hardware for a single-house homelab. Setup: 2x Pi 5 16GB (1 worker, 1 AI node), AI HAT+ 2, Pi 3B+ as 4G router, gigabit switch, ethernet cables.',
    0, 'refining', 112400, unixepoch()
);

-- Compute nodes
INSERT INTO checklist_items (step_id, name, group_name, budget_cents, actual_cost_cents, status, created_at) VALUES
    (1, 'Raspberry Pi 5 16GB (x2)',       'Compute',  52000, 52000, 'ordered', unixepoch()),
    (1, 'AI HAT+ 2 (40 TOPS, Hailo-10H)', 'Compute', 20100, 20100, 'ordered', unixepoch()),
    (1, 'GeeekPi N05 NVMe HAT',           'Compute',  1400,  1400, 'ordered', unixepoch()),
    (1, 'Crucial P310 1TB NVMe 2230',      'Compute', 13200, 13200, 'ordered', unixepoch()),
    (1, 'SanDisk Extreme 128GB microSD',   'Compute',  2500,  2500, 'ordered', unixepoch()),
    (1, 'Official Pi 5 27W PSU (x2)',      'Compute',  3000,  3000, 'ordered', unixepoch());

-- Router
INSERT INTO checklist_items (step_id, name, group_name, budget_cents, actual_cost_cents, status, created_at) VALUES
    (1, 'Raspberry Pi 3 Model B+',          'Router',  5000,  5000, 'ordered', unixepoch()),
    (1, 'Waveshare SIM7600G-H 4G HAT',      'Router', 11200, 11200, 'ordered', unixepoch()),
    (1, 'SanDisk Ultra 64GB microSD',        'Router',  1800,  1800, 'ordered', unixepoch());

-- Networking
INSERT INTO checklist_items (step_id, name, group_name, budget_cents, actual_cost_cents, status, created_at) VALUES
    (1, 'TP-Link LS1005G 5-port switch',    'Networking', 1200, 1200, 'ordered', unixepoch()),
    (1, 'Cable Matters Cat6 0.3m (5-pack)', 'Networking', 1000, 1000, 'ordered', unixepoch());

-- Add selected options with Amazon links
INSERT INTO item_options (item_id, name, url, price_cents, notes, created_at) VALUES
    (1, 'Raspberry Pi 5 16GB — amazon.es', 'https://www.amazon.es/dp/B0DTB9JN5F', 26000, 'x2 needed', unixepoch()),
    (2, 'AI HAT+ 2 (40 TOPS) — kubii.com', 'https://www.kubii.com/pt/placas-integradas/4343-2576-ai-kit-ai-hat-para-raspberry-pi-5-3272496319608.html', 20100, 'Hailo-10H, 8GB dedicated RAM', unixepoch()),
    (3, 'GeeekPi N05 — amazon.es', 'https://www.amazon.es/GeeekPi-N05-Raspberry-perif%C3%A9rica-Incluidas/dp/B0CTLT39Y3', 1400, 'Supports 2230 and 2242', unixepoch()),
    (4, 'Crucial P310 1TB — amazon.es', 'https://www.amazon.es/dp/B0D61Z8R1W', 13200, 'PCIe Gen4, 7100 MB/s, M.2 2230', unixepoch()),
    (5, 'SanDisk Extreme 128GB — amazon.es', 'https://www.amazon.es/SanDisk-microSDXC-adaptador-rendimiento-aplicaci%C3%B3n/dp/B09X7BK27V', 2500, '190 MB/s, A2, for AI node boot', unixepoch()),
    (6, 'Official Pi 5 27W PSU — amazon.es', 'https://www.amazon.es/Fuente-alimentaci%C3%B3n-Oficial-Raspberry-Pi/dp/B0CM46P7MC', 1500, 'x2, 5V/5A USB-C', unixepoch()),
    (7, 'Raspberry Pi 3B+ — amazon.es', 'https://www.amazon.es/Raspberry-Pi-3-Model-3-Modelo/dp/B07BDR5PDW', 5000, 'Router + WiFi hotspot', unixepoch()),
    (8, 'Waveshare SIM7600G-H — amazon.es', 'https://www.amazon.es/dp/B09W5M4D3G', 11200, '4G LTE Cat-4, global bands, antenna included', unixepoch()),
    (9, 'SanDisk Ultra 64GB — amazon.es', 'https://www.amazon.es/dp/B09X7D3Y59', 1800, 'For Pi 3B+ router boot', unixepoch()),
    (10, 'TP-Link LS1005G — amazon.es', 'https://www.amazon.es/TP-Link-LS1005G-Conmutador-Ethernet-Velocidad/dp/B07VC68RW1', 1200, '5-port gigabit, plastic case', unixepoch()),
    (11, 'Cable Matters Cat6 5-pack — amazon.es', 'https://www.amazon.es/Cable-Matters-160021-1x5-Cables-CAT6/dp/B00E5I7T9I', 1000, '0.3m, 5 colours', unixepoch());

-- Mark selected options on items
UPDATE checklist_items SET selected_option_id = 1 WHERE id = 1;
UPDATE checklist_items SET selected_option_id = 2 WHERE id = 2;
UPDATE checklist_items SET selected_option_id = 3 WHERE id = 3;
UPDATE checklist_items SET selected_option_id = 4 WHERE id = 4;
UPDATE checklist_items SET selected_option_id = 5 WHERE id = 5;
UPDATE checklist_items SET selected_option_id = 6 WHERE id = 6;
UPDATE checklist_items SET selected_option_id = 7 WHERE id = 7;
UPDATE checklist_items SET selected_option_id = 8 WHERE id = 8;
UPDATE checklist_items SET selected_option_id = 9 WHERE id = 9;
UPDATE checklist_items SET selected_option_id = 10 WHERE id = 10;
UPDATE checklist_items SET selected_option_id = 11 WHERE id = 11;

INSERT INTO steps (title, description, position, category, created_at) VALUES (
    'Decide on control plane strategy',
    'Decide whether the control plane runs on GCP (hibernatable to save costs) or self-hosted. Key questions: where does etcd run, what happens when GCP master is hibernated, how do worker nodes behave without a reachable control plane.',
    1, 'refining', unixepoch()
);

INSERT INTO steps (title, description, position, category, created_at) VALUES (
    'Decide on cluster orchestration platform',
    'Choose between Kubernetes, OpenStack, or another platform. Consider: multi-site support, ARM64 compatibility, resource overhead, ecosystem maturity, hybrid GCP model support.',
    2, 'refining', unixepoch()
);

-- Executing steps (build/do)

INSERT INTO steps (title, description, position, category, created_at) VALUES (
    'Assemble the compute nodes',
    'Mount NVMe HAT on worker Pi 5, install Crucial P310 SSD. Mount AI HAT+ 2 on AI node Pi 5, insert 128GB microSD. Flash OS images, configure NVMe boot on worker, verify both nodes boot and run.',
    3, 'executing', unixepoch()
);

INSERT INTO steps (title, description, position, category, created_at) VALUES (
    'Build the 4G router',
    'Mount Waveshare SIM7600G-H 4G HAT on Pi 3B+. Insert SIM card, configure modem drivers (qmi_wwan). Set up NAT/iptables, DHCP via dnsmasq, WiFi hotspot via hostapd. Connect to ethernet switch and verify internet for both Pi 5 nodes.',
    4, 'executing', unixepoch()
);

INSERT INTO steps (title, description, position, category, created_at) VALUES (
    'Set up local networking',
    'Connect both Pi 5s to the TP-Link switch via Cat6 cables. Connect Pi 3B+ ethernet to the switch. Configure static IPs on the ethernet LAN. Pi 5s use ethernet for node-to-node traffic and WiFi for internet via Pi 3B+ hotspot.',
    5, 'executing', unixepoch()
);

INSERT INTO steps (title, description, position, category, created_at) VALUES (
    'Onboard nodes into the cluster',
    'Join worker Pi 5 and AI node Pi 5 to the chosen cluster platform. Configure node labels (role=worker, role=ai). Verify pods can be scheduled and run on both nodes.',
    6, 'executing', unixepoch()
);

INSERT INTO steps (title, description, position, category, created_at) VALUES (
    'Expand to second house',
    'Order hardware for house 2. Add 2 more Pi 5 worker nodes, a second Pi 3B+ router with 4G HAT, and a second switch. Set up cross-site networking and distributed storage with RAID replication.',
    7, 'executing', unixepoch()
);
