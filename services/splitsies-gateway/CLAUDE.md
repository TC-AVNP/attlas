# splitsies-gateway

Subdomain-level reverse proxy for `splitsies.attlas.uk`. Caddy
terminates TLS for the subdomain and forwards everything here on
`127.0.0.1:7700`; this gateway then dispatches requests to backend
services registered in `/etc/splitsies-gateway.d/`.

Today only the splitsies app itself is registered (`/` →
`127.0.0.1:7692`), but the pattern supports adding more services
under the same subdomain without touching Caddy.

## Files

- `main.go` — the reverse proxy with SIGHUP-triggered route reload.
- `go.mod` — stdlib only, no external deps.
- `splitsies-gateway.caddy` — dropped into `/etc/caddy/sites.d/` as
  a full site block for `splitsies.attlas.uk`.
- `install.sh` — builds the binary, writes the systemd unit, creates
  the Cloudflare A record for `splitsies.attlas.uk`, patches the
  base Caddyfile (idempotently) to import `sites.d/*.caddy`.

## Route registry

Services register by dropping a file in `/etc/splitsies-gateway.d/`:

```
# Format: <path_prefix> <backend_url>
/ http://127.0.0.1:7692
```

Longest prefix wins. `systemctl reload splitsies-gateway` (which
sends SIGHUP) makes the gateway re-read the directory without
dropping connections.

## Port

7700 (bound to 127.0.0.1).
