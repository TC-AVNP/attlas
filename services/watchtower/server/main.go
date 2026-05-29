package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type App struct {
	db   *sql.DB
	tmpl *template.Template
}

type BeaconEvent struct {
	Email     string `json:"email"`
	App       string `json:"app"`
	Origin    string `json:"origin"`
	Path      string `json:"path"`
	SessionID string `json:"session_id"`
	EventType string `json:"event_type"`
	Meta      string `json:"meta"`
	Timestamp int64  `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	port := envOr("WATCHTOWER_PORT", "7702")
	dbPath := envOr("WATCHTOWER_DB", "watchtower.db")

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)

	if err := migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// Retention: delete events older than 90 days, run every hour
	go func() {
		for {
			cutoff := time.Now().AddDate(0, 0, -90).UnixMilli()
			res, err := db.Exec(`DELETE FROM events WHERE server_ts < ?`, cutoff)
			if err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					log.Printf("retention: deleted %d events older than 90 days", n)
				}
			}
			time.Sleep(1 * time.Hour)
		}
	}()

	funcMap := template.FuncMap{
		"json": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
	}
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	app := &App{db: db, tmpl: tmpl}

	mux := http.NewServeMux()

	// Public endpoints (beacon collection + health) — Caddy bypasses forward_auth for these
	mux.HandleFunc("POST /api/beacon", app.handleBeacon)
	mux.HandleFunc("OPTIONS /api/beacon", app.handleBeaconOptions)
	mux.HandleFunc("GET /beacon.js", app.handleBeaconJS)
	mux.HandleFunc("GET /api/health", app.handleHealth)

	// Dashboard — protected by Caddy forward_auth, email in X-Auth-User header
	mux.HandleFunc("GET /{$}", app.handleDashboard)
	mux.HandleFunc("GET /api/live", app.handleLive)
	mux.HandleFunc("GET /api/apps", app.handleApps)
	mux.HandleFunc("GET /api/users", app.handleUsers)
	mux.HandleFunc("GET /api/heatmap", app.handleHeatmap)
	mux.HandleFunc("GET /api/user/{email}", app.handleUserDetail)
	mux.HandleFunc("GET /api/user/{email}/app/{app}", app.handleUserAppTimeline)
	mux.HandleFunc("GET /api/stats", app.handleStats)

	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}

	go func() {
		log.Printf("watchtower listening on 127.0.0.1:%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// ---------------------------------------------------------------------------
// Migration
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Beacon endpoints (public — no auth)
// ---------------------------------------------------------------------------

func (a *App) setCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", "86400")
	}
}

func (a *App) handleBeaconOptions(w http.ResponseWriter, r *http.Request) {
	a.setCORS(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleBeacon(w http.ResponseWriter, r *http.Request) {
	a.setCORS(w, r)

	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var ev BeaconEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if ev.Email == "" || ev.Email == "anonymous" || ev.App == "" || ev.SessionID == "" || ev.EventType == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if len(ev.Email) > 200 {
		ev.Email = ev.Email[:200]
	}
	if len(ev.App) > 100 {
		ev.App = ev.App[:100]
	}
	if len(ev.Path) > 500 {
		ev.Path = ev.Path[:500]
	}
	if len(ev.Meta) > 200 {
		ev.Meta = ev.Meta[:200]
	}

	clientTS := ev.Timestamp
	if clientTS == 0 {
		clientTS = time.Now().UnixMilli()
	}
	serverTS := time.Now().UnixMilli()

	_, err = a.db.Exec(
		`INSERT INTO events (email, app, origin, path, session_id, event_type, meta, client_ts, server_ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.Email, ev.App, ev.Origin, ev.Path, ev.SessionID, ev.EventType, ev.Meta, clientTS, serverTS,
	)
	if err != nil {
		log.Printf("insert event: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleBeaconJS(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/beacon.js")
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(data)
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	var count int
	a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE server_ts > ?`, time.Now().Add(-24*time.Hour).UnixMilli()).Scan(&count)
	sendJSON(w, map[string]any{"status": "ok", "events_24h": count})
}

// ---------------------------------------------------------------------------
// Dashboard — protected by Caddy forward_auth
// ---------------------------------------------------------------------------

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	a.tmpl.ExecuteTemplate(w, "index.html", map[string]any{
		"Base": "",
	})
}

// ---------------------------------------------------------------------------
// API: Live users
// ---------------------------------------------------------------------------

func (a *App) handleLive(w http.ResponseWriter, r *http.Request) {
	cutoff := time.Now().Add(-5 * time.Minute).UnixMilli()
	rows, err := a.db.Query(`
		SELECT email, app, path, MAX(server_ts) as last_seen,
		       MIN(CASE WHEN event_type='pageview' THEN server_ts END) as first_pv
		FROM events
		WHERE server_ts > ?
		GROUP BY email
		ORDER BY last_seen DESC`, cutoff)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type LiveUser struct {
		Email    string `json:"email"`
		App      string `json:"app"`
		Path     string `json:"path"`
		LastSeen int64  `json:"last_seen"`
		ActiveMS int64  `json:"active_ms"`
	}

	var users []LiveUser
	now := time.Now().UnixMilli()
	for rows.Next() {
		var u LiveUser
		var firstPV sql.NullInt64
		rows.Scan(&u.Email, &u.App, &u.Path, &u.LastSeen, &firstPV)
		if firstPV.Valid {
			u.ActiveMS = now - firstPV.Int64
		}
		users = append(users, u)
	}
	if users == nil {
		users = []LiveUser{}
	}
	sendJSON(w, map[string]any{"users": users})
}

// ---------------------------------------------------------------------------
// API: App breakdown
// ---------------------------------------------------------------------------

func (a *App) handleApps(w http.ResponseWriter, r *http.Request) {
	since := parseSince(r.URL.Query().Get("range"))

	rows, err := a.db.Query(`
		SELECT app,
		       COUNT(*) as event_count,
		       COUNT(DISTINCT email) as unique_users,
		       SUM(CASE WHEN event_type='pageview' THEN 1 ELSE 0 END) as page_views,
		       COUNT(DISTINCT email || '|' || CAST(server_ts / 60000 AS TEXT)) as est_minutes
		FROM events
		WHERE server_ts > ?
		GROUP BY app
		ORDER BY est_minutes DESC`, since)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type AppStats struct {
		App         string `json:"app"`
		Events      int    `json:"events"`
		UniqueUsers int    `json:"unique_users"`
		PageViews   int    `json:"page_views"`
		EstMinutes  int    `json:"est_minutes"`
	}

	var apps []AppStats
	for rows.Next() {
		var s AppStats
		rows.Scan(&s.App, &s.Events, &s.UniqueUsers, &s.PageViews, &s.EstMinutes)
		apps = append(apps, s)
	}
	if apps == nil {
		apps = []AppStats{}
	}
	sendJSON(w, map[string]any{"apps": apps})
}

// ---------------------------------------------------------------------------
// API: User list
// ---------------------------------------------------------------------------

func (a *App) handleUsers(w http.ResponseWriter, r *http.Request) {
	since := parseSince(r.URL.Query().Get("range"))

	rows, err := a.db.Query(`
		SELECT email,
		       COUNT(*) as event_count,
		       COUNT(DISTINCT app) as app_count,
		       MAX(server_ts) as last_seen,
		       COUNT(DISTINCT email || '|' || CAST(server_ts / 60000 AS TEXT)) as est_minutes
		FROM events
		WHERE server_ts > ?
		GROUP BY email
		ORDER BY last_seen DESC`, since)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type UserStats struct {
		Email      string `json:"email"`
		Events     int    `json:"events"`
		AppCount   int    `json:"app_count"`
		LastSeen   int64  `json:"last_seen"`
		EstMinutes int    `json:"est_minutes"`
	}

	var users []UserStats
	for rows.Next() {
		var u UserStats
		rows.Scan(&u.Email, &u.Events, &u.AppCount, &u.LastSeen, &u.EstMinutes)
		users = append(users, u)
	}
	if users == nil {
		users = []UserStats{}
	}
	sendJSON(w, map[string]any{"users": users})
}

// ---------------------------------------------------------------------------
// API: Heatmap (hour-of-day x app)
// ---------------------------------------------------------------------------

func (a *App) handleHeatmap(w http.ResponseWriter, r *http.Request) {
	since := parseSince(r.URL.Query().Get("range"))

	rows, err := a.db.Query(`
		SELECT app,
		       CAST((server_ts / 1000 % 86400) / 3600 AS INTEGER) as hour,
		       COUNT(*) as cnt
		FROM events
		WHERE server_ts > ?
		GROUP BY app, hour
		ORDER BY app, hour`, since)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type HeatmapRow struct {
		App   string  `json:"app"`
		Hours [24]int `json:"hours"`
	}

	appMap := map[string]*HeatmapRow{}
	for rows.Next() {
		var appName string
		var hour, cnt int
		rows.Scan(&appName, &hour, &cnt)
		if hour < 0 || hour > 23 {
			continue
		}
		row, ok := appMap[appName]
		if !ok {
			row = &HeatmapRow{App: appName}
			appMap[appName] = row
		}
		row.Hours[hour] = cnt
	}

	var data []HeatmapRow
	for _, v := range appMap {
		data = append(data, *v)
	}
	sort.Slice(data, func(i, j int) bool { return data[i].App < data[j].App })
	if data == nil {
		data = []HeatmapRow{}
	}
	sendJSON(w, map[string]any{"data": data})
}

// ---------------------------------------------------------------------------
// API: User detail — Level 2: per-app breakdown
// ---------------------------------------------------------------------------

func (a *App) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if email == "" {
		http.Error(w, "missing email", 400)
		return
	}

	since := parseSince(r.URL.Query().Get("range"))

	type AppBreakdown struct {
		App        string `json:"app"`
		PageViews  int    `json:"page_views"`
		EstMinutes int    `json:"est_minutes"`
		LastSeen   int64  `json:"last_seen"`
	}

	rows, err := a.db.Query(`
		SELECT app,
		       SUM(CASE WHEN event_type='pageview' THEN 1 ELSE 0 END) as page_views,
		       COUNT(DISTINCT app || '|' || CAST(server_ts / 60000 AS TEXT)) as est_minutes,
		       MAX(server_ts) as last_seen
		FROM events
		WHERE email = ? AND server_ts > ?
		GROUP BY app
		ORDER BY est_minutes DESC`, email, since)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var apps []AppBreakdown
	var totalMinutes int
	for rows.Next() {
		var a AppBreakdown
		rows.Scan(&a.App, &a.PageViews, &a.EstMinutes, &a.LastSeen)
		totalMinutes += a.EstMinutes
		apps = append(apps, a)
	}
	if apps == nil {
		apps = []AppBreakdown{}
	}

	var lastSeen int64
	a.db.QueryRow(`SELECT MAX(server_ts) FROM events WHERE email = ? AND server_ts > ?`, email, since).Scan(&lastSeen)

	sendJSON(w, map[string]any{
		"email":         email,
		"total_minutes": totalMinutes,
		"total_apps":    len(apps),
		"last_seen":     lastSeen,
		"apps":          apps,
	})
}

// ---------------------------------------------------------------------------
// API: User app timeline — Level 3: paginated page visits
// ---------------------------------------------------------------------------

func (a *App) handleUserAppTimeline(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	appName := r.PathValue("app")
	if email == "" || appName == "" {
		http.Error(w, "missing email or app", 400)
		return
	}

	since := parseSince(r.URL.Query().Get("range"))
	until := parseUntil(r.URL.Query().Get("until"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 10
	offset := (page - 1) * limit

	var total int
	a.db.QueryRow(`
		SELECT COUNT(*) FROM events
		WHERE email = ? AND app = ? AND event_type = 'pageview' AND server_ts > ? AND server_ts < ?`,
		email, appName, since, until).Scan(&total)

	rows, err := a.db.Query(`
		SELECT path, server_ts FROM events
		WHERE email = ? AND app = ? AND event_type = 'pageview' AND server_ts > ? AND server_ts < ?
		ORDER BY server_ts DESC
		LIMIT ? OFFSET ?`, email, appName, since, until, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type PageVisit struct {
		Path       string `json:"path"`
		Timestamp  int64  `json:"timestamp"`
		DurationMS int64  `json:"duration_ms"`
	}

	var visits []PageVisit
	for rows.Next() {
		var v PageVisit
		rows.Scan(&v.Path, &v.Timestamp)
		visits = append(visits, v)
	}

	// Calculate durations: time until next event from same user, capped at 30 min.
	for i := range visits {
		var nextTS sql.NullInt64
		a.db.QueryRow(`
			SELECT MIN(server_ts) FROM events
			WHERE email = ? AND server_ts > ? AND server_ts != ?
			LIMIT 1`, email, visits[i].Timestamp, visits[i].Timestamp).Scan(&nextTS)
		if nextTS.Valid {
			dur := nextTS.Int64 - visits[i].Timestamp
			if dur > 30*60*1000 {
				dur = 0 // session gap, unknown duration
			}
			visits[i].DurationMS = dur
		}
	}

	if visits == nil {
		visits = []PageVisit{}
	}

	pages := (total + limit - 1) / limit
	if pages < 1 {
		pages = 1
	}

	sendJSON(w, map[string]any{
		"email":  email,
		"app":    appName,
		"total":  total,
		"page":   page,
		"pages":  pages,
		"events": visits,
	})
}

// ---------------------------------------------------------------------------
// API: Overall stats
// ---------------------------------------------------------------------------

func (a *App) handleStats(w http.ResponseWriter, r *http.Request) {
	var totalEvents, totalUsers, totalApps int
	a.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&totalEvents)
	a.db.QueryRow(`SELECT COUNT(DISTINCT email) FROM events`).Scan(&totalUsers)
	a.db.QueryRow(`SELECT COUNT(DISTINCT app) FROM events`).Scan(&totalApps)

	var eventsToday int
	todayStart := time.Now().Truncate(24 * time.Hour).UnixMilli()
	a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE server_ts > ?`, todayStart).Scan(&eventsToday)

	sendJSON(w, map[string]any{
		"total_events": totalEvents,
		"total_users":  totalUsers,
		"total_apps":   totalApps,
		"events_today": eventsToday,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func sendJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func parseUntil(s string) int64 {
	if s == "" {
		return time.Now().Add(time.Hour).UnixMilli() // slightly in the future to catch everything
	}
	// Try as unix ms
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return ms
	}
	// Try as date string YYYY-MM-DD (end of day)
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Add(24*time.Hour - time.Millisecond).UnixMilli()
	}
	return time.Now().Add(time.Hour).UnixMilli()
}

func parseSince(rangeStr string) int64 {
	now := time.Now()
	switch rangeStr {
	case "hour":
		return now.Add(-1 * time.Hour).UnixMilli()
	case "today":
		return now.Truncate(24 * time.Hour).UnixMilli()
	case "week":
		return now.AddDate(0, 0, -7).UnixMilli()
	case "month":
		return now.AddDate(0, -1, 0).UnixMilli()
	case "all":
		return 0
	default:
		if ms, err := strconv.ParseInt(rangeStr, 10, 64); err == nil {
			return ms
		}
		// Try as date string YYYY-MM-DD (start of day)
		if t, err := time.Parse("2006-01-02", rangeStr); err == nil {
			return t.UnixMilli()
		}
		return now.Truncate(24 * time.Hour).UnixMilli()
	}
}
