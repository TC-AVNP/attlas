#!/usr/bin/env python3
"""
Helper that runs claude auth login with a PTY.
Communicates with the dashboard via files.
Heavy debug logging to /tmp/claude-login-helper.log
"""

import pty
import os
import select
import time
import sys
import fcntl
import struct
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

# Clean up old files
for f in [URL_FILE, CODE_FILE, RESULT_FILE]:
    try:
        os.remove(f)
    except FileNotFoundError:
        pass

# Create PTY
master_fd, slave_fd = pty.openpty()

# Set terminal size (some programs need this)
winsize = struct.pack("HHHH", 24, 80, 0, 0)
fcntl.ioctl(slave_fd, termios.TIOCSWINSZ, winsize)
log(f"PTY created: master_fd={master_fd}, slave_fd={slave_fd}")

env = os.environ.copy()
env["BROWSER"] = "echo"
env["TERM"] = "xterm-256color"
env["COLUMNS"] = "80"
env["LINES"] = "24"

pid = os.fork()
if pid == 0:
    # Child: run claude auth login
    os.close(master_fd)
    os.setsid()
    # Set controlling terminal
    fcntl.ioctl(slave_fd, termios.TIOCSCTTY, 0)
    os.dup2(slave_fd, 0)
    os.dup2(slave_fd, 1)
    os.dup2(slave_fd, 2)
    if slave_fd > 2:
        os.close(slave_fd)
    os.execvpe("claude", ["claude", "auth", "login"], env)

# Parent: manage the PTY
os.close(slave_fd)
log(f"Forked child PID: {pid}")

# Step 1: Read output until we find the URL
log("Reading PTY output for URL...")
output = b""
url = None
for i in range(60):
    r, _, _ = select.select([master_fd], [], [], 1)
    if r:
        try:
            data = os.read(master_fd, 4096)
            log(f"  read {len(data)} bytes: {repr(data[:200])}")
            output += data
        except OSError as e:
            log(f"  read error: {e}")
            break
        text = output.decode(errors="replace")
        for word in text.split():
            if word.startswith("https://claude.com/") or word.startswith("https://console.anthropic.com/"):
                url = word.strip()
                break
        if url:
            break
    else:
        if i % 10 == 0:
            log(f"  waiting... ({i}s)")

if not url:
    log(f"ERROR: No URL found. Full output: {repr(output[:1000])}")
    with open(RESULT_FILE, "w") as f:
        f.write("ERROR: Could not find auth URL")
    os.kill(pid, 9)
    os.close(master_fd)
    sys.exit(1)

log(f"URL found: {url[:80]}...")
with open(URL_FILE, "w") as f:
    f.write(url)

# Drain any remaining output after URL
time.sleep(2)
for _ in range(10):
    r, _, _ = select.select([master_fd], [], [], 0.5)
    if r:
        try:
            extra = os.read(master_fd, 4096)
            log(f"  drained {len(extra)} bytes: {repr(extra[:200])}")
        except OSError:
            break

# Check if process is still alive
try:
    os.kill(pid, 0)
    log("Process is still alive, waiting for code...")
except ProcessLookupError:
    log("ERROR: Process died before code could be entered!")
    with open(RESULT_FILE, "w") as f:
        f.write("ERROR: claude auth login exited prematurely")
    sys.exit(1)

# Step 2: Wait for code file
code = None
for i in range(300):
    if os.path.exists(CODE_FILE):
        with open(CODE_FILE) as f:
            code = f.read().strip()
        if code:
            os.remove(CODE_FILE)
            break
    if i % 30 == 0:
        log(f"  waiting for code file... ({i}s)")
        # Check process is alive
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            log("ERROR: Process died while waiting for code!")
            with open(RESULT_FILE, "w") as f:
                f.write("ERROR: claude auth login exited while waiting")
            sys.exit(1)
    time.sleep(1)

if not code:
    log("ERROR: Timed out waiting for code (5 min)")
    with open(RESULT_FILE, "w") as f:
        f.write("ERROR: Timed out")
    os.kill(pid, 9)
    os.close(master_fd)
    sys.exit(1)

# Step 3: Send code to PTY
log(f"CODE RECEIVED: {repr(code)}")
log(f"Code length: {len(code)}")
log(f"Writing code to PTY...")

# Write code character by character with small delays (simulate typing)
code_bytes = code.encode()
for i, byte in enumerate(code_bytes):
    os.write(master_fd, bytes([byte]))
    if i % 20 == 0:
        time.sleep(0.05)  # small delay every 20 chars

log(f"Wrote {len(code_bytes)} bytes to PTY")

# Send Enter
time.sleep(0.5)
os.write(master_fd, b"\r")
log("Sent Enter (\\r)")

# Also try \n
time.sleep(0.5)
os.write(master_fd, b"\n")
log("Sent newline (\\n)")

# Step 4: Read response
log("Reading response...")
time.sleep(3)
response = b""
for i in range(60):
    r, _, _ = select.select([master_fd], [], [], 1)
    if r:
        try:
            data = os.read(master_fd, 4096)
            log(f"  response read {len(data)} bytes: {repr(data[:300])}")
            response += data
        except OSError as e:
            log(f"  response read error: {e}")
            break
    else:
        if response:
            log(f"  no more data after {len(response)} total bytes")
            break
        if i % 10 == 0:
            log(f"  waiting for response... ({i}s)")
    # Check if process exited
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        log(f"  process exited")
        break

log(f"FULL RESPONSE ({len(response)} bytes): {repr(response)}")
log(f"RESPONSE decoded: {response.decode(errors='replace')}")

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
        f.write(f"FAILED: {response.decode(errors='replace')[:200]}")
    log("AUTH FAILED")

log_f.close()
