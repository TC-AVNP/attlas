# Refactor Checkpoint

**Last updated:** 2026-04-12 20:02 UTC
**Agent session:** splitsies/major-refactor autonomous build

This file is pushed to origin on a rolling basis (every ~15 min) so
another agent can resume this refactor from a clean state if this
machine dies mid-session.

## Task

Petboard feature 22 — "Reorganise repo into per-service folders".
Plan of record: `attlas/refactor.md` (read it first if resuming).
Test checklist: `attlas/service-tests.md`.

## Constraints (from the user)

- Never run `terraform apply` or `terraform destroy`.
- Never destroy the tmux session.
- Do NOT ask for clarification — make the call and move on.
- All tests that pass BEFORE the refactor must pass AFTER the refactor.
- Push checkpoints to origin roughly every 15 min.

## Step log

### 2026-04-12 20:02 UTC — prep
- Feature 22 marked in_progress in petboard.
- Read `attlas/refactor.md` (432 lines) and absorbed the plan.
- Wrote `service-tests.md` enumerating every test that must survive
  the refactor.
- Created tasks 29–38 for each refactor step.
- Captured current state:
  - 8 active systemd services: alive-server, ttyd, code-server,
    openclaw-gateway, petboard, splitsies, splitsies-gateway,
    homelab-planner.
  - Services added after refactor.md was written:
    splitsies, splitsies-gateway, homelab-planner. These already
    follow the per-service folder pattern so they don't need to move.
  - Still-flat services that must be folderized:
    terminal, code-server, openclaw (has a folder but install script
    is flat), diary (top-level orphan).
  - alive-server still lives at `base-setup/alive-server/` and its
    main.go is ~2600 lines with `splitsies_detail.go` added recently.

## Status

| Step | State |
|---|---|
| 0 — Survey / service-tests.md | in progress |
| 1 — Move alive-server to services/ | pending |
| 2 — Split main.go into internal/ packages | pending |
| 3 — Flatten services/ into per-service folders | pending |
| 4 — Move diary into services/ | pending |
| 5 — Extract claude-login helper | pending |
| 6 — Tidy up base-setup | pending |
| 7 — Per-service CLAUDE.md | pending |
| Final — Run all tests | pending |

## Next steps

1. Run baseline service-tests to capture "known-broken" if any.
2. Start Step 1: git mv base-setup/alive-server → services/alive-server
   as a single commit (move only, no code edits).
3. Rename the Go module path and fix imports as a second commit.
4. Reinstall the systemd unit to point at the new ExecStart, redeploy,
   and verify the dashboard still responds identically.

## Recovery notes (for future agents)

If you resume from here:
- `cd /home/agnostic-user/iapetus/attlas`
- Read `refactor.md`, `checkpoint.md` (this file), and `service-tests.md`.
- `git log --oneline -20` will show where the refactor left off.
- Follow the plan. Don't ask questions. Don't run `terraform`.
- `systemctl is-active alive-server` must stay `active` end-to-end —
  if it flips to `failed`, stop and fix before moving on.
