#!/usr/bin/env python3
"""
Helper that runs 'claude login' with a PTY and navigates the setup wizard.
Steps: theme picker → login method → URL + code → security notice → trust folder
Communicates with the dashboard via files in /tmp/.
"""

import pty
import os
import select
import time
import sys
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

def read_pty(master_fd, timeout_secs=10):
    """Read all available PTY output."""
    buf = b""
    deadline = time.time() + timeout_secs
    while time.time() < deadline:
        r, _, _ = select.select([master_fd], [], [], 0.5)
        if r:
            try:
                buf += os.read(master_fd, 4096)
            except OSError:
                break
        else:
            if buf:
                break
    return buf

def send_and_read(master_fd, keys, label, wait_before=1, wait_after=3, read_timeout=10):
    """Send keys to PTY, wait, read response."""
    time.sleep(wait_before)
    log(f"Sending: {repr(keys)} ({label})")
    os.write(master_fd, keys)
    time.sleep(wait_after)
    resp = read_pty(master_fd, read_timeout)
    log(f"Response ({label}): {len(resp)} bytes")
    if resp:
        # Strip most ANSI for readability in log
        import re
        clean = re.sub(rb"\x1b\[[0-9;]*[a-zA-Z]|\x1b\[\?[0-9;]*[a-zA-Z]", b"", resp)
        log(f"  cleaned: {clean.decode(errors='replace')[:300]}")
    return resp

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
log(f"PTY created")

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

# Read initial output (welcome screen + theme picker)
log("Waiting for initial output...")
time.sleep(5)
initial = read_pty(master_fd, 10)
log(f"Initial output: {len(initial)} bytes")

import re
clean_initial = re.sub(rb"\x1b\[[0-9;]*[a-zA-Z]|\x1b\[\?[0-9;]*[a-zA-Z]", b"", initial)
initial_text = clean_initial.decode(errors="replace")
log(f"Initial text: {initial_text[:500]}")

# Step 1: Theme picker — just accept whatever is selected
if "theme" in initial_text.lower() or "text style" in initial_text.lower():
    log("Theme picker detected — pressing Enter to accept default")
    os.write(master_fd, b"\r")
    log("  Enter sent, waiting 8s for next screen...")
    time.sleep(8)
    # Drain everything from the transition
    while True:
        buf = read_pty(master_fd, 3)
        if not buf:
            break
        log(f"  drained {len(buf)} bytes")
    log("  transition complete")
else:
    log("No theme picker detected, continuing...")

# Step 2: Login method — wait for it, then press Enter
log("Waiting for login method screen...")
time.sleep(3)
resp = read_pty(master_fd, 15)
resp_text = re.sub(rb"\x1b\[[0-9;]*[a-zA-Z]|\x1b\[\?[0-9;]*[a-zA-Z]", b"", resp).decode(errors="replace")
log(f"  screen text: {resp_text[:200]}")

if "login method" in resp_text.lower() or "select login" in resp_text.lower() or "Claude account" in resp_text:
    log("Login method picker detected — pressing Enter for option 1")
    os.write(master_fd, b"\r")
    log("  Enter sent, waiting 8s for URL screen...")
    time.sleep(8)
    while True:
        buf = read_pty(master_fd, 3)
        if not buf:
            break
        log(f"  drained {len(buf)} bytes")
else:
    log(f"No login method detected — maybe already past it")

# Step 3: URL screen — read the URL
log("Looking for auth URL...")
time.sleep(3)
all_output = read_pty(master_fd, 15)
combined = resp + all_output
combined_text = combined.decode(errors="replace")

url = None
for word in combined_text.split():
    if word.startswith("https://claude.com/") or word.startswith("https://console.anthropic.com/"):
        url = word.strip()
        break

if not url:
    log(f"ERROR: No URL found. Output: {combined_text[:1000]}")
    with open(RESULT_FILE, "w") as f:
        f.write("ERROR: Could not find auth URL after wizard steps")
    os.kill(pid, 9)
    os.close(master_fd)
    sys.exit(1)

log(f"URL found: {url[:80]}...")
with open(URL_FILE, "w") as f:
    f.write(url)

# Step 4: Wait for code from dashboard
log("Waiting for code file...")
code = None
for i in range(300):  # 5 minutes
    if os.path.exists(CODE_FILE):
        with open(CODE_FILE) as f:
            code = f.read().strip()
        if code:
            os.remove(CODE_FILE)
            break
    if i % 30 == 0:
        log(f"  waiting for code... ({i}s)")
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            log("ERROR: Process died while waiting")
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

# Step 5: Type the code character by character
log(f"Typing code ({len(code)} chars)...")
for ch in code:
    os.write(master_fd, ch.encode())
    time.sleep(0.01)
log("Code typed, pressing Enter...")
time.sleep(0.5)
os.write(master_fd, b"\r")

# Step 6: Security notice — "Press Enter to continue"
resp = read_pty(master_fd, 15)
log(f"After code Enter: {len(resp)} bytes")
resp_text = re.sub(rb"\x1b\[[0-9;]*[a-zA-Z]|\x1b\[\?[0-9;]*[a-zA-Z]", b"", resp).decode(errors="replace")
log(f"  text: {resp_text[:300]}")

if "enter" in resp_text.lower() or "continue" in resp_text.lower() or "security" in resp_text.lower():
    log("Security notice detected, pressing Enter...")
    send_and_read(master_fd, b"\r", "Enter for security", wait_before=2, wait_after=3)

# Step 7: Trust folder — "Yes, I trust this folder"
time.sleep(2)
trust_resp = read_pty(master_fd, 10)
trust_text = re.sub(rb"\x1b\[[0-9;]*[a-zA-Z]|\x1b\[\?[0-9;]*[a-zA-Z]", b"", trust_resp).decode(errors="replace")
log(f"Trust screen: {trust_text[:300]}")

if "trust" in trust_text.lower() or "folder" in trust_text.lower():
    log("Trust prompt detected, pressing Enter (option 1)...")
    send_and_read(master_fd, b"\r", "Enter for trust", wait_before=1, wait_after=3)

# Exit the TUI
log("Sending Ctrl-C to exit...")
os.write(master_fd, b"\x03")
time.sleep(2)

# Clean up
try:
    os.kill(pid, 9)
    log("Killed child process")
except ProcessLookupError:
    log("Child already exited")
try:
    os.close(master_fd)
except OSError:
    pass
try:
    os.waitpid(pid, 0)
except ChildProcessError:
    pass

# Check auth status
import subprocess
result = subprocess.run(["claude", "auth", "status"], capture_output=True, text=True, timeout=5)
log(f"Auth status: {result.stdout.strip()}")

if '"loggedIn": true' in result.stdout:
    with open(RESULT_FILE, "w") as f:
        f.write("SUCCESS")
    log("AUTH SUCCEEDED!")
else:
    with open(RESULT_FILE, "w") as f:
        f.write(f"FAILED: check /tmp/claude-login-helper.log")
    log("AUTH FAILED")

log_f.close()
