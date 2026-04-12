# Service Tests

Tests that MUST pass before and after the refactor. Run against the VM
(127.0.0.1 for internal checks, public URLs for end-to-end).

## Running these tests

```bash
bash -c '
set +e
FAIL=0
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1 — $2"; FAIL=$((FAIL+1)); }
# inline each test block below here, or source a runner
'
```

Most tests use `curl -s -o /dev/null -w "%{http_code}"` against localhost.
End-to-end HTTPS tests go through Caddy.

## Baseline capture

Before running tests, record the current commit SHA so we can tell
"passed then, passing now" from "was never working":

```bash
git log -1 --format='%H %s' > /tmp/baseline-sha.txt
```

## A. Systemd units (must all be `active`)

| Unit | Expected | Command |
|---|---|---|
| `alive-server.service` | active | `systemctl is-active alive-server` |
| `ttyd.service` | active | `systemctl is-active ttyd` |
| `code-server.service` | active | `systemctl is-active code-server` |
| `openclaw-gateway.service` | active | `systemctl is-active openclaw-gateway` |
| `petboard.service` | active | `systemctl is-active petboard` |
| `splitsies.service` | active | `systemctl is-active splitsies` |
| `splitsies-gateway.service` | active | `systemctl is-active splitsies-gateway` |
| `homelab-planner.service` | active | `systemctl is-active homelab-planner` |
| `caddy.service` | active | `systemctl is-active caddy` |

## B. Listen ports (internal)

| Port | Process | Command |
|---|---|---|
| 3000 | attlas-server | `ss -tlnp \| grep ':3000.*attlas-server'` |
| 7681 | ttyd | `ss -tlnp \| grep ':7681'` |
| 8080 | code-server | `ss -tlnp \| grep ':8080'` |
| 18789 | openclaw-gateway | `ss -tlnp \| grep ':18789'` |
| 7690 | petboard | `ss -tlnp \| grep ':7690'` |
| 7691 | homelab-planner | `ss -tlnp \| grep ':7691'` |
| 7692 | splitsies | `ss -tlnp \| grep ':7692'` |
| 7700 | splitsies-gateway | `ss -tlnp \| grep ':7700'` |

## C. alive-server API endpoints (localhost:3000 — bypasses OAuth)

| Endpoint | Expected | Notes |
|---|---|---|
| `GET /api/auth/verify` | 200 or 401 | forward_auth handler |
| `GET /api/status` | 200 | returns JSON with services, vm, system_load, claude, dotfiles, domain |
| `GET /api/services/openclaw` | 200 | returns openclaw detail JSON |
| `GET /api/services/terminal` | 200 | returns ttyd sessions list |
| `GET /api/services/infrastructure` | 200 | VM + uptime info |
| `GET /api/services/splitsies` | 200 | list of splitsies users (2 active admins) |
| `GET /api/cloud-spend` | 200 | GCP spend summary |
| `GET /api/costs/breakdown` | 200 | categorized costs |

Checks via:
```bash
for p in /api/status /api/services/openclaw /api/services/terminal /api/services/infrastructure /api/services/splitsies /api/cloud-spend /api/costs/breakdown; do
  code=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:3000$p")
  [[ "$code" == "200" ]] && pass "localhost:3000$p" || fail "localhost:3000$p" "got $code"
done
```

## D. /api/status payload sanity

Fetch once and assert key fields exist:

```bash
S=$(curl -s http://localhost:3000/api/status)
echo "$S" | python3 -c "
import sys, json
d = json.load(sys.stdin)
assert 'services' in d and len(d['services']) >= 7, f'services: {len(d.get(\"services\",[]))}'
ids = [s['id'] for s in d['services']]
for want in ['terminal','code-server','openclaw','diary','petboard','homelab-planner','splitsies']:
    assert want in ids, f'missing {want}'
assert 'vm' in d or 'vm_info' in d, 'no VM info'
assert 'system_load' in d or 'load' in d or 'cpu_percent' in d, 'no system_load'
print('OK')
"
```

## E. Splitsies detail page endpoint returns users

