// Package auth owns all things session + Google OAuth + forward_auth
// for the alive-server dashboard.
//
// Entry points used by the main mux (in priority order):
//
//	HandleAuthVerify     — Caddy forward_auth target
//	HandleOAuth2Login    — redirect to Google
//	HandleOAuth2Callback — exchange code, set session cookie
//	HandleLogout         — clear session cookie
//
// Public helpers for other packages:
//
//	IsAuthenticated(r)  — bool, for handlers that need to know
//	GetSessionEmail(r)  — string, for "who is currently logged in"
//
// Init() must be called once at startup with the loaded OAuth config
// and the HMAC session secret before any handler is used.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"attlas-server/internal/config"
)

// --- Constants ---

const (
	cookieName   = "attlas_session"
	cookieMaxAge = 86400 * 7 // 7 days
)

const defaultPublicPathsDir = "/etc/attlas-public-paths.d"

// --- Package-level state (set via Init) ---

var (
	sessionSecret []byte
	oauthConfig   *config.OAuthConfig
)

// Init wires up the package with the runtime session secret and the
// parsed OAuth config. Safe to call multiple times — the last values
// win.
func Init(secret []byte, cfg *config.OAuthConfig) {
	sessionSecret = secret
	oauthConfig = cfg
}

// AllowedEmails returns the configured whitelist, or nil if OAuth
// isn't configured. Used by handlers that want to surface the list
// (e.g. /api/status → "who can log in?").
func AllowedEmails() []string {
	if oauthConfig == nil {
		return nil
	}
	return oauthConfig.AllowedEmails
}

// AnthropicAdminKey returns the admin key stored in the OAuth config
// (used by cost handlers). Empty string if OAuth isn't configured or
// the key isn't present in the config file.
func AnthropicAdminKey() string {
	if oauthConfig == nil {
		return ""
	}
	return oauthConfig.AnthropicAdminKey
}

// --- State store ---

// A stateEntry holds both the creation time (for expiry) and an optional
// return URL captured at /oauth2/login time. The callback handler uses
// the return URL to send the user back to the page they originally
// tried to visit — critical for flows like petboard's MCP /authorize,
// which must land back on a specific URL with its query params intact.
type stateEntry struct {
	createdAt time.Time
	returnTo  string
}

type stateStore struct {
	mu     sync.Mutex
	states map[string]stateEntry
}

var oauthStates = &stateStore{
	states: make(map[string]stateEntry),
}

func (ss *stateStore) generate(returnTo string) string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	state := hex.EncodeToString(b)

	ss.mu.Lock()
	defer ss.mu.Unlock()

	now := time.Now()
	for k, v := range ss.states {
		if now.Sub(v.createdAt) > 10*time.Minute {
			delete(ss.states, k)
		}
	}

	ss.states[state] = stateEntry{createdAt: now, returnTo: returnTo}
	return state
}

// validate consumes the state token and returns (returnTo, ok). The
// entry is single-use — it is deleted from the store regardless of
// whether the caller ends up redirecting to the return URL.
func (ss *stateStore) validate(state string) (string, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if entry, ok := ss.states[state]; ok {
		delete(ss.states, state)
		if time.Since(entry.createdAt) < 10*time.Minute {
			return entry.returnTo, true
		}
	}
	return "", false
}

// --- Public-path registry ---
//
// Any service that needs a subset of its paths to bypass alive-server's
// Google-OAuth gate drops a file into /etc/attlas-public-paths.d/. Each
// file is a newline-separated list of path prefixes; '#' starts a
// comment, blank lines are ignored. On startup and on SIGHUP the
// directory is re-read and an in-memory prefix list is atomically
// swapped in. HandleAuthVerify consults this list before doing any
// session check, returning 200 OK for matches so Caddy forward_auth
// lets the request through to the downstream service.

type pathRegistry struct {
	mu       sync.RWMutex
	prefixes []string
}

var publicPathRegistry = &pathRegistry{}

// ReloadPublicPaths re-reads /etc/attlas-public-paths.d/ and atomically
// installs the new prefix list. Called at startup and on SIGHUP.
func ReloadPublicPaths() { publicPathRegistry.load() }

func publicPathsDir() string {
	if dir := os.Getenv("ATTLAS_PUBLIC_PATHS_DIR"); dir != "" {
		return dir
	}
	return defaultPublicPathsDir
}

