// David's Checklist service detail page handlers.
//
// Same pattern as splitsies_detail.go — the attlas alive dashboard owns
// super-admin operations for david-s-checklist (listing users, adding/
// removing, promoting/demoting admins). It reaches the david backend
// via loopback on 127.0.0.1:7693. David trusts loopback requests
// without X-Forwarded-For as admin (DAVID_LOCAL_BYPASS=1 or the
// requireAdmin middleware sees the env-var admin email).
//
// Backend routes (all loopback-trusted against the david backend):
//
//	GET    /api/services/david-s-checklist              list users
//	POST   /api/services/david-s-checklist/users        add user
//	PATCH  /api/services/david-s-checklist/users/:email promote/demote
//	DELETE /api/services/david-s-checklist/users/:email revoke access
package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

const davidBackend = "http://127.0.0.1:7693"

var davidHTTPClient = &http.Client{Timeout: 10 * time.Second}

func proxyToDavid(w http.ResponseWriter, method, path string, body []byte) {
	req, err := http.NewRequest(method, davidBackend+path, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := davidHTTPClient.Do(req)
	if err != nil {
		http.Error(w, "david backend unreachable: "+err.Error(), http.StatusBadGateway)
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

func handleDavidDetail(w http.ResponseWriter, r *http.Request) {
	proxyToDavid(w, http.MethodGet, "/api/users", nil)
}

func handleDavidAddUser(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	proxyToDavid(w, http.MethodPost, "/api/users", body)
}

func handleDavidPatchUser(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	proxyToDavid(w, http.MethodPatch, fmt.Sprintf("/api/users/%s", email), body)
}

func handleDavidRemoveUser(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	proxyToDavid(w, http.MethodDelete, fmt.Sprintf("/api/users/%s", email), nil)
}
