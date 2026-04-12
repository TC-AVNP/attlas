package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"splitsies/service"
)

// authGoogle redirects the user to Google's OAuth consent screen.
func (a *API) authGoogle(w http.ResponseWriter, r *http.Request) {
	if a.LocalBypass && a.GoogleClientID == "" {
		// Dev shortcut: no Google configured, bypass OAuth and log in as
		// the first active admin (seeded by SPLITSIES_INITIAL_ADMIN).
		users, err := a.Svc.ListUsers()
		if err != nil {
			writeError(w, err)
			return
		}
		var admin *service.User
		for i := range users {
			if users[i].IsAdmin && users[i].IsActive {
				admin = &users[i]
				break
			}
		}
		if admin == nil {
			http.Error(w, "no admin exists — set SPLITSIES_INITIAL_ADMIN and restart", http.StatusServiceUnavailable)
			return
		}
		token, err := a.Svc.CreateSession(admin.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "splitsies_session",
			Value:    token,
			Path:     "/",
			MaxAge:   30 * 24 * 3600,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	redirectURI := a.BaseURL + "/api/auth/callback"
	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=openid+email+profile&access_type=online",
		url.QueryEscape(a.GoogleClientID),
		url.QueryEscape(redirectURI),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// authCallback handles the Google OAuth callback.
func (a *API) authCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	redirectURI := a.BaseURL + "/api/auth/callback"

	// Exchange code for tokens
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"code":          {code},
		"client_id":     {a.GoogleClientID},
		"client_secret": {a.GoogleClientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		log.Printf("auth: token exchange failed: %v", err)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		log.Printf("auth: bad token response: %s", body)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}

	// Get user info from Google
	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	infoResp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("auth: userinfo failed: %v", err)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}
	defer infoResp.Body.Close()

	var userInfo struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(infoResp.Body).Decode(&userInfo); err != nil {
		log.Printf("auth: decode userinfo: %v", err)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}

	// Check if user is whitelisted
	user, err := a.Svc.FindOrCreateUser(userInfo.Email, userInfo.Name, userInfo.Picture)
	if err != nil {
		// Not whitelisted or deactivated → access denied page
		http.Redirect(w, r, "/access-denied", http.StatusFound)
		return
	}

	// Create session
	token, err := a.Svc.CreateSession(user.ID)
	if err != nil {
		log.Printf("auth: create session: %v", err)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}

	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "splitsies_session",
		Value:    token,
		Path:     "/",
		MaxAge:   30 * 24 * 3600,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// authLogout clears the session.
func (a *API) authLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("splitsies_session")
	if err == nil {
		a.Svc.DeleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "splitsies_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Expires:  time.Unix(0, 0),
	})
	w.WriteHeader(http.StatusNoContent)
}

// authMe returns the current user or 401.
func (a *API) authMe(w http.ResponseWriter, r *http.Request) {
	user, err := a.authenticateRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not logged in"})
		return
	}
	writeJSON(w, http.StatusOK, user)
}
