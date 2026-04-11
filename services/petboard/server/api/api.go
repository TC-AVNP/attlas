// Package api implements the REST HTTP surface of petboard. Every
// handler delegates business logic to the service package; this layer
// is only about decoding requests, mapping errors to HTTP status codes,
// and emitting JSON responses.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"petboard/service"
)

// API bundles the dependencies every handler needs.
type API struct {
	Svc *service.Service
}

// Register attaches every petboard REST route to the given mux under
// the /api prefix. Caller is responsible for any outer path prefix
// (e.g. Caddy mapping /petboard/* to this server) — routes are
// registered with their final on-server paths.
func (a *API) Register(mux *http.ServeMux) {
	// Projects
	mux.HandleFunc("GET /api/projects", a.listProjects)
	mux.HandleFunc("POST /api/projects", a.createProject)
	mux.HandleFunc("GET /api/projects/{slug}", a.getProject)
	mux.HandleFunc("PATCH /api/projects/{slug}", a.updateProject)
	mux.HandleFunc("DELETE /api/projects/{slug}", a.deleteProject)

	// Nested under projects
	mux.HandleFunc("POST /api/projects/{slug}/features", a.createFeature)
	mux.HandleFunc("POST /api/projects/{slug}/effort", a.logEffort)

	// Features
	mux.HandleFunc("PATCH /api/features/{id}", a.updateFeature)
	mux.HandleFunc("DELETE /api/features/{id}", a.deleteFeature)
}

// --- shared helpers ----------------------------------------------------

// writeJSON serializes v as JSON with the given status. If encoding
// fails we log it but there's nothing useful to send to the client at
// that point — the headers are already on the wire.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: encode response: %v", err)
	}
}

// writeError maps a service error to an appropriate HTTP status and a
// JSON body. Unknown errors become 500.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, service.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, service.ErrDuplicateSlug):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, service.ErrInvalidTransition):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		log.Printf("api: unexpected error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}

// decodeBody reads a JSON body into dst. Returns an error suitable for
// writeError (wrapped as ErrInvalidInput) on malformed JSON.
func decodeBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return wrapInvalid("malformed JSON body: " + err.Error())
	}
	return nil
}

// wrapInvalid builds an ErrInvalidInput-wrapped error with a message.
func wrapInvalid(msg string) error {
	return &wrappedInvalid{msg: msg}
}

type wrappedInvalid struct{ msg string }

func (e *wrappedInvalid) Error() string { return "invalid input: " + e.msg }
func (e *wrappedInvalid) Unwrap() error { return service.ErrInvalidInput }

// parseInt64 is a thin wrapper so path-value parsing reads cleanly at
// call sites.
func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}
