# Services

Browser-accessible services that run behind the Caddy gateway.

## Current Services

- **Cloud Terminal** (`/terminal`) — [ttyd](https://github.com/tsl0922/ttyd) on port 7681
- **Cloud VS Code** (`/code`) — [code-server](https://github.com/coder/code-server) on port 8080
- **OpenClaw** — AI agent daemon (no web UI)

## Usage

```bash
~/attlas/services/install.sh
```

This presents a menu of available services. Select which ones to install, or `a` for all.

## Adding a New Service

1. Create `install-myservice.sh` (second line comment = menu description)
2. Install binary + create systemd unit + copy `.caddy` snippet to `/etc/caddy/conf.d/`
3. Create `myservice.caddy` with the reverse proxy route
4. It will auto-appear in the install menu

**Without a `.caddy` snippet in `/etc/caddy/conf.d/`, a web service will NOT be reachable from the browser.**
