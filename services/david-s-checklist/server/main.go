// david-s-checklist — task assignment and handover tool.
//
// Two roles:
//   - Admin (DAVID_ADMIN_EMAIL) can create handovers, assign tasks to people.
//   - Users (anyone with tasks assigned) can view and tick off their tasks.
//
// Configuration via environment variables:
//
//	DAVID_PORT               TCP port (default 7693)
//	DAVID_DB                 SQLite path (default /var/lib/david-s-checklist/david.db)
//	DAVID_TODOS              path to todos.json for initial seed (default ./todos.json)
//	DAVID_ADMIN_EMAIL        admin email (default condecopedro@gmail.com)
//	DAVID_KNOWN_EMAILS       comma-separated list of known assignee emails
//	DAVID_GOOGLE_CLIENT_ID   Google OAuth client ID
//	DAVID_GOOGLE_SECRET      Google OAuth client secret
//	DAVID_BASE_URL           canonical base URL (e.g. https://david.attlas.uk)
//	DAVID_LOCAL_BYPASS       set "1" to skip auth on loopback (dev)
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed templates/*.html
var templatesFS embed.FS

type contextKey string

const emailCtxKey contextKey = "email"

type Todo struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Assignee    string `json:"assignee"`
	HandoverID  *int   `json:"handover_id,omitempty"`
	Position    int    `json:"position"`
	Completed   bool   `json:"completed"`
	CompletedAt string `json:"completed_at,omitempty"`
}

type Handover struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Assignee    string `json:"assignee"`
	Archived    bool   `json:"archived"`
	CreatedAt   string `json:"created_at"`
	Todos       []Todo `json:"todos"`
	Done        int
	Total       int
	Percent     int
}

type PageData struct {
	Handovers         []Handover
	ArchivedHandovers []Handover
	StandaloneTodos   []Todo
	Email             string
	IsAdmin           bool
	Done              int
	Total             int
	Percent           int
	Assignees         []string
}

type App struct {
	db             *sql.DB
	tmpl           *template.Template
	todosPath      string
	adminEmail     string
	knownEmails    []string
	clientID       string
	clientSecret   string
	baseURL        string
	localBypass    bool
	telegramToken  string
	telegramChatID string
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	port := envInt("DAVID_PORT", 7693)
	dbPath := envString("DAVID_DB", "/var/lib/david-s-checklist/david.db")

	conn, err := openDB(dbPath)
	if err != nil {
		log.Fatalf("david: open db: %v", err)
	}
	defer conn.Close()

	baseURL := envString("DAVID_BASE_URL", "")
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", port)
	}

	var knownEmails []string
	if raw := envString("DAVID_KNOWN_EMAILS", ""); raw != "" {
		for _, e := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(strings.ToLower(e)); trimmed != "" {
				knownEmails = append(knownEmails, trimmed)
			}
		}
	}

	app := &App{
		db:             conn,
		tmpl:           template.Must(template.ParseFS(templatesFS, "templates/*.html")),
		todosPath:      envString("DAVID_TODOS", "todos.json"),
		adminEmail:     envString("DAVID_ADMIN_EMAIL", "condecopedro@gmail.com"),
		knownEmails:    knownEmails,
		telegramToken:  envString("DAVID_TELEGRAM_BOT_TOKEN", ""),
		telegramChatID: envString("DAVID_TELEGRAM_CHAT_ID", "929618433"),
		clientID:       envString("DAVID_GOOGLE_CLIENT_ID", ""),
		clientSecret: envString("DAVID_GOOGLE_SECRET", ""),
		baseURL:      baseURL,
		localBypass:  envString("DAVID_LOCAL_BYPASS", "") == "1",
	}

	app.seedTodos()
	app.seedAdmin()

	if app.localBypass {
		log.Printf("david: WARNING — local bypass enabled")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/google", app.handleAuthGoogle)
	mux.HandleFunc("GET /auth/callback", app.handleAuthCallback)
	mux.HandleFunc("POST /auth/logout", app.handleAuthLogout)
	mux.HandleFunc("POST /api/toggle/{id}", app.requireAuth(app.handleToggle))
	mux.HandleFunc("POST /api/todos", app.requireAdmin(app.handleAddTodo))
	mux.HandleFunc("PUT /api/todos/{id}", app.requireAdmin(app.handleUpdateTodo))
	mux.HandleFunc("DELETE /api/todos/{id}", app.requireAdmin(app.handleDeleteTodo))
	mux.HandleFunc("POST /api/handovers", app.requireAdmin(app.handleAddHandover))
	mux.HandleFunc("POST /api/handovers/{id}/archive", app.requireAdmin(app.handleArchiveHandover))
	mux.HandleFunc("DELETE /api/handovers/{id}", app.requireAdmin(app.handleDeleteHandover))
	mux.HandleFunc("GET /api/info", app.requireAdmin(app.handleInfo))
	mux.HandleFunc("GET /api/users", app.requireAdmin(app.handleListUsers))
	mux.HandleFunc("POST /api/users", app.requireAdmin(app.handleAddUserAPI))
	mux.HandleFunc("PATCH /api/users/{email}", app.requireAdmin(app.handlePatchUser))
	mux.HandleFunc("DELETE /api/users/{email}", app.requireAdmin(app.handleRemoveUser))
	mux.HandleFunc("GET /{$}", app.requireAuth(app.handleIndex))

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("david-s-checklist listening on http://%s (db=%s)", addr, dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("david: received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	case err := <-errCh:
		if err != nil {
			log.Fatalf("david: serve failed: %v", err)
		}
	}
}

// ── Database ──────────────────────────────────────────────────────────

func openDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(2)

	for _, name := range []string{"001_init.sql", "002_todos_table.sql", "003_handovers.sql", "005_users.sql"} {
		migration, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := conn.Exec(string(migration)); err != nil {
			conn.Close()
			return nil, fmt.Errorf("apply migration %s: %w", name, err)
		}
	}

	// Add columns if they don't exist (idempotent).
	for _, stmt := range []string{
		"ALTER TABLE todos ADD COLUMN assignee TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE todos ADD COLUMN handover_id INTEGER REFERENCES handovers(id)",
		"ALTER TABLE handovers ADD COLUMN archived INTEGER NOT NULL DEFAULT 0",
	} {
		conn.Exec(stmt) // Ignore "duplicate column" errors.
	}

	return conn, nil
}

// ── Seed ──────────────────────────────────────────────────────────────

func (a *App) seedTodos() {
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM todos").Scan(&count); err != nil || count > 0 {
		return
	}

	f, err := os.Open(a.todosPath)
	if err != nil {
		return
	}
	defer f.Close()

	var items []struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(f).Decode(&items); err != nil {
		log.Printf("david: seed: failed to parse %s: %v", a.todosPath, err)
		return
	}

	for i, item := range items {
		var completedAt sql.NullString
		_ = a.db.QueryRow("SELECT completed_at FROM completions WHERE todo_id = ?", item.ID).Scan(&completedAt)

		completed := 0
		var catPtr *string
		if completedAt.Valid {
			completed = 1
			catPtr = &completedAt.String
		}

		a.db.Exec(
			"INSERT INTO todos (title, description, position, completed, completed_at) VALUES (?, ?, ?, ?, ?)",
			item.Title, item.Description, i, completed, catPtr,
		)
	}
	log.Printf("david: seeded %d todos from %s", len(items), a.todosPath)
}

// ── Data loading ──────────────────────────────────────────────────────

func (a *App) loadHandovers(filterEmail string, includeArchived bool) ([]Handover, error) {
	query := "SELECT id, title, description, assignee, archived, created_at FROM handovers WHERE 1=1"
	var args []any
	if !includeArchived {
		query += " AND archived = 0"
	}
	if filterEmail != "" {
		query += " AND LOWER(assignee) = LOWER(?)"
		args = append(args, filterEmail)
	}
	query += " ORDER BY archived ASC, created_at DESC"

	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var handovers []Handover
	for rows.Next() {
		var h Handover
		var archived int
		if err := rows.Scan(&h.ID, &h.Title, &h.Description, &h.Assignee, &archived, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.Archived = archived == 1
		handovers = append(handovers, h)
	}

	for i := range handovers {
		todos, err := a.loadTodosByHandover(handovers[i].ID)
		if err != nil {
			return nil, err
		}
		handovers[i].Todos = todos
		for _, t := range todos {
			handovers[i].Total++
			if t.Completed {
				handovers[i].Done++
			}
		}
		if handovers[i].Total > 0 {
			handovers[i].Percent = handovers[i].Done * 100 / handovers[i].Total
		}
	}
	return handovers, nil
}

func (a *App) loadTodosByHandover(handoverID int) ([]Todo, error) {
	rows, err := a.db.Query(
		"SELECT id, title, description, assignee, handover_id, position, completed, completed_at FROM todos WHERE handover_id = ? ORDER BY position, id",
		handoverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTodos(rows)
}

func (a *App) loadStandaloneTodos(filterEmail string) ([]Todo, error) {
	query := "SELECT id, title, description, assignee, handover_id, position, completed, completed_at FROM todos WHERE handover_id IS NULL"
	var args []any
	if filterEmail != "" {
		query += " AND LOWER(assignee) = LOWER(?)"
		args = append(args, filterEmail)
	}
	query += " ORDER BY position, id"

	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTodos(rows)
}

func scanTodos(rows *sql.Rows) ([]Todo, error) {
	var todos []Todo
	for rows.Next() {
		var t Todo
		var completedAt sql.NullString
		var completed int
		var handoverID sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Assignee, &handoverID, &t.Position, &completed, &completedAt); err != nil {
			return nil, err
		}
		t.Completed = completed == 1
		if completedAt.Valid {
			t.CompletedAt = completedAt.String
		}
		if handoverID.Valid {
			hid := int(handoverID.Int64)
			t.HandoverID = &hid
		}
		todos = append(todos, t)
	}
	return todos, nil
}

func (a *App) listAssignees() ([]string, error) {
	rows, err := a.db.Query(`
		SELECT DISTINCT LOWER(assignee) FROM todos WHERE assignee != ''
		UNION
		SELECT DISTINCT LOWER(assignee) FROM handovers
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	var emails []string
	// Add known emails first.
	for _, e := range a.knownEmails {
		if !seen[e] {
			seen[e] = true
			emails = append(emails, e)
		}
	}
	// Add DB emails.
	for rows.Next() {
		var e string
		rows.Scan(&e)
		if e != "" && !seen[e] {
			seen[e] = true
			emails = append(emails, e)
		}
	}
	return emails, nil
}

// ── Sessions ──────────────────────────────────────────────────────────

func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (a *App) createSession(email string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	hash := sha256Hash(token)
	expires := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_, err := a.db.Exec(
		"INSERT INTO sessions (token_hash, email, expires_at) VALUES (?, ?, ?)",
		hash, email, expires,
	)
	return token, err
}

func (a *App) validateSession(r *http.Request) (string, error) {
	cookie, err := r.Cookie("david_session")
	if err != nil {
		return "", err
	}
	hash := sha256Hash(cookie.Value)

	var email, expiresAt string
	err = a.db.QueryRow(
		"SELECT email, expires_at FROM sessions WHERE token_hash = ?", hash,
	).Scan(&email, &expiresAt)
	if err != nil {
		return "", err
	}

	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(expires) {
		_, _ = a.db.Exec("DELETE FROM sessions WHERE token_hash = ?", hash)
		return "", errors.New("session expired")
	}
	return email, nil
}

func (a *App) deleteSession(token string) {
	_, _ = a.db.Exec("DELETE FROM sessions WHERE token_hash = ?", sha256Hash(token))
}

// ── Auth ──────────────────────────────────────────────────────────────

func (a *App) isAllowed(email string) bool {
	if a.isAdmin(email) {
		return true
	}
	// Check if this email exists in the users table.
	var ucount int
	a.db.QueryRow("SELECT COUNT(*) FROM users WHERE LOWER(email) = LOWER(?)", email).Scan(&ucount)
	if ucount > 0 {
		return true
	}
	// Check if this email has any tasks or handovers assigned.
	var count int
	a.db.QueryRow("SELECT COUNT(*) FROM todos WHERE LOWER(assignee) = LOWER(?)", email).Scan(&count)
	if count > 0 {
		return true
	}
	a.db.QueryRow("SELECT COUNT(*) FROM handovers WHERE LOWER(assignee) = LOWER(?)", email).Scan(&count)
	return count > 0
}

func (a *App) isAdmin(email string) bool {
	if strings.EqualFold(email, a.adminEmail) {
		return true
	}
	var count int
	a.db.QueryRow("SELECT COUNT(*) FROM users WHERE LOWER(email) = LOWER(?) AND is_admin = 1", email).Scan(&count)
	return count > 0
}

func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.localBypass && r.Header.Get("X-Forwarded-For") == "" {
			ctx := context.WithValue(r.Context(), emailCtxKey, a.adminEmail)
			next(w, r.WithContext(ctx))
			return
		}

		email, err := a.validateSession(r)
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			a.tmpl.ExecuteTemplate(w, "login.html", nil)
			return
		}
		ctx := context.WithValue(r.Context(), emailCtxKey, email)
		next(w, r.WithContext(ctx))
	}
}

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		email := r.Context().Value(emailCtxKey).(string)
		if !a.isAdmin(email) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// ── Page Handler ─────────────────────────────────────────────────────

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	email := r.Context().Value(emailCtxKey).(string)
	isAdmin := a.isAdmin(email)

	// Admin sees everything, users see only their assigned items.
	filterEmail := email
	if isAdmin {
		filterEmail = ""
	}

	allHandovers, err := a.loadHandovers(filterEmail, isAdmin)
	if err != nil {
		log.Printf("david: load handovers: %v", err)
		http.Error(w, "failed to load data", http.StatusInternalServerError)
		return
	}

	standalone, err := a.loadStandaloneTodos(filterEmail)
	if err != nil {
		log.Printf("david: load standalone: %v", err)
		http.Error(w, "failed to load data", http.StatusInternalServerError)
		return
	}

	var handovers, archivedHandovers []Handover
	for _, h := range allHandovers {
		if h.Archived {
			archivedHandovers = append(archivedHandovers, h)
		} else {
			handovers = append(handovers, h)
		}
	}

	done, total := 0, 0
	for _, h := range handovers {
		done += h.Done
		total += h.Total
	}
	for _, t := range standalone {
		total++
		if t.Completed {
			done++
		}
	}

	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}

	var assignees []string
	if isAdmin {
		assignees, _ = a.listAssignees()
	}

	data := PageData{
		Handovers:         handovers,
		ArchivedHandovers: archivedHandovers,
		StandaloneTodos:   standalone,
		Email:             email,
		IsAdmin:           isAdmin,
		Done:              done,
		Total:             total,
		Percent:           pct,
		Assignees:         assignees,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Printf("david: render: %v", err)
	}
}

// ── Todo Handlers ────────────────────────────────────────────────────

func (a *App) handleToggle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var completed int
	err = a.db.QueryRow("SELECT completed FROM todos WHERE id = ?", id).Scan(&completed)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	email := r.Context().Value(emailCtxKey).(string)

	if completed == 0 {
		_, err = a.db.Exec("UPDATE todos SET completed = 1, completed_at = datetime('now') WHERE id = ?", id)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"completed":true}`)
			// Notify admin via Telegram.
			var title string
			a.db.QueryRow("SELECT title FROM todos WHERE id = ?", id).Scan(&title)
			go a.notifyTelegram(fmt.Sprintf("%s completed: %s", email, title))
		}
	} else {
		_, err = a.db.Exec("UPDATE todos SET completed = 0, completed_at = NULL WHERE id = ?", id)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"completed":false}`)
		}
	}
	if err != nil {
		log.Printf("david: toggle %d: %v", id, err)
		http.Error(w, "toggle failed", http.StatusInternalServerError)
	}
}

func (a *App) handleAddTodo(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Assignee    string `json:"assignee"`
		HandoverID  *int   `json:"handover_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(input.Title) == "" {
		http.Error(w, "title required", http.StatusBadRequest)
		return
	}

	var maxPos int
	_ = a.db.QueryRow("SELECT COALESCE(MAX(position), -1) FROM todos").Scan(&maxPos)

	result, err := a.db.Exec(
		"INSERT INTO todos (title, description, assignee, handover_id, position) VALUES (?, ?, ?, ?, ?)",
		strings.TrimSpace(input.Title), strings.TrimSpace(input.Description),
		strings.TrimSpace(strings.ToLower(input.Assignee)), input.HandoverID, maxPos+1,
	)
	if err != nil {
		log.Printf("david: add todo: %v", err)
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	id, _ := result.LastInsertId()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":%d}`, id)
}

func (a *App) handleUpdateTodo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	res, err := a.db.Exec("UPDATE todos SET title = ?, description = ? WHERE id = ?",
		strings.TrimSpace(input.Title), strings.TrimSpace(input.Description), id)
	if err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleDeleteTodo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	res, err := a.db.Exec("DELETE FROM todos WHERE id = ?", id)
	if err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Handover Handlers ────────────────────────────────────────────────

func (a *App) handleAddHandover(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Assignee    string `json:"assignee"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Assignee) == "" {
		http.Error(w, "title and assignee required", http.StatusBadRequest)
		return
	}

	result, err := a.db.Exec(
		"INSERT INTO handovers (title, description, assignee) VALUES (?, ?, ?)",
		strings.TrimSpace(input.Title), strings.TrimSpace(input.Description),
		strings.TrimSpace(strings.ToLower(input.Assignee)),
	)
	if err != nil {
		log.Printf("david: add handover: %v", err)
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	id, _ := result.LastInsertId()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":%d}`, id)
}

func (a *App) handleArchiveHandover(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Toggle archived state.
	var archived int
	if err := a.db.QueryRow("SELECT archived FROM handovers WHERE id = ?", id).Scan(&archived); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	newVal := 1
	if archived == 1 {
		newVal = 0
	}
	a.db.Exec("UPDATE handovers SET archived = ? WHERE id = ?", newVal, id)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"archived":%v}`, newVal == 1)
}

func (a *App) handleDeleteHandover(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	// Delete all tasks in the handover first.
	a.db.Exec("DELETE FROM todos WHERE handover_id = ?", id)
	res, err := a.db.Exec("DELETE FROM handovers WHERE id = ?", id)
	if err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Seed admin ───────────────────────────────────────────────────────

func (a *App) seedAdmin() {
	// Ensure the env-var admin is always in the users table.
	a.db.Exec(
		"INSERT OR IGNORE INTO users (email, is_admin) VALUES (LOWER(?), 1)",
		a.adminEmail,
	)
}

// ── User Management Handlers (for alive dashboard) ──────────────────

type UserInfo struct {
	Email    string `json:"email"`
	IsAdmin  bool   `json:"is_admin"`
	Tasks    int    `json:"tasks"`
	LoggedIn bool   `json:"logged_in"`
}

func (a *App) handleListUsers(w http.ResponseWriter, r *http.Request) {
	// Build a unified user list from the users table, assignees, and sessions.
	users := make(map[string]*UserInfo)

	// 1. Users table entries.
	rows, err := a.db.Query("SELECT email, is_admin FROM users ORDER BY email")
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var email string
		var isAdmin int
		rows.Scan(&email, &isAdmin)
		email = strings.ToLower(email)
		users[email] = &UserInfo{Email: email, IsAdmin: isAdmin == 1}
	}
	rows.Close()

	// 2. Also include emails from task/handover assignees not yet in users table.
	aRows, err := a.db.Query(`
		SELECT DISTINCT LOWER(assignee) FROM todos WHERE assignee != ''
		UNION
		SELECT DISTINCT LOWER(assignee) FROM handovers`)
	if err == nil {
		for aRows.Next() {
			var email string
			aRows.Scan(&email)
			if _, ok := users[email]; !ok {
				users[email] = &UserInfo{Email: email}
			}
		}
		aRows.Close()
	}

	// 3. Fill task counts.
	tRows, _ := a.db.Query("SELECT LOWER(assignee), COUNT(*) FROM todos WHERE assignee != '' GROUP BY LOWER(assignee)")
	if tRows != nil {
		for tRows.Next() {
			var email string
			var count int
			tRows.Scan(&email, &count)
			if u, ok := users[email]; ok {
				u.Tasks = count
			}
		}
		tRows.Close()
	}
	hRows, _ := a.db.Query("SELECT LOWER(assignee), COUNT(*) FROM handovers GROUP BY LOWER(assignee)")
	if hRows != nil {
		for hRows.Next() {
			var email string
			var count int
			hRows.Scan(&email, &count)
			if u, ok := users[email]; ok {
				u.Tasks += count
			}
		}
		hRows.Close()
	}

	// 4. Mark logged-in users.
	sRows, _ := a.db.Query("SELECT DISTINCT LOWER(email) FROM sessions")
	if sRows != nil {
		for sRows.Next() {
			var email string
			sRows.Scan(&email)
			if u, ok := users[email]; ok {
				u.LoggedIn = true
			}
		}
		sRows.Close()
	}

	// 5. Ensure env-var admin is always marked.
	adminLower := strings.ToLower(a.adminEmail)
	if u, ok := users[adminLower]; ok {
		u.IsAdmin = true
	}

	// Collect and sort.
	var result []UserInfo
	for _, u := range users {
		result = append(result, *u)
	}
	sort.Slice(result, func(i, j int) bool {
		// Admins first, then alphabetical.
		if result[i].IsAdmin != result[j].IsAdmin {
			return result[i].IsAdmin
		}
		return result[i].Email < result[j].Email
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (a *App) handleAddUserAPI(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email   string `json:"email"`
		IsAdmin bool   `json:"is_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(input.Email))
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}
	isAdmin := 0
	if input.IsAdmin {
		isAdmin = 1
	}
	_, err := a.db.Exec(
		"INSERT INTO users (email, is_admin) VALUES (?, ?) ON CONFLICT(email) DO UPDATE SET is_admin = excluded.is_admin",
		email, isAdmin,
	)
	if err != nil {
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"email":%q,"is_admin":%v}`, email, input.IsAdmin)
}

func (a *App) handlePatchUser(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(r.PathValue("email"))
	var input struct {
		IsAdmin bool `json:"is_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	isAdmin := 0
	if input.IsAdmin {
		isAdmin = 1
	}
	// Upsert: if user exists update, otherwise insert.
	_, err := a.db.Exec(
		"INSERT INTO users (email, is_admin) VALUES (?, ?) ON CONFLICT(email) DO UPDATE SET is_admin = excluded.is_admin",
		email, isAdmin,
	)
	if err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleRemoveUser(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(r.PathValue("email"))
	if strings.EqualFold(email, a.adminEmail) {
		http.Error(w, "cannot remove the primary admin", http.StatusForbidden)
		return
	}
	// Remove from users table.
	a.db.Exec("DELETE FROM users WHERE LOWER(email) = LOWER(?)", email)
	// Delete their sessions so they're logged out.
	a.db.Exec("DELETE FROM sessions WHERE LOWER(email) = LOWER(?)", email)
	w.WriteHeader(http.StatusNoContent)
}

// ── Info Handler (for alive dashboard) ───────────────────────────────

func (a *App) handleInfo(w http.ResponseWriter, r *http.Request) {
	assignees, _ := a.listAssignees()

	// List all sessions (recent logins).
	rows, err := a.db.Query("SELECT DISTINCT email FROM sessions ORDER BY email")
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var loggedIn []string
	for rows.Next() {
		var e string
		rows.Scan(&e)
		loggedIn = append(loggedIn, e)
	}

	info := map[string]any{
		"admin":     a.adminEmail,
		"assignees": assignees,
		"sessions":  loggedIn,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// ── Telegram Notification ─────────────────────────────────────────────

func (a *App) notifyTelegram(message string) {
	if a.telegramToken == "" || a.telegramChatID == "" {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"chat_id": a.telegramChatID,
		"text":    message,
	})
	resp, err := http.Post(
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", a.telegramToken),
		"application/json",
		strings.NewReader(string(payload)),
	)
	if err != nil {
		log.Printf("david: telegram notify failed: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("david: telegram notify status: %d", resp.StatusCode)
	}
}

// ── OAuth ─────────────────────────────────────────────────────────────

func (a *App) handleAuthGoogle(w http.ResponseWriter, r *http.Request) {
	if a.localBypass && a.clientID == "" {
		token, err := a.createSession(a.adminEmail)
		if err != nil {
			http.Error(w, "session failed", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: "david_session", Value: token, Path: "/",
			MaxAge: 30 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	redirectURI := a.baseURL + "/auth/callback"
	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=openid+email+profile&access_type=online",
		url.QueryEscape(a.clientID),
		url.QueryEscape(redirectURI),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (a *App) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	redirectURI := a.baseURL + "/auth/callback"
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"code":          {code},
		"client_id":     {a.clientID},
		"client_secret": {a.clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		log.Printf("david: token exchange: %v", err)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		log.Printf("david: bad token response: %s", body)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}

	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	infoResp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("david: userinfo: %v", err)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}
	defer infoResp.Body.Close()

	var userInfo struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(infoResp.Body).Decode(&userInfo); err != nil {
		log.Printf("david: decode userinfo: %v", err)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}

	log.Printf("david: login attempt from %s (admin=%s, allowed=%v)", userInfo.Email, a.adminEmail, a.isAllowed(userInfo.Email))

	if !a.isAllowed(userInfo.Email) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.ExecuteTemplate(w, "denied.html", nil)
		return
	}

	token, err := a.createSession(userInfo.Email)
	if err != nil {
		log.Printf("david: create session: %v", err)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}

	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name: "david_session", Value: token, Path: "/",
		MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *App) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("david_session"); err == nil {
		a.deleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: "david_session", Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true, Expires: time.Unix(0, 0),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// ── Logging ───────────────────────────────────────────────────────────

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(lw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, lw.status, time.Since(start))
	})
}

type loggingWriter struct {
	http.ResponseWriter
	status int
}

func (w *loggingWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// ── Helpers ───────────────────────────────────────────────────────────

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
