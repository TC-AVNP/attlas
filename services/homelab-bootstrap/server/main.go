package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var db *sql.DB

// --- Types ---

type RegisterRequest struct {
	MACAddress string `json:"mac_address"`
	NVMeSerial string `json:"nvme_serial"`
	Hostname   string `json:"hostname"`
	Model      string `json:"model"`
	CPUCores   int    `json:"cpu_cores"`
	MemoryMB   int    `json:"memory_mb"`
	LanIP      string `json:"lan_ip"`
}

type TLSConfig struct {
	ClientCert string `json:"client_cert"`
	ClientKey  string `json:"client_key"`
	CACert     string `json:"ca_cert"`
}

type SSHConfig struct {
	AuthorizedKeys []string `json:"authorized_keys"`
}

type JoinConfig struct {
	APIServerEndpoint string `json:"api_server_endpoint"`
	Token             string `json:"token"`
	CACertHash        string `json:"ca_cert_hash"`
	CertificateKey    string `json:"certificate_key"`
	ControlPlane      bool   `json:"control_plane"`
}

type RegisterResponse struct {
	NodeID   int         `json:"node_id"`
	TokenID  int         `json:"token_id"`
	Hostname string      `json:"hostname"`
	Label    string      `json:"label"`
	Message  string      `json:"message"`
	TLS      *TLSConfig  `json:"tls,omitempty"`
	SSH      *SSHConfig  `json:"ssh,omitempty"`
	Playbook string      `json:"playbook,omitempty"`
	Dotfiles string      `json:"dotfiles,omitempty"`
	Join     *JoinConfig `json:"join,omitempty"`
}

type ImageToken struct {
	ID             int    `json:"id"`
	NodeType       string `json:"node_type"`
	Status         string `json:"status"`
	Label          string `json:"label"`
	CertSerial     string `json:"cert_serial"`
	MACAddress     string `json:"mac_address"`
	Hostname       string `json:"hostname"`
	ImageFilename  string `json:"image_filename,omitempty"`
	DownloadURL    string `json:"download_url,omitempty"`
	CreatedAt      string `json:"created_at"`
	ImageBuiltAt   string `json:"image_built_at,omitempty"`
	DownloadedAt   string `json:"downloaded_at,omitempty"`
	RedeemedAt     string `json:"redeemed_at,omitempty"`
	FirstMetricsAt string `json:"first_metrics_at,omitempty"`
	RevokedAt      string `json:"revoked_at,omitempty"`
}

type Node struct {
	ID              int    `json:"id"`
	MACAddress      string `json:"mac_address"`
	NVMeSerial      string `json:"nvme_serial"`
	Hostname        string `json:"hostname"`
	Model           string `json:"model"`
	CPUCores        int    `json:"cpu_cores"`
	MemoryMB        int    `json:"memory_mb"`
	LanIP           string `json:"lan_ip"`
	CertFingerprint string `json:"cert_fingerprint"`
	RegisteredAt    string `json:"registered_at"`
}

// --- Main ---

