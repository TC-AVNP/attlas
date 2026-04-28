---
title: "MCP Servers"
date: 2026-04-27
layout: "catalog"
---

MCP servers in the attlas ecosystem.

## Petboard

**Endpoint:** `http://127.0.0.1:7690/petboard/mcp`
**Auth:** OAuth 2.1 with PKCE (reuses attlas Google OAuth session)
**Used by:** Claude Code (via `~/.claude.json` mcpServers config)

| Tool | Description |
|------|-------------|
| `list_projects` | List every project with feature counts and total minutes |
| `get_project` | Get one project by slug, including features and effort log |
| `create_project` | Create a new project (requires name, problem, priority) |
| `update_project` | Patch fields of an existing project |
| `add_feature` | Append a feature to a project's backlog |
| `set_feature_status` | Move a feature to backlog, in_progress, done, or dropped |
| `update_feature` | Patch a feature's title and/or description |
| `log_effort` | Record minutes of work against a project |
| `link_repo` | Link a git repo for automatic effort tracking from commits |
| `sync_repo` | Sync effort logs from git commit history |

## Diary

**Type:** stdio (Go binary)
**Command:** `~/iapetus/attlas/services/diary/mcp/diary-mcp`
**Used by:** Claude Code (via `~/.claude.json` mcpServers config)

| Tool | Description |
|------|-------------|
| `search_by_project` | Find diary entries tagged with a project slug, with summaries and lessons |
| `search_by_keyword` | Search entries for a keyword/phrase with context around matches |
| `list_entries` | List all entries, optionally filtered by date range |
| `get_entry` | Get full content of a single entry by date |
