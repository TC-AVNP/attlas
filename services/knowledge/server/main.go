// knowledge — Builder's Knowledge Base for the iapetus ecosystem.
//
// A knowledge graph where each node is a self-contained document.
// Entries link to other entries (leaves) for drill-down detail.
//
// Configuration via environment variables:
//
//	KNOWLEDGE_PORT               TCP port (default 7694)
//	KNOWLEDGE_DB                 SQLite path (default /var/lib/knowledge/knowledge.db)
//	KNOWLEDGE_ADMIN_EMAIL        admin email (default condecopedro@gmail.com)
//	KNOWLEDGE_GOOGLE_CLIENT_ID   Google OAuth client ID
//	KNOWLEDGE_GOOGLE_SECRET      Google OAuth client secret
//	KNOWLEDGE_BASE_URL           canonical base URL (e.g. https://knowledge.attlas.uk)
//	KNOWLEDGE_LOCAL_BYPASS       set "1" to skip auth on loopback (dev)
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
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

// --- Data types ---

type Entry struct {
	ID          int    `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	Placeholder bool   `json:"placeholder"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Link struct {
	ID        int    `json:"id"`
	SourceID  int    `json:"source_id"`
	TargetID  int    `json:"target_id"`
	Label     string `json:"label"`
	CreatedAt string `json:"created_at"`
}

type GraphNode struct {
	ID          int    `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Placeholder bool   `json:"placeholder"`
}

type GraphEdge struct {
	Source int    `json:"source"`
	Target int    `json:"target"`
	Label  string `json:"label"`
}

type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type EntryPage struct {
	Entry       Entry
	Children    []Entry
	Parents     []Entry
	Email       string
	IsAdmin     bool
	ContentHTML template.HTML
}

type IndexPage struct {
	Entries []Entry
	Graph   template.JS
	Email   string
	IsAdmin bool
}

// --- App ---

type App struct {
	db           *sql.DB
	tmpl         *template.Template
	adminEmail   string
	clientID     string
	clientSecret string
	baseURL      string
	localBypass  bool
}

func main() {
	port := envOr("KNOWLEDGE_PORT", "7694")
	dbPath := envOr("KNOWLEDGE_DB", "/var/lib/knowledge/knowledge.db")
	adminEmail := envOr("KNOWLEDGE_ADMIN_EMAIL", "condecopedro@gmail.com")
	clientID := envOr("KNOWLEDGE_GOOGLE_CLIENT_ID", "")
	clientSecret := envOr("KNOWLEDGE_GOOGLE_SECRET", "")
	baseURL := envOr("KNOWLEDGE_BASE_URL", "http://localhost:"+port)
	localBypass := os.Getenv("KNOWLEDGE_LOCAL_BYPASS") == "1"

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	funcMap := template.FuncMap{
		"markdown": renderMarkdown,
	}
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	app := &App{
		db:           db,
		tmpl:         tmpl,
		adminEmail:   adminEmail,
		clientID:     clientID,
		clientSecret: clientSecret,
		baseURL:      baseURL,
		localBypass:  localBypass,
	}

	mux := http.NewServeMux()

	// Auth routes (no middleware).
	mux.HandleFunc("GET /auth/login", app.handleLogin)
	mux.HandleFunc("GET /auth/callback", app.handleCallback)
	mux.HandleFunc("GET /auth/logout", app.handleLogout)

	// Pages.
	mux.Handle("GET /{$}", app.auth(http.HandlerFunc(app.handleIndex)))
	mux.Handle("GET /entry/{slug}", app.auth(http.HandlerFunc(app.handleEntry)))

	// API.
	mux.Handle("GET /api/graph", app.auth(http.HandlerFunc(app.handleGraphAPI)))
	mux.Handle("POST /api/entries", app.adminOnly(http.HandlerFunc(app.handleCreateEntry)))
	mux.Handle("PUT /api/entries/{id}", app.adminOnly(http.HandlerFunc(app.handleUpdateEntry)))
	mux.Handle("DELETE /api/entries/{id}", app.adminOnly(http.HandlerFunc(app.handleDeleteEntry)))
	mux.Handle("POST /api/links", app.adminOnly(http.HandlerFunc(app.handleCreateLink)))
	mux.Handle("DELETE /api/links/{id}", app.adminOnly(http.HandlerFunc(app.handleDeleteLink)))

	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}

	go func() {
		log.Printf("knowledge listening on 127.0.0.1:%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// --- Database ---

func migrate(db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		data, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if _, err := db.Exec(string(data)); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
	}
	return nil
}

// --- Auth ---

func (a *App) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.localBypass && r.Header.Get("X-Forwarded-For") == "" {
			ctx := context.WithValue(r.Context(), emailCtxKey, a.adminEmail)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		cookie, err := r.Cookie("knowledge_session")
		if err != nil {
			http.Redirect(w, r, "/auth/login?return_to="+url.QueryEscape(r.URL.Path), http.StatusFound)
			return
		}

		hash := sha256sum(cookie.Value)
		var email string
		err = a.db.QueryRow("SELECT email FROM sessions WHERE token = ?", hash).Scan(&email)
		if err != nil {
			http.Redirect(w, r, "/auth/login?return_to="+url.QueryEscape(r.URL.Path), http.StatusFound)
			return
		}

		ctx := context.WithValue(r.Context(), emailCtxKey, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *App) adminOnly(next http.Handler) http.Handler {
	return a.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email := r.Context().Value(emailCtxKey).(string)
		if email != a.adminEmail {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if a.clientID == "" {
		http.Error(w, "OAuth not configured", http.StatusServiceUnavailable)
		return
	}
	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" {
		returnTo = "/"
	}
	state := randomHex(16)
	http.SetCookie(w, &http.Cookie{
		Name: "knowledge_oauth_state", Value: state + "|" + returnTo,
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300,
	})
	u := fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=email&state=%s",
		url.QueryEscape(a.clientID),
		url.QueryEscape(a.baseURL+"/auth/callback"),
		url.QueryEscape(state))
	http.Redirect(w, r, u, http.StatusFound)
}

func (a *App) handleCallback(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("knowledge_oauth_state")
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}
	parts := strings.SplitN(cookie.Value, "|", 2)
	if len(parts) != 2 || parts[0] != r.URL.Query().Get("state") {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	returnTo := parts[1]

	// Exchange code for token.
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"code":          {r.URL.Query().Get("code")},
		"client_id":     {a.clientID},
		"client_secret": {a.clientSecret},
		"redirect_uri":  {a.baseURL + "/auth/callback"},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(body, &tok)
	if tok.AccessToken == "" {
		http.Error(w, "no access token", http.StatusBadGateway)
		return
	}

	// Get user email.
	infoResp, err := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + tok.AccessToken)
	if err != nil {
		http.Error(w, "userinfo failed", http.StatusBadGateway)
		return
	}
	defer infoResp.Body.Close()
	infoBody, _ := io.ReadAll(infoResp.Body)

	var info struct {
		Email string `json:"email"`
	}
	json.Unmarshal(infoBody, &info)
	if info.Email == "" {
		http.Error(w, "no email", http.StatusBadGateway)
		return
	}

	// Only admin can access for now.
	if info.Email != a.adminEmail {
		a.tmpl.ExecuteTemplate(w, "denied.html", nil)
		return
	}

	// Create session.
	raw := randomHex(32)
	hash := sha256sum(raw)
	a.db.Exec("INSERT INTO sessions (token, email) VALUES (?, ?)", hash, info.Email)

	http.SetCookie(w, &http.Cookie{
		Name: "knowledge_session", Value: raw,
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: 86400 * 30,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "knowledge_oauth_state", Value: "", Path: "/", MaxAge: -1,
	})
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("knowledge_session")
	if err == nil {
		hash := sha256sum(cookie.Value)
		a.db.Exec("DELETE FROM sessions WHERE token = ?", hash)
	}
	http.SetCookie(w, &http.Cookie{
		Name: "knowledge_session", Value: "", Path: "/", MaxAge: -1,
	})
	http.Redirect(w, r, "/auth/login", http.StatusFound)
}

// --- Page handlers ---

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	email := r.Context().Value(emailCtxKey).(string)

	entries, err := a.allEntries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	graph, err := a.graphData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	graphJSON, _ := json.Marshal(graph)

	a.tmpl.ExecuteTemplate(w, "index.html", IndexPage{
		Entries: entries,
		Graph:   template.JS(graphJSON),
		Email:   email,
		IsAdmin: email == a.adminEmail,
	})
}

func (a *App) handleEntry(w http.ResponseWriter, r *http.Request) {
	email := r.Context().Value(emailCtxKey).(string)
	slug := r.PathValue("slug")

	entry, err := a.entryBySlug(slug)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	children, _ := a.childEntries(entry.ID)
	parents, _ := a.parentEntries(entry.ID)

	a.tmpl.ExecuteTemplate(w, "entry.html", EntryPage{
		Entry:       entry,
		Children:    children,
		Parents:     parents,
		Email:       email,
		IsAdmin:     email == a.adminEmail,
		ContentHTML: template.HTML(renderMarkdown(entry.Content)),
	})
}

// --- API handlers ---

func (a *App) handleGraphAPI(w http.ResponseWriter, r *http.Request) {
	graph, err := a.graphData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(graph)
}

func (a *App) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug        string `json:"slug"`
		Title       string `json:"title"`
		Content     string `json:"content"`
		Placeholder bool   `json:"placeholder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Slug == "" || req.Title == "" {
		http.Error(w, "slug and title required", http.StatusBadRequest)
		return
	}

	placeholder := 0
	if req.Placeholder {
		placeholder = 1
	}

	res, err := a.db.Exec(
		"INSERT INTO entries (slug, title, content, placeholder) VALUES (?, ?, ?, ?)",
		req.Slug, req.Title, req.Content, placeholder)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	id, _ := res.LastInsertId()

	entry, _ := a.entryByID(int(id))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(entry)
}

