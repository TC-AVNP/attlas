-- Add group_name to checklist items for per-rig aggregation
ALTER TABLE checklist_items ADD COLUMN group_name TEXT NOT NULL DEFAULT '';

-- Add adjustable total budget to steps
ALTER TABLE steps ADD COLUMN total_budget_cents INTEGER;