func main() {
	port := envOr("HOMELAB_PORT", "7697")
	dbPath := envOr("HOMELAB_DB", "/var/lib/homelab-bootstrap/homelab.db")

	var err error
	db, err = sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	mux := http.NewServeMux()

	// Registration (token-authenticated)
	mux.HandleFunc("POST /api/register/worker", handleRegisterWorker)
	mux.HandleFunc("POST /api/register/router", handleRegisterRouter)

	// Token management
	mux.HandleFunc("POST /api/tokens/create", handleCreateToken)
	mux.HandleFunc("GET /api/tokens", handleListTokens)
	mux.HandleFunc("POST /api/tokens/{id}/revoke", handleRevokeToken)
	mux.HandleFunc("DELETE /api/tokens/{id}", handleDeleteToken)
	mux.HandleFunc("GET /api/tokens/{id}/timeline", handleTokenTimeline)
	mux.HandleFunc("POST /api/tokens/{id}/event", handleTokenEvent)
	mux.HandleFunc("POST /api/event", handleTokenEventByToken)

	// Node inventory
	mux.HandleFunc("GET /api/nodes", handleListNodes)
	mux.HandleFunc("DELETE /api/nodes/{mac}", handleDeregister)
	mux.HandleFunc("GET /api/router-nodes", handleListRouterNodes)
	mux.HandleFunc("DELETE /api/router-nodes/{mac}", handleDeregisterRouter)

	// Config (SSH key refresh for existing nodes)
	mux.HandleFunc("GET /api/config", handleConfig)

	// Image building
	mux.HandleFunc("POST /api/provision/{type}", handleProvisionImage)
	mux.HandleFunc("GET /api/provision/download/{filename}", handleDownloadImage)

	addr := "127.0.0.1:" + port
	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		log.Printf("homelab-bootstrap listening on %s", addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("shutdown complete")
}

// --- Token validation ---

// validateToken extracts the X-Image-Token header, hashes it, and looks up the token.
// Returns the token row and status. On error, writes an HTTP error and returns nil.
func validateToken(w http.ResponseWriter, r *http.Request, expectedType string) *ImageToken {
	rawToken := r.Header.Get("X-Image-Token")
	if rawToken == "" {
		http.Error(w, `{"error":"missing X-Image-Token header"}`, http.StatusUnauthorized)
		return nil
	}

	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	var tok ImageToken
	var redeemedAt, revokedAt sql.NullString
	err := db.QueryRow(`SELECT id, node_type, status, label, cert_serial, mac_address, hostname,
		created_at, redeemed_at, revoked_at FROM image_tokens WHERE token_hash = ?`, tokenHash).
		Scan(&tok.ID, &tok.NodeType, &tok.Status, &tok.Label, &tok.CertSerial,
			&tok.MACAddress, &tok.Hostname, &tok.CreatedAt, &redeemedAt, &revokedAt)
	if err != nil {
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
		return nil
	}
	if redeemedAt.Valid {
		tok.RedeemedAt = redeemedAt.String
	}
	if revokedAt.Valid {
		tok.RevokedAt = revokedAt.String
	}

	if tok.Status == "revoked" {
		http.Error(w, `{"error":"token revoked"}`, http.StatusForbidden)
		return nil
	}

	if tok.NodeType != expectedType {
		http.Error(w, fmt.Sprintf(`{"error":"token is for %s, not %s"}`, tok.NodeType, expectedType), http.StatusForbidden)
		return nil
	}

	return &tok
}

// --- Registration handlers ---

func handleRegisterWorker(w http.ResponseWriter, r *http.Request) {
	tok := validateToken(w, r, "worker")
	if tok == nil {
		return
	}
	handleRegistration(w, r, tok, "worker")
}

func handleRegisterRouter(w http.ResponseWriter, r *http.Request) {
	tok := validateToken(w, r, "router")
	if tok == nil {
		return
	}
	handleRegistration(w, r, tok, "router")
}

func handleRegistration(w http.ResponseWriter, r *http.Request, tok *ImageToken, nodeType string) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.MACAddress == "" || req.Hostname == "" {
		http.Error(w, `{"error":"mac_address and hostname are required"}`, http.StatusBadRequest)
		return
	}

	var tlsConfig *TLSConfig

	if tok.Status == "redeemed" {
		// Return stored cert (idempotent re-registration)
		tlsConfig = &TLSConfig{
			ClientCert: tok.CertSerial, // we'll fix this below
		}
		// Load stored certs from token row
		var certPEM, keyPEM, caPEM string
		db.QueryRow("SELECT cert_pem, key_pem, ca_pem FROM image_tokens WHERE id = ?", tok.ID).
			Scan(&certPEM, &keyPEM, &caPEM)
		tlsConfig = &TLSConfig{
			ClientCert: certPEM,
			ClientKey:  keyPEM,
			CACert:     caPEM,
		}

		// Update hardware info
		table := "nodes"
		if nodeType == "router" {
			table = "router_nodes"
		}
		db.Exec(fmt.Sprintf(`UPDATE %s SET hostname=?, model=?, cpu_cores=?, memory_mb=?, lan_ip=?, registered_at=datetime('now')
			WHERE mac_address=?`, table), req.Hostname, req.Model, req.CPUCores, req.MemoryMB, req.LanIP, req.MACAddress)

		log.Printf("re-registration: %s (%s) — token %d, returning stored cert", req.Hostname, req.MACAddress, tok.ID)
	} else {
		// First registration — generate cert
		tls, certSerial, certFingerprint, err := generateClientCert(tok.Label, req.MACAddress)
		if err != nil {
			log.Printf("cert generation error: %v", err)
			http.Error(w, `{"error":"failed to generate certificate"}`, http.StatusInternalServerError)
			return
		}
		tlsConfig = tls

		// Store cert in token row and mark redeemed
		db.Exec(`UPDATE image_tokens SET status='redeemed', cert_serial=?, cert_pem=?, key_pem=?, ca_pem=?,
			mac_address=?, hostname=?, redeemed_at=datetime('now') WHERE id=?`,
			certSerial, tls.ClientCert, tls.ClientKey, tls.CACert,
			req.MACAddress, req.Hostname, tok.ID)

		// Insert into node inventory
		if nodeType == "worker" {
			db.Exec(`INSERT INTO nodes (mac_address, nvme_serial, hostname, model, cpu_cores, memory_mb, lan_ip, cert_fingerprint, cert_serial)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(mac_address) DO UPDATE SET hostname=excluded.hostname, model=excluded.model,
				cpu_cores=excluded.cpu_cores, memory_mb=excluded.memory_mb, lan_ip=excluded.lan_ip,
				cert_fingerprint=excluded.cert_fingerprint, cert_serial=excluded.cert_serial, registered_at=datetime('now')`,
				req.MACAddress, req.NVMeSerial, req.Hostname, req.Model, req.CPUCores, req.MemoryMB, req.LanIP, certFingerprint, certSerial)
		} else {
			db.Exec(`INSERT INTO router_nodes (mac_address, hostname, model, cpu_cores, memory_mb, lan_ip, subdomain, tunnel_id, tunnel_token, dns_record_id, cert_serial)
				VALUES (?, ?, ?, ?, ?, ?, '', '', '', '', ?)
				ON CONFLICT(mac_address) DO UPDATE SET hostname=excluded.hostname, model=excluded.model,
				cpu_cores=excluded.cpu_cores, memory_mb=excluded.memory_mb, lan_ip=excluded.lan_ip,
				cert_serial=excluded.cert_serial, registered_at=datetime('now')`,
				req.MACAddress, req.Hostname, req.Model, req.CPUCores, req.MemoryMB, req.LanIP, certSerial)
		}

		log.Printf("registered %s: %s (%s) — token %d, cert serial %s", nodeType, req.Hostname, req.MACAddress, tok.ID, certSerial)
	}

	// Build playbook tarball
	playbook, err := createPlaybookTarball(nodeType)
	if err != nil {
		log.Printf("playbook tarball error: %v", err)
		http.Error(w, `{"error":"failed to bundle playbook"}`, http.StatusInternalServerError)
		return
	}

	// Build dotfiles tarball
	dotfiles, err := createDotfilesTarball()
	if err != nil {
		log.Printf("warning: dotfiles tarball error: %v", err)
		// Non-fatal — dotfiles are nice-to-have
	}

	// Get node ID
	var nodeID int
	if nodeType == "worker" {
		db.QueryRow("SELECT id FROM nodes WHERE mac_address = ?", req.MACAddress).Scan(&nodeID)
	} else {
		db.QueryRow("SELECT id FROM router_nodes WHERE mac_address = ?", req.MACAddress).Scan(&nodeID)
	}

	resp := RegisterResponse{
		NodeID:   nodeID,
		Hostname: req.Hostname,
		Label:    tok.Label,
		Message:  "registered",
		TLS:      tlsConfig,
		SSH:      getSSHConfig(),
		Playbook: playbook,
		Dotfiles: dotfiles,
	}

	// Include k8s join config for workers
	if nodeType == "worker" {
		join, err := generateJoinConfig()
		if err != nil {
			log.Printf("warning: could not generate join config: %v", err)
		}
		resp.Join = join
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// --- Token CRUD ---

func handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeType string `json:"node_type"`
		Label    string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.NodeType != "router" && req.NodeType != "worker" {
		http.Error(w, `{"error":"node_type must be 'router' or 'worker'"}`, http.StatusBadRequest)
		return
	}
	if req.Label == "" {
		http.Error(w, `{"error":"label is required"}`, http.StatusBadRequest)
		return
	}

	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}
	rawToken := hex.EncodeToString(tokenBytes)

	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	result, err := db.Exec(`INSERT INTO image_tokens (token_hash, node_type, label) VALUES (?, ?, ?)`,
		tokenHash, req.NodeType, req.Label)
	if err != nil {
		log.Printf("create token error: %v", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	tokenID, _ := result.LastInsertId()
	log.Printf("created %s token %d: %q", req.NodeType, tokenID, req.Label)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":        tokenID,
		"token":     rawToken,
		"node_type": req.NodeType,
		"label":     req.Label,
	})
}

