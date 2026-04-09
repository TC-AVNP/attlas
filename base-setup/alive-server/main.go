package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	port         = 3000
	cookieName   = "attlas_session"
	cookieMaxAge = 86400 * 7 // 7 days
	authUser     = "Testuser"
	authPass     = "password123"
)

// Rate limiting config
const (
	rateLimitMax    = 10
	rateLimitWindow = 5 * time.Minute
	rateLimitBlock  = 5 * time.Minute
)

var (
	sessionSecret []byte
	distDir       string
	attlasDir     string
)

// Known services
var knownServices = []Service{
	{ID: "terminal", Name: "Cloud Terminal", ServiceName: "ttyd", Command: "ttyd",
		Path: "/terminal/", Script: "install-terminal.sh"},
	{ID: "code-server", Name: "Cloud VS Code", ServiceName: "code-server", Command: "code-server",
		Path: "/code/", Script: "install-code-server.sh"},
	{ID: "openclaw", Name: "OpenClaw", ServiceName: "openclaw-gateway", Command: "openclaw",
		Path: "/openclaw/", Script: "install-openclaw.sh", CheckProcess: "openclaw-gateway"},
}

type Service struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ServiceName  string `json:"service"`
	Command      string `json:"command"`
	Path         string `json:"path"`
	Script       string `json:"script"`
	CheckProcess string `json:"check_process,omitempty"`
	Installed    bool   `json:"installed"`
	Running      bool   `json:"running"`
}

// --- Rate limiter ---

type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	blocked  map[string]time.Time
}

var limiter = &rateLimiter{
	attempts: make(map[string][]time.Time),
	blocked:  make(map[string]time.Time),
}

func (rl *rateLimiter) isBlocked(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if until, ok := rl.blocked[ip]; ok {
		if time.Now().Before(until) {
			return true
		}
		delete(rl.blocked, ip)
	}
	return false
}

func (rl *rateLimiter) recordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rateLimitWindow)

	// Clean old attempts
	var recent []time.Time
	for _, t := range rl.attempts[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	rl.attempts[ip] = recent

	if len(recent) >= rateLimitMax {
		rl.blocked[ip] = now.Add(rateLimitBlock)
		delete(rl.attempts, ip)
	}
}

func (rl *rateLimiter) clearAttempts(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, ip)
}

// --- CSRF tokens ---

type csrfStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time
}

var csrf = &csrfStore{
	tokens: make(map[string]time.Time),
}

func (cs *csrfStore) generate() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)

	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Clean expired tokens
	now := time.Now()
	for k, v := range cs.tokens {
		if now.Sub(v) > 10*time.Minute {
			delete(cs.tokens, k)
		}
	}

	cs.tokens[token] = now
	return token
}

func (cs *csrfStore) validate(token string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if t, ok := cs.tokens[token]; ok {
		delete(cs.tokens, token)
		return time.Since(t) < 10*time.Minute
	}
	return false
}

// --- Session ---

func loadOrCreateSecret() []byte {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".attlas-session-secret")

	data, err := os.ReadFile(path)
	if err == nil && len(data) >= 32 {
		return data
	}

	// Generate new secret
	secret := make([]byte, 32)
	rand.Read(secret)
	encoded := []byte(hex.EncodeToString(secret))
	os.WriteFile(path, encoded, 0600)
	return encoded
}

func makeSessionToken(username string) string {
	payload := fmt.Sprintf("%s:%d", username, time.Now().Unix())
	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))[:32]
	return fmt.Sprintf("%s:%s", payload, sig)
}

func verifySessionToken(token string) bool {
	lastColon := strings.LastIndex(token, ":")
	if lastColon < 0 {
		return false
	}
	payload := token[:lastColon]
	sig := token[lastColon+1:]

	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))[:32]

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return false
	}

	// Check expiry
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return false
	}
	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix()-ts <= cookieMaxAge
}

func getSessionCookie(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func isAuthenticated(r *http.Request) bool {
	token := getSessionCookie(r)
	return token != "" && verifySessionToken(token)
}

// --- VM info ---

func gcpMeta(path string) string {
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest("GET", "http://metadata.google.internal/computeMetadata/v1/"+path, nil)
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := client.Do(req)
	if err != nil {
		return "unknown"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(body))
}

func getVMInfo() map[string]string {
	ip := gcpMeta("instance/network-interfaces/0/access-configs/0/external-ip")
	zoneRaw := gcpMeta("instance/zone")
	zone := zoneRaw
	if i := strings.LastIndex(zoneRaw, "/"); i >= 0 {
		zone = zoneRaw[i+1:]
	}
	name := gcpMeta("instance/name")
	return map[string]string{
		"name":        name,
		"zone":        zone,
		"external_ip": ip,
		"domain":      "attlas.uk",
	}
}

// --- Claude status ---

func isClaudeInstalled() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

func isClaudeLoggedIn() bool {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return false
	}
	var m map[string]interface{}
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	_, hasOAuth := m["oauthAccount"]
	_, hasKey := m["apiKey"]
	return hasOAuth || hasKey
}

