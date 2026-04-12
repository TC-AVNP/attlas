# Refactor Checkpoint

**Last updated:** 2026-04-12 20:10 UTC
**Agent session:** splitsies/major-refactor autonomous build

## Task

Petboard feature 22 — "Reorganise repo into per-service folders".
Plan of record: `attlas/refactor.md`.
Test checklist: `attlas/service-tests.md`.

## Constraints

- Never run `terraform`.
- Never destroy the tmux session.
- Don't ask questions — make the call and proceed.
- All passing tests before refactor must pass after.
- Push checkpoints to origin every ~15 min.

## Status

| Step | State | Last commit |
|---|---|---|
| 0 — Survey / service-tests.md | done | `be6a66c` |
| 1a — Add build artifacts to .gitignore | done | `a7bd4af` |
| 1b — git mv base-setup/alive-server → services/alive-server | done | `96824a2` |
| 1c — Update setup.sh paths + systemd unit on VM | done | `bc494b2` |
| 1d — Move main.go into cmd/attlas-server/ | done | `d82e646` |
| 2 — Split main.go into internal/ packages | in progress | — |
| 3 — Flatten services/ into per-service folders | pending | — |
| 4 — Move diary into services/ | pending | — |
| 5 — Extract claude-login helper | pending | — |
| 6 — Tidy up base-setup | pending | — |
| 7 — Per-service CLAUDE.md | pending | — |
| Final — Run all tests | pending | — |

## Baseline test run

Zero failures at `be6a66c`. Documented ttyd binding on `0.0.0.0:7681`
as pre-existing (not caused by this refactor) in service-tests.md's
"Known-broken baseline" section.

## Service state on VM (all active)

alive-server, ttyd, code-server, openclaw-gateway, petboard,
splitsies, splitsies-gateway, homelab-planner, caddy.

After step 1: alive-server serving from
`services/alive-server/attlas-server` (was `base-setup/...`). Systemd
unit rewritten on disk, running binary updated, API endpoints all
returning 200.

## Step 2 plan (main.go split)

Source: `services/alive-server/cmd/attlas-server/main.go` (~2600 lines,
64 top-level funcs). Will be split in this order (each sub-commit
must leave `go build` green):

1. `internal/util/` — runCmd, runCmdCtx, humanDuration, sendJSON,
   externalAPICacheTTL (65 call sites to update).
2. `internal/gcp/` — gcpMeta, getMetadataToken.
3. `internal/config/` — OAuthConfig, loadOAuthConfig, loadOrCreateSecret.
4. `internal/auth/` — stateStore, session funcs, OAuth2 handlers,
   handleAuthVerify, pathRegistry.
5. `internal/status/` — handleStatus + getVMInfo + system load +
   claude + dotfiles + domain.
6. `internal/costs/` — handleCloudSpend, handleCostsBreakdown,
   fetchAnthropicSpend, fetchGCPSpendBigQuery,
   fetchGCPCategorizedCosts.
7. `internal/openclaw/` — handleOpenclawDetail (depends on costs for
   fetchAnthropicSpend).
8. `internal/infra/` — handleInfrastructureDetail, fetchInstanceUptime,
   handleStopVM.
9. `internal/services/` — knownServices, handleInstallService,
   handleUninstallService, findService, loadInstalledServices,
   getServicesStatus + handleTerminalDetail + tmux helpers +
   splitsies_detail handlers.
10. `internal/static/` — serveStatic + error/access-denied pages.

After each: rebuild, restart, smoke-test `/api/status`.

## Resume instructions

If this session dies:
1. `cd /home/agnostic-user/iapetus/attlas`
2. `git log --oneline -10` to see last commit
3. Read this file + `refactor.md` + `service-tests.md`
4. Continue at the in-progress step
5. Never run terraform; never kill the tmux session
