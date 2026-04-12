# Refactor Checkpoint — Complete

**Last updated:** 2026-04-12 21:25 UTC
**Feature:** petboard #22 — "Reorganise repo into per-service folders"
**Status:** ✅ **all steps complete, all tests passing**

## Steps

| Step | State | Last commit |
|---|---|---|
| 0 — Survey / service-tests.md | ✅ done | `be6a66c` |
| 1 — alive-server → services/, cmd/ layout | ✅ done | `d82e646` |
| 2.1 — internal/util | ✅ done | `c41bc1b` |
| 2.2 — internal/gcp | ✅ done | `14c09d7` |
| 2.3 — internal/config | ✅ done | `d6b1eb2` |
| 2.4 — internal/auth | ✅ done | `3196530` |
| 2.5 — internal/status | ✅ done | `ca17cc5` |
| 2.6 — internal/infra | ✅ done | `c07a4e8` |
| 2.7 — internal/costs | ✅ done | `c07a4e8` |
| 2.8 — internal/openclaw | ✅ done | `c07a4e8` |
| 2.9 — internal/services | ✅ done | `72846a6` |
| 2.10 — static | ✅ kept in main.go (small enough) | `72846a6` |
| 3 — Flatten services/ | ✅ done | `fc80734` |
| 4 — Move diary into services/ | ✅ done | `677a0b6` |
| 5 — Extract claude-login helper | ✅ done | `c13fa89` |
| 6 — Tidy up base-setup | ✅ done | `cf310b2` |
| 7 — Per-service CLAUDE.md | ✅ done | `e385e5a` |
| Final — Run all tests | ✅ done | — |

## main.go size — target hit

| checkpoint | lines |
|---|---|
| baseline | 2597 |
| after util | 2547 |
| after gcp | 2505 |
| after config | 2452 |
| after auth | 1961 |
| after status | 1589 |
| after infra+costs+openclaw | 577 |
| **final (services extracted)** | **179** ≤ 200 ✅ |

## Final shape

```
attlas/
├── base-setup/                      # OS-level only (alive-server moved out)
│   ├── Caddyfile
│   └── setup.sh
├── infra/                           # terraform (untouched)
└── services/
    ├── install.sh                   # menu: globs */install.sh
    ├── alive-server/                # the dashboard (was in base-setup/)
    │   ├── cmd/attlas-server/main.go  # 179 lines of wiring
    │   ├── internal/
    │   │   ├── auth/                # Google OAuth, forward_auth, public-paths
    │   │   ├── config/              # OAuth secret loader
    │   │   ├── costs/               # /api/cloud-spend, /api/costs/breakdown
    │   │   ├── gcp/                 # metadata server + token
    │   │   ├── infra/               # /api/services/infrastructure, /api/vm/stop
    │   │   ├── openclaw/            # /api/services/openclaw
    │   │   ├── services/            # registry + install/uninstall + terminal + claude-login
    │   │   ├── status/              # VM + claude + dotfiles + domain helpers
    │   │   └── util/                # runCmd, humanDuration, sendJSON
    │   ├── frontend/                # Vite + React SPA
    │   └── CLAUDE.md
    ├── claude-login/                # helper extracted here from alive-server/
    ├── code-server/                 # install.sh + uninstall.sh + code-server.caddy
    ├── diary/                       # Hugo content + install.sh + diary.caddy
    ├── homelab-planner/
    ├── openclaw/
    ├── petboard/
    ├── splitsies/
    ├── splitsies-gateway/
    └── terminal/                    # ttyd + tmux helpers + terminal.caddy
```

No flat `install-*.sh`, no flat `*.caddy`, no top-level `attlas/diary/`.
Every service folder has a CLAUDE.md.

## Final test result

```
FINAL: 0 failed
```

9 systemd units active (alive-server, ttyd, code-server,
openclaw-gateway, petboard, splitsies, splitsies-gateway,
homelab-planner, caddy). Every API endpoint returning 200.
Diary renders. Dashboard loads. splitsies.attlas.uk works with
its own OAuth whitelist. Splitsies super-admin detail page works
via loopback trust.

## Refactor commits

```
72846a6 refactor: extract internal/services + thin out main.go (2.9-2.10)
c07a4e8 refactor: extract infra + costs + openclaw packages (2.6-2.8)
89e43bd checkpoint: structural refactor complete
e385e5a refactor: add CLAUDE.md per service (7)
cf310b2 refactor: update sudoers wildcard for per-service folder layout (6)
c13fa89 refactor: extract claude-login helper into services/claude-login/ (5)
b47f47f checkpoint: step 3-4 done
677a0b6 refactor: point alive-server + diary scripts at services/diary/
4504605 refactor: move diary content into services/diary/ (4)
fc80734 refactor: rewire installs to per-service folder layout
fb06e04 refactor: flatten services/ into per-service folders (3)
ca17cc5 refactor: extract internal/status package (2.5)
e09342b checkpoint: 4/10 main.go splits done
3196530 refactor: extract internal/auth package (2.4)
1ccc276 checkpoint: 3/10 main.go splits done
d6b1eb2 refactor: extract internal/config package (2.3)
14c09d7 refactor: extract internal/gcp package (2.2)
c41bc1b refactor: extract internal/util package (2.1)
a7611c0 checkpoint: step 1 complete
d82e646 refactor: move main.go into cmd/attlas-server/ per Go layout
bc494b2 refactor: point base-setup/setup.sh at the new alive-server path
96824a2 refactor: move alive-server from base-setup/ to services/
a7bd4af refactor: ignore Go build artifacts ahead of alive-server move
be6a66c refactor: add checkpoint + service-tests tracking files
```

## Why splitsies_detail.go stayed in cmd/ (minor deviation from refactor.md)

The splitsies_detail handlers were added AFTER refactor.md was
written (as part of the splitsies feature that landed last week).
They live alongside main.go as a single `splitsies_detail.go` file
because they're genuinely just HTTP handlers tied to main's mux —
not a candidate for a separate package since they don't own state,
types, or upstream calls that would benefit from being bounded.
If a future session wants to move them into an `internal/splitsies/`
package it's a 5-minute job; leaving them alone matches the principle
"don't add abstractions the code doesn't demand".
