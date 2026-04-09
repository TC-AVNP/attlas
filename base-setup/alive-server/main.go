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
)

var (
	sessionSecret []byte
	distDir       string
	attlasDir     string
	oauthConfig   *OAuthConfig
)

// OAuth2 config loaded from ~/.attlas-server-config.json
type OAuthConfig struct {
	ClientID     string   `json:"google_oauth_client_id"`
	ClientSecret string   `json:"google_oauth_client_secret"`
	AllowedEmails []string `json:"allowed_emails"`
}

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

// --- OAuth2 state tokens ---

type stateStore struct {
	mu     sync.Mutex
	states map[string]time.Time
}

var oauthStates = &stateStore{
	states: make(map[string]time.Time),
}

func (ss *stateStore) generate() string {
	b := make([]byte, 32)
	rand.Read(b)
	state := hex.EncodeToString(b)

	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Clean expired states
	now := time.Now()
	for k, v := range ss.states {
		if now.Sub(v) > 10*time.Minute {
			delete(ss.states, k)
		}
	}

	ss.states[state] = now
	return state
}

func (ss *stateStore) validate(state string) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if t, ok := ss.states[state]; ok {
		delete(ss.states, state)
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

func loadOAuthConfig() *OAuthConfig {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".attlas-server-config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("WARNING: OAuth config not found at %s: %v", path, err)
		return nil
	}

	var config OAuthConfig
	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("WARNING: Failed to parse OAuth config: %v", err)
		return nil
	}

	if config.ClientID == "" || config.ClientSecret == "" {
		log.Printf("WARNING: OAuth config missing client_id or client_secret")
		return nil
	}

	log.Printf("OAuth2 configured with %d allowed email(s)", len(config.AllowedEmails))
	return &config
}

func makeSessionToken(email string) string {
	payload := fmt.Sprintf("%s:%d", email, time.Now().Unix())
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
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
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

// --- Error page ---

const errorPageTemplate = `<!DOCTYPE html>
<html>
<head>
    <title>Attlas — Access Denied</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: -apple-system, sans-serif; background: #1a1a2e; color: #eee;
               display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; }
        .box { max-width: 400px; width: 100%%; padding: 2rem; text-align: center; }
        h1 { color: #fc8181; font-size: 1.5rem; margin-bottom: 1rem; }
        p { color: #888; line-height: 1.6; }
        a { color: #5a67d8; }
    </style>
</head>
<body>
    <div class="box">
        <h1>Access Denied</h1>
        <p>%s</p>
        <p style="margin-top: 1.5rem;"><a href="/oauth2/login">Try again</a></p>
    </div>
</body>
</html>`

const accessDeniedPage = `<!DOCTYPE html>
<html>
<head>
    <title>KEEP AWAY</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: -apple-system, sans-serif; background: #0a0a0a; color: #ff0000;
               display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0;
               overflow: hidden; }
        .container { text-align: center; animation: pulse 1.5s ease-in-out infinite; }
        .icon { font-size: 8rem; margin-bottom: 1rem; filter: drop-shadow(0 0 30px #ff0000); }
        h1 { font-size: 4rem; font-weight: 900; letter-spacing: 0.3rem; text-transform: uppercase;
             text-shadow: 0 0 20px #ff0000, 0 0 40px #ff4444, 0 0 80px #ff0000; margin: 0.5rem 0; }
        .sub { font-size: 1.2rem; color: #ff4444; letter-spacing: 0.5rem; text-transform: uppercase; }
        .bar { height: 4px; background: repeating-linear-gradient(90deg, #ff0000 0px, #ff0000 20px, #000 20px, #000 40px);
               margin: 2rem auto; width: 80%%; max-width: 500px; }
        @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.7; } }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">&#9889;</div>
        <div class="bar"></div>
        <h1>KEEP AWAY</h1>
        <div class="bar"></div>
        <div class="sub">unauthorized access</div>
    </div>
</body>
</html>`

// --- Handlers ---

func handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if isAuthenticated(r) {
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/oauth2/login", http.StatusFound)
	}
}

