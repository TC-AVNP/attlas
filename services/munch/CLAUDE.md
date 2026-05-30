# Munch

Meal-prep companion at `munch.attlas.uk`.

## Problem

When commonlisp wants to cook a specific dish, he has to figure out every
ingredient, build a shopping list by hand, and track what he's grabbed
while moving through the shop. No record of whether a meal was worth
making again.

## Architecture

```
Internet
   |
   v
 Caddy  (forward_auth -> alive-server, then reverse_proxy)
   |
   v
 munch  (Go, 127.0.0.1:7698)
   |
   +-> SQLite (/var/lib/munch/munch.db)
   +-> Anthropic API (ingredient fetching)
```

## Layout

```
services/munch/
├── CLAUDE.md
├── install.sh
├── uninstall.sh
├── munch.caddy
└── server/
    ├── go.mod / go.sum
    ├── main.go
    ├── migrations/
    │   └── 001_init.sql
    └── templates/
        ├── index.html    (main dashboard)
        └── shop.html     (shopping checklist)
```

## Features

1. **Dish + ingredient fetch** — Enter a dish name, an LLM fetches the
   full ingredient list with quantities and units.
2. **Shopping checklist** — Interactive, touch-friendly checklist for
   in-store use. Persisted server-side.
3. **Rating** — Rate dishes 0-10 after eating. Multiple raters per dish.
4. **Rankings** — See best-rated dishes sorted by average score.

## API

| Method | Path | Purpose |
|--------|------|---------|
| GET | / | Main dashboard |
| GET | /shop/{id} | Shopping checklist page |
| POST | /api/dishes | Create dish + fetch ingredients |
| GET | /api/dishes/{id} | Dish detail with ingredients |
| DELETE | /api/dishes/{id} | Delete a dish |
| POST | /api/dishes/{id}/shop | Start shopping session |
| GET | /api/shop/{id} | Get shopping session |
| PUT | /api/shop/{id}/toggle/{ingredientId} | Toggle ingredient check |
| POST | /api/dishes/{id}/rate | Submit rating {rater, score} |
| GET | /api/rankings | All dishes ranked by avg score |

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `MUNCH_PORT` | `7703` | HTTP listen port |
| `MUNCH_DB` | `/var/lib/munch/munch.db` | SQLite path |
| `MUNCH_ANTHROPIC_KEY` | — | Anthropic API key (from openclaw-config) |

## Development

```bash
cd server
ANTHROPIC_KEY=$(gcloud secrets versions access latest --secret=openclaw-config | python3 -c "import sys,json; print(json.load(sys.stdin)['anthropic_api_key'])")
PATH="/usr/local/go/bin:$PATH" go build -o /tmp/munch .
MUNCH_DB=/tmp/munch-dev.db MUNCH_ANTHROPIC_KEY="$ANTHROPIC_KEY" /tmp/munch
```

## Deployment

```bash
sudo bash install.sh
```
