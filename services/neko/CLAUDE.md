# Neko

Hello-world page showing cats playing, hosted at `neko.attlas.uk`.

## Problem

When commonlisp6 wants a quick, fun hello-world project to spin up and
show off, there's nothing lightweight and playful in his ecosystem to
reach for.

## Architecture

```
Internet
   |
   v
 Caddy  (terminates TLS for neko.attlas.uk via /etc/caddy/sites.d/)
   |
   v
 neko  (Go, 127.0.0.1:7699)
```

No database. No authentication. Just a single HTML page with CSS cat
animations served by a minimal Go binary.

## Layout

```
services/neko/
├── CLAUDE.md           # this file
├── install.sh          # idempotent install script
├── uninstall.sh        # cleanup script
├── neko.caddy          # Caddy site block
└── server/
    ├── main.go         # Go server (~50 lines)
    ├── go.mod
    └── templates/
        └── index.html  # page with CSS cat animations
```

## Development

```bash
cd server
PATH="/usr/local/go/bin:$PATH" go build -o /tmp/neko .
/tmp/neko
```

Then visit http://localhost:7699/

## Environment Variables

| Variable    | Default | Purpose          |
|-------------|---------|------------------|
| `NEKO_PORT` | `7699`  | HTTP listen port |

## Deployment

```bash
sudo bash install.sh
```