func handleListTokens(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, node_type, status, label, cert_serial, mac_address, hostname,
		image_filename, created_at, image_built_at, downloaded_at, redeemed_at, first_metrics_at, revoked_at
		FROM image_tokens ORDER BY created_at DESC`)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	tokens := []ImageToken{}
	for rows.Next() {
		var tok ImageToken
		var imageBuiltAt, downloadedAt, redeemedAt, firstMetricsAt, revokedAt sql.NullString
		if err := rows.Scan(&tok.ID, &tok.NodeType, &tok.Status, &tok.Label, &tok.CertSerial,
			&tok.MACAddress, &tok.Hostname, &tok.ImageFilename, &tok.CreatedAt,
			&imageBuiltAt, &downloadedAt, &redeemedAt, &firstMetricsAt, &revokedAt); err != nil {
			continue
		}
		if imageBuiltAt.Valid {
			tok.ImageBuiltAt = imageBuiltAt.String
		}
		if downloadedAt.Valid {
			tok.DownloadedAt = downloadedAt.String
		}
		if redeemedAt.Valid {
			tok.RedeemedAt = redeemedAt.String
		}
		if firstMetricsAt.Valid {
			tok.FirstMetricsAt = firstMetricsAt.String
		}
		if revokedAt.Valid {
			tok.RevokedAt = revokedAt.String
		}
		if tok.ImageFilename != "" {
			tok.DownloadURL = "/api/homelab/provision/download/" + tok.ImageFilename
		}
		tokens = append(tokens, tok)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokens)
}

func handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"token id required"}`, http.StatusBadRequest)
		return
	}

	result, err := db.Exec(`UPDATE image_tokens SET status='revoked', revoked_at=datetime('now') WHERE id=? AND status != 'revoked'`, id)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, `{"error":"token not found or already revoked"}`, http.StatusNotFound)
		return
	}

	log.Printf("revoked token %s", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "revoked", "id": id})
}

func handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"token id required"}`, http.StatusBadRequest)
		return
	}

	// Get the image filename before deleting the row
	var imageFilename string
	db.QueryRow(`SELECT image_filename FROM image_tokens WHERE id=?`, id).Scan(&imageFilename)

	result, err := db.Exec(`DELETE FROM image_tokens WHERE id=?`, id)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, `{"error":"token not found"}`, http.StatusNotFound)
		return
	}

	// Delete the image file if it exists
	if imageFilename != "" {
		imgPath := filepath.Join("/var/lib/homelab-bootstrap/images", imageFilename)
		if err := os.Remove(imgPath); err != nil {
			log.Printf("warning: could not delete image %s: %v", imgPath, err)
		} else {
			log.Printf("deleted image file %s", imgPath)
		}
	}

	log.Printf("deleted token %s", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "deleted", "id": id})
}

func handleTokenTimeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"token id required"}`, http.StatusBadRequest)
		return
	}

	var tok ImageToken
	var imageBuiltAt, downloadedAt, redeemedAt, firstMetricsAt, revokedAt sql.NullString
	err := db.QueryRow(`SELECT id, node_type, status, label, cert_serial, mac_address, hostname,
		image_filename, created_at, image_built_at, downloaded_at, redeemed_at, first_metrics_at, revoked_at
		FROM image_tokens WHERE id=?`, id).Scan(
		&tok.ID, &tok.NodeType, &tok.Status, &tok.Label, &tok.CertSerial,
		&tok.MACAddress, &tok.Hostname, &tok.ImageFilename, &tok.CreatedAt,
		&imageBuiltAt, &downloadedAt, &redeemedAt, &firstMetricsAt, &revokedAt)
	if err != nil {
		http.Error(w, `{"error":"token not found"}`, http.StatusNotFound)
		return
	}

	type Event struct {
		Name   string `json:"event"`
		At     string `json:"at,omitempty"`
		Detail string `json:"detail,omitempty"`
	}

	events := []Event{
		{Name: "provisioned", At: tok.CreatedAt},
	}
	if imageBuiltAt.Valid {
		events = append(events, Event{Name: "image_built", At: imageBuiltAt.String})
	}
	if downloadedAt.Valid {
		events = append(events, Event{Name: "downloaded", At: downloadedAt.String})
	}
	if redeemedAt.Valid {
		detail := ""
		if tok.MACAddress != "" {
			detail = "mac=" + tok.MACAddress
		}
		events = append(events, Event{Name: "registered", At: redeemedAt.String, Detail: detail})
	}
	if firstMetricsAt.Valid {
		events = append(events, Event{Name: "first_metrics", At: firstMetricsAt.String})
	}
	if revokedAt.Valid {
		events = append(events, Event{Name: "revoked", At: revokedAt.String})
	}

	// Append custom events from token_events table
	rows, err := db.Query(`SELECT event, detail, created_at FROM token_events WHERE token_id=? ORDER BY created_at`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ev Event
			rows.Scan(&ev.Name, &ev.Detail, &ev.At)
			events = append(events, ev)
		}
	}

	result := map[string]any{
		"id":        tok.ID,
		"label":     tok.Label,
		"node_type": tok.NodeType,
		"status":    tok.Status,
		"events":    events,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func handleTokenEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"token id required"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		Event  string `json:"event"`
		Detail string `json:"detail"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Event == "" {
		http.Error(w, `{"error":"event name required"}`, http.StatusBadRequest)
		return
	}

	// Verify token exists
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM image_tokens WHERE id=?`, id).Scan(&exists); err != nil || exists == 0 {
		http.Error(w, `{"error":"token not found"}`, http.StatusNotFound)
		return
	}

	_, err := db.Exec(`INSERT INTO token_events (token_id, event, detail) VALUES (?, ?, ?)`,
		id, req.Event, req.Detail)
	if err != nil {
		http.Error(w, `{"error":"failed to record event"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("event recorded: token=%s event=%s", id, req.Event)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "recorded", "event": req.Event})
}

func handleTokenEventByToken(w http.ResponseWriter, r *http.Request) {
	rawToken := r.Header.Get("X-Image-Token")
	if rawToken == "" {
		http.Error(w, `{"error":"X-Image-Token header required"}`, http.StatusUnauthorized)
		return
	}

	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	var tokenID int
	if err := db.QueryRow(`SELECT id FROM image_tokens WHERE token_hash=?`, tokenHash).Scan(&tokenID); err != nil {
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		Event  string `json:"event"`
		Detail string `json:"detail"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Event == "" {
		http.Error(w, `{"error":"event name required"}`, http.StatusBadRequest)
		return
	}

	_, err := db.Exec(`INSERT INTO token_events (token_id, event, detail) VALUES (?, ?, ?)`,
		tokenID, req.Event, req.Detail)
	if err != nil {
		http.Error(w, `{"error":"failed to record event"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("event recorded: token=%d event=%s (via token auth)", tokenID, req.Event)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "recorded", "event": req.Event})
}

// --- Node inventory ---

func handleListNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, mac_address, nvme_serial, hostname, model, cpu_cores, memory_mb, lan_ip, cert_fingerprint, registered_at
		FROM nodes ORDER BY registered_at DESC`)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	nodes := []Node{}
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.MACAddress, &n.NVMeSerial, &n.Hostname, &n.Model, &n.CPUCores, &n.MemoryMB, &n.LanIP, &n.CertFingerprint, &n.RegisteredAt); err != nil {
			continue
		}
		nodes = append(nodes, n)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

func handleDeregister(w http.ResponseWriter, r *http.Request) {
	mac := r.PathValue("mac")
	result, err := db.Exec("DELETE FROM nodes WHERE mac_address = ?", mac)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "deregistered", "mac_address": mac})
	log.Printf("deregistered node: %s", mac)
}

func handleListRouterNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, mac_address, hostname, model, cpu_cores, memory_mb, lan_ip, registered_at
		FROM router_nodes ORDER BY registered_at DESC`)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type RouterNode struct {
		ID           int    `json:"id"`
		MACAddress   string `json:"mac_address"`
		Hostname     string `json:"hostname"`
		Model        string `json:"model"`
		CPUCores     int    `json:"cpu_cores"`
		MemoryMB     int    `json:"memory_mb"`
		LanIP        string `json:"lan_ip"`
		RegisteredAt string `json:"registered_at"`
	}

	nodes := []RouterNode{}
	for rows.Next() {
		var n RouterNode
		if err := rows.Scan(&n.ID, &n.MACAddress, &n.Hostname, &n.Model, &n.CPUCores, &n.MemoryMB, &n.LanIP, &n.RegisteredAt); err != nil {
			continue
		}
		nodes = append(nodes, n)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

func handleDeregisterRouter(w http.ResponseWriter, r *http.Request) {
	mac := r.PathValue("mac")
	result, err := db.Exec("DELETE FROM router_nodes WHERE mac_address = ?", mac)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		http.Error(w, `{"error":"router node not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "deregistered", "mac_address": mac})
	log.Printf("deregistered router node: %s", mac)
}

// --- Config ---

