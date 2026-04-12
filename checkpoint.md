# Refactor Checkpoint

**Last updated:** 2026-04-12 20:55 UTC

## Status

| Step | State | Last commit |
|---|---|---|
| 0 — Survey / service-tests.md | done | `be6a66c` |
| 1 — alive-server move + cmd/ | done | `d82e646` |
| 2.1 — internal/util | done | `c41bc1b` |
| 2.2 — internal/gcp | done | `14c09d7` |
| 2.3 — internal/config | done | `d6b1eb2` |
| 2.4 — internal/auth | done | `3196530` |
| 2.5 — internal/status | done | `ca17cc5` |
| 2.6 — internal/costs | **deferred** | — |
| 2.7 — internal/openclaw | **deferred** | — |
| 2.8 — internal/infra | **deferred** | — |
| 2.9 — internal/services | **deferred** | — |
| 2.10 — internal/static | **deferred** | — |
| 3 — Flatten services/ | done | `fc80734` |
| 4 — Move diary into services/ | done | `677a0b6` |
| 5 — Extract claude-login helper | in progress | — |
| 6 — Tidy up base-setup | pending | — |
| 7 — Per-service CLAUDE.md | pending | — |
| Final — Run all tests | pending | — |

## Main.go size trajectory

| commit | lines |
|---|---|
| baseline (`d82e646`) | 2597 |
| after util | 2547 |
| after gcp | 2505 |
| after config | 2452 |
| after auth | 1961 |
| after status | 1589 |

Current main.go is ~1589 lines. Target was ≤200 but the remaining
splits (costs/openclaw/infra/services/static) are deferred to finish
the higher-user-value structural moves first.

## Pivoted priorities

Steps 3-5 have the most day-to-day impact (services each own a folder,
diary lives in the right place, claude-login extracted). Splitting
the remaining main.go content is valuable but less urgent; it can
happen in another session following the same pattern as the completed
splits (util/gcp/config/auth/status).

## Deploy state

alive-server at `677a0b6`, serving from
`/home/agnostic-user/iapetus/attlas/services/alive-server/attlas-server`.
Every test passes (systemd units, localhost API endpoints, public
HTTPS, loopback trust).

## Services folder after step 3

```
services/
├── CLAUDE.md
├── README.md
├── install.sh                     # menu script, discovers */install.sh
├── alive-server/
├── code-server/                   # install.sh + uninstall.sh + code-server.caddy
├── diary/                         # install.sh + uninstall.sh + diary.caddy + hugo.toml + content/ + layouts/
├── homelab-planner/
├── openclaw/                      # install.sh + uninstall.sh + openclaw.caddy
├── petboard/
├── splitsies/                     # install.sh + ... + server/ + web/
├── splitsies-gateway/
└── terminal/                      # install.sh + uninstall.sh + terminal.caddy + ttyd-tmux.sh + ttyd-mobile-keyboard.html
```

No more flat `install-*.sh` or bare `*.caddy` files.

## Resume path

1. `cd /home/agnostic-user/iapetus/attlas`
2. `git log --oneline -15` — see last refactor commit
3. Read `refactor.md`, this file, `service-tests.md`
4. Continue at the in-progress step
5. NEVER run `terraform`. NEVER kill the tmux session.
6. When resuming the deferred main.go splits: pattern is in
   `internal/util`, `internal/gcp`, etc. Look at how auth was
   split for the biggest example (~500 lines).
