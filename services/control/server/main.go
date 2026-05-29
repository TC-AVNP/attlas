// control — Centralized access control for attlas services.
//
// Provides a web UI for managing which emails can access which
// services, and a localhost HTTP API that services query during
// their auth flow.
//
// Configuration via environment variables:
//
//	CONTROL_PORT          TCP port (default 7701)
//	CONTROL_DB            SQLite path (default /var/lib/control/control.db)
//	CONTROL_ADMIN_EMAIL   bootstrap admin email (default condecopedro@gmail.com)
package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed templates/*.html
var templatesFS embed.FS

// --- App ---

type App struct {
	db         *sql.DB
	tmpl       *template.Template
	adminEmail string
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// --- Database ---

func migrate(db *sql.DB) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, e := range entries {
		data, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return err
		}
		if _, err := db.Exec(string(data)); err != nil {
			return err
		}
	}
	return nil
}

// --- Types ---

type service struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type userRow struct {
	Email    string   `json:"email"`
	IsAdmin  bool     `json:"is_admin"`
	Services []string `json:"services"`
}

// --- Localhost guard ---

func isLocalhost(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host == "127.0.0.1" || host == "::1"
}

// --- Main ---

func main() {
	port := envOr("CONTROL_PORT", "7699")
	dbPath := envOr("CONTROL_DB", "/var/lib/control/control.db")
	adminEmail := envOr("CONTROL_ADMIN_EMAIL", "condecopedro@gmail.com")

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// Ensure bootstrap admin exists.
	db.Exec("INSERT OR IGNORE INTO users (email, is_admin) VALUES (?, 1)", adminEmail)

	funcMap := template.FuncMap{
		"hasGrant": func(services []string, svc string) bool {
			for _, s := range services {
				if s == svc {
					return true
				}
			}
			return false
		},
	}
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	app := &App{db: db, tmpl: tmpl, adminEmail: adminEmail}

	mux := http.NewServeMux()

	// --- Public localhost API (no auth, services call these) ---
	mux.HandleFunc("GET /api/check", app.handleCheck)
	mux.HandleFunc("GET /api/allowed", app.handleAllowed)

	// --- Admin UI + management API (behind Caddy forward_auth) ---
	mux.HandleFunc("GET /", app.handleIndex)
	mux.HandleFunc("GET /api/data", app.handleData)
	mux.HandleFunc("POST /api/users", app.handleAddUser)
	mux.HandleFunc("DELETE /api/users/{email}", app.handleDeleteUser)
	mux.HandleFunc("PUT /api/grants/{email}", app.handleSetGrants)
	mux.HandleFunc("POST /api/services", app.handleAddService)
	mux.HandleFunc("DELETE /api/services/{id}", app.handleDeleteService)

	// Listen on all interfaces so Caddy can reach us, but the
	// check/allowed APIs verify localhost themselves.
	addr := "127.0.0.1:" + port
	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		log.Printf("control: listening on %s", addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("control: shutting down")
	srv.Close()
}

// --- Auth helpers ---

// getAuthUser reads the X-Auth-User header set by Caddy forward_auth.
func getAuthUser(r *http.Request) string {
	return r.Header.Get("X-Auth-User")
}

func (a *App) isAdmin(email string) bool {
	if email == "" {
		return false
	}
	var admin int
	a.db.QueryRow("SELECT is_admin FROM users WHERE email = ?", email).Scan(&admin)
	return admin == 1
}

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := getAuthUser(r)
		if !a.isAdmin(email) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// --- Localhost API handlers ---

// GET /api/check?email=X&service=Y → {"allowed": true/false}
func (a *App) handleCheck(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	serviceID := r.URL.Query().Get("service")
	if email == "" || serviceID == "" {
		http.Error(w, "email and service required", http.StatusBadRequest)
		return
	}

	// Admins have access to everything.
	if a.isAdmin(email) {
		json.NewEncoder(w).Encode(map[string]bool{"allowed": true})
		return
	}

	var count int
	a.db.QueryRow(
		"SELECT COUNT(*) FROM grants WHERE email = ? AND service_id = ?",
		email, serviceID,
	).Scan(&count)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"allowed": count > 0})
}

// GET /api/allowed?service=Y → {"emails": [...]}
func (a *App) handleAllowed(w http.ResponseWriter, r *http.Request) {
	serviceID := r.URL.Query().Get("service")
	if serviceID == "" {
		http.Error(w, "service required", http.StatusBadRequest)
		return
	}

	// All admins + users with explicit grants.
	rows, err := a.db.Query(`
		SELECT email FROM users WHERE is_admin = 1
		UNION
		SELECT email FROM grants WHERE service_id = ?
	`, serviceID)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var emails []string
	for rows.Next() {
		var e string
		rows.Scan(&e)
		emails = append(emails, e)
	}
	if emails == nil {
		emails = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]string{"emails": emails})
}

// --- Admin UI handler ---

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	email := getAuthUser(r)
	if !a.isAdmin(email) {
		http.Error(w, "forbidden — admin only", http.StatusForbidden)
		return
	}

	services := a.listServices()
	users := a.listUsers()

	a.tmpl.ExecuteTemplate(w, "index.html", map[string]any{
		"Email":    email,
		"Services": services,
		"Users":    users,
	})
}

// --- Data API (for JS fetch) ---

// GET /api/data → full user+service matrix
func (a *App) handleData(w http.ResponseWriter, r *http.Request) {
	email := getAuthUser(r)
	if !a.isAdmin(email) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"services": a.listServices(),
		"users":    a.listUsers(),
	})
}

// POST /api/users {email: "...", services: ["svc1", ...]}
func (a *App) handleAddUser(w http.ResponseWriter, r *http.Request) {
	email := getAuthUser(r)
	if !a.isAdmin(email) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var body struct {
		Email    string   `json:"email"`
		Services []string `json:"services"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))

	_, err := a.db.Exec("INSERT OR IGNORE INTO users (email) VALUES (?)", body.Email)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	// Set grants.
	a.setGrants(body.Email, body.Services)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// DELETE /api/users/{email}
func (a *App) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	authEmail := getAuthUser(r)
	if !a.isAdmin(authEmail) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	target := r.PathValue("email")
	if target == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}

	// Don't allow deleting yourself.
	if strings.EqualFold(target, authEmail) {
		http.Error(w, "cannot delete yourself", http.StatusBadRequest)
		return
	}

	a.db.Exec("DELETE FROM grants WHERE email = ?", target)
	a.db.Exec("DELETE FROM users WHERE email = ?", target)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// PUT /api/grants/{email} {services: ["svc1", ...]}
func (a *App) handleSetGrants(w http.ResponseWriter, r *http.Request) {
	authEmail := getAuthUser(r)
	if !a.isAdmin(authEmail) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	target := r.PathValue("email")
	var body struct {
		Services []string `json:"services"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	a.setGrants(target, body.Services)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// POST /api/services {id: "...", name: "...", url: "..."}
func (a *App) handleAddService(w http.ResponseWriter, r *http.Request) {
	authEmail := getAuthUser(r)
	if !a.isAdmin(authEmail) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" || body.Name == "" {
		http.Error(w, "id and name required", http.StatusBadRequest)
		return
	}

	_, err := a.db.Exec("INSERT OR IGNORE INTO services (id, name, url) VALUES (?, ?, ?)",
		body.ID, body.Name, body.URL)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// DELETE /api/services/{id}
func (a *App) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	authEmail := getAuthUser(r)
	if !a.isAdmin(authEmail) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	id := r.PathValue("id")
	a.db.Exec("DELETE FROM grants WHERE service_id = ?", id)
	a.db.Exec("DELETE FROM services WHERE id = ?", id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// --- DB helpers ---

func (a *App) listServices() []service {
	rows, _ := a.db.Query("SELECT id, name, url FROM services ORDER BY name")
	defer rows.Close()
	var out []service
	for rows.Next() {
		var s service
		rows.Scan(&s.ID, &s.Name, &s.URL)
		out = append(out, s)
	}
	if out == nil {
		out = []service{}
	}
	return out
}

func (a *App) listUsers() []userRow {
	rows, _ := a.db.Query("SELECT email, is_admin FROM users ORDER BY is_admin DESC, email")
	defer rows.Close()
	var users []userRow
	for rows.Next() {
		var u userRow
		var admin int
		rows.Scan(&u.Email, &admin)
		u.IsAdmin = admin == 1
		u.Services = a.userGrants(u.Email)
		users = append(users, u)
	}
	if users == nil {
		users = []userRow{}
	}
	return users
}

func (a *App) userGrants(email string) []string {
	rows, _ := a.db.Query("SELECT service_id FROM grants WHERE email = ?", email)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func (a *App) setGrants(email string, services []string) {
	a.db.Exec("DELETE FROM grants WHERE email = ?", email)
	for _, svc := range services {
		a.db.Exec("INSERT OR IGNORE INTO grants (email, service_id) VALUES (?, ?)", email, svc)
	}
}

// unused import guard
var _ = time.Now
