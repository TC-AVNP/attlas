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

URL_FILE = "/tmp/claude-login-url"
CODE_FILE = "/tmp/claude-login-code"
RESULT_FILE = "/tmp/claude-login-result"
LOG_FILE = "/tmp/claude-login-helper.log"

log_f = open(LOG_FILE, "w")

def log(msg):
    ts = time.strftime("%H:%M:%S")
    line = f"[{ts}] {msg}"
    print(line, flush=True)
    log_f.write(line + "\n")
    log_f.flush()

def read_all(master_fd, timeout=5):
    """Read everything available from PTY within timeout."""
    buf = b""
    deadline = time.time() + timeout
    while time.time() < deadline:
        r, _, _ = select.select([master_fd], [], [], 0.3)
        if r:
            try:
                buf += os.read(master_fd, 8192)
            except OSError:
                break
        elif buf:
            break
    return buf

def clean(data):
    """Strip ANSI escape codes for keyword matching."""
    return re.sub(rb"\x1b\[[0-9;?]*[a-zA-Z]|\x1b[()][AB012]|\x1b[=>]|\x1b\][^\x07]*\x07", b"", data).decode(errors="replace")

def wait_for(master_fd, keyword, timeout=30, press_enter_on=None):
    """Keep reading PTY until keyword appears. Optionally press Enter when press_enter_on is found."""
    buf = b""
    entered = set()
    deadline = time.time() + timeout
    while time.time() < deadline:
        data = read_all(master_fd, 2)
        if data:
            buf += data
            text = clean(buf)
            log(f"  [{len(buf)} bytes] looking for '{keyword}'")

            # Press Enter on intermediate screens
            if press_enter_on:
                for trigger in press_enter_on:
                    if trigger in text.lower() and trigger not in entered:
                        log(f"  found '{trigger}' — pressing Enter")
                        time.sleep(1)
                        os.write(master_fd, b"\r")
                        entered.add(trigger)
                        time.sleep(3)
                        buf += read_all(master_fd, 5)

            if keyword in clean(buf).lower():
                return buf, True
        else:
            time.sleep(1)
    return buf, False

# Clean up old files
for f in [URL_FILE, CODE_FILE, RESULT_FILE]:
    try:
        os.remove(f)
    except FileNotFoundError:
        pass

# Create PTY
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
    if slave_fd > 2:
        os.close(slave_fd)
    os.execvpe("claude", ["claude", "login"], env)

os.close(slave_fd)
log(f"Started claude login (PID {pid})")

# Navigate through the wizard to the URL screen.
# We press Enter on theme picker and login method, then look for the URL.
# The keyword "paste code" appears on the URL screen.
log("Navigating wizard — looking for 'paste code' screen...")
log("  will press Enter on: theme, login method")

buf, found = wait_for(
    master_fd,
    "paste code",
    timeout=60,
    press_enter_on=["theme", "text style", "login method", "select login", "subscription"]
)

if not found:
    text = clean(buf)
    log(f"ERROR: Never reached 'paste code' screen. Last output: {text[-500:]}")
    with open(RESULT_FILE, "w") as f:
        f.write("ERROR: Could not reach code input screen")
    os.kill(pid, 9)
    os.close(master_fd)
    sys.exit(1)

# Extract URL
text = clean(buf)
url = None
for word in buf.decode(errors="replace").split():
    w = re.sub(r"\x1b\[[0-9;?]*[a-zA-Z]", "", word)
    if w.startswith("https://claude.com/") or w.startswith("https://console.anthropic.com/"):
        url = w.strip()
        break

if not url:
    # Try from cleaned text
    for word in text.split():
        if word.startswith("https://claude.com/") or word.startswith("https://console.anthropic.com/"):
            url = word.strip()
            break

if not url:
    log(f"ERROR: Found 'paste code' but no URL. Text: {text[-500:]}")
    with open(RESULT_FILE, "w") as f:
        f.write("ERROR: No URL found on code screen")
    os.kill(pid, 9)
    os.close(master_fd)
    sys.exit(1)

log(f"URL: {url[:80]}...")
with open(URL_FILE, "w") as f:
    f.write(url)

# Wait for code from dashboard
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
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            log("ERROR: Process died")
            with open(RESULT_FILE, "w") as f:
                f.write("ERROR: claude login exited")
            sys.exit(1)
    time.sleep(1)

if not code:
    log("ERROR: Timed out waiting for code")
    with open(RESULT_FILE, "w") as f:
        f.write("ERROR: Timed out")
    os.kill(pid, 9)
    os.close(master_fd)
    sys.exit(1)

# Type code and press Enter
log(f"Typing code ({len(code)} chars)...")
for ch in code:
    os.write(master_fd, ch.encode())
    time.sleep(0.01)
time.sleep(0.5)
os.write(master_fd, b"\r")
log("Code submitted")

# Handle remaining prompts: security notice + trust folder
log("Handling post-login prompts...")
buf2, _ = wait_for(
    master_fd,
    "trust",
    timeout=30,
    press_enter_on=["press enter", "continue"]
)

# Press Enter on trust prompt
text2 = clean(buf2)
if "trust" in text2.lower():
    log("Trust prompt found, pressing Enter")
    os.write(master_fd, b"\r")
    time.sleep(3)

# Exit
log("Sending Ctrl-C to exit TUI...")
os.write(master_fd, b"\x03")
time.sleep(2)

# Clean up
try:
    os.kill(pid, 9)
except ProcessLookupError:
    pass
try:
    os.close(master_fd)
except OSError:
    pass
try:
    os.waitpid(pid, 0)
except ChildProcessError:
    pass

# Check result
import subprocess
result = subprocess.run(["claude", "auth", "status"], capture_output=True, text=True, timeout=5)
log(f"Auth status: {result.stdout.strip()}")

if '"loggedIn": true' in result.stdout:
    with open(RESULT_FILE, "w") as f:
        f.write("SUCCESS")
    log("AUTH SUCCEEDED!")
else:
    with open(RESULT_FILE, "w") as f:
        f.write("FAILED: check /tmp/claude-login-helper.log")
    log("AUTH FAILED")

log_f.close()