// --- Service status ---

func runCmd(name string, args ...string) (string, bool) {
	ctx, cancel := exec.Command(name, args...), func() {}
	_ = cancel
	out, err := ctx.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

func getServicesStatus() []Service {
	var results []Service
	for _, svc := range knownServices {
		s := svc
		_, err := exec.LookPath(svc.Command)
		s.Installed = err == nil
		if s.Installed {
			if svc.CheckProcess != "" {
				_, s.Running = runCmd("pgrep", "-f", svc.CheckProcess)
			} else {
				out, _ := runCmd("systemctl", "is-active", svc.ServiceName)
				s.Running = out == "active"
			}
		}
		results = append(results, s)
	}
	return results
}

// --- Login page ---

const loginPageTemplate = `<!DOCTYPE html>
<html>
<head>
    <title>Attlas Login</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: -apple-system, sans-serif; background: #1a1a2e; color: #eee;
               display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; }
        .box { max-width: 350px; width: 100%%; padding: 2rem; }
        h1 { color: #68d391; font-size: 1.5rem; margin-bottom: 1.5rem; }
        label { display: block; margin-bottom: 0.3rem; color: #888; font-size: 0.85rem; }
        input { width: 100%%; padding: 0.6rem; margin-bottom: 1rem; font-size: 1rem;
                background: #2d2d44; color: #eee; border: 1px solid #555; border-radius: 4px;
                box-sizing: border-box; }
        button { width: 100%%; padding: 0.7rem; font-size: 1rem; cursor: pointer;
                 background: #5a67d8; color: white; border: none; border-radius: 4px; }
        button:hover { background: #4c51bf; }
        .error { color: #fc8181; margin-bottom: 1rem; font-size: 0.9rem; }
    </style>
</head>
<body>
    <div class="box">
        <h1>Attlas VM</h1>
        %s
        <form method="POST" action="/login">
            <input type="hidden" name="csrf_token" value="%s">
            <label>Username</label>
            <input type="text" name="username" autofocus>
            <label>Password</label>
            <input type="password" name="password">
            <button type="submit">Sign in</button>
        </form>
    </div>
</body>
</html>`

// --- Handlers ---

func handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if isAuthenticated(r) {
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

func handleLoginPage(w http.ResponseWriter, r *http.Request) {
	token := csrf.generate()
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, loginPageTemplate, "", token)
}

func handleLoginPost(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	// CSRF check
	csrfToken := r.FormValue("csrf_token")
	if !csrf.validate(csrfToken) {
		token := csrf.generate()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, loginPageTemplate, `<div class="error">Invalid request. Please try again.</div>`, token)
		return
	}

	ip := extractIP(r)

	// Rate limit check
	if limiter.isBlocked(ip) {
		token := csrf.generate()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, loginPageTemplate, `<div class="error">Too many failed attempts. Try again later.</div>`, token)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == authUser && password == authPass {
		limiter.clearAttempts(ip)
		sessionToken := makeSessionToken(username)
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    sessionToken,
			Path:     "/",
			MaxAge:   cookieMaxAge,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   true,
		})
		http.Redirect(w, r, "/", http.StatusFound)
	} else {
		limiter.recordFailure(ip)
		token := csrf.generate()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, loginPageTemplate, `<div class="error">Invalid username or password</div>`, token)
	}
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"vm": getVMInfo(),
		"claude": map[string]bool{
			"installed":     isClaudeInstalled(),
			"authenticated": isClaudeLoggedIn(),
		},
		"services": getServicesStatus(),
	})
}