func handleConfig(w http.ResponseWriter, r *http.Request) {
	ssh := getSSHConfig()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ssh": ssh})
}

// --- Image provisioning ---

func handleProvisionImage(w http.ResponseWriter, r *http.Request) {
	nodeType := r.PathValue("type")
	if nodeType != "router" && nodeType != "worker" {
		http.Error(w, `{"error":"type must be 'router' or 'worker'"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		Label        string `json:"label"`
		WifiSSID     string `json:"wifi_ssid"`
		WifiPassword string `json:"wifi_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Label == "" {
		http.Error(w, `{"error":"label is required"}`, http.StatusBadRequest)
		return
	}
	// WiFi is optional — workers get ethernet from the router's switch.
	// Only required for routers (which need internet before the SIM modem is configured).

	// Create a token for this image
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}
	rawToken := hex.EncodeToString(tokenBytes)
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	result, err := db.Exec(`INSERT INTO image_tokens (token_hash, node_type, label) VALUES (?, ?, ?)`,
		tokenHash, nodeType, req.Label)
	if err != nil {
		http.Error(w, `{"error":"failed to create token"}`, http.StatusInternalServerError)
		return
	}
	tokenID, _ := result.LastInsertId()

	// Determine build script
	attlasDir := envOr("ATTLAS_DIR", "/home/agnostic-user/iapetus/attlas")
	dirMap := map[string]string{"router": "router-node", "worker": "basic-node"}
	buildDir := filepath.Join(attlasDir, dirMap[nodeType])
	buildScript := filepath.Join(buildDir, "build-image.sh")

	if _, err := os.Stat(buildScript); err != nil {
		http.Error(w, `{"error":"build script not found"}`, http.StatusInternalServerError)
		return
	}

	// Output image path
	imagesDir := "/var/lib/homelab-bootstrap/images"
	os.MkdirAll(imagesDir, 0755)
	filename := fmt.Sprintf("%d-%s-%s.img", time.Now().Unix(), nodeType, sanitizeLabel(req.Label))
	outputPath := filepath.Join(imagesDir, filename)

	// Run build-image.sh, stream progress as SSE
	log.Printf("building %s image %q → %s", nodeType, req.Label, outputPath)
	cmd := exec.Command("bash", buildScript, outputPath)
	cmd.Dir = buildDir
	cmd.Env = append(os.Environ(), "TOKEN="+rawToken, "WIFI_SSID="+req.WifiSSID, "WIFI_PASSWORD="+req.WifiPassword)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, `{"error":"failed to start build"}`, http.StatusInternalServerError)
		return
	}
	cmd.Stderr = cmd.Stdout

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	// Emit token_id immediately so the frontend can show the token in the list
	fmt.Fprintf(w, "data: {\"token_id\":%d,\"label\":\"%s\",\"node_type\":\"%s\",\"progress\":0,\"message\":\"Starting...\"}\n\n", tokenID, req.Label, nodeType)
	flusher.Flush()

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "data: {\"error\":\"failed to start build: %v\"}\n\n", err)
		flusher.Flush()
		return
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PROGRESS:") {
			parts := strings.SplitN(line, ":", 3)
			if len(parts) == 3 {
				fmt.Fprintf(w, "data: {\"progress\":%s,\"message\":\"%s\"}\n\n", parts[1], parts[2])
				flusher.Flush()
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		log.Printf("build error: %v", err)
		fmt.Fprintf(w, "data: {\"error\":\"build failed\"}\n\n")
		flusher.Flush()
		return
	}

	// Build scripts zstd-compress the image, so actual file is filename + ".zst"
	filename += ".zst"
	log.Printf("image built: %s (%s)", filename, req.Label)

	// Persist filename and build timestamp
	db.Exec(`UPDATE image_tokens SET image_filename=?, image_built_at=datetime('now') WHERE id=?`, filename, tokenID)
	doneMsg := map[string]any{
		"token_id":     tokenID,
		"filename":     filename,
		"download_url": "/api/homelab/provision/download/" + filename,
		"label":        req.Label,
		"node_type":    nodeType,
		"done":         true,
	}
	resultJSON, _ := json.Marshal(doneMsg)
	fmt.Fprintf(w, "data: %s\n\n", resultJSON)
	flusher.Flush()
}