func (a *App) handleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))

	var req struct {
		Title       *string `json:"title"`
		Content     *string `json:"content"`
		Placeholder *bool   `json:"placeholder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	if req.Title != nil {
		a.db.Exec("UPDATE entries SET title = ?, updated_at = datetime('now') WHERE id = ?", *req.Title, id)
	}
	if req.Content != nil {
		a.db.Exec("UPDATE entries SET content = ?, updated_at = datetime('now') WHERE id = ?", *req.Content, id)
	}
	if req.Placeholder != nil {
		p := 0
		if *req.Placeholder {
			p = 1
		}
		a.db.Exec("UPDATE entries SET placeholder = ?, updated_at = datetime('now') WHERE id = ?", p, id)
	}

	entry, _ := a.entryByID(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

func (a *App) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	a.db.Exec("DELETE FROM entries WHERE id = ?", id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCreateLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceID int    `json:"source_id"`
		TargetID int    `json:"target_id"`
		Label    string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	res, err := a.db.Exec("INSERT INTO links (source_id, target_id, label) VALUES (?, ?, ?)",
		req.SourceID, req.TargetID, req.Label)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	id, _ := res.LastInsertId()

	var link Link
	a.db.QueryRow("SELECT id, source_id, target_id, label, created_at FROM links WHERE id = ?", id).
		Scan(&link.ID, &link.SourceID, &link.TargetID, &link.Label, &link.CreatedAt)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(link)
}

func (a *App) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	a.db.Exec("DELETE FROM links WHERE id = ?", id)
	w.WriteHeader(http.StatusNoContent)
}

// --- Data access ---

func (a *App) allEntries() ([]Entry, error) {
	rows, err := a.db.Query("SELECT id, slug, title, content, placeholder, created_at, updated_at FROM entries ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var p int
		rows.Scan(&e.ID, &e.Slug, &e.Title, &e.Content, &p, &e.CreatedAt, &e.UpdatedAt)
		e.Placeholder = p == 1
		entries = append(entries, e)
	}
	return entries, nil
}

func (a *App) entryBySlug(slug string) (Entry, error) {
	var e Entry
	var p int
	err := a.db.QueryRow("SELECT id, slug, title, content, placeholder, created_at, updated_at FROM entries WHERE slug = ?", slug).
		Scan(&e.ID, &e.Slug, &e.Title, &e.Content, &p, &e.CreatedAt, &e.UpdatedAt)
	e.Placeholder = p == 1
	return e, err
}

func (a *App) entryByID(id int) (Entry, error) {
	var e Entry
	var p int
	err := a.db.QueryRow("SELECT id, slug, title, content, placeholder, created_at, updated_at FROM entries WHERE id = ?", id).
		Scan(&e.ID, &e.Slug, &e.Title, &e.Content, &p, &e.CreatedAt, &e.UpdatedAt)
	e.Placeholder = p == 1
	return e, err
}

func (a *App) childEntries(parentID int) ([]Entry, error) {
	rows, err := a.db.Query(`
		SELECT e.id, e.slug, e.title, e.content, e.placeholder, e.created_at, e.updated_at
		FROM entries e JOIN links l ON e.id = l.target_id
		WHERE l.source_id = ? ORDER BY e.title`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var p int
		rows.Scan(&e.ID, &e.Slug, &e.Title, &e.Content, &p, &e.CreatedAt, &e.UpdatedAt)
		e.Placeholder = p == 1
		entries = append(entries, e)
	}
	return entries, nil
}

func (a *App) parentEntries(childID int) ([]Entry, error) {
	rows, err := a.db.Query(`
		SELECT e.id, e.slug, e.title, e.content, e.placeholder, e.created_at, e.updated_at
		FROM entries e JOIN links l ON e.id = l.source_id
		WHERE l.target_id = ? ORDER BY e.title`, childID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var p int
		rows.Scan(&e.ID, &e.Slug, &e.Title, &e.Content, &p, &e.CreatedAt, &e.UpdatedAt)
		e.Placeholder = p == 1
		entries = append(entries, e)
	}
	return entries, nil
}

func (a *App) graphData() (GraphData, error) {
	entries, err := a.allEntries()
	if err != nil {
		return GraphData{}, err
	}

	nodes := make([]GraphNode, len(entries))
	for i, e := range entries {
		nodes[i] = GraphNode{ID: e.ID, Slug: e.Slug, Title: e.Title, Placeholder: e.Placeholder}
	}

	rows, err := a.db.Query("SELECT source_id, target_id, label FROM links")
	if err != nil {
		return GraphData{}, err
	}
	defer rows.Close()

	var edges []GraphEdge
	for rows.Next() {
		var e GraphEdge
		rows.Scan(&e.Source, &e.Target, &e.Label)
		edges = append(edges, e)
	}

	return GraphData{Nodes: nodes, Edges: edges}, nil
}

// --- Helpers ---

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sha256sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// renderMarkdown converts markdown to basic HTML.
// Supports: headers, paragraphs, bold, italic, code, links, lists, horizontal rules.
func renderMarkdown(md string) string {
	if md == "" {
		return ""
	}

	lines := strings.Split(md, "\n")
	var out strings.Builder
	inList := false
	inOrderedList := false
	inParagraph := false

	closeLists := func() {
		if inList {
			out.WriteString("</ul>\n")
			inList = false
		}
		if inOrderedList {
			out.WriteString("</ol>\n")
			inOrderedList = false
		}
	}

	closeParagraph := func() {
		if inParagraph {
			out.WriteString("</p>\n")
			inParagraph = false
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Blank line.
		if trimmed == "" {
			closeParagraph()
			closeLists()
			continue
		}

		// Horizontal rule.
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			closeParagraph()
			closeLists()
			out.WriteString("<hr>\n")
			continue
		}

		// Headers.
		if strings.HasPrefix(trimmed, "# ") {
			closeParagraph()
			closeLists()
			out.WriteString("<h1>" + inlineMarkdown(trimmed[2:]) + "</h1>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			closeParagraph()
			closeLists()
			out.WriteString("<h2>" + inlineMarkdown(trimmed[3:]) + "</h2>\n")
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			closeParagraph()
			closeLists()
			out.WriteString("<h3>" + inlineMarkdown(trimmed[4:]) + "</h3>\n")
			continue
		}

		// Unordered list.
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			closeParagraph()
			if inOrderedList {
				out.WriteString("</ol>\n")
				inOrderedList = false
			}
			if !inList {
				out.WriteString("<ul>\n")
				inList = true
			}
			out.WriteString("<li>" + inlineMarkdown(trimmed[2:]) + "</li>\n")
			continue
		}

		// Ordered list.
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			dotIdx := strings.Index(trimmed, ". ")
			if dotIdx > 0 && dotIdx <= 3 {
				closeParagraph()
				if inList {
					out.WriteString("</ul>\n")
					inList = false
				}
				if !inOrderedList {
					out.WriteString("<ol>\n")
					inOrderedList = true
				}
				out.WriteString("<li>" + inlineMarkdown(trimmed[dotIdx+2:]) + "</li>\n")
				continue
			}
		}

		// Regular text -> paragraph.
		closeLists()
		if !inParagraph {
			out.WriteString("<p>")
			inParagraph = true
		} else {
			out.WriteString("<br>")
		}
		out.WriteString(inlineMarkdown(trimmed))
		out.WriteString("\n")
	}

	closeParagraph()
	closeLists()
	return out.String()
}

func inlineMarkdown(s string) string {
	// Escape HTML.
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")

	// Inline code.
	s = replaceInline(s, "`", "<code>", "</code>")
	// Bold.
	s = replaceInline(s, "**", "<strong>", "</strong>")
	// Italic.
	s = replaceInline(s, "*", "<em>", "</em>")

	// Links: [text](url)
	for {
		start := strings.Index(s, "[")
		if start == -1 {
			break
		}
		mid := strings.Index(s[start:], "](")
		if mid == -1 {
			break
		}
		mid += start
		end := strings.Index(s[mid:], ")")
		if end == -1 {
			break
		}
		end += mid
		text := s[start+1 : mid]
		href := s[mid+2 : end]
		link := fmt.Sprintf(`<a href="%s">%s</a>`, href, text)
		s = s[:start] + link + s[end+1:]
	}

	return s
}

func replaceInline(s, delim, open, close string) string {
	for {
		start := strings.Index(s, delim)
		if start == -1 {
			break
		}
		end := strings.Index(s[start+len(delim):], delim)
		if end == -1 {
			break
		}
		end += start + len(delim)
		inner := s[start+len(delim) : end]
		s = s[:start] + open + inner + close + s[end+len(delim):]
	}
	return s
}
