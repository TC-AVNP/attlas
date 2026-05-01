-- Add separate human and LLM content fields.
-- Migrate existing content to content_llm (it was written for agents).
ALTER TABLE entries ADD COLUMN content_human TEXT NOT NULL DEFAULT '';
ALTER TABLE entries RENAME COLUMN content TO content_llm;
