#!/usr/bin/env python3
"""
Helper that runs claude auth login with a PTY.
Communicates with the dashboard via files:
  /tmp/claude-login-url    — URL written here after capture
  /tmp/claude-login-code   — dashboard writes the code here
  /tmp/claude-login-result — result written here after code submission
"""

import pty
import os
import select
import time
import sys

URL_FILE = "/tmp/claude-login-url"
CODE_FILE = "/tmp/claude-login-code"
RESULT_FILE = "/tmp/claude-login-result"

# Clean up old files
for f in [URL_FILE, CODE_FILE, RESULT_FILE]:
    try:
        os.remove(f)
    except FileNotFoundError:
        pass

# Create PTY
master_fd, slave_fd = pty.openpty()

env = os.environ.copy()
env["BROWSER"] = "echo"
env["TERM"] = "dumb"

pid = os.fork()
if pid == 0:
    # Child: run claude auth login
    os.close(master_fd)
    os.setsid()
    os.dup2(slave_fd, 0)
    os.dup2(slave_fd, 1)
    os.dup2(slave_fd, 2)
    os.close(slave_fd)
    os.execvpe("claude", ["claude", "auth", "login"], env)

# Parent: manage the PTY
os.close(slave_fd)

# Step 1: Read output until we find the URL
print("Waiting for auth URL...", flush=True)
output = b""
url = None
for _ in range(60):  # wait up to 60 seconds
    r, _, _ = select.select([master_fd], [], [], 1)
    if r:
        try:
            data = os.read(master_fd, 4096)
            output += data
        except OSError:
            break
        # Search for URL
        text = output.decode(errors="replace")
        for word in text.split():
            if word.startswith("https://claude.com/") or word.startswith("https://console.anthropic.com/"):
                url = word.strip()
                break
        if url:
            break

if not url:
    print(f"ERROR: Could not find URL in output: {output.decode(errors='replace')[:500]}", flush=True)
    with open(RESULT_FILE, "w") as f:
        f.write("ERROR: Could not find auth URL")
    os.kill(pid, 9)
    os.close(master_fd)
    sys.exit(1)

print(f"URL: {url}", flush=True)
with open(URL_FILE, "w") as f:
    f.write(url)

# Step 2: Wait for code file to appear (up to 5 minutes)
print("Waiting for code...", flush=True)
code = None
for _ in range(300):
    if os.path.exists(CODE_FILE):
        with open(CODE_FILE) as f:
            code = f.read().strip()
        if code:
            os.remove(CODE_FILE)
            break
    time.sleep(1)

if not code:
    print("ERROR: Timed out waiting for code", flush=True)
    with open(RESULT_FILE, "w") as f:
        f.write("ERROR: Timed out waiting for code")
    os.kill(pid, 9)
    os.close(master_fd)
    sys.exit(1)

# Step 3: Send code to the PTY
print(f"Sending code ({len(code)} chars)...", flush=True)
os.write(master_fd, (code + "\n").encode())

# Step 4: Wait for response
time.sleep(8)
response = b""
for _ in range(30):
    r, _, _ = select.select([master_fd], [], [], 1)
    if r:
        try:
            response += os.read(master_fd, 4096)
        except OSError:
            break
    else:
        if response:
            break

print(f"Response: {response.decode(errors='replace')[:500]}", flush=True)

# Clean up
try:
    os.kill(pid, 9)
except ProcessLookupError:
    pass
os.close(master_fd)
try:
    os.waitpid(pid, 0)
except ChildProcessError:
    pass

# Check if auth succeeded
import subprocess
result = subprocess.run(["claude", "auth", "status"], capture_output=True, text=True, timeout=5)
if '"loggedIn": true' in result.stdout:
    with open(RESULT_FILE, "w") as f:
        f.write("SUCCESS")
    print("Auth succeeded!", flush=True)
else:
    with open(RESULT_FILE, "w") as f:
        f.write(f"FAILED: {response.decode(errors='replace')[:200]}")
    print(f"Auth may have failed. Status: {result.stdout}", flush=True)