func handleDownloadImage(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, `{"error":"invalid filename"}`, http.StatusBadRequest)
		return
	}

	path := filepath.Join("/var/lib/homelab-bootstrap/images", filename)
	if _, err := os.Stat(path); err != nil {
		http.Error(w, `{"error":"image not found"}`, http.StatusNotFound)
		return
	}

	// Record first download timestamp
	db.Exec(`UPDATE image_tokens SET downloaded_at=datetime('now') WHERE image_filename=? AND downloaded_at IS NULL`, filename)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	http.ServeFile(w, r, path)
}

func sanitizeLabel(label string) string {
	s := strings.ToLower(label)
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// --- Helpers ---

func createPlaybookTarball(nodeType string) (string, error) {
	dirMap := map[string]string{"router": "router-node", "worker": "basic-node"}
	attlasDir := envOr("ATTLAS_DIR", "/home/agnostic-user/iapetus/attlas")
	srcDir := filepath.Join(attlasDir, dirMap[nodeType])

	if _, err := os.Stat(srcDir); err != nil {
		return "", fmt.Errorf("directory not found: %s", srcDir)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip binary images and git files
		if strings.HasSuffix(path, ".img") || strings.Contains(path, ".git") {
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(srcDir, path)
		header.Name = rel
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if _, err := tw.Write(data); err != nil {
				return err
			}
		}
		return nil
	})
	tw.Close()
	gz.Close()

	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func createDotfilesTarball() (string, error) {
	// Dotfiles live at ~/iapetus/dotfiels (sibling of attlas)
	attlasDir := envOr("ATTLAS_DIR", "/home/agnostic-user/iapetus/attlas")
	srcDir := filepath.Join(attlasDir, "..", "dotfiels")

	if _, err := os.Stat(srcDir); err != nil {
		return "", fmt.Errorf("dotfiles not found: %s", srcDir)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(path, ".git") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(srcDir, path)
		header.Name = rel
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if _, err := tw.Write(data); err != nil {
				return err
			}
		}
		return nil
	})
	tw.Close()
	gz.Close()

	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func generateClientCert(label, mac string) (*TLSConfig, string, string, error) {
	stateDir := envOr("HOMELAB_STATE_DIR", "/var/lib/homelab-bootstrap")
	caCertPEM, err := os.ReadFile(filepath.Join(stateDir, "ca.crt"))
	if err != nil {
		return nil, "", "", fmt.Errorf("read CA cert: %w", err)
	}
	caKeyPEM, err := os.ReadFile(filepath.Join(stateDir, "ca.key"))
	if err != nil {
		return nil, "", "", fmt.Errorf("read CA key: %w", err)
	}

	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil {
		return nil, "", "", fmt.Errorf("invalid CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse CA cert: %w", err)
	}

	caKeyBlock, _ := pem.Decode(caKeyPEM)
	if caKeyBlock == nil {
		return nil, "", "", fmt.Errorf("invalid CA key PEM")
	}
	var caKey *rsa.PrivateKey
	caKey, err = x509.ParsePKCS1PrivateKey(caKeyBlock.Bytes)
	if err != nil {
		parsed, err2 := x509.ParsePKCS8PrivateKey(caKeyBlock.Bytes)
		if err2 != nil {
			return nil, "", "", fmt.Errorf("parse CA key: %w (pkcs1: %v)", err2, err)
		}
		var ok bool
		caKey, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, "", "", fmt.Errorf("CA key is not RSA")
		}
	}

	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", "", fmt.Errorf("generate key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, "", "", fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   label + "/" + mac,
			Organization: []string{"attlas"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		return nil, "", "", fmt.Errorf("create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)})

	fingerprint := fmt.Sprintf("%x", sha256.Sum256(certDER))
	serial := serialNumber.Text(16)

	return &TLSConfig{
		ClientCert: string(certPEM),
		ClientKey:  string(keyPEM),
		CACert:     string(caCertPEM),
	}, serial, fingerprint, nil
}

func generateJoinConfig() (*JoinConfig, error) {
	tokenOut, err := exec.Command("sudo", "kubeadm", "token", "create", "--ttl", "1h").Output()
	if err != nil {
		return nil, fmt.Errorf("kubeadm token create: %w", err)
	}
	token := strings.TrimSpace(string(tokenOut))

	hashOut, err := exec.Command("bash", "-c",
		"openssl x509 -pubkey -in /etc/kubernetes/pki/ca.crt | "+
			"openssl rsa -pubin -outform der 2>/dev/null | "+
			"openssl dgst -sha256 -hex | sed 's/^.* //'").Output()
	if err != nil {
		return nil, fmt.Errorf("ca cert hash: %w", err)
	}
	caHash := "sha256:" + strings.TrimSpace(string(hashOut))

	certKeyOut, err := exec.Command("sudo", "kubeadm", "init", "phase", "upload-certs", "--upload-certs").Output()
	if err != nil {
		return nil, fmt.Errorf("upload-certs: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(certKeyOut)), "\n")
	certKey := strings.TrimSpace(lines[len(lines)-1])

	endpoint := envOr("HOMELAB_API_ENDPOINT", "")
	if endpoint == "" {
		epOut, err := exec.Command("kubectl", "config", "view",
			"--minify", "-o", "jsonpath={.clusters[0].cluster.server}").Output()
		if err != nil {
			return nil, fmt.Errorf("get api endpoint: %w", err)
		}
		endpoint = strings.TrimSpace(string(epOut))
	}

	// kubeadm join expects host:port, not a full URL
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")

	return &JoinConfig{
		APIServerEndpoint: endpoint,
		Token:             token,
		CACertHash:        caHash,
		CertificateKey:    certKey,
		ControlPlane:      false,
	}, nil
}

func getSSHConfig() *SSHConfig {
	if keys := os.Getenv("HOMELAB_SSH_KEYS"); keys != "" {
		var parsed []string
		for _, k := range strings.Split(keys, "\n") {
			k = strings.TrimSpace(k)
			if k != "" && !strings.HasPrefix(k, "#") {
				parsed = append(parsed, k)
			}
		}
		if len(parsed) > 0 {
			return &SSHConfig{AuthorizedKeys: parsed}
		}
	}

	filePath := envOr("HOMELAB_SSH_KEYS_FILE", "/var/lib/homelab-bootstrap/authorized_keys")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			keys = append(keys, line)
		}
	}
	if len(keys) > 0 {
		return &SSHConfig{AuthorizedKeys: keys}
	}
	return nil
}

// --- Migration ---

func migrate(db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := migrationsFS.ReadFile(filepath.Join("migrations", entry.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			// ALTER TABLE ADD COLUMN is not idempotent in SQLite —
			// ignore "duplicate column" errors on re-run.
			if strings.Contains(err.Error(), "duplicate column") {
				continue
			}
			return fmt.Errorf("exec %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
