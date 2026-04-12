# Refactor Checkpoint

**Last updated:** 2026-04-12 20:22 UTC
**Agent session:** splitsies/major-refactor autonomous build

## Status

| Step | State | Last commit |
|---|---|---|
| 0 — Survey / service-tests.md | done | `be6a66c` |
| 1 — alive-server move + cmd/ | done | `d82e646` |
| 2.1 — internal/util | done | `c41bc1b` |
| 2.2 — internal/gcp | done | `14c09d7` |
| 2.3 — internal/config | done | `d6b1eb2` |
| 2.4 — internal/auth | in progress | — |
| 2.5 — internal/status | pending | — |
| 2.6 — internal/costs | pending | — |
| 2.7 — internal/openclaw | pending | — |
| 2.8 — internal/infra | pending | — |
| 2.9 — internal/services | pending | — |
| 2.10 — internal/static | pending | — |
| 3 — Flatten services/ | pending | — |
| 4 — Move diary into services/ | pending | — |
| 5 — Extract claude-login helper | pending | — |
| 6 — Tidy up base-setup | pending | — |
| 7 — Per-service CLAUDE.md | pending | — |
| Final — Run all tests | pending | — |

## Main.go size trajectory

| commit | lines | delta |
|---|---|---|
| before any split (`d82e646`) | 2597 | baseline |
| after util (`c41bc1b`) | 2547 | −50 |
| after gcp (`14c09d7`) | 2505 | −42 |
| after config (`d6b1eb2`) | 2452 | −53 |

Target: ≤ 200 lines.

## Pattern for future splits (same procedure each time)

1. Create `services/alive-server/internal/<pkg>/<pkg>.go` (or
   multiple files) with exported versions of the functions to extract.
2. `sed -i -E 's/\boldFn\(/pkg.NewFn(/g' cmd/attlas-server/main.go cmd/attlas-server/splitsies_detail.go`
3. Delete the original function bodies and type defs from main.go.
4. Add `"attlas-server/internal/<pkg>"` to main.go's import block.
5. `cd services/alive-server && go build -o attlas-server ./cmd/attlas-server`
6. `sudo systemctl restart alive-server && sleep 2 && curl ... /api/status`
7. `git commit` + push.

## Deploy state

alive-server on VM: running commit `d6b1eb2`. Every test in
service-tests.md passes.

## Recovery instructions

If this session dies mid-refactor:
1. `cd /home/agnostic-user/iapetus/attlas`
2. `git log --oneline -20` — find the last `refactor:` commit
3. Read this file, `refactor.md`, `service-tests.md`.
4. Continue the step that's "in progress" above. Don't restart
   finished sub-steps.
5. NEVER run `terraform` or kill the tmux session.
6. Push a new checkpoint after each sub-step or every 15 min.
