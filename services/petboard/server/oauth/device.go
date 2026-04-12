// Device authorization grant (RFC 8628) for headless environments.
//
// The flow:
//
//  1. Client POSTs /oauth/device/code → gets {device_code, user_code,
//     verification_uri, expires_in, interval}
//  2. Client shows user_code to the operator: "Go to <URL> and enter
//     code ABCD-EFGH"
//  3. Operator opens verification_uri on any browser (phone, laptop).
//     That page is behind Caddy auth → Google login → lands on a form.
//  4. Operator enters user_code, submits → petboard approves the
//     device_code and mints an access token.
//  5. Client polls POST /oauth/token with
//     grant_type=urn:ietf:params:oauth:grant-type:device_code →
//     gets "authorization_pending" until the operator approves, then
//     gets the access token.
//
// Storage is in-memory — device codes are ephemeral (5 minute TTL,
// one pending at a time is the typical case). No migration needed.
package oauth

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ----- in-memory store ----------------------------------------------------

type devicePending struct {
	userCode    string
	clientID    string
	scope       string
	expiresAt   time.Time
	approved    bool
	denied      bool
	userEmail   string // set on approval
	accessToken string // set on approval (raw token, not hash)
}

// deviceStore is a goroutine-safe map of device_code_hash → pending.
// Expired entries are lazily cleaned up on each write.
type deviceStore struct {
	mu    sync.Mutex
	codes map[string]*devicePending // key = sha256(device_code)
}

func newDeviceStore() *deviceStore {
	return &deviceStore{codes: make(map[string]*devicePending)}
}

func (ds *deviceStore) put(deviceCodeHash string, p *devicePending) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	// Lazy cleanup
	now := time.Now()
	for k, v := range ds.codes {
		if now.After(v.expiresAt) {
			delete(ds.codes, k)
		}
	}
	ds.codes[deviceCodeHash] = p
}

func (ds *deviceStore) get(deviceCodeHash string) *devicePending {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.codes[deviceCodeHash]
}

func (ds *deviceStore) getByUserCode(userCode string) (string, *devicePending) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	uc := strings.ToUpper(strings.TrimSpace(userCode))
	for hash, p := range ds.codes {
		if p.userCode == uc && time.Now().Before(p.expiresAt) {
			return hash, p
		}
	}
	return "", nil
}

// devices is the package-level store, initialized once per Server.
// We attach it to the Server so tests could replace it.
func (s *Server) ensureDeviceStore() {
	s.deviceOnce.Do(func() {
		s.devices = newDeviceStore()
	})
}

// ----- handlers -----------------------------------------------------------

// handleDeviceCode handles POST /oauth/device/code.
// Public (no auth needed) — the whole point is that the CLI can't auth yet.
func (s *Server) handleDeviceCode(w http.ResponseWriter, r *http.Request) {
	s.ensureDeviceStore()

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	clientID := r.Form.Get("client_id")
	scope := r.Form.Get("scope")

	// clientID is optional for device flow (RFC 8628 §3.1). If empty,
	// we use (or create) a built-in "device-flow" client so the FK
	// constraint on oauth_access_tokens is satisfied.
	if clientID == "" {
		clientID = s.ensureDeviceClient()
	} else {
		if _, err := s.lookupClientRedirectURIs(clientID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_client", err.Error())
			return
		}
	}

	deviceCode := randomString(32)
	userCode := generateUserCode()
	expiresAt := time.Now().Add(5 * time.Minute)

	s.devices.put(sha256Hex(deviceCode), &devicePending{
		userCode:  userCode,
		clientID:  clientID,
		scope:     scope,
		expiresAt: expiresAt,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"device_code":      deviceCode,
		"user_code":        userCode,
		"verification_uri": s.Issuer + "/oauth/device",
		"expires_in":       300,
		"interval":         5,
	})
}

// handleDevicePage handles GET /oauth/device.
// This is Caddy-authed (NOT in the public-paths list) — the user must
// be logged in via Google before they can approve a device code.
func (s *Server) handleDevicePage(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get("msg")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, devicePageHTML, msg)
}

// handleDeviceApprove handles POST /oauth/device/approve.
// Also Caddy-authed. Approves a user_code, mints an access token.
func (s *Server) handleDeviceApprove(w http.ResponseWriter, r *http.Request) {
	s.ensureDeviceStore()

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	userCode := r.Form.Get("user_code")
	if userCode == "" {
		http.Redirect(w, r, "/petboard/oauth/device?msg=Please+enter+a+code", http.StatusSeeOther)
		return
	}

	hash, pending := s.devices.getByUserCode(userCode)
	if pending == nil {
		http.Redirect(w, r, "/petboard/oauth/device?msg=Invalid+or+expired+code", http.StatusSeeOther)
		return
	}
	if pending.approved {
		http.Redirect(w, r, "/petboard/oauth/device?msg=Already+approved", http.StatusSeeOther)
		return
	}

	// Capture user identity from upstream headers
	userEmail := r.Header.Get("X-Forwarded-User")
	if userEmail == "" {
		userEmail = r.Header.Get("X-Auth-User")
	}
	if userEmail == "" {
		userEmail = "unknown"
	}

	// Mint token and store it on the pending entry so the polling
	// endpoint can return it.
	token := randomString(40)
	tokenHash := sha256Hex(token)
	now := time.Now().Unix()
	expires := now + 30*24*3600
	if _, err := s.DB.Exec(
		`INSERT INTO oauth_access_tokens(token_hash, client_id, scope, user_email, created_at, expires_at)
		 VALUES (?,?,?,?,?,?)`,
		tokenHash, pending.clientID, pending.scope, userEmail, now, expires,
	); err != nil {
		http.Redirect(w, r, "/petboard/oauth/device?msg=Server+error", http.StatusSeeOther)
		return
	}

	// Mark approved with the raw token so the polling side can pick it up
	s.devices.mu.Lock()
	p := s.devices.codes[hash]
	if p != nil {
		p.approved = true
		p.userEmail = userEmail
		p.accessToken = token
	}
	s.devices.mu.Unlock()

	http.Redirect(w, r, "/petboard/oauth/device?msg=Approved!+You+can+close+this+tab.", http.StatusSeeOther)
}

