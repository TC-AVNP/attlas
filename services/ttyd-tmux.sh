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

# new-session -A: attach if it exists, create if it doesn't.
exec /usr/bin/tmux -L attlas new-session -A -s "$SESSION"
