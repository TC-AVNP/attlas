# Revista Maria Tennis

Tennis tournament bracket manager hosted at `rm.attlas.uk`.

## Problem

When commonlisp6 organises casual tennis sessions with friends, there's
no easy way to set up a tournament bracket, track who's playing next, or
record match results -- especially with only one court available. Today
everyone stands around asking "who's up next?" and scores get forgotten.

## Architecture

```
Internet
   |
   v
 Caddy  (terminates TLS for rm.attlas.uk via /etc/caddy/sites.d/)
   |
   v
 revista-maria  (Go, 127.0.0.1:7696)
   |
   v
 SQLite (/var/lib/revista-maria/rm.db)
```

No Google OAuth. Public site is completely open (view-only). Admin
backoffice is protected by a shared passphrase.

## Concepts

- **Public view**: anyone can see the bracket, scores, current match,
  and who's up next. No login required.
- **Admin backoffice**: protected by passphrase. Admins register players,
  start the tournament, and record match results.
- **Single elimination**: first to 6 points wins. Byes added if player
  count isn't a power of 2.
- **One court**: the app always shows which match is current and which
  is next.

## Layout

```
services/revista-maria/
+-- CLAUDE.md                     # this file
+-- install.sh                    # idempotent install script
+-- uninstall.sh                  # cleanup script
+-- revista-maria.caddy           # Caddy site block
+-- server/
    +-- main.go                   # all server code
    +-- go.mod / go.sum
    +-- migrations/
    |   +-- 001_init.sql          # schema
    +-- templates/
        +-- public.html           # public bracket view
        +-- admin.html            # admin backoffice
        +-- admin_login.html      # passphrase entry
```

## API

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | / | none | Public bracket view |
| GET | /admin | admin | Admin backoffice |
| GET | /admin/login | none | Passphrase entry form |
| POST | /admin/login | none | Submit passphrase |
| POST | /admin/logout | admin | Logout |
| POST | /api/players | admin | Register a player |
| DELETE | /api/players/{id} | admin | Remove a player |
| POST | /api/tournament/start | admin | Generate brackets and start |
| POST | /api/tournament/reset | admin | Reset tournament |
| POST | /api/matches/{id}/score | admin | Record match result |
| GET | /api/state | none | Full tournament state as JSON |

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `RM_PORT` | `7696` | HTTP listen port |
| `RM_DB` | `/var/lib/revista-maria/rm.db` | SQLite path |
| `RM_ADMIN_PASSPHRASE` | `revista-maria-2026` | Admin passphrase |

## Development

```bash
cd server
PATH="/usr/local/go/bin:$PATH" go build -o /tmp/revista-maria .
RM_DB=/tmp/rm-test.db /tmp/revista-maria
```

Then visit http://localhost:7696/

## Deployment

```bash
sudo bash install.sh
```
