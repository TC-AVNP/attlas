-- Add expected delivery date to checklist items
ALTER TABLE checklist_items ADD COLUMN delivery_date TEXT NOT NULL DEFAULT '';
