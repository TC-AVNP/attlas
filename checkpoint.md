# Refactor Checkpoint

**Last updated:** 2026-04-12 20:38 UTC

## Status

| Step | State | Last commit |
|---|---|---|
| 0 — Survey / service-tests.md | done | `be6a66c` |
| 1 — alive-server move + cmd/ | done | `d82e646` |
| 2.1 — internal/util | done | `c41bc1b` |
| 2.2 — internal/gcp | done | `14c09d7` |
| 2.3 — internal/config | done | `d6b1eb2` |
| 2.4 — internal/auth | done | `3196530` |
| 2.5 — internal/status | in progress | — |
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
| baseline (`d82e646`) | 2597 | — |
| util (`c41bc1b`) | 2547 | −50 |
| gcp (`14c09d7`) | 2505 | −42 |
| config (`d6b1eb2`) | 2452 | −53 |
| auth (`3196530`) | 1961 | −491 |

Target: ≤ 200. Still ~1760 lines to move. Remaining packages:
status, costs, openclaw, infra, services, static.

## Deploy state

alive-server at commit `3196530`, all tests passing (9 units active,
all API endpoints returning 200, gateway still 401s correctly,
loopback trust still routes to system user).

## Resume path

If session dies:
1. `cd /home/agnostic-user/iapetus/attlas`
2. `git log --oneline -10` — find last refactor commit
3. Read `refactor.md`, this file, `service-tests.md`.
4. `services/alive-server/cmd/attlas-server/main.go` is the source of
   remaining splits. Follow the order in "Step 2 plan".
5. Each split follows the same pattern:
   a. Create `internal/<pkg>/*.go` with exported versions
   b. `sed -i -E 's/\boldFn\(/pkg.NewFn(/g' cmd/attlas-server/*.go`
   c. Delete originals from main.go (Python script by line range
      works well for big chunks — see auth split for example)
   d. Add import to main.go
   e. `cd services/alive-server && go build -o attlas-server ./cmd/attlas-server`
   f. `sudo systemctl restart alive-server && curl .../api/status`
   g. commit + push
6. NEVER run `terraform`. NEVER kill the tmux session.
