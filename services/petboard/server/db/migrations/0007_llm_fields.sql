-- Dual-version content: every text field has a human version (concise,
-- readable) and an LLM version (detailed, technical, for agents).
-- Projects also get a screenshot_url for the overview page.
ALTER TABLE projects ADD COLUMN description_llm TEXT;
ALTER TABLE projects ADD COLUMN notes_llm TEXT;
ALTER TABLE projects ADD COLUMN screenshot_url TEXT;
ALTER TABLE features ADD COLUMN description_llm TEXT;
