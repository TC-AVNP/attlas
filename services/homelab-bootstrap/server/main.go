package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"log"
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

type RegisterRequest struct {
	MACAddress string `json:"mac_address"`
	NVMeSerial string `json:"nvme_serial"`
	Hostname   string `json:"hostname"`
	Model      string `json:"model"`
	CPUCores   int    `json:"cpu_cores"`
	MemoryMB   int    `json:"memory_mb"`
	LanIP      string `json:"lan_ip"`
}

type JoinConfig struct {
	APIServerEndpoint string `json:"api_server_endpoint"`
	Token             string `json:"token"`
	CACertHash        string `json:"ca_cert_hash"`
	CertificateKey    string `json:"certificate_key"`
	ControlPlane      bool   `json:"control_plane"`
}

type SSHConfig struct {
	AuthorizedKeys []string `json:"authorized_keys"`
}

type RegisterResponse struct {
	NodeID   int         `json:"node_id"`
	Hostname string      `json:"hostname"`
	Message  string      `json:"message"`
	Join     *JoinConfig `json:"join,omitempty"`
	SSH      *SSHConfig  `json:"ssh,omitempty"`
}

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
	mux.HandleFunc("POST /api/register", handleRegister)
	mux.HandleFunc("GET /api/nodes", handleListNodes)
	mux.HandleFunc("GET /api/config", handleConfig)
	mux.HandleFunc("DELETE /api/nodes/{mac}", handleDeregister)

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

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if req.MACAddress == "" || req.Hostname == "" {
		http.Error(w, `{"error":"mac_address and hostname are required"}`, http.StatusBadRequest)
		return
	}

	// Extract client cert fingerprint from Caddy header
	certFingerprint := r.Header.Get("X-Client-Cert-Fingerprint")
	if certFingerprint == "" {
		certFingerprint = "unknown"
	}

	// Upsert: if MAC already exists, update the record
	_, err := db.Exec(`
		INSERT INTO nodes (mac_address, nvme_serial, hostname, model, cpu_cores, memory_mb, lan_ip, cert_fingerprint)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(mac_address) DO UPDATE SET
			nvme_serial = excluded.nvme_serial,
			hostname = excluded.hostname,
			model = excluded.model,
			cpu_cores = excluded.cpu_cores,
			memory_mb = excluded.memory_mb,
			lan_ip = excluded.lan_ip,
			cert_fingerprint = excluded.cert_fingerprint,
			registered_at = datetime('now')
	`, req.MACAddress, req.NVMeSerial, req.Hostname, req.Model, req.CPUCores, req.MemoryMB, req.LanIP, certFingerprint)
	if err != nil {
		log.Printf("register error: %v", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	var nodeID int
	db.QueryRow("SELECT id FROM nodes WHERE mac_address = ?", req.MACAddress).Scan(&nodeID)

	// Generate kubeadm join credentials for this node
	join, err := generateJoinConfig()
	if err != nil {
		log.Printf("warning: could not generate join config: %v", err)
		// Registration succeeds even if join token generation fails —
		// the Pi can retry or join manually later
	}

	// Load SSH authorized keys from env/file
	ssh := getSSHConfig()

	resp := RegisterResponse{
		NodeID:   nodeID,
		Hostname: req.Hostname,
		Message:  "registered",
		Join:     join,
		SSH:      ssh,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
	log.Printf("registered node: %s (%s) — %s, %d cores, %d MB RAM", req.Hostname, req.MACAddress, req.Model, req.CPUCores, req.MemoryMB)
}

// generateJoinConfig creates a short-lived kubeadm join token and
// returns the full join configuration the Pi needs to join the cluster.
// Uses sudo for kubeadm commands (configured in sudoers by install.sh).
func generateJoinConfig() (*JoinConfig, error) {
	// Create a token with 1h TTL (enough for the Pi to join)
	tokenOut, err := exec.Command("sudo", "kubeadm", "token", "create", "--ttl", "1h").Output()
	if err != nil {
		return nil, fmt.Errorf("kubeadm token create: %w", err)
	}
	token := strings.TrimSpace(string(tokenOut))

	// Get the CA cert hash for discovery
	hashOut, err := exec.Command("bash", "-c",
		"openssl x509 -pubkey -in /etc/kubernetes/pki/ca.crt | "+
			"openssl rsa -pubin -outform der 2>/dev/null | "+
			"openssl dgst -sha256 -hex | sed 's/^.* //'").Output()
	if err != nil {
		return nil, fmt.Errorf("ca cert hash: %w", err)
	}
	caHash := "sha256:" + strings.TrimSpace(string(hashOut))

	// Upload certs and get the certificate key (for control-plane join)
	certKeyOut, err := exec.Command("sudo", "kubeadm", "init", "phase", "upload-certs", "--upload-certs").Output()
	if err != nil {
		return nil, fmt.Errorf("upload-certs: %w", err)
	}
	// The certificate key is the last line of output
	lines := strings.Split(strings.TrimSpace(string(certKeyOut)), "\n")
	certKey := strings.TrimSpace(lines[len(lines)-1])

	// Get the API server endpoint — use env var (set in systemd unit)
	// or fall back to kubectl config
	endpoint := envOr("HOMELAB_API_ENDPOINT", "")
	if endpoint == "" {
		epOut, err := exec.Command("kubectl", "config", "view",
			"--minify", "-o", "jsonpath={.clusters[0].cluster.server}").Output()
		if err != nil {
			return nil, fmt.Errorf("get api endpoint: %w", err)
		}
		endpoint = strings.TrimSpace(string(epOut))
	}

	return &JoinConfig{
		APIServerEndpoint: endpoint,
		Token:             token,
		CACertHash:        caHash,
		CertificateKey:    certKey,
		ControlPlane:      true,
	}, nil
}

// handleConfig returns the current node configuration (SSH keys, secrets).
// Pis poll this every 30s to pick up key changes.
func handleConfig(w http.ResponseWriter, r *http.Request) {
	ssh := getSSHConfig()
	resp := map[string]any{
		"ssh":        ssh,
		"github_pat": envOr("HOMELAB_GITHUB_PAT", ""),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// getSSHConfig reads authorized keys from HOMELAB_SSH_KEYS env var (newline-separated)
// or from a file at HOMELAB_SSH_KEYS_FILE (one key per line).
func getSSHConfig() *SSHConfig {
	// Try env var first
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

	// Try file
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

func handleListNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, mac_address, nvme_serial, hostname, model, cpu_cores, memory_mb, lan_ip, cert_fingerprint, registered_at FROM nodes ORDER BY registered_at DESC")
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
	if mac == "" {
		http.Error(w, `{"error":"mac address required"}`, http.StatusBadRequest)
		return
	}

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
