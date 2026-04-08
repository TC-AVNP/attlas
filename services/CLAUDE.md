# services

Browser-accessible services running behind the Caddy gateway.

## Current Services

| Service | Port | URL Path | systemd unit | Description |
|---------|------|----------|-------------|-------------|
| ttyd | 7681 | /terminal | ttyd.service | Web terminal (zsh with dotfiles) |
| code-server | 8080 | /code | code-server.service | VS Code in the browser |

Both bind to localhost only. Caddy handles TLS + basic auth.

## IMPORTANT: Adding a New Service

Every service that needs to be reachable from the internet **MUST** have a Caddy route snippet. Without it, the service is only accessible on localhost.

To add a new service:

1. Add the install logic to `install.sh` (with idempotency guard: `if ! command -v ... &>/dev/null`)
2. Create a systemd unit for the service
3. **Create a `.caddy` route snippet file** in this directory (e.g., `myservice.caddy`):
   ```caddyfile
   handle /myservice* {
       reverse_proxy localhost:PORT
   }
   ```
4. Copy the snippet to `/etc/caddy/conf.d/` and reload Caddy:
   ```bash
   sudo cp myservice.caddy /etc/caddy/conf.d/
   sudo systemctl reload caddy
   ```

## Usage

```bash
~/attlas/services/install.sh
```

This is typically run via the prompt at the end of `base-setup/setup.sh`, but can also be run independently.
