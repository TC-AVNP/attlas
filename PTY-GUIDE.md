# PTY (Pseudo-Terminal) Guide

How to programmatically interact with interactive CLI programs that require a terminal (TTY). This is the approach used by the Claude Code login helper in `base-setup/alive-server/claude-login-helper.py`.

## Why PTY?

Many CLI tools (like `claude login`) use interactive terminal features:
- Raw mode input (reading keypresses, not lines)
- ANSI escape codes for colors and cursor positioning
- Terminal size detection
- TUI frameworks (Ink, curses) that require a real terminal

Using `subprocess.PIPE` for stdin/stdout does NOT work for these programs because pipes are not terminals. The program detects it's not attached to a TTY and either fails or behaves differently.

A PTY (pseudo-terminal) creates a virtual terminal that looks like a real one to the program.

## Basic Setup

```python
import pty
import os
import select
import struct
import fcntl
import termios

# 1. Create a PTY pair
master_fd, slave_fd = pty.openpty()

# 2. Set terminal size (rows, cols, xpixel, ypixel)
winsize = struct.pack("HHHH", 30, 120, 0, 0)
fcntl.ioctl(slave_fd, termios.TIOCSWINSZ, winsize)

# 3. Fork — child becomes the terminal program
pid = os.fork()
if pid == 0:
    # CHILD PROCESS
    os.close(master_fd)
    os.setsid()                                    # new session
    fcntl.ioctl(slave_fd, termios.TIOCSCTTY, 0)   # set controlling terminal
    os.dup2(slave_fd, 0)                           # stdin  = slave
    os.dup2(slave_fd, 1)                           # stdout = slave
    os.dup2(slave_fd, 2)                           # stderr = slave
    if slave_fd > 2:
        os.close(slave_fd)
    os.execvpe("my-command", ["my-command", "arg1"], os.environ)

# PARENT PROCESS — controls the terminal
os.close(slave_fd)  # parent only uses master_fd
```

## Reading from the Terminal

Reading `master_fd` gives you everything the program writes to stdout/stderr — including ANSI escape codes, cursor movement, colors, etc.

```python
def read_all(master_fd, timeout=5):
    """Read all available output from the PTY."""
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
            # Had data but nothing new — done
            break
    return buf

# Usage
output = read_all(master_fd, timeout=10)
print(output.decode(errors="replace"))
```

### Stripping ANSI Escape Codes

The raw output contains escape codes for colors, cursor positioning, etc. To get readable text:

```python
import re

def clean(data):
    """Strip ANSI escape codes for keyword matching."""
    return re.sub(
        rb"\x1b\[[0-9;?]*[a-zA-Z]"   # CSI sequences (colors, cursor)
        rb"|\x1b[()][AB012]"           # character set selection
        rb"|\x1b[=>]"                  # keypad modes
        rb"|\x1b\][^\x07]*\x07",      # OSC sequences (title, etc)
        b"",
        data
    ).decode(errors="replace")

text = clean(output)
```

**Warning**: After stripping ANSI codes, spaces and newlines may also be gone (the program uses cursor positioning instead of actual spaces). Text like `Hello World` might become `HelloWorld`.

### Handling Line-Wrapped Output

Programs wrap long text at the terminal width (set by TIOCSWINSZ). A URL that's 450 characters will span multiple lines. When you strip newlines to reconstruct it, you may concatenate trailing text. Use known delimiters to cut:

```python
# Remove newlines to join wrapped content
text = stripped.replace("\r", "").replace("\n", "")

# Find URL and cut at known trailing text
url_match = re.search(r"(https://example\.com/\S+)", text)
if url_match:
    url = url_match.group(1)
    # Cut off any trailing text that got concatenated
    for cut_word in ["Paste", "Press", "Continue"]:
        idx = url.find(cut_word)
        if idx > 0:
            url = url[:idx]
```

## Writing to the Terminal

Writing to `master_fd` simulates keyboard input. The program receives it exactly as if someone typed it.

```python
# Type a string
os.write(master_fd, b"hello world")

# Press Enter (carriage return)
os.write(master_fd, b"\r")

# Press arrow keys
os.write(master_fd, b"\x1b[A")  # Up
os.write(master_fd, b"\x1b[B")  # Down
os.write(master_fd, b"\x1b[C")  # Right
os.write(master_fd, b"\x1b[D")  # Left

# Press Ctrl+C
os.write(master_fd, b"\x03")

# Press Escape
os.write(master_fd, b"\x1b")

# Press Tab
os.write(master_fd, b"\t")
```

### Typing Long Strings (Character by Character)

Some TUI programs process input character by character. For reliability, add small delays:

```python
code = "my-long-auth-code-here"
for ch in code:
    os.write(master_fd, ch.encode())
    time.sleep(0.01)  # 10ms between chars
time.sleep(0.5)
os.write(master_fd, b"\r")  # Enter
```

### Waiting Between Screens

Interactive programs with multiple screens (wizards) need time to transition. Always wait after pressing Enter before reading the next screen:

```python
os.write(master_fd, b"\r")       # press Enter
time.sleep(3)                     # wait for transition
output = read_all(master_fd, 5)  # read next screen
```

## Pattern: Navigate a Wizard

Read continuously, press Enter when recognizing known screens:

```python
buf = b""
entered = set()
triggers = ["theme", "login method", "subscription"]
target = "paste code"

deadline = time.time() + 90
while time.time() < deadline:
    data = read_all(master_fd, 3)
    if data:
        buf += data

    text = clean(buf).lower()

    # Reached the target screen?
    if target in text:
        break

    # Press Enter on intermediate screens
    for trigger in triggers:
        if trigger in text and trigger not in entered:
            time.sleep(2)
            os.write(master_fd, b"\r")
            entered.add(trigger)
            time.sleep(3)
            buf += read_all(master_fd, 5)  # append new output
            break

    if not data:
        time.sleep(2)
```

## Cleanup

Always clean up the child process and file descriptors:

```python
try:
    os.kill(pid, 9)           # kill child
except ProcessLookupError:
    pass                       # already exited
try:
    os.close(master_fd)       # close PTY
except OSError:
    pass
try:
    os.waitpid(pid, 0)       # reap zombie
except ChildProcessError:
    pass
```

## Gotchas

1. **No echo doesn't mean no input**: Some programs (Ink/React TUIs) disable echo. Your keystrokes arrive but nothing is echoed back. Use `strace` to verify: `strace -p PID -e read` shows `read(22, "X", 65536) = 1`.

2. **Node.js opens extra fds**: Programs like Claude Code open the TTY on additional file descriptors (e.g., fd 20, 22, 23) beyond the standard 0/1/2. The PTY still works — all fds point to the same slave device.

3. **`os.fork()` in HTTP handlers is risky**: The child inherits all parent file descriptors (sockets, etc.). Use a separate helper process instead of forking inside a web server request handler.

4. **Terminal width affects output**: Set TIOCSWINSZ wide enough (120+ columns) to minimize line wrapping, especially for long URLs.

5. **`subprocess.Popen` with `stdin=PIPE` is NOT a PTY**: Programs that check `isatty()` will detect the pipe and refuse to run interactively. Always use `pty.openpty()` + `os.fork()` for interactive programs.

## Reference Implementation

See `base-setup/alive-server/claude-login-helper.py` for a complete working example that navigates Claude Code's multi-step login wizard via PTY.
