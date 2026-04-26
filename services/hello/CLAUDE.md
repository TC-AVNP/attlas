# hello

Public hello-world static site served at `hello.attlas.uk`. No auth.

## Files

- `index.html` — the page, copied to `/var/www/hello/index.html` on install.
- `hello.caddy` — site block for `hello.attlas.uk`, dropped into
  `/etc/caddy/sites.d/`. No `forward_auth`, so the site is public.
- `install.sh` / `uninstall.sh` — install copies files + caddy snippet
  and creates the Cloudflare A record; uninstall reverses everything.

## No backend

There's no binary, no systemd unit, no localhost port. Caddy serves the
static files directly via `file_server`.
