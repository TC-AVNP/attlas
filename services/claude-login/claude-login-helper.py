#!/usr/bin/env python3
"""
Helper that runs 'claude login' with a PTY and navigates the setup wizard.
Communicates with the dashboard via files in /tmp/.
"""

import pty
import os
import select
import time
import sys
import re
import struct
import fcntl
import termios
import traceback

URL_FILE = "/tmp/claude-login-url"
CODE_FILE = "/tmp/claude-login-code"
RESULT_FILE = "/tmp/claude-login-result"
LOG_FILE = "/tmp/claude-login-helper.log"

log_f = open(LOG_FILE, "w")

def log(msg):
    ts = time.strftime("%H:%M:%S")
    line = f"[{ts}] {msg}"
    log_f.write(line + "\n")
    log_f.flush()

def read_all(fd, timeout=5):
    buf = b""
    deadline = time.time() + timeout
    while time.time() < deadline:
        r, _, _ = select.select([fd], [], [], 0.3)
        if r:
            try:
                buf += os.read(fd, 8192)
            except OSError:
                break
        elif buf:
            break
    return buf

def clean(data):
    return re.sub(rb"\x1b\[[0-9;?]*[a-zA-Z]|\x1b[()][AB012]|\x1b[=>]|\x1b\][^\x07]*\x07", b"", data).decode(errors="replace")

