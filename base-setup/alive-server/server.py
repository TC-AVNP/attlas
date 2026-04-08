#!/usr/bin/env python3
"""
Alive server — API backend for the VM dashboard.
Serves pre-built React frontend from ./frontend/dist/ and provides API endpoints.
Runs on localhost:3000 behind Caddy.
"""

import http.server
import json
import subprocess
import os
import mimetypes

PORT = 3000
DIST_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "frontend", "dist")
LOGIN_STATE = {"process": None, "url": None}

KNOWN_SERVICES = [
    {"id": "terminal", "name": "Cloud Terminal", "service": "ttyd", "command": "ttyd",
     "path": "/terminal", "script": "install-terminal.sh"},
    {"id": "code-server", "name": "Cloud VS Code", "service": "code-server", "command": "code-server",
     "path": "/code", "script": "install-code-server.sh"},
    {"id": "openclaw", "name": "OpenClaw", "service": "openclaw", "command": "openclaw",
     "path": None, "script": "install-openclaw.sh"},
]


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
            out, _ = run_cmd(["systemctl", "is-active", svc["service"]])
            running = out == "active"
        results.append({**svc, "installed": installed, "running": running})
    return results


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
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
            # SPA fallback — serve index.html for client-side routing
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
        if self.path == "/api/claude-login":
            self.handle_start_login()
        elif self.path == "/api/claude-login/code":
            self.handle_submit_code()
        elif self.path == "/api/install-service":
            self.handle_install_service()
        elif self.path == "/api/uninstall-service":
            self.handle_uninstall_service()
        else:
            self.send_response(404)
            self.end_headers()

    def handle_start_login(self):
        if is_claude_logged_in():
            self.send_json({"error": "Already logged in"})
            return
        try:
            # Kill any existing login process
            if LOGIN_STATE.get("process"):
                try:
                    LOGIN_STATE["process"].terminate()
                except Exception:
                    pass

            proc = subprocess.Popen(
                ["claude", "auth", "login"],
                stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT, text=True,
                env={**os.environ, "BROWSER": "echo"},  # prevent browser launch
            )
            LOGIN_STATE["process"] = proc

            url = None
            lines = []
            for line in iter(proc.stdout.readline, ""):
                lines.append(line.strip())
                # Look for the auth URL in "If the browser didn't open, visit: URL"
                for word in line.split():
                    if word.startswith("https://claude.com/") or word.startswith("https://console.anthropic.com/"):
                        url = word.strip()
                        break
                if url or len(lines) > 20:
                    break

            if url:
                LOGIN_STATE["url"] = url
                self.send_json({"url": url})
            else:
                proc.terminate()
                LOGIN_STATE["process"] = None
                self.send_json({"error": "Could not find auth URL", "output": lines})
        except FileNotFoundError:
            self.send_json({"error": "claude not found — run base-setup first"})
        except Exception as e:
            self.send_json({"error": str(e)})

    def handle_submit_code(self):
        """Pipe the auth code from the browser to claude auth login's stdin."""
        data = self.read_json()
        code = data.get("code", "")
        proc = LOGIN_STATE.get("process")
        if not proc:
            self.send_json({"error": "No login in progress. Click 'Login' first."})
            return
        if not code:
            self.send_json({"error": "No code provided."})
            return
        try:
            # Write code to stdin and close it
            proc.stdin.write(code + "\n")
            proc.stdin.flush()
            proc.stdin.close()

            # Wait for process to finish
            proc.wait(timeout=30)

            LOGIN_STATE["process"] = None
            LOGIN_STATE["url"] = None

            if is_claude_logged_in():
                self.send_json({"success": True})
            else:
                # Read any remaining output for debugging
                remaining = proc.stdout.read() if proc.stdout else ""
                self.send_json({"error": f"Code submitted but auth not detected. Output: {remaining[:500]}"})
        except subprocess.TimeoutExpired:
            proc.terminate()
            LOGIN_STATE["process"] = None
            self.send_json({"error": "Login timed out after 30s."})
        except Exception as e:
            LOGIN_STATE["process"] = None
            self.send_json({"error": str(e)})

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
