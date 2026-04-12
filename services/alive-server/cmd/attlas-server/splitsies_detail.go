// Splitsies service detail page handlers.
//
// The attlas alive dashboard owns super-admin operations for splitsies
// (adding/removing users, promoting/demoting admins). It reaches the
// splitsies backend via loopback on 127.0.0.1:7692, bypassing both
// Caddy and splitsies-gateway. Splitsies trusts loopback requests
// without X-Forwarded-For as a synthetic "system" admin — see
// services/splitsies/server/api/api.go#authenticateRequest.
//
// Every handler here:
//   1. Accepts a request authenticated by alive-server's Google OAuth
//      (attlas.uk whitelist).
//   2. Translates it to a plain HTTP call against splitsies.
//   3. Proxies the body back to the frontend.
//
// Splitsies errors propagate as-is (same status codes and JSON bodies).
package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

const splitsiesBackend = "http://127.0.0.1:7692"

var splitsiesHTTPClient = &http.Client{Timeout: 10 * time.Second}

// proxyToSplitsies forwards the given request to the splitsies backend
// over loopback (so it trusts us as system admin) and writes the
// response back to w. Request body is read into memory — fine for the
// small JSON payloads these endpoints handle.
func proxyToSplitsies(w http.ResponseWriter, method, path string, body []byte) {
	req, err := http.NewRequest(method, splitsiesBackend+path, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	// Deliberately do NOT forward X-Forwarded-For — we want splitsies to
	// treat this as a trusted loopback call.
	resp, err := splitsiesHTTPClient.Do(req)
	if err != nil {
		http.Error(w, "splitsies backend unreachable: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleSplitsiesDetail(w http.ResponseWriter, r *http.Request) {
	proxyToSplitsies(w, http.MethodGet, "/api/users", nil)
}

func handleSplitsiesAddUser(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	proxyToSplitsies(w, http.MethodPost, "/api/users", body)
}

func handleSplitsiesPatchUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	proxyToSplitsies(w, http.MethodPatch, fmt.Sprintf("/api/users/%s", id), body)
}

func handleSplitsiesRemoveUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	proxyToSplitsies(w, http.MethodDelete, fmt.Sprintf("/api/users/%s", id), nil)
}
