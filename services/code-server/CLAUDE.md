# code-server

VS Code in the browser at `attlas.uk/code/`. Upstream is
`coder/code-server`; we install the Debian package and drop in a
Caddy route + systemd unit.

## Files

- `install.sh` — adds the code-server APT source, installs the
  package, writes a systemd unit running as `agnostic-user`, deploys
  `code-server.caddy`.
- `uninstall.sh` — removes the systemd unit and the Caddy snippet.
- `code-server.caddy` — Caddy route for `/code*` → `localhost:8080`
  (uses `handle_path` so the prefix is stripped before the upstream
  sees the URL).

## Port

8080.

## Credentials

code-server's config lives in `/home/agnostic-user/.config/code-server/`
and contains an auto-generated password. Since the whole Caddy site is
behind Google OAuth already, we tell code-server to skip its own auth
by setting `auth: none` in that config.