// handleDeviceToken is called from the existing handleToken when
// grant_type is the device code URN. It polls for the approved token.
func (s *Server) handleDeviceToken(w http.ResponseWriter, r *http.Request) {
	s.ensureDeviceStore()

	deviceCode := r.Form.Get("device_code")
	clientID := r.Form.Get("client_id")

	if deviceCode == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "device_code is required")
		return
	}

	hash := sha256Hex(deviceCode)
	pending := s.devices.get(hash)
	if pending == nil {
		writeError(w, http.StatusBadRequest, "invalid_grant", "unknown or expired device_code")
		return
	}
	if time.Now().After(pending.expiresAt) {
		writeError(w, http.StatusBadRequest, "expired_token", "device code expired")
		return
	}
	if clientID != "" && pending.clientID != "" && clientID != pending.clientID {
		writeError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}
	if pending.denied {
		writeError(w, http.StatusBadRequest, "access_denied", "user denied the request")
		return
	}
	if !pending.approved {
		// Still waiting — tell the client to keep polling
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusBadRequest, "authorization_pending", "waiting for user to approve")
		return
	}

	// Approved — return the token and clean up
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": pending.accessToken,
		"token_type":   "Bearer",
		"expires_in":   30 * 24 * 3600,
		"scope":        pending.scope,
	})

	// One-shot: remove from the store
	s.devices.mu.Lock()
	delete(s.devices.codes, hash)
	s.devices.mu.Unlock()
}

// ensureDeviceClient creates (or returns the ID of) the built-in
// "device-flow" OAuth client used when the device code request doesn't
// carry an explicit client_id.
func (s *Server) ensureDeviceClient() string {
	const id = "device-flow-builtin"
	var exists int
	_ = s.DB.QueryRow(`SELECT 1 FROM oauth_clients WHERE id = ?`, id).Scan(&exists)
	if exists == 1 {
		return id
	}
	_, _ = s.DB.Exec(
		`INSERT OR IGNORE INTO oauth_clients(id, name, redirect_uris, created_at)
		 VALUES (?, 'Device Flow (built-in)', '[]', ?)`,
		id, time.Now().Unix(),
	)
	return id
}

// ----- user code generation -----------------------------------------------

// generateUserCode produces a human-friendly 8-character code in the
// format "ABCD-EFGH". Uses only uppercase consonants + digits to avoid
// ambiguity (no O/0, no I/1/L confusion, no vowels to avoid accidental
// words).
func generateUserCode() string {
	// Unambiguous charset: no vowels (avoid words), no 0/O/1/I/L
	const charset = "BCDFGHJKMNPQRSTVWXYZ23456789"
	b := make([]byte, 8)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b[:4]) + "-" + string(b[4:])
}

// ----- HTML page ----------------------------------------------------------

const devicePageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>petboard — device login</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: ui-sans-serif, system-ui, sans-serif;
         background: #0a0a0a; color: #e5e5e5;
         display: flex; align-items: center; justify-content: center;
         min-height: 100vh; }
  .card { background: #171717; border: 1px solid #262626;
          border-radius: 12px; padding: 2rem; max-width: 400px;
          width: 100%%; }
  h1 { font-size: 1.25rem; font-weight: 600; margin-bottom: 0.5rem; }
  p { color: #a3a3a3; font-size: 0.875rem; line-height: 1.5;
      margin-bottom: 1.5rem; }
  label { display: block; font-size: 0.75rem; color: #737373;
          text-transform: uppercase; letter-spacing: 0.05em;
          margin-bottom: 0.5rem; }
  input[type=text] { width: 100%%; padding: 0.75rem 1rem;
         background: #0a0a0a; border: 1px solid #404040;
         border-radius: 8px; color: #e5e5e5; font-size: 1.5rem;
         font-family: monospace; letter-spacing: 0.2em;
         text-align: center; text-transform: uppercase; }
  input:focus { outline: none; border-color: #7aa2f7; }
  button { width: 100%%; margin-top: 1rem; padding: 0.75rem;
           background: #7aa2f7; color: #0a0a0a; border: none;
           border-radius: 8px; font-size: 0.875rem; font-weight: 600;
           cursor: pointer; }
  button:hover { background: #93b4f9; }
  .msg { color: #fbbf24; font-size: 0.875rem; margin-bottom: 1rem;
         text-align: center; }
</style>
</head>
<body>
<div class="card">
  <h1>petboard</h1>
  <p>Enter the code shown in your terminal to authorize this device.</p>
  %s
  <form method="POST" action="/petboard/oauth/device/approve">
    <label for="user_code">device code</label>
    <input type="text" id="user_code" name="user_code"
           placeholder="XXXX-XXXX" maxlength="9" autocomplete="off"
           autofocus>
    <button type="submit">approve</button>
  </form>
</div>
<script>
  // Auto-format: insert dash after 4 chars
  const input = document.getElementById('user_code');
  input.addEventListener('input', function() {
    let v = this.value.replace(/[^A-Za-z0-9]/g, '').toUpperCase();
    if (v.length > 4) v = v.slice(0,4) + '-' + v.slice(4,8);
    this.value = v;
  });
</script>
</body>
</html>`
