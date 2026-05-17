// Homelab node inventory and SSH key management proxy.
//
// Proxies to the homelab-bootstrap service on localhost:7695.
// The bootstrap service is mTLS-protected externally (only Pis with
// the golden image cert can reach it via Caddy), but alive-server
// reaches it over loopback without TLS.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const homelabBackend = "http://127.0.0.1:7697"
const homelabSSHKeysFile = "/var/lib/homelab-bootstrap/authorized_keys"

var homelabHTTPClient = &http.Client{Timeout: 5 * time.Second}

func handleHomelabNodes(w http.ResponseWriter, r *http.Request) {
	resp, err := homelabHTTPClient.Get(homelabBackend + "/api/nodes")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleHomelabSSHKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		data, err := os.ReadFile(homelabSSHKeysFile)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"keys": []string{}})
			return
		}
		var keys []string
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				keys = append(keys, line)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"keys": keys})
		return
	}

	// PUT — replace all keys
	var body struct {
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	content := strings.Join(body.Keys, "\n") + "\n"
	if err := os.WriteFile(homelabSSHKeysFile, []byte(content), 0644); err != nil {
		http.Error(w, `{"error":"failed to write keys file"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"keys": body.Keys, "message": "updated"})
}