```bash
N=$(curl -s http://localhost:3000/api/services/splitsies | python3 -c "import sys, json; print(len(json.load(sys.stdin)))")
[[ "$N" -ge 2 ]] && pass "splitsies users count >= 2" || fail "splitsies users" "got $N"
```

## F. Public routing via Caddy + DNS

Public URLs — should all return 200 or a redirect (302) to attlas
dashboard/login. Non-authed endpoints like /diary and /petboard/api/events
may differ, but the host should be reachable.

| URL | Expected | Notes |
|---|---|---|
| `https://attlas.uk/` | 200 or 302 | dashboard (redirects to google login if unauth) |
| `https://attlas.uk/terminal/` | 200 or 302 | terminal page |
| `https://attlas.uk/code/` | 200 or 302 | code-server |
| `https://attlas.uk/openclaw/` | 200 or 302 | openclaw status endpoint |
| `https://attlas.uk/petboard/` | 200 or 302 | petboard SPA |
| `https://attlas.uk/homelab-planner/` | 200 or 302 | homelab SPA |
| `https://attlas.uk/diary/` | 200 or 302 | Hugo static site |
| `https://splitsies.attlas.uk/` | 200 | splitsies SPA (own OAuth, separate host) |
| `https://splitsies.attlas.uk/api/auth/me` | 401 | unauth API |

Check:
```bash
for u in https://attlas.uk/ https://attlas.uk/terminal/ https://attlas.uk/code/ https://attlas.uk/openclaw/ https://attlas.uk/petboard/ https://attlas.uk/homelab-planner/ https://attlas.uk/diary/ https://splitsies.attlas.uk/; do
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 15 "$u")
  [[ "$code" == "200" || "$code" == "302" ]] && pass "$u" || fail "$u" "got $code"
done
```

## G. Diary static site renders

```bash
curl -s http://localhost:3000/diary/ | grep -q -E '(Day [0-9]|<title>)' && pass "diary HTML" || fail "diary HTML" "no diary content"
```

## H. Dashboard frontend served

```bash
curl -s http://localhost:3000/ | grep -q '<div id="root"' && pass "dashboard HTML shell" || fail "dashboard HTML shell" "missing root div"
asset=$(curl -s http://localhost:3000/ | grep -oE '/assets/index-[A-Za-z0-9]+\.js' | head -1)
[[ -n "$asset" ]] && curl -s -o /dev/null -w "%{http_code}\n" "http://localhost:3000$asset" | grep -q 200 && pass "dashboard JS asset" || fail "dashboard JS asset"
```

## I. Splitsies loopback-trust (super-admin path)

```bash
J=$(curl -s http://127.0.0.1:7692/api/auth/me)
echo "$J" | python3 -c "import sys,json; d=json.load(sys.stdin); assert d['email']=='system' and d['is_admin']==True, d" && pass "splitsies loopback trust" || fail "splitsies loopback trust"
```

## J. Splitsies-gateway routes to backend

```bash
code=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:7700/api/auth/me)
# 401 is expected since X-Forwarded-For is set by the gateway
[[ "$code" == "401" ]] && pass "gateway → splitsies (401 expected)" || fail "gateway → splitsies" "got $code"
```

## K. Caddy config validates

```bash
sudo caddy validate --config /etc/caddy/Caddyfile 2>&1 | grep -qv 'error' && pass "caddy config" || fail "caddy config"
```

## L. Frontend-only sanity (manual, for reference)

These are visual/interactive — skip during automated runs but verify
manually after the refactor:

- attlas.uk dashboard loads the services card with all 8 services listed
- "open" links work for each
- Splitsies details page lists the two admin users and promote/demote
  buttons render
- Openclaw details page shows channels/status
- Terminal details page lists sessions

## M. Git repo sanity

```bash
cd /home/agnostic-user/iapetus/attlas
git status --short | grep -v -E '(alive-server|attlas-server|node_modules|/dist/)' | grep -q . && fail "unexpected uncommitted files" "$(git status --short)" || pass "working tree clean (modulo binaries)"
```

---

## Known-broken baseline (tests that already fail before refactor)

Record any baseline failures here so we know the refactor didn't cause
them. Populate after the first run of these tests against main.
