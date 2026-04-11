-- Bootstrap seed for petboard.
--
-- Loaded by the Go binary on first initialization only — specifically, the
-- startup migration step runs this file iff the `projects` table is empty
-- after all schema migrations have been applied. If you restore from a
-- backup or wipe the DB, this seed re-materializes.
--
-- This is petboard's origin story: the first project tracked in petboard
-- is petboard itself.

INSERT INTO projects (
    slug, name, problem, description, priority, color, created_at, canvas_x, canvas_y
) VALUES (
    'petboard',
    'Petboard',
    'When commonlisp6 wants to plan and track the handful of pet projects running in parallel — home labs, dotfiles rewrites, the next CLI tool — there is no single place to see what is active, how much time has gone into each, what is next, and why any of them matter. Today it lives in his head and scattered across markdown files, which makes prioritization hard and momentum easy to lose between sessions.',
    'Personal project tracker installed as an attlas service. Go + SQLite backend, React + react-konva infinite-canvas frontend, managed interactively by Claude Code via an MCP server with proper OAuth 2.1 flow. Reuses the existing Caddy + alive-server Google OAuth for browser sessions; MCP clients get their own Bearer tokens via PKCE.',
    'high',
    '#7aa2f7',
    unixepoch(),
    0,
    0
);

-- Features — 8 total. One done (the design + plan doc, which we just
-- wrote together), four representing the actual implementation work still
-- ahead, and three speculative future ideas that commonlisp6 will review
-- and either keep in backlog or move to dropped.

-- 1. DONE — the design work that produced PLAN.md
INSERT INTO features (project_id, title, description, status, created_at, started_at, completed_at)
VALUES (
    (SELECT id FROM projects WHERE slug = 'petboard'),
    'Design architecture and write plan doc',
    'Collaborative design session: data model (mandatory problem field, features with status lifecycle, effort logs in minutes), infinite-canvas UX with semantic zoom and glowing project threads, MCP OAuth 2.1 flow that reuses Caddy''s existing Google session for /authorize, generic public-path registry in alive-server, skill policy for how Claude drafts problem statements. Result: services/petboard/PLAN.md.',
    'done',
    unixepoch(),
    unixepoch(),
    unixepoch()
);

-- 2-5. BACKLOG — real implementation work
INSERT INTO features (project_id, title, description, status, created_at)
VALUES
    (
        (SELECT id FROM projects WHERE slug = 'petboard'),
        'Build Go backend with SQLite, REST API, and SSE',
        'Embedded migrations, projects/features/effort CRUD, mandatory-problem validation, list-with-aggregates endpoint, SSE mutation stream. Ships behind the existing Caddy + alive-server auth.',
        'backlog',
        unixepoch()
    ),
    (
        (SELECT id FROM projects WHERE slug = 'petboard'),
        'Implement infinite-zoom canvas with react-konva',
        'Dark-space background, project threads colored and thickness-scaled by priority, feature orbs (created / started / completed / dropped), now-line with particles, semantic zoom layers, drag-to-reposition persisted in canvas_x/canvas_y, time-window slider, problem statement as pull-quote at full zoom and tooltip at mid-zoom.',
        'backlog',
        unixepoch()
    ),
    (
        (SELECT id FROM projects WHERE slug = 'petboard'),
        'Add MCP OAuth 2.1 flow and MCP tool endpoint',
        'Well-known metadata, dynamic client registration, authorize endpoint (reuses Caddy session via the alive-server public-path registry), PKCE token exchange, Bearer middleware, streamable HTTP MCP transport exposing create_project / add_feature / set_feature_status / log_effort / search / etc. Includes the small but real alive-server change in base-setup to support /etc/attlas-public-paths.d/*.conf.',
        'backlog',
        unixepoch()
    ),
    (
        (SELECT id FROM projects WHERE slug = 'petboard'),
        'Write Claude Code skill and connect end-to-end',
        'Skill at dotfiels/claude/skills/petboard.md encoding the problem-statement policy, session start/end effort logging, priority guidance, duplicate-check before create_project. Register the MCP server in Claude Code on both mac and VM and verify the full flow (natural-language request → OAuth browser click → tool call → canvas updates live via SSE).',
        'backlog',
        unixepoch()
    );

-- 6-8. BACKLOG — speculative future ideas, will be reviewed and possibly dropped
INSERT INTO features (project_id, title, description, status, created_at)
VALUES
    (
        (SELECT id FROM projects WHERE slug = 'petboard'),
        'Export the universe view as SVG for diary entries',
        'Future idea: a button on the canvas toolbar that exports the current viewport as an SVG snapshot, ready to embed in a Hugo diary post as a visual record of where things stood on a given day.',
        'backlog',
        unixepoch()
    ),
    (
        (SELECT id FROM projects WHERE slug = 'petboard'),
        'Auto-detect effort from git history on linked repos',
        'Future idea: optionally link a project to one or more local git repositories and derive effort entries from commit timestamps and durations, so effort logging becomes mostly automatic and only needs manual notes for context.',
        'backlog',
        unixepoch()
    ),
    (
        (SELECT id FROM projects WHERE slug = 'petboard'),
        'Stale-project nudge for silent high-priority work',
        'Future idea: subtle visual cue (pulsing dashed ring around the project label) when a project marked high priority has had no effort logged in N days. Keeps prioritization honest without nagging.',
        'backlog',
        unixepoch()
    );

-- Effort log — roughly two hours of design conversation captured on the
-- feature that represents the planning phase.
INSERT INTO effort_logs (project_id, feature_id, minutes, note, logged_at)
VALUES (
    (SELECT id FROM projects WHERE slug = 'petboard'),
    (SELECT id FROM features
     WHERE project_id = (SELECT id FROM projects WHERE slug = 'petboard')
       AND title = 'Design architecture and write plan doc'),
    120,
    'Design conversation with Claude: data model, canvas UX (infinite zoom, semantic zoom layers, glowing threads), MCP OAuth 2.1 architecture with alive-server public-path registry, mandatory problem field and skill guidance for writing it. Plan doc landed at services/petboard/PLAN.md.',
    unixepoch()
);
