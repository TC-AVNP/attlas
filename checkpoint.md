# Refactor Checkpoint — Final State

**Last updated:** 2026-04-12 21:10 UTC
**Feature:** petboard #22 — "Reorganise repo into per-service folders"

## Step status

| Step | State | Last commit |
|---|---|---|
| 0 — Survey / service-tests.md | done | `be6a66c` |
| 1 — alive-server → services/, cmd/ layout | done | `d82e646` |
| 2.1 — internal/util | done | `c41bc1b` |
| 2.2 — internal/gcp | done | `14c09d7` |
| 2.3 — internal/config | done | `d6b1eb2` |
| 2.4 — internal/auth | done | `3196530` |
| 2.5 — internal/status | done | `ca17cc5` |
| 2.6 — internal/costs | **deferred** | pending next session |
| 2.7 — internal/openclaw | **deferred** | pending next session |
| 2.8 — internal/infra | **deferred** | pending next session |
| 2.9 — internal/services | **deferred** | pending next session |
| 2.10 — internal/static | **deferred** | pending next session |
| 3 — Flatten services/ | done | `fc80734` |
| 4 — Move diary into services/ | done | `677a0b6` |
| 5 — Extract claude-login helper | done | `c13fa89` |
| 6 — Tidy up base-setup (sudoers) | done | `cf310b2` |
| 7 — Per-service CLAUDE.md | done | `e385e5a` |
| Final — Run all tests | done | — |

## Final test result

```
RESULT: 0 test(s) failed
```

Covering: 9 systemd units active, 8 internal ports bound, 7 API
endpoints returning 200, status payload carries all 7 services,
splitsies has ≥2 users, 8 external HTTPS URLs return 200/302,
diary HTML renders, dashboard shell loads, splitsies loopback trust
returns the system user, splitsies-gateway refuses non-loopback
requests (401), Caddy config is valid.

## Repo shape now

```
attlas/
├── base-setup/              # OS-level setup only (alive-server is gone from here)
│   ├── Caddyfile
│   └── setup.sh
├── infra/                   # unchanged
├── services/
│   ├── install.sh           # menu, discovers */install.sh
│   ├── alive-server/        # the dashboard
│   │   ├── cmd/attlas-server/main.go      # ~1589 lines (down from 2597)
│   │   ├── internal/
│   │   │   ├── auth/
│   │   │   ├── config/
│   │   │   ├── gcp/
│   │   │   ├── status/
│   │   │   └── util/
│   │   ├── frontend/
│   │   └── CLAUDE.md
│   ├── terminal/            # install.sh + ttyd-tmux.sh + *.caddy + CLAUDE.md
│   ├── code-server/
│   ├── openclaw/
│   ├── diary/               # was at attlas/diary/; Hugo content lives here now
│   ├── petboard/
│   ├── splitsies/
│   ├── splitsies-gateway/
│   ├── homelab-planner/
│   └── claude-login/        # was alive-server/claude-login-helper.py
```

No more flat `install-*.sh` or bare `*.caddy` files under `services/`.
No more top-level `attlas/diary/`. Every service folder has a
`CLAUDE.md`.

## Main.go size

- Baseline: 2597 lines
- After completed splits (util/gcp/config/auth/status): **1589 lines**
- Refactor.md target: ≤200 lines — not reached this session.

The remaining content in `cmd/attlas-server/main.go` is:
- `knownServices` registry + `Service` type
- `loadInstalledServices`
- `handleOpenclawDetail` + `OpenclawDetail`/`DayCost`/`openclawStatusJSON` + openclaw cache
- tmux helpers + `TerminalSession`/`TerminalDetail` + `handleTerminalDetail` + `handleTerminalKill`
- `VMUptimeSeries`/`InfrastructureDetail` + `fetchInstanceUptime` + `osBootTime` + `handleInfrastructureDetail`
- `CloudSpend` + `handleCloudSpend` + `fetchGCPSpendBigQuery`
- `CostCategorySeries`/`CostsBreakdown` + `handleCostsBreakdown` + `fetchGCPCategorizedCosts` + `buildCostSeries`
- `handleStopVM` + `fetchInstanceCreationTimestamp`
- `fetchAnthropicSpend`
- `getServicesStatus`
- `handleStatus`, `handleClaudeLogin`, `handleClaudeCode`
- `handleInstallService` + `handleUninstallService` + `findService`
- `extractIP`, `serveStatic`, `main`, `init`

Next sessions can extract `costs` (cloud_spend + breakdown + anthropic
+ bigquery), `openclaw` (detail + cache), `infra` (uptime + vm/stop),
`services` (registry + install/uninstall + terminal detail + splitsies
detail proxy), and `static` (serveStatic) following the pattern
established by the completed splits.

## Everything that still works after the refactor

- attlas.uk dashboard loads, all 7 services show, correct install/run state
- splitsies.attlas.uk works via its own gateway
- Google OAuth gate in front of attlas.uk, splitsies's own whitelist
  for splitsies.attlas.uk
- Services card Install / Uninstall buttons resolve to the new
  per-service folder paths (sudoers wildcard updated)
- Diary renders at attlas.uk/diary/ from its new home under
  services/diary/public
- Claude login flow resolves the helper via the new path, fallback
  to the legacy location for VMs that haven't redeployed

## Recovery path (if a future session needs to finish step 2)

1. `cd /home/agnostic-user/iapetus/attlas`
2. Read `refactor.md`, this file, `service-tests.md`
3. Start at `services/alive-server/cmd/attlas-server/main.go` — find
   a contiguous chunk (e.g. the costs block ~lines 620-1300) and
   extract to `internal/costs/costs.go` with exported names.
4. Update call sites in main.go (`sed`-style rename).
5. Delete originals, fix imports, `go build`, deploy, smoke test
   `/api/status`, commit.
6. Repeat for openclaw, infra, services, static.
7. NEVER run `terraform`. NEVER kill the tmux session.