func handleOAuth2Login(w http.ResponseWriter, r *http.Request) {
	if oauthConfig == nil {
		http.Error(w, "OAuth2 not configured", http.StatusInternalServerError)
		return
	}

	state := oauthStates.generate()
	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s&access_type=online&prompt=select_account",
		url.QueryEscape(oauthConfig.ClientID),
		url.QueryEscape("https://attlas.uk/oauth2/callback"),
		url.QueryEscape("openid email"),
		url.QueryEscape(state),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func handleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	if oauthConfig == nil {
		http.Error(w, "OAuth2 not configured", http.StatusInternalServerError)
		return
	}

	// Validate state
	state := r.URL.Query().Get("state")
	if !oauthStates.validate(state) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Invalid or expired login request. Please try again.")
		return
	}

	// Check for error from Google
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Google sign-in was cancelled or failed.")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "No authorization code received from Google.")
		return
	}

	// Exchange code for token
	tokenResp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"code":          {code},
		"client_id":     {oauthConfig.ClientID},
		"client_secret": {oauthConfig.ClientSecret},
		"redirect_uri":  {"https://attlas.uk/oauth2/callback"},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Failed to contact Google. Please try again.")
		return
	}
	defer tokenResp.Body.Close()

	var tokenData struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&tokenData)
	if tokenData.AccessToken == "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Failed to authenticate with Google.")
		return
	}

	// Get user info
	userReq, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	userReq.Header.Set("Authorization", "Bearer "+tokenData.AccessToken)
	userResp, err := http.DefaultClient.Do(userReq)
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Failed to get user info from Google.")
		return
	}
	defer userResp.Body.Close()

	var userInfo struct {
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
	}
	json.NewDecoder(userResp.Body).Decode(&userInfo)

	if userInfo.Email == "" || !userInfo.VerifiedEmail {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Could not verify your email address.")
		return
	}

	// Check against allowed emails
	allowed := false
	for _, e := range oauthConfig.AllowedEmails {
		if strings.EqualFold(e, userInfo.Email) {
			allowed = true
			break
		}
	}
	if !allowed {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, accessDeniedPage)
		return
	}

	// Create session
	sessionToken := makeSessionToken(userInfo.Email)
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
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/oauth2/login", http.StatusFound)
}

func getSessionEmail(r *http.Request) string {
	token := getSessionCookie(r)
	if token == "" {
		return ""
	}
	lastColon := strings.LastIndex(token, ":")
	if lastColon < 0 {
		return ""
	}
	payload := token[:lastColon]
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"vm": getVMInfo(),
		"user": map[string]string{
			"email": getSessionEmail(r),
		},
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
		helperPath = filepath.Join(distDir, "..", "claude-login-helper.py")
	}

	logFile, _ := os.Create("/tmp/claude-login-helper.log")
	cmd := exec.Command("python3", helperPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = nil
	cmd.Start()

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
	oauthConfig = loadOAuthConfig()

	// Resolve paths
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)
	distDir = filepath.Join(execDir, "frontend", "dist")
	home, _ := os.UserHomeDir()
	attlasDir = filepath.Join(home, "attlas")

	if _, err := os.Stat(distDir); err != nil {
		wd, _ := os.Getwd()
		distDir = filepath.Join(wd, "frontend", "dist")
	}

	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("/api/auth/verify", handleAuthVerify)
	mux.HandleFunc("GET /oauth2/login", handleOAuth2Login)
	mux.HandleFunc("GET /oauth2/callback", handleOAuth2Callback)
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
	mime.AddExtensionType(".js", "application/javascript")
	mime.AddExtensionType(".css", "text/css")
	mime.AddExtensionType(".svg", "image/svg+xml")
	mime.AddExtensionType(".json", "application/json")
	mime.AddExtensionType(".woff2", "font/woff2")
}
