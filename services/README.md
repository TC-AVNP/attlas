# Services

Browser-accessible services that run behind the Caddy gateway.

## Current Services

- **Cloud Terminal** (`/terminal`) — [ttyd](https://github.com/tsl0922/ttyd) on port 7681. Opens a zsh session with dotfiles.
- **Cloud VS Code** (`/code`) — [code-server](https://github.com/coder/code-server) on port 8080. VS Code in the browser.

## Usage

```bash
~/attlas/services/install.sh
```

## Adding a New Service

1. Add install logic to `install.sh`
2. Create a systemd unit
3. **Create a `.caddy` route snippet** — this is required to expose the service via HTTPS:
   ```caddyfile
   handle /myservice* {
       reverse_proxy localhost:PORT
   }
   ```
4. Add the snippet copy + Caddy reload to `install.sh`

**Without a `.caddy` snippet in `/etc/caddy/conf.d/`, the service will NOT be reachable from the browser.**
