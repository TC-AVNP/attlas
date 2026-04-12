// Package config loads the OAuth + admin-key JSON that sits alongside
// the alive-svc home directory, and the HMAC session secret.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// OAuthConfig is the shape of ~/.attlas-server-config.json — Google
// OAuth client + allowed-email whitelist + the Anthropic admin key
// used by the cost-report fetcher.
type OAuthConfig struct {
	ClientID          string   `json:"google_oauth_client_id"`
	ClientSecret      string   `json:"google_oauth_client_secret"`
	AllowedEmails     []string `json:"allowed_emails"`
	AnthropicAdminKey string   `json:"anthropic_admin_key"`
}

// Load reads ~/.attlas-server-config.json. Returns nil (with a warning
// logged) if the file is missing, malformed, or missing client
// credentials — callers must tolerate that shape, as alive-server
// deliberately boots without OAuth while someone is still provisioning
// the secret.
func Load() *OAuthConfig {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".attlas-server-config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("WARNING: OAuth config not found at %s: %v", path, err)
		return nil
	}

	var cfg OAuthConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("WARNING: Failed to parse OAuth config: %v", err)
		return nil
	}

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		log.Printf("WARNING: OAuth config missing client_id or client_secret")
		return nil
	}

	log.Printf("OAuth2 configured with %d allowed email(s)", len(cfg.AllowedEmails))
	return &cfg
}

// LoadOrCreateSecret returns the HMAC key used to sign session cookies.
// Persisted to ~/.attlas-session-secret so restarts don't invalidate
// every active session; rotates on demand just by deleting the file.
// The on-disk format is hex-encoded so it can be inspected safely.
func LoadOrCreateSecret() []byte {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".attlas-session-secret")

	data, err := os.ReadFile(path)
	if err == nil && len(data) >= 32 {
		return data
	}

	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	encoded := []byte(hex.EncodeToString(secret))
	_ = os.WriteFile(path, encoded, 0600)
	return encoded
}
