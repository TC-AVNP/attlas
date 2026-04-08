#!/usr/bin/env python3
"""
Alive server — API backend for the VM dashboard.
Serves pre-built React frontend, handles auth via session cookie,
and provides API endpoints for service management and Claude login.
Runs on localhost:3000 behind Caddy.
"""

import http.server
import json
import subprocess
import os
import mimetypes
import hashlib
import hmac
import time
import http.cookies

PORT = 3000
DIST_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "frontend", "dist")

# Auth config — username/password, session secret
AUTH_USER = "Testuser"
AUTH_PASS = "password123"
SESSION_SECRET = "attlas-session-secret-change-me"
COOKIE_NAME = "attlas_session"
COOKIE_MAX_AGE = 86400 * 7  # 7 days

KNOWN_SERVICES = [
    {"id": "terminal", "name": "Cloud Terminal", "service": "ttyd", "command": "ttyd",
     "path": "/terminal/", "script": "install-terminal.sh"},
    {"id": "code-server", "name": "Cloud VS Code", "service": "code-server", "command": "code-server",
     "path": "/code/", "script": "install-code-server.sh"},
    {"id": "openclaw", "name": "OpenClaw", "service": "openclaw-gateway", "command": "openclaw",
     "path": "/openclaw", "script": "install-openclaw.sh", "check_process": "openclaw-gateway"},
]


def make_session_token(username):
    """Create a signed session token."""
    payload = f"{username}:{int(time.time())}"
    sig = hmac.new(SESSION_SECRET.encode(), payload.encode(), hashlib.sha256).hexdigest()[:32]
    return f"{payload}:{sig}"


def verify_session_token(token):
    """Verify a session token is valid."""
    try:
        parts = token.rsplit(":", 1)
        if len(parts) != 2:
            return False
        payload, sig = parts
        expected = hmac.new(SESSION_SECRET.encode(), payload.encode(), hashlib.sha256).hexdigest()[:32]
        if not hmac.compare_digest(sig, expected):
            return False
        # Check not expired
        username, ts = payload.split(":", 1)
        if time.time() - int(ts) > COOKIE_MAX_AGE:
            return False
        return True
    except Exception:
        return False


def get_cookie(handler, name):
    """Extract a cookie value from the request."""
    cookie_header = handler.headers.get("Cookie", "")
    cookies = http.cookies.SimpleCookie()
    try:
        cookies.load(cookie_header)
        if name in cookies:
            return cookies[name].value
    except Exception:
        pass
    return None


def is_authenticated(handler):
    """Check if the request has a valid session cookie."""
    token = get_cookie(handler, COOKIE_NAME)
    return token and verify_session_token(token)


LOGIN_PAGE_TEMPLATE = """<!DOCTYPE html>
<html>
<head>
    <title>Attlas Login</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body {{ font-family: -apple-system, sans-serif; background: #1a1a2e; color: #eee;
               display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; }}
        .box {{ max-width: 350px; width: 100%; padding: 2rem; }}
        h1 {{ color: #68d391; font-size: 1.5rem; margin-bottom: 1.5rem; }}
        label {{ display: block; margin-bottom: 0.3rem; color: #888; font-size: 0.85rem; }}
        input {{ width: 100%; padding: 0.6rem; margin-bottom: 1rem; font-size: 1rem;
                background: #2d2d44; color: #eee; border: 1px solid #555; border-radius: 4px;
                box-sizing: border-box; }}
        button {{ width: 100%; padding: 0.7rem; font-size: 1rem; cursor: pointer;
                 background: #5a67d8; color: white; border: none; border-radius: 4px; }}
        button:hover {{ background: #4c51bf; }}
        .error {{ color: #fc8181; margin-bottom: 1rem; font-size: 0.9rem; }}
    </style>
</head>
<body>
    <div class="box">
        <h1>Attlas VM</h1>
        {error}
        <form method="POST" action="/login">
            <label>Username</label>
            <input type="text" name="username" autofocus>
            <label>Password</label>
            <input type="password" name="password">
            <button type="submit">Sign in</button>
        </form>
    </div>
</body>
</html>"""


def run_cmd(cmd, timeout=5):
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
        return r.stdout.strip(), r.returncode == 0
    except Exception:
        return "", False


