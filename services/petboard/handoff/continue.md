# petboard — continuation handoff

You are picking up a petboard session that was started on commonlisp6's
mac. This directory holds everything you need to continue from where
the previous agent left off.

## Read this first

Before doing anything else, read the full transcript of the prior
session:

```
services/petboard/handoff/session.jsonl
```

It is a Claude Code session jsonl. Each line is a JSON object; you
want the `message` blocks (user and assistant) to reconstruct the
conversation. The file is ~2 MB and contains the design discussion,
every tool call, and every decision made so far. **Read it in full
before touching any files** — it encodes roughly two hours of
back-and-forth design work you will not be able to rediscover from
the code alone.

After the transcript, read:

- `services/petboard/PLAN.md` — full design doc (schema, canvas UX,
  auth architecture, install layout, implementation order, open risks)
- `services/petboard/README.md` — one-paragraph pointer to PLAN
- `services/petboard/server/db/seed.sql` — the bootstrap data that
  plants petboard as its own first project on first init

## Current state

**Committed and pushed to origin/main:**

- `a3a0fde` "Add petboard service scaffold and backend v1"
  - `services/petboard/` full tree (Go backend, React scaffold, PLAN,
    seed, caddy snippet, install/uninstall scripts)
- A later commit adding this handoff directory and a PATH fix to
  `services/install-petboard.sh`.

**Inside the `3d347fa` commit (already on origin) alive-server has
three changes relevant to petboard:**

1. A public-path registry that reads `/etc/attlas-public-paths.d/*.conf`
   on startup and on SIGHUP, and short-circuits `handleAuthVerify` to
   return 200 for any request whose `X-Forwarded-Uri` starts with a
   registered prefix.
2. Return-URL preservation across the Google OAuth round-trip. The old
   behavior always redirected to `/` after login, losing the original
   URL and all its query params. Now `handleAuthVerify` passes the
   original URI as `return_to=`, the state store carries it through to
   `handleOAuth2Callback`, and the callback redirects to the captured
   URL (validated as a same-origin relative path via
   `isSafeRelativePath`).
3. An `os/signal` + `syscall` SIGHUP handler that triggers
   `publicPathRegistry.load()` so services can reload without dropping
   sessions: `systemctl kill --signal=SIGHUP alive-server`.

These were tested only at the source level (grep + code review). The
alive-server binary on the VM has ALREADY been rebuilt and restarted
during this session (see session.jsonl around the `go build` +
`systemctl restart alive-server` calls). Confirm with
`systemctl status alive-server` — the journalctl should show a start
time of ~19:41 UTC on Apr 11 2026 or later.

## Task tracker

14 tasks live in the Claude task system. State as of handoff:

| id | status | subject |
|----|--------|---------|
| 1 | completed | Probe Caddy forward_auth for X-Forwarded-Uri |
| 2 | completed | alive-server public-path registry + return-URL preservation |
| 3 | completed | Scaffold petboard directory tree |
| 4 | completed | Backend v1 (CRUD + seed loader) — verified locally |
| 5 | completed | Install script, systemd unit, Caddy snippet |
| 6 | pending | Frontend scaffold (fetch /api/projects, render list) |
| 7 | pending | Project detail page |
| 8 | pending | Canvas v1 (react-konva threads/orbs/semantic zoom) |
| 9 | pending | Canvas v2 (drag, filters, time window, polish) |
| 10 | pending | SSE live updates from server to canvas |
| 11 | pending | OAuth 2.1 endpoints (well-known, DCR, authorize, token) |
| 12 | pending | MCP endpoint and tool surface |
| 13 | pending | Claude Code skill + end-to-end test |
| 14 | pending | Polish (errors, empty states, shortcuts) |

The task list on the VM side will NOT have these pre-populated. You
should re-create them via `TaskCreate` using this table as the source
of truth.

## Verified-works slice

The backend was built and fully exercised on the mac locally before
handoff. Every piece below was confirmed working end-to-end via curl
at `http://127.0.0.1:7690/petboard/...`:

- `go build` on `server/` produces a 14 MB binary
- `npm run build` on `web/` produces a ~143 KB bundle
- Server startup applies migration `0001_init.sql` cleanly
- On an empty DB the bootstrap seed runs and plants petboard-as-first-project
- `GET /petboard/` serves the React scaffold HTML with `/petboard/assets/*` paths rewritten by Vite's base
- `GET /petboard/api/projects` returns petboard with aggregates `feature_counts={backlog:7, done:1}` and `total_minutes=120`
- `GET /petboard/api/projects/petboard` returns 8 features + 1 effort log row
- `POST /api/projects` with no `problem` → 400 "invalid input: problem is required" (validation fires)
- `POST /api/projects` with valid payload → 201, slug + color derived
- `POST .../features` + `PATCH /api/features/{id}` with `status=in_progress` → auto-sets `started_at`
- `POST .../effort` → effort log row + aggregate update on re-fetch
- SPA prefix stripping and static asset MIME types correct

The local iteration loop used `/tmp/petboard` as the binary and
`/tmp/petboard-dev.db` as the DB. Both were cleaned up before the
handoff commit.

## The ONE thing still left on this slice: deploy to the VM

The user explicitly asked for local-first iteration but also said
"make sure to deploy and test in the end after we are happy with the
local dev setup". The local dev is working. The next step on this
slice is to run the install script on the VM and confirm petboard
comes up through `https://attlas.uk/petboard/`.

### What broke on the first VM deploy attempt (already fixed in commit)

Earlier in the session I ran the install script on the VM and it got
as far as `npm run build` (success) then died on:

```
Building petboard Go binary...
bash: line 2: go: command not found
```