def main():
    # Clean up
    for f in [URL_FILE, CODE_FILE, RESULT_FILE]:
        try: os.remove(f)
        except FileNotFoundError: pass

    master_fd, slave_fd = pty.openpty()
    winsize = struct.pack("HHHH", 30, 120, 0, 0)
    fcntl.ioctl(slave_fd, termios.TIOCSWINSZ, winsize)

    env = os.environ.copy()
    env["BROWSER"] = "echo"
    env["TERM"] = "xterm-256color"
    env["COLUMNS"] = "120"
    env["LINES"] = "30"

    pid = os.fork()
    if pid == 0:
        os.close(master_fd)
        os.setsid()
        fcntl.ioctl(slave_fd, termios.TIOCSCTTY, 0)
        os.dup2(slave_fd, 0)
        os.dup2(slave_fd, 1)
        os.dup2(slave_fd, 2)
        if slave_fd > 2: os.close(slave_fd)
        # Run `claude login` as agnostic-user, not as alive-svc, so the
        # resulting .claude.json lands in the interactive user's home and
        # `claude` invoked from /terminal sees the same login.
        # Requires /etc/sudoers.d/alive-svc-claude to grant alive-svc
        # NOPASSWD sudo to /usr/bin/claude as agnostic-user.
        # -H switches HOME to agnostic-user's home so claude writes its
        # config there. -n forbids password prompts (fail-fast on misconfig).
        os.execvpe(
            "sudo",
            ["sudo", "-n", "-u", "agnostic-user", "-H", "claude", "login"],
            env,
        )

    # Parent
    os.close(slave_fd)
    log(f"Started claude login (PID {pid})")

    # Continuously read PTY and navigate the wizard
    # Goal: reach the "paste code" screen
    # Along the way, press Enter on theme picker, login method, etc.
    buf = b""
    entered = set()
    url = None
    triggers = ["theme", "text style", "login method", "select login", "subscription", "claude account"]

    log("Navigating wizard to 'paste code' screen...")

    deadline = time.time() + 90
    while time.time() < deadline:
        data = read_all(master_fd, 3)
        if data:
            buf += data
            log(f"  [{len(buf)} bytes total] last read: {len(data)} bytes")

        text = clean(buf).lower()

        # Check if we reached the code input screen
        if "paste code" in text or ("paste" in text and "code" in text):
            log("  REACHED 'paste code' screen!")
            break

        # Press Enter on any wizard screen we recognize
        matched = False
        for trigger in triggers:
            if trigger in text and trigger not in entered:
                log(f"  found '{trigger}' — waiting 2s then pressing Enter")
                time.sleep(2)
                os.write(master_fd, b"\r")
                entered.add(trigger)
                log(f"  Enter pressed for '{trigger}'")
                time.sleep(3)
                # Read new data and APPEND (don't reset)
                new_data = read_all(master_fd, 5)
                buf += new_data
                log(f"  after Enter: +{len(new_data)} bytes, total {len(buf)}")
                matched = True
                break

        if not matched and not data:
            log(f"  no data, waiting...")
            time.sleep(2)

    # Extract URL from accumulated output
    # The URL may be wrapped across lines in the terminal output
    # Strip all ANSI codes and whitespace, then find the URL
    raw = buf.decode(errors="replace")
    # Remove ANSI escape sequences
    stripped = re.sub(r"\x1b\[[0-9;?]*[a-zA-Z]|\x1b[()][AB012]|\x1b[=>]|\x1b\][^\x07]*\x07", "", raw)
    # Remove carriage returns AND newlines (URL wraps across lines at terminal width)
    stripped = stripped.replace("\r", "").replace("\n", "")

    # Find URL. After stripping ANSI + newlines, the URL runs into the next text.
    # Known trailing text is "Pastecodehereifprompted>" or "Browserdidn'topen..."
    # Simply cut at "Paste" or at the first uppercase letter after "state="
    url_match = re.search(r"(https://claude\.com/[^\s]*|https://console\.anthropic\.com/[^\s]*)", stripped)
    if url_match:
        url = url_match.group(1)
        # Cut at "Paste" if it got concatenated
        for cut_word in ["Paste", "Browser", "Press", "Sign"]:
            idx = url.find(cut_word)
            if idx > 0:
                url = url[:idx]
                break
    log(f"Extracted URL ({len(url) if url else 0} chars): ends with ...{url[-30:] if url else ''}")

    if not url:
        log(f"ERROR: No URL found. Clean text: {clean(buf)[-500:]}")
        with open(RESULT_FILE, "w") as f:
            f.write("ERROR: Could not find auth URL")
        os.kill(pid, 9)
        return

    log(f"URL: {url[:80]}...")
    with open(URL_FILE, "w") as f:
        f.write(url)

    # Wait for code
    log("Waiting for code file...")
    code = None
    for i in range(300):
        if os.path.exists(CODE_FILE):
            with open(CODE_FILE) as f:
                code = f.read().strip()
            if code:
                os.remove(CODE_FILE)
                break
        if i % 30 == 0:
            log(f"  waiting... ({i}s)")
            try: os.kill(pid, 0)
            except ProcessLookupError:
                log("ERROR: Process died")
                with open(RESULT_FILE, "w") as f:
                    f.write("ERROR: claude login exited")
                return
        time.sleep(1)

    if not code:
        log("ERROR: Timed out")
        with open(RESULT_FILE, "w") as f:
            f.write("ERROR: Timed out waiting for code")
        os.kill(pid, 9)
        return

    # Type code
    log(f"Typing code ({len(code)} chars)...")
    for ch in code:
        os.write(master_fd, ch.encode())
        time.sleep(0.01)
    time.sleep(0.5)
    os.write(master_fd, b"\r")
    log("Code submitted, handling post-login prompts...")

    # Handle remaining prompts (security notice, trust folder)
    time.sleep(5)
    post_buf = read_all(master_fd, 10)
    post_text = clean(post_buf).lower()
    log(f"Post-login: {len(post_buf)} bytes")

    if "enter" in post_text or "continue" in post_text:
        log("  pressing Enter (security notice)")
        time.sleep(1)
        os.write(master_fd, b"\r")
        time.sleep(5)
        post_buf = read_all(master_fd, 10)
        post_text = clean(post_buf).lower()

    if "trust" in post_text:
        log("  pressing Enter (trust folder)")
        time.sleep(1)
        os.write(master_fd, b"\r")
        time.sleep(3)

    # Exit
    os.write(master_fd, b"\x03")
    time.sleep(2)

    # Clean up
    try: os.kill(pid, 9)
    except ProcessLookupError: pass
    try: os.close(master_fd)
    except OSError: pass
    try: os.waitpid(pid, 0)
    except ChildProcessError: pass

    # Check auth (also via sudo so we read agnostic-user's auth state)
    import subprocess
    result = subprocess.run(
        ["sudo", "-n", "-u", "agnostic-user", "-H", "claude", "auth", "status"],
        capture_output=True, text=True, timeout=5,
    )
    log(f"Auth status: {result.stdout.strip()}")

    if '"loggedIn": true' in result.stdout:
        with open(RESULT_FILE, "w") as f:
            f.write("SUCCESS")
        log("AUTH SUCCEEDED!")
    else:
        with open(RESULT_FILE, "w") as f:
            f.write("FAILED: check log")
        log("AUTH FAILED")

try:
    main()
except Exception as e:
    log(f"FATAL: {e}")
    log(traceback.format_exc())
    with open(RESULT_FILE, "w") as f:
        f.write(f"ERROR: {e}")
finally:
    log_f.close()
