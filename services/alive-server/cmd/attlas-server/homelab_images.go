package main

import (
	"io"
	"net/http"
	"strings"
	"time"
)

func handleHomelabTokens(w http.ResponseWriter, r *http.Request) {
	resp, err := homelabHTTPClient.Get(homelabBackend + "/api/tokens")
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

func handleHomelabRevokeToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := http.NewRequest("POST", homelabBackend+"/api/tokens/"+id+"/revoke", nil)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	resp, err := homelabHTTPClient.Do(req)
	if err != nil {
		http.Error(w, `{"error":"bootstrap service unreachable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleHomelabTokenTimeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	resp, err := homelabHTTPClient.Get(homelabBackend + "/api/tokens/" + id + "/timeline")
	if err != nil {
		http.Error(w, `{"error":"bootstrap service unreachable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleHomelabTokenEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := http.NewRequest("POST", homelabBackend+"/api/tokens/"+id+"/event", r.Body)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := homelabHTTPClient.Do(req)
	if err != nil {
		http.Error(w, `{"error":"bootstrap service unreachable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleHomelabDeleteToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := http.NewRequest("DELETE", homelabBackend+"/api/tokens/"+id, nil)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	resp, err := homelabHTTPClient.Do(req)
	if err != nil {
		http.Error(w, `{"error":"bootstrap service unreachable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleHomelabProvision(w http.ResponseWriter, r *http.Request) {
	nodeType := r.PathValue("type")
	if nodeType != "router" && nodeType != "worker" {
		http.Error(w, `{"error":"type must be router or worker"}`, http.StatusBadRequest)
		return
	}

	req, err := http.NewRequest("POST", homelabBackend+"/api/provision/"+nodeType, r.Body)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Long timeout for image build, no buffering for SSE pass-through
	resp, err := (&http.Client{Timeout: 10 * 60 * time.Second}).Do(req)
	if err != nil {
		http.Error(w, `{"error":"image build failed or timed out"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Pass through SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 256)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
}

func handleHomelabDownloadImage(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		http.Error(w, `{"error":"invalid filename"}`, http.StatusBadRequest)
		return
	}

	// Use a long timeout — images are multi-GB.
	client := &http.Client{Timeout: 10 * 60 * time.Second}
	resp, err := client.Get(homelabBackend + "/api/provision/download/" + filename)
	if err != nil {
		http.Error(w, `{"error":"download failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for _, h := range []string{"Content-Type", "Content-Disposition", "Content-Length"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