Root cause: `sudo -u agnostic-user -H bash -c "..."` runs a non-login
shell, so `/etc/profile.d/*` (where `go` is added to PATH by the
attlas base-setup) is never sourced. Fix that is now in the committed
install script: every `sudo -u` invocation goes through
`env PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin" bash -c ...`
so the build environment is deterministic regardless of how the
operator's shell is configured.

### How to finish the deploy

```bash
# Pull (use agnostic-user since that's who owns the clone)
sudo -u agnostic-user git -C /home/agnostic-user/iapetus/attlas pull --ff-only

# Run the install script. It will build the frontend + backend as
# agnostic-user, install the systemd unit, register public paths,
# SIGHUP alive-server, and drop the Caddy snippet.
sudo bash /home/agnostic-user/iapetus/attlas/services/install-petboard.sh

# services/install.sh reloads Caddy at the end when run via the menu;
# when running install-petboard.sh directly, reload manually:
sudo systemctl reload caddy
```

Verify:

```bash
sudo journalctl -u petboard -n 30 --no-pager
sudo journalctl -u alive-server -n 20 --no-pager   # look for "public-paths: loaded 4 prefix(es)"
curl -sI https://attlas.uk/petboard/                # expect 302 to /oauth2/login?return_to=/petboard/
```

Then open `https://attlas.uk/petboard/` in a browser — after Google
OAuth you should land on the petboard scaffold page (task #6 hasn't
been done yet, so it'll say "scaffold — the universe will live
here"), and `GET https://attlas.uk/petboard/api/projects` (with the
Caddy session cookie) should return the seeded data with petboard as
the first project.

### Important: verify the return-URL preservation

This is the single most load-bearing alive-server change and it has
only been tested at the source level. When you `curl -sI
https://attlas.uk/petboard/`, the Location header should be
`/oauth2/login?return_to=%2Fpetboard%2F` — not just `/oauth2/login`.
That's the proof the new handleAuthVerify code path is live. If it's
still plain `/oauth2/login`, alive-server either wasn't rebuilt or
wasn't restarted with the new binary.

## After deploy

Once the VM deploy is green, move into task #6. It is the smallest
frontend task that actually starts rendering server data:

1. Add `react-router` with `basename="/petboard"` and two routes:
   `/` → `<Universe />`, `/p/:slug` → `<ProjectDetail />`.
2. Add `QueryClientProvider` around the app.
3. Build a tiny `api/client.ts` — a thin wrapper over `fetch` that
   prepends `/petboard/api` and throws on non-2xx responses.
4. In `Universe.tsx` fetch `/api/projects` via `useQuery` and render a
   plain unordered list: name, priority pill, progress ring ("7/12"),
   total hours. Ugly is fine, no canvas yet. This is the smoke test
   that proves the frontend ↔ backend round-trips through the real
   auth gate on the VM.
5. Same pattern for `ProjectDetail.tsx` → use task #7's content.

Do NOT jump directly into the canvas (task #8). It is much more work
and debugging canvas + routing + auth simultaneously is miserable. Get
the plain list working first.

## Rules I am holding you to

These are saved as `feedback` memories. Follow them without being
reminded.

1. **Never commit more than once per hour**, and only when deploying
   AND the deployment is verified working. Batch work across multiple
   tasks into a single commit at deploy-verify time. The only
   exception is an explicit user request to commit.
   (`feedback_commit_frequency.md`)
2. **Never add `Co-Authored-By: Claude`** to git commit messages.
   (`feedback_no_claude_coauthor.md`)
3. **Never tell the user to try something without testing it
   yourself first.** If you propose a command, run it.
   (`feedback_test_before_telling.md`)
4. **Commit and push every memory update to the dotfiles repo.** The
   memory directory is a symlink into `dotfiels/claude/memory` — any
   new or updated memory file needs a dotfiles commit + push.
   (`feedback_commit_memory_to_dotfiles.md`)
5. **Write an attlas diary entry when the user signals the session
   is done** — `attlas/diary/` contains Hugo markdown.
   (`feedback_diary.md`)
6. The user is **commonlisp6**. Never refer to them as pedro.
   (`user_identity.md`)

## Useful paths

| path | purpose |
|------|---------|
| `services/petboard/PLAN.md` | Full design doc |
| `services/petboard/server/` | Go backend (see `cmd/petboard/main.go`) |
| `services/petboard/web/` | Vite + React + Tailwind |
| `services/petboard/server/db/seed.sql` | Bootstrap data (petboard ↦ petboard) |
| `services/install-petboard.sh` | Installer (needs sudo) |
| `services/uninstall-petboard.sh` | Uninstaller (keeps /var/lib/petboard) |
| `base-setup/alive-server/main.go` | Has the public-path registry + return-URL fix |
| `/etc/attlas-public-paths.d/petboard.conf` | Registered public paths on the VM |
| `/var/lib/petboard/petboard.db` | Live sqlite DB on the VM |
| `/usr/local/share/petboard/dist` | Installed static asset directory |

## Known gotchas

- `go` is in `/usr/local/go/bin` on the VM, not on agnostic-user's
  non-login PATH. Install script now handles this explicitly.
- `alive-server` does not have an `ExecReload=` directive in its
  systemd unit. To trigger the public-path SIGHUP reloader, use
  `systemctl kill --signal=SIGHUP alive-server` (the install script
  does this automatically, falling back to `systemctl restart` on
  failure).
- The VM's attlas clone lives at `/home/agnostic-user/iapetus/attlas`.
  Git operations must be run as agnostic-user (`sudo -u`) because the
  working tree is owned by them.
- Caddy's `forward_auth` sets `X-Forwarded-Uri` on the subrequest to
  alive-server — this is what the public-path registry matches
  against. Confirmed via the Caddy docs during the probe.
