# diary

Hugo-powered project diary at `attlas.uk/diary/`. One file per working
session in `content/`, rendered as a static site into `public/` which
alive-server serves directly (no separate web server needed for this
one).

## Files

- `install.sh` — installs Hugo if missing and builds `public/`. Safe
  to re-run to rebuild after adding a new entry.
- `uninstall.sh` — removes `public/` and the Caddy snippet.
- `diary.caddy` — Caddy route snippet, though the actual serving is
  done by alive-server's static passthrough. Kept so the snippet is
  in place if the passthrough is ever removed.
- `hugo.toml` — site config.
- `content/` — Markdown entries (`YYYY-MM-DD.md`).
- `layouts/` — templates.

## Rebuild

```bash
cd services/diary
hugo --baseURL /diary/ --destination public
```

alive-server serves `${ATTLAS_DIR}/services/diary/public` at
`/diary/`; no service restart is needed after a rebuild, just
refreshing the page.

## MCP server

`mcp/` contains a stdio-based MCP server (Go) that lets Claude Code
query diary entries without filesystem access. Configured in
`~/.claude.json` under `mcpServers.diary`.

Tools: `search_by_project`, `search_by_keyword`, `list_entries`,
`get_entry`.

Rebuild after changes:
```bash
cd services/diary/mcp
/usr/local/go/bin/go build -o diary-mcp .
```

## Writing entries

Entry files are `content/YYYY-MM-DD.md`. Run the `hugo` build command
after adding or editing one, then commit both the `.md` and the
regenerated `public/` is gitignored so only the source is tracked.
