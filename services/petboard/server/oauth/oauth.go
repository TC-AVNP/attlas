// Package oauth implements a minimal OAuth 2.1 + PKCE authorization
// server scoped to the MCP use case. It piggy-backs on the existing
// Caddy + alive-server Google login: the /authorize endpoint is
// NOT in the public-paths registry, so any unauthenticated browser
// hitting it gets a 302 to /oauth2/login first. After Google login,
// the request bounces back through alive-server's return-URL preserver
// and reaches us with a valid session cookie + X-Forwarded-User header.
//
// We persist:
//   - clients (RFC 7591 dynamic registration)
//   - one-shot auth codes (with PKCE challenge + redirect_uri)
//   - 30-day bearer access tokens
//
// Tokens and codes are stored as SHA-256 hashes; the raw values only
// ever exist in HTTP responses to the legitimate caller.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ----- public types -------------------------------------------------------

// Server bundles the dependencies for all OAuth handlers.
type Server struct {
	DB *sql.DB
	// Issuer is the canonical https://attlas.uk/petboard URL. Used in
	// the well-known metadata endpoints and as the audience for tokens.
	Issuer string
	// LocalBypass allows requests that arrive directly on the loopback
	// interface (i.e. NOT through Caddy — distinguished by the absence
	// of an X-Forwarded-For header) to skip the full OAuth flow. This
	// exists so the operator can drive the MCP server from a local
	// shell or from a Claude Code session running in /terminal without
	// needing a browser to complete Google login.
	//
	// Public-internet requests always go through Caddy, which sets
	// X-Forwarded-For unconditionally — so this flag cannot weaken the
	// auth on the real attlas.uk surface. See BearerMiddleware.
	LocalBypass bool
}

// New constructs a Server.
func New(db *sql.DB, issuer string, localBypass bool) *Server {
	return &Server{DB: db, Issuer: strings.TrimRight(issuer, "/"), LocalBypass: localBypass}
}

// TokenInfo is what BearerMiddleware attaches to the request context
// for downstream handlers (the MCP package consumes this).
type TokenInfo struct {
	ClientID  string
	UserEmail string
	Scope     string
	ExpiresAt int64
}

// ----- routes registration ------------------------------------------------

// Register attaches every OAuth + well-known route to the given mux.
// Caller is responsible for the outer /petboard prefix.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleAuthServerMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.handleProtectedResourceMetadata)
	mux.HandleFunc("POST /oauth/register", s.handleRegister)
	mux.HandleFunc("GET /oauth/authorize", s.handleAuthorize)
	mux.HandleFunc("POST /oauth/token", s.handleToken)
}

// ----- well-known metadata ------------------------------------------------

func (s *Server) handleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.Issuer,
		"authorization_endpoint":                s.Issuer + "/oauth/authorize",
		"token_endpoint":                        s.Issuer + "/oauth/token",
		"registration_endpoint":                 s.Issuer + "/oauth/register",
		"scopes_supported":                      []string{"petboard:read", "petboard:write"},
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"}, // public clients (PKCE)
	})
}

func (s *Server) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":              s.Issuer,
		"authorization_servers": []string{s.Issuer},
		"bearer_methods_supported": []string{"header"},
	})
}

// ----- dynamic client registration (RFC 7591) -----------------------------

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(body.RedirectURIs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "redirect_uris is required")
		return
	}
	for _, u := range body.RedirectURIs {
		if !isAllowedRedirectURI(u) {
			writeError(w, http.StatusBadRequest, "invalid_request",
				"redirect_uri must be http://127.0.0.1[:port][/path] or http://localhost[:port][/path]")
			return
		}
	}

	clientID := randomString(24)
	now := time.Now().Unix()
	urisJSON, _ := json.Marshal(body.RedirectURIs)

	if _, err := s.DB.Exec(
		`INSERT INTO oauth_clients(id, name, redirect_uris, created_at) VALUES (?,?,?,?)`,
		clientID, body.ClientName, string(urisJSON), now,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":         clientID,
		"client_id_issued_at": now,
		"client_name":       body.ClientName,
		"redirect_uris":     body.RedirectURIs,
		"token_endpoint_auth_method": "none",
	})
}

