#!/usr/bin/env bash
# ttyd-tmux.sh — launched by ttyd for every browser connection.
#
# Wraps zsh inside a persistent tmux session so shells survive browser
# disconnects (laptop closed, internet lost, etc.). The named socket
# (-L attlas) gives both the shell and the alive-server dashboard a
# deterministic path to talk to the same tmux server.
#
# Session name comes from the optional URL argument
# (ttyd is launched with --url-arg, so ?arg=foo → $1=foo). Sanitized
# to a tight character class to avoid any chance of shell injection
# even though the arg is only reachable by OAuth-authenticated users.
set -euo pipefail

RAW="${1:-main}"
SESSION="$(printf '%s' "$RAW" | tr -cd 'a-zA-Z0-9_-' | cut -c1-32)"
if [[ -z "$SESSION" ]]; then
  SESSION="main"
fi

# IAPETUS_TTYD is picked up by the `if-shell` guard in tmux.conf to
# force `mouse off` on this server only. Without it, xterm.js in the
# browser (ttyd 1.7.7 → xterm.js 5.4.0) can't do native drag-select +
# Cmd+C copy because tmux intercepts the drag events and has no
# working clipboard path out of the browser. Local laptop tmux
# servers never set this var, so their mouse-on behavior is
# unchanged. The env var only matters on the very first wrapper
# invocation after a server restart (when tmux.conf is sourced);
# later invocations inherit the already-applied setting.
export IAPETUS_TTYD=1

# Set the browser tab title to the session name. The \033]0;…\007
# escape is interpreted by xterm.js inside ttyd and updates the
# document title. tmux does not override this unless set-titles is on
# (it isn't on the attlas socket).
printf '\033]0;%s · terminal\007' "$SESSION"

# If the session already exists, just reattach (reconnect after browser disconnect).
if /usr/bin/tmux -L attlas has-session -t "$SESSION" 2>/dev/null; then
  exec /usr/bin/tmux -L attlas attach-session -t "$SESSION"
fi

# New session — start claude directly as the shell command so no
# typed command is visible. Wrap in zsh -c so the session stays alive
# (drops to zsh if claude ever exits).
/usr/bin/tmux -L attlas new-session -d -s "$SESSION" \
  "zsh -c 'claude --dangerously-skip-permissions; exec zsh'"

# Show a loading screen while claude initializes in the background.
# Poll the tmux pane content until claude's UI appears.
clear
printf '\n'
printf '   \033[1;34m◆\033[0m Starting Claude...\n'
printf '\n'

for i in $(seq 1 40); do
  CONTENT=$(/usr/bin/tmux -L attlas capture-pane -t "$SESSION" -p 2>/dev/null || true)
  # Claude shows its prompt marker (◆ or >) once ready
  if printf '%s' "$CONTENT" | grep -qE '[◆>❯]'; then
    break
  fi
  sleep 0.25
done

# Clear the loading screen and attach
clear
exec /usr/bin/tmux -L attlas attach-session -t "$SESSION"
