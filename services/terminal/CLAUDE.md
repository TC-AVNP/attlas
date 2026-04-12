# terminal

`ttyd`-backed web terminal served at `attlas.uk/terminal/`. Every
shell runs inside a named tmux session so browser crashes or network
blips don't drop the user's work.

## Files

- `install.sh` — installs `ttyd`, writes systemd unit, deploys
  `ttyd-tmux.sh`, assembles the ttyd custom index with the mobile
  keyboard overlay, and copies `terminal.caddy` to Caddy's conf.d.
- `uninstall.sh` — reverse of the above.
- `terminal.caddy` — Caddy route snippet for `/terminal*` →
  `localhost:7681`.
- `ttyd-tmux.sh` — the command ttyd runs per connection; wraps zsh
  in `tmux new-session -A -s <name>` so sessions persist across
  reconnects.
- `ttyd-mobile-keyboard.html` — a small overlay that appears only on
  touch devices, adding rows of tap-to-insert keys (tab, esc, arrows,
  ctrl, etc.) above the on-screen keyboard.

## Port

7681 (binds `0.0.0.0` — Caddy still fronts it).

## Sessions

Each browser connection gets a tmux session named from the URL arg
(`/terminal/?arg=foo` → tmux session `foo`). The dashboard's terminal
detail page (`/services/details/terminal`) lists live sessions and
offers a kill button; backend is at `GET /api/services/terminal`.