// ----- authorize ----------------------------------------------------------

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	respType := q.Get("response_type")
	state := q.Get("state")
	scope := q.Get("scope")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	if respType != "code" {
		writeError(w, http.StatusBadRequest, "unsupported_response_type", "only response_type=code is supported")
		return
	}
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		writeError(w, http.StatusBadRequest, "invalid_request", "PKCE S256 challenge is required")
		return
	}

	// Validate client and redirect_uri
	allowedURIs, err := s.lookupClientRedirectURIs(clientID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_client", err.Error())
		return
	}
	if !contains(allowedURIs, redirectURI) {
		writeError(w, http.StatusBadRequest, "invalid_request", "redirect_uri does not match registered URIs")
		return
	}

	// Capture authenticated user from the upstream alive-server.
	// alive-server forwards X-Forwarded-User; if it's missing we treat
	// the request as unauthenticated (which it shouldn't be — Caddy
	// should have redirected to Google login already).
	userEmail := r.Header.Get("X-Forwarded-User")
	if userEmail == "" {
		// Try the alternate header names alive-server might set.
		userEmail = r.Header.Get("X-Auth-User")
	}
	if userEmail == "" {
		// Last resort: a generic value. The session check upstream is
		// what really matters; this just records *who* approved.
		userEmail = "unknown"
	}

	// Mint code, store hash + parameters
	code := randomString(32)
	codeHash := sha256Hex(code)
	expiresAt := time.Now().Add(10 * time.Minute).Unix()
	if _, err := s.DB.Exec(
		`INSERT INTO oauth_auth_codes(
			code_hash, client_id, code_challenge, code_challenge_method,
			redirect_uri, scope, user_email, expires_at, used
		) VALUES (?,?,?,?,?,?,?,?,0)`,
		codeHash, clientID, codeChallenge, codeChallengeMethod,
		redirectURI, scope, userEmail, expiresAt,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	// Build redirect URL
	u, err := url.Parse(redirectURI)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "redirect_uri parse: "+err.Error())
		return
	}
	rq := u.Query()
	rq.Set("code", code)
	if state != "" {
		rq.Set("state", state)
	}
	u.RawQuery = rq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// ----- token --------------------------------------------------------------

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	grantType := r.Form.Get("grant_type")
	code := r.Form.Get("code")
	redirectURI := r.Form.Get("redirect_uri")
	clientID := r.Form.Get("client_id")
	codeVerifier := r.Form.Get("code_verifier")

	if grantType != "authorization_code" {
		writeError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	if code == "" || codeVerifier == "" || clientID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "missing code, code_verifier, or client_id")
		return
	}

	codeHash := sha256Hex(code)
	row := s.DB.QueryRow(
		`SELECT client_id, code_challenge, redirect_uri, scope, user_email, expires_at, used
		   FROM oauth_auth_codes WHERE code_hash = ?`,
		codeHash,
	)
	var (
		dbClientID    string
		dbChallenge   string
		dbRedirectURI string
		dbScope       string
		dbUserEmail   string
		dbExpires     int64
		dbUsed        int
	)
	if err := row.Scan(&dbClientID, &dbChallenge, &dbRedirectURI, &dbScope, &dbUserEmail, &dbExpires, &dbUsed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "invalid_grant", "unknown code")
			return
		}
		writeError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	if dbUsed == 1 {
		writeError(w, http.StatusBadRequest, "invalid_grant", "code already used")
		return
	}
	if time.Now().Unix() > dbExpires {
		writeError(w, http.StatusBadRequest, "invalid_grant", "code expired")
		return
	}
	if dbClientID != clientID {
		writeError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}
	if dbRedirectURI != redirectURI {
		writeError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}
	// Verify PKCE: S256(code_verifier) must equal code_challenge.
	if pkceS256(codeVerifier) != dbChallenge {
		writeError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}

	// Mark code used (one-shot)
	if _, err := s.DB.Exec(`UPDATE oauth_auth_codes SET used = 1 WHERE code_hash = ?`, codeHash); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	// Mint a 30-day access token
	token := randomString(40)
	tokenHash := sha256Hex(token)
	now := time.Now().Unix()
	expires := now + 30*24*3600
	if _, err := s.DB.Exec(
		`INSERT INTO oauth_access_tokens(token_hash, client_id, scope, user_email, created_at, expires_at)
		 VALUES (?,?,?,?,?,?)`,
		tokenHash, clientID, dbScope, dbUserEmail, now, expires,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   30 * 24 * 3600,
		"scope":        dbScope,
	})
}

