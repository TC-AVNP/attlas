# claude-login

PTY automation script that drives the `claude login` CLI wizard
non-interactively for the dashboard's "Log in to Claude" button.
Not a standalone service — there's no systemd unit, no Caddy route.
The alive-server dashboard shells out to this Python script whenever
the user clicks the button.

## Files

- `claude-login-helper.py` — the PTY driver. Communicates with the
  Go side via files in `/tmp/`:
  - `/tmp/claude-login-url` — the OAuth URL claude prints during the
    login flow, read by the dashboard to display to the user.
  - `/tmp/claude-login-code` — the paste-back code from the OAuth
    redirect, written by the dashboard for the Python side to pipe
    into the claude CLI.
  - `/tmp/claude-login-result` — final success/failure signal.
  - `/tmp/claude-login-helper.log` — running log for debugging.
- `install.sh` — marker install script. Ensures python3 is present
  and prints where the helper lives. No systemd / Caddy artifacts.

## How the dashboard finds it

alive-server's `handleClaudeLogin` resolves the helper via, in order:
1. `${ATTLAS_DIR}/services/claude-login/claude-login-helper.py`
2. Next to the binary (`filepath.Dir(os.Args[0])/claude-login-helper.py`) — legacy
3. `${frontend/dist}/../claude-login-helper.py` — older legacy layout

Only (1) matches on current installs; (2) and (3) keep old VMs alive
until they redeploy.

## Sudoers dependency

The alive-svc user needs `/etc/sudoers.d/alive-svc-claude` to run
`claude auth status` and `python3 claude-login-helper.py` as
`agnostic-user`. That rule is installed by base-setup/setup.sh (same
place the user's Google OAuth config gets seeded).