def get_vm_info():
    def meta(path):
        out, ok = run_cmd([
            "curl", "-sf", "-H", "Metadata-Flavor: Google",
            f"http://metadata.google.internal/computeMetadata/v1/{path}"
        ], timeout=3)
        return out if ok else "unknown"

    ip = meta("instance/network-interfaces/0/access-configs/0/external-ip")
    zone_raw = meta("instance/zone")
    zone = zone_raw.split("/")[-1] if "/" in zone_raw else zone_raw
    name = meta("instance/name")
    return {
        "name": name,
        "zone": zone,
        "external_ip": ip,
        "domain": f"{ip}.sslip.io" if ip != "unknown" else "unknown",
    }


def is_claude_installed():
    _, ok = run_cmd(["which", "claude"])
    return ok


def is_claude_logged_in():
    try:
        home = os.path.expanduser("~")
        claude_json = os.path.join(home, ".claude.json")
        if os.path.exists(claude_json):
            with open(claude_json) as f:
                data = json.load(f)
                if data.get("oauthAccount") or data.get("apiKey"):
                    return True
        return False
    except Exception:
        return False


def get_services_status():
    results = []
    for svc in KNOWN_SERVICES:
        _, installed = run_cmd(["which", svc["command"]])
        running = False
        if installed:
            if svc.get("check_process"):
                _, running = run_cmd(["pgrep", "-f", svc["check_process"]])
            else:
                out, _ = run_cmd(["systemctl", "is-active", svc["service"]])
                running = out == "active"
        results.append({**svc, "installed": installed, "running": running})
    return results


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        # Auth verification endpoint for Caddy forward_auth
        if self.path == "/api/auth/verify":
            if is_authenticated(self):
                self.send_response(200)
                self.end_headers()
            else:
                # Return 302 so Caddy copies the redirect to the client
                self.send_response(302)
                self.send_header("Location", "/login")
                self.end_headers()
            return

        # Login page — always accessible
        if self.path == "/login":
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(LOGIN_PAGE_TEMPLATE.format(error="").encode())
            return

        # Logout
        if self.path == "/logout":
            self.send_response(302)
            self.send_header("Set-Cookie", f"{COOKIE_NAME}=; Path=/; Max-Age=0")
            self.send_header("Location", "/login")
            self.end_headers()
            return

        # API endpoints
        if self.path == "/api/status":
            self.send_json({
                "vm": get_vm_info(),
                "claude": {
                    "installed": is_claude_installed(),
                    "authenticated": is_claude_logged_in(),
                },
                "services": get_services_status(),
            })
            return

        # Serve static files from dist/
        path = self.path.split("?")[0]
        if path == "/":
            path = "/index.html"

        file_path = os.path.join(DIST_DIR, path.lstrip("/"))
        if os.path.isfile(file_path):
            content_type, _ = mimetypes.guess_type(file_path)
            self.send_response(200)
            self.send_header("Content-Type", content_type or "application/octet-stream")
            self.end_headers()
            with open(file_path, "rb") as f:
                self.wfile.write(f.read())
        else:
            index_path = os.path.join(DIST_DIR, "index.html")
            if os.path.isfile(index_path):
                self.send_response(200)
                self.send_header("Content-Type", "text/html")
                self.end_headers()
                with open(index_path, "rb") as f:
                    self.wfile.write(f.read())
            else:
                self.send_response(404)
                self.end_headers()

    def do_POST(self):
        # Login form submission
        if self.path == "/login":
            self.handle_login()
            return

        if self.path == "/api/claude-login":
            self.handle_claude_login()
        elif self.path == "/api/claude-login/code":
            self.handle_claude_code()
        elif self.path == "/api/install-service":
            self.handle_install_service()
        elif self.path == "/api/uninstall-service":
            self.handle_uninstall_service()
        else:
            self.send_response(404)
            self.end_headers()

    def handle_login(self):
        """Process login form submission."""
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length).decode()
        # Parse form data
        params = {}
        for pair in body.split("&"):
            if "=" in pair:
                k, v = pair.split("=", 1)
                from urllib.parse import unquote_plus
                params[k] = unquote_plus(v)

        username = params.get("username", "")
        password = params.get("password", "")

        if username == AUTH_USER and password == AUTH_PASS:
            token = make_session_token(username)
            self.send_response(302)
            self.send_header("Set-Cookie",
                             f"{COOKIE_NAME}={token}; Path=/; Max-Age={COOKIE_MAX_AGE}; HttpOnly; SameSite=Lax; Secure")
            self.send_header("Location", "/")
            self.end_headers()
        else:
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(LOGIN_PAGE_TEMPLATE.format(
                error='<div class="error">Invalid username or password</div>'
            ).encode())

    def handle_claude_login(self):
        if is_claude_logged_in():
            self.send_json({"error": "Already logged in"})
            return

        subprocess.run(["pkill", "-f", "claude-login-helper"], capture_output=True)
        time.sleep(1)

        for f in ["/tmp/claude-login-url", "/tmp/claude-login-code", "/tmp/claude-login-result"]:
            try:
                os.remove(f)
            except FileNotFoundError:
                pass

        helper = os.path.join(os.path.dirname(os.path.abspath(__file__)), "claude-login-helper.py")
        subprocess.Popen(
            ["python3", helper],
            stdout=open("/tmp/claude-login-helper.log", "w"),
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )

        url = None
        for _ in range(60):
            if os.path.exists("/tmp/claude-login-url"):
                with open("/tmp/claude-login-url") as f:
                    url = f.read().strip()
                if url:
                    break
            time.sleep(1)

        if url:
            self.send_json({"url": url})
        else:
            log = ""
            try:
                with open("/tmp/claude-login-helper.log") as f:
                    log = f.read()[:500]
            except Exception:
                pass
            self.send_json({"error": f"Timed out waiting for URL. Log: {log}"})

    def handle_claude_code(self):
        data = self.read_json()
        code = data.get("code", "")
        if not code:
            self.send_json({"error": "No code provided."})
            return

        with open("/tmp/claude-login-code", "w") as f:
            f.write(code)

        result = None
        for _ in range(30):
            if os.path.exists("/tmp/claude-login-result"):
                with open("/tmp/claude-login-result") as f:
                    result = f.read().strip()
                if result:
                    break
            time.sleep(1)

        if result and result == "SUCCESS":
            self.send_json({"success": True})
        elif result:
            self.send_json({"error": result})
        else:
            self.send_json({"error": "Timed out waiting for login result."})

    def handle_install_service(self):
        data = self.read_json()
        svc_id = data.get("id", "")
        svc = next((s for s in KNOWN_SERVICES if s["id"] == svc_id), None)
        if not svc:
            self.send_json({"error": f"Unknown service: {svc_id}"})
            return

        attlas_dir = os.path.expanduser("~/attlas")
        script = os.path.join(attlas_dir, "services", svc["script"])
        if not os.path.exists(script):
            self.send_json({"error": f"Script not found: {script}"})
            return

        try:
            result = subprocess.run(
                ["bash", script],
                capture_output=True, text=True, timeout=300,
                cwd=os.path.join(attlas_dir, "services"),
            )
            if result.returncode == 0:
                subprocess.run(["sudo", "systemctl", "reload", "caddy"],
                               capture_output=True, timeout=10)
                self.send_json({"success": True})
            else:
                self.send_json({"error": result.stderr or result.stdout})
        except subprocess.TimeoutExpired:
            self.send_json({"error": "Install timed out (5min)"})
        except Exception as e:
            self.send_json({"error": str(e)})

    def handle_uninstall_service(self):
        data = self.read_json()
        svc_id = data.get("id", "")
        svc = next((s for s in KNOWN_SERVICES if s["id"] == svc_id), None)
        if not svc:
            self.send_json({"error": f"Unknown service: {svc_id}"})
            return

        attlas_dir = os.path.expanduser("~/attlas")
        script = os.path.join(attlas_dir, "services", f"uninstall-{svc_id}.sh")
        if not os.path.exists(script):
            self.send_json({"error": f"Uninstall script not found: {script}"})
            return

        try:
            result = subprocess.run(
                ["bash", script],
                capture_output=True, text=True, timeout=60,
                cwd=os.path.join(attlas_dir, "services"),
            )
            if result.returncode == 0:
                subprocess.run(["sudo", "systemctl", "reload", "caddy"],
                               capture_output=True, timeout=10)
                self.send_json({"success": True})
            else:
                self.send_json({"error": result.stderr or result.stdout})
        except Exception as e:
            self.send_json({"error": str(e)})

    def read_json(self):
        length = int(self.headers.get("Content-Length", 0))
        return json.loads(self.rfile.read(length).decode()) if length else {}

    def send_json(self, data):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(data).encode())

    def log_message(self, format, *args):
        pass


if __name__ == "__main__":
    server = http.server.HTTPServer(("127.0.0.1", PORT), Handler)
    print(f"Alive server running on http://127.0.0.1:{PORT}")
    print(f"Serving frontend from {DIST_DIR}")
    server.serve_forever()