func handleClaudeLogin(w http.ResponseWriter, r *http.Request) {
	if isClaudeLoggedIn() {
		sendJSON(w, map[string]interface{}{"error": "Already logged in"})
		return
	}

	exec.Command("pkill", "-f", "claude-login-helper").Run()
	time.Sleep(1 * time.Second)

	for _, f := range []string{"/tmp/claude-login-url", "/tmp/claude-login-code", "/tmp/claude-login-result"} {
		os.Remove(f)
	}

	helperPath := filepath.Join(filepath.Dir(os.Args[0]), "claude-login-helper.py")
	if _, err := os.Stat(helperPath); err != nil {
		// Try relative to working directory
		helperPath = filepath.Join(distDir, "..", "claude-login-helper.py")
	}

	logFile, _ := os.Create("/tmp/claude-login-helper.log")
	cmd := exec.Command("python3", helperPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = nil
	cmd.Start()

	// Wait for URL
	var authURL string
	for i := 0; i < 60; i++ {
		data, err := os.ReadFile("/tmp/claude-login-url")
		if err == nil {
			authURL = strings.TrimSpace(string(data))
			if authURL != "" {
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	if authURL != "" {
		sendJSON(w, map[string]interface{}{"url": authURL})
	} else {
		logData, _ := os.ReadFile("/tmp/claude-login-helper.log")
		snippet := string(logData)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Timed out waiting for URL. Log: %s", snippet)})
	}
}

func handleClaudeCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Code == "" {
		sendJSON(w, map[string]interface{}{"error": "No code provided."})
		return
	}

	os.WriteFile("/tmp/claude-login-code", []byte(body.Code), 0644)

	var result string
	for i := 0; i < 30; i++ {
		data, err := os.ReadFile("/tmp/claude-login-result")
		if err == nil {
			result = strings.TrimSpace(string(data))
			if result != "" {
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	if result == "SUCCESS" {
		sendJSON(w, map[string]interface{}{"success": true})
	} else if result != "" {
		sendJSON(w, map[string]interface{}{"error": result})
	} else {
		sendJSON(w, map[string]interface{}{"error": "Timed out waiting for login result."})
	}
}

func handleInstallService(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	svc := findService(body.ID)
	if svc == nil {
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Unknown service: %s", body.ID)})
		return
	}

	script := filepath.Join(attlasDir, "services", svc.Script)
	if _, err := os.Stat(script); err != nil {
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Script not found: %s", script)})
		return
	}

	cmd := exec.Command("bash", script)
	cmd.Dir = filepath.Join(attlasDir, "services")
	out, err := cmd.CombinedOutput()
	if err != nil {
		sendJSON(w, map[string]interface{}{"error": string(out)})
		return
	}

	exec.Command("sudo", "systemctl", "reload", "caddy").Run()
	sendJSON(w, map[string]interface{}{"success": true})
}

func handleUninstallService(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	svc := findService(body.ID)
	if svc == nil {
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Unknown service: %s", body.ID)})
		return
	}

	script := filepath.Join(attlasDir, "services", fmt.Sprintf("uninstall-%s.sh", svc.ID))
	if _, err := os.Stat(script); err != nil {
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Uninstall script not found: %s", script)})
		return
	}

	cmd := exec.Command("bash", script)
	cmd.Dir = filepath.Join(attlasDir, "services")
	out, err := cmd.CombinedOutput()
	if err != nil {
		sendJSON(w, map[string]interface{}{"error": string(out)})
		return
	}

	exec.Command("sudo", "systemctl", "reload", "caddy").Run()
	sendJSON(w, map[string]interface{}{"success": true})
}

// --- Helpers ---

func findService(id string) *Service {
	for _, s := range knownServices {
		if s.ID == id {
			return &s
		}
	}
	return nil
}

func sendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func extractIP(r *http.Request) string {
	// Behind Caddy, use X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	host, _, _ := strings.Cut(r.RemoteAddr, ":")
	return host
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	filePath := filepath.Join(distDir, filepath.Clean(path))

	// Prevent directory traversal
	if !strings.HasPrefix(filePath, distDir) {
		http.NotFound(w, r)
		return
	}

	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		contentType := mime.TypeByExtension(filepath.Ext(filePath))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, filePath)
		return
	}

	// SPA fallback — serve index.html for unmatched routes
	indexPath := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		w.Header().Set("Content-Type", "text/html")
		http.ServeFile(w, r, indexPath)
		return
	}

	http.NotFound(w, r)
}

// --- Main ---

func main() {
	sessionSecret = loadOrCreateSecret()

	// Resolve paths
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)
	distDir = filepath.Join(execDir, "frontend", "dist")
	home, _ := os.UserHomeDir()
	attlasDir = filepath.Join(home, "attlas")

	// If dist dir doesn't exist relative to executable, try working directory
	if _, err := os.Stat(distDir); err != nil {
		wd, _ := os.Getwd()
		distDir = filepath.Join(wd, "frontend", "dist")
	}

	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("/api/auth/verify", handleAuthVerify)
	mux.HandleFunc("GET /login", handleLoginPage)
	mux.HandleFunc("POST /login", handleLoginPost)
	mux.HandleFunc("/logout", handleLogout)

	// API
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("POST /api/claude-login", handleClaudeLogin)
	mux.HandleFunc("POST /api/claude-login/code", handleClaudeCode)
	mux.HandleFunc("POST /api/install-service", handleInstallService)
	mux.HandleFunc("POST /api/uninstall-service", handleUninstallService)

	// Static files (catch-all)
	mux.HandleFunc("/", serveStatic)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	log.Printf("Attlas server running on http://%s", addr)
	log.Printf("Serving frontend from %s", distDir)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func init() {
	// Register common MIME types
	mime.AddExtensionType(".js", "application/javascript")
	mime.AddExtensionType(".css", "text/css")
	mime.AddExtensionType(".svg", "image/svg+xml")
	mime.AddExtensionType(".json", "application/json")
	mime.AddExtensionType(".woff2", "font/woff2")
}
