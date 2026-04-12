-- Seed: first step from the planning session
INSERT INTO steps (title, description, position, created_at) VALUES (
    'Buy and assemble the rigs',
    'Purchase all hardware for two homelab rigs (one per house). Each rig: 2x Raspberry Pi 5 8GB, 2x NVMe HAT, 2x 1TB NVMe SSD, 1x Pi Zero 2W (5G router), 1x 65-100W GaN USB-C charger, 1x gigabit ethernet switch, ethernet cables.',
    0,
    unixepoch()
);

-- Checklist items for the first step
INSERT INTO checklist_items (step_id, name, budget_cents, status, created_at) VALUES
    (1, 'Raspberry Pi 5 8GB (x4)',        36000, 'researching', unixepoch()),
    (1, 'NVMe HAT for Pi 5 (x4)',         10000, 'researching', unixepoch()),
    (1, '1TB NVMe SSD (x4)',              28000, 'researching', unixepoch()),
    (1, 'Pi Zero 2W (x2)',                 4000, 'researching', unixepoch()),
    (1, '65-100W GaN USB-C charger (x2)',  8000, 'researching', unixepoch()),
    (1, 'Gigabit ethernet switch (x2)',    4000, 'researching', unixepoch()),
    (1, 'Ethernet cables',                 2000, 'researching', unixepoch()),
    (1, 'USB 5G dongle with SIM slot (x2)',16000, 'researching', unixepoch());