func (r *pathRegistry) load() {
	dir := publicPathsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		r.mu.Lock()
		r.prefixes = nil
		r.mu.Unlock()
		if !os.IsNotExist(err) {
			log.Printf("public-paths: read %s: %v", dir, err)
		}
		return
	}

	var prefixes []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("public-paths: read %s: %v", path, err)
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if i := strings.Index(line, "#"); i >= 0 {
				line = line[:i]
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "/") {
				log.Printf("public-paths: ignoring non-absolute prefix %q in %s", line, path)
				continue
			}
			prefixes = append(prefixes, line)
		}
	}

	r.mu.Lock()
	r.prefixes = prefixes
	r.mu.Unlock()
	log.Printf("public-paths: loaded %d prefix(es) from %s", len(prefixes), dir)
}

func (r *pathRegistry) matches(path string) bool {
	if path == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// isSafeRelativePath reports whether a candidate return URL can safely
// be used as a redirect target. Only same-origin relative paths are
// accepted: must start with '/', must not start with '//'
// (protocol-relative), must parse with empty scheme and empty host.
// Prevents open redirects via a crafted return_to query param.
func isSafeRelativePath(p string) bool {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return false
	}
	if strings.ContainsAny(p, "\\\r\n") {
		return false
	}
	u, err := url.Parse(p)
	if err != nil {
		return false
	}
	return u.Scheme == "" && u.Host == ""
}

// --- Session token ---

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

// IsAuthenticated returns true iff the request carries a valid,
// unexpired session cookie.
func IsAuthenticated(r *http.Request) bool {
	token := getSessionCookie(r)
	return token != "" && verifySessionToken(token)
}

// GetSessionEmail returns the email embedded in the session cookie, or
// "" if there's no valid session.
func GetSessionEmail(r *http.Request) string {
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

// --- HTML error pages ---

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

// HandleAuthVerify is Caddy's forward_auth target. It returns 200 iff
// the request should pass through to the downstream service, 302 to
// /oauth2/login otherwise.
func HandleAuthVerify(w http.ResponseWriter, r *http.Request) {
	origURI := r.Header.Get("X-Forwarded-Uri")
	if publicPathRegistry.matches(origURI) {
		w.WriteHeader(http.StatusOK)
		return
	}

	// RFC 8414 discovery URL shim for subpath issuers (petboard's
	// /petboard/.well-known/oauth-authorization-server lives under the
	// service, not at the root).
	if strings.HasPrefix(origURI, "/.well-known/oauth-authorization-server/") {
		svcPath := strings.TrimPrefix(origURI, "/.well-known/oauth-authorization-server")
		redirect := svcPath + "/.well-known/oauth-authorization-server"
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}
	if IsAuthenticated(r) {
		w.WriteHeader(http.StatusOK)
		return
	}
	loginURL := "/oauth2/login"
	if isSafeRelativePath(origURI) {
		loginURL += "?return_to=" + url.QueryEscape(origURI)
	}
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func HandleOAuth2Login(w http.ResponseWriter, r *http.Request) {
	if oauthConfig == nil {
		http.Error(w, "OAuth2 not configured", http.StatusInternalServerError)
		return
	}

	returnTo := r.URL.Query().Get("return_to")
	if !isSafeRelativePath(returnTo) {
		returnTo = ""
	}

	state := oauthStates.generate(returnTo)
	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s&access_type=online&prompt=select_account",
		url.QueryEscape(oauthConfig.ClientID),
		url.QueryEscape("https://attlas.uk/oauth2/callback"),
		url.QueryEscape("openid email"),
		url.QueryEscape(state),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func HandleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	if oauthConfig == nil {
		http.Error(w, "OAuth2 not configured", http.StatusInternalServerError)
		return
	}

	state := r.URL.Query().Get("state")
	returnTo, ok := oauthStates.validate(state)
	if !ok {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Invalid or expired login request. Please try again.")
		return
	}

	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		_ = errMsg
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
	_ = json.NewDecoder(tokenResp.Body).Decode(&tokenData)
	if tokenData.AccessToken == "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Failed to authenticate with Google.")
		return
	}

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
	_ = json.NewDecoder(userResp.Body).Decode(&userInfo)

	if userInfo.Email == "" || !userInfo.VerifiedEmail {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Could not verify your email address.")
		return
	}

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

	dest := "/"
	if isSafeRelativePath(returnTo) {
		dest = returnTo
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/oauth2/login", http.StatusFound)
}