// ----- bearer middleware --------------------------------------------------

// BearerMiddleware validates the Authorization: Bearer header against
// oauth_access_tokens. On failure it returns 401 with the appropriate
// WWW-Authenticate header pointing at the resource metadata so MCP
// clients can discover the auth flow. On success it stores TokenInfo
// in the request context for downstream handlers.
type ctxKey int

const tokenCtxKey ctxKey = 1

func (s *Server) BearerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Local-loopback bypass. If the request did not come through
		// Caddy (X-Forwarded-For is empty) and the operator has opted
		// in via PETBOARD_LOCAL_BYPASS=1, we skip the bearer check and
		// inject a sentinel TokenInfo. This is the escape hatch the
		// operator uses to drive MCP from /terminal without a browser.
		if s.LocalBypass && r.Header.Get("X-Forwarded-For") == "" {
			info := &TokenInfo{
				ClientID:  "local-bypass",
				UserEmail: "local@petboard",
				Scope:     "petboard:read petboard:write",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
			}
			ctx := context.WithValue(r.Context(), tokenCtxKey, info)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			s.writeAuthChallenge(w, "missing bearer token")
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		tokenHash := sha256Hex(token)
		row := s.DB.QueryRow(
			`SELECT client_id, scope, user_email, expires_at FROM oauth_access_tokens WHERE token_hash = ?`,
			tokenHash,
		)
		var info TokenInfo
		if err := row.Scan(&info.ClientID, &info.Scope, &info.UserEmail, &info.ExpiresAt); err != nil {
			s.writeAuthChallenge(w, "invalid token")
			return
		}
		if time.Now().Unix() > info.ExpiresAt {
			s.writeAuthChallenge(w, "token expired")
			return
		}
		// Touch last_used_at — best effort, ignore failure
		_, _ = s.DB.Exec(
			`UPDATE oauth_access_tokens SET last_used_at = ? WHERE token_hash = ?`,
			time.Now().Unix(), tokenHash,
		)
		ctx := context.WithValue(r.Context(), tokenCtxKey, &info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) writeAuthChallenge(w http.ResponseWriter, errDesc string) {
	w.Header().Set(
		"WWW-Authenticate",
		fmt.Sprintf(`Bearer realm="petboard", error="invalid_token", error_description=%q, resource_metadata=%q`,
			errDesc, s.Issuer+"/.well-known/oauth-protected-resource"),
	)
	writeError(w, http.StatusUnauthorized, "invalid_token", errDesc)
}

// ----- helpers ------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, errCode, desc string) {
	writeJSON(w, status, map[string]string{
		"error":             errCode,
		"error_description": desc,
	})
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// isAllowedRedirectURI restricts dynamic client registration to local
// loopback URIs, per OAuth 2.1's recommendations for native/PKCE
// clients. This means Claude Code's local callback listener works but
// nobody can register a public-internet redirect that would let them
// receive other users' codes.
func isAllowedRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func (s *Server) lookupClientRedirectURIs(clientID string) ([]string, error) {
	if clientID == "" {
		return nil, errors.New("client_id required")
	}
	var raw string
	if err := s.DB.QueryRow(
		`SELECT redirect_uris FROM oauth_clients WHERE id = ?`, clientID,
	).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("unknown client")
		}
		return nil, err
	}
	var uris []string
	if err := json.Unmarshal([]byte(raw), &uris); err != nil {
		return nil, err
	}
	return uris, nil
}

// TokenFromContext returns the TokenInfo attached by BearerMiddleware,
// or nil if the request did not pass through it.
func TokenFromContext(ctx context.Context) *TokenInfo {
	v, _ := ctx.Value(tokenCtxKey).(*TokenInfo)
	return v
}
