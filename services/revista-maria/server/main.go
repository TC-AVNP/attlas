// revista-maria — doubles tennis tournament at rm.attlas.uk
//
// 8 players, random pairs, single-elimination bracket.
//
//	RM_PORT                TCP port (default 7696)
//	RM_DB                  SQLite path (default /var/lib/revista-maria/rm.db)
//	RM_ADMIN_PASSPHRASE    Admin passphrase (default rm2026)
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
	"log"
	"net/http"
	"os"
	"os/signal"
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

var defaultPlayers = []string{"Spadz", "Condeco", "Jfig", "Rosinha", "PP13", "RV", "Maria", "Gordo"}

// ── Types ────────────────────────────────────────────────────────────

type Player struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Team struct {
	ID      int    `json:"id"`
	Player1 Player `json:"player1"`
	Player2 Player `json:"player2"`
	Name    string `json:"name"`
}

type Match struct {
	ID       int    `json:"id"`
	Round    int    `json:"round"`
	Position int    `json:"position"`
	Seq      int    `json:"seq"`
	Team1    *Team  `json:"team1"`
	Team2    *Team  `json:"team2"`
	Score1   int    `json:"score1"`
	Score2   int    `json:"score2"`
	Winner   *Team  `json:"winner"`
	Status   string `json:"status"`
}

type TournamentState struct {
	Status       string  `json:"status"`
	Players      []Player `json:"players"`
	Teams        []Team   `json:"teams"`
	Matches      []Match  `json:"matches"`
	Rounds       int      `json:"rounds"`
	CurrentMatch *Match   `json:"current_match"`
	NextMatch    *Match   `json:"next_match"`
	Champion     *Team    `json:"champion"`
}

type App struct {
	db         *sql.DB
	tmpl       *template.Template
	passphrase string
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	port := envInt("RM_PORT", 7696)
	dbPath := envString("RM_DB", "/var/lib/revista-maria/rm.db")

	conn, err := openDB(dbPath)
	if err != nil {
		log.Fatalf("rm: open db: %v", err)
	}
	defer conn.Close()

	// Auto-seed players.
	var count int
	conn.QueryRow("SELECT COUNT(*) FROM players").Scan(&count)
	if count == 0 {
		for _, name := range defaultPlayers {
			conn.Exec("INSERT INTO players (name) VALUES (?)", name)
		}
		log.Printf("rm: seeded %d players", len(defaultPlayers))
	}

	app := &App{
		db: conn,
		tmpl: template.Must(template.New("").Funcs(template.FuncMap{
			"seq": func(n int) []int { s := make([]int, n); for i := range s { s[i] = i + 1 }; return s },
			"add": func(a, b int) int { return a + b },
			"sub": func(a, b int) int { return a - b },
		}).ParseFS(templatesFS, "templates/*.html")),
		passphrase: envString("RM_ADMIN_PASSPHRASE", "rm2026"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", app.handlePublic)
	mux.HandleFunc("GET /bracket", app.handleBracket)
	mux.HandleFunc("GET /api/state", app.handleState)
	mux.HandleFunc("GET /admin/login", app.handleAdminLoginPage)
	mux.HandleFunc("POST /admin/login", app.handleAdminLogin)
	mux.HandleFunc("POST /admin/logout", app.handleAdminLogout)
	mux.HandleFunc("GET /admin", app.requireAdmin(app.handleAdmin))
	mux.HandleFunc("POST /api/players", app.requireAdmin(app.handleAddPlayer))
	mux.HandleFunc("DELETE /api/players/{id}", app.requireAdmin(app.handleRemovePlayer))
	mux.HandleFunc("POST /api/tournament/start", app.requireAdmin(app.handleStartTournament))
	mux.HandleFunc("POST /api/tournament/reset", app.requireAdmin(app.handleResetTournament))
	mux.HandleFunc("POST /api/matches/{id}/score", app.requireAdmin(app.handleRecordScore))

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv := &http.Server{Addr: addr, Handler: loggingMiddleware(mux),
		ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("revista-maria listening on http://%s (db=%s)", addr, dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("rm: received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	case err := <-errCh:
		if err != nil {
			log.Fatalf("rm: serve failed: %v", err)
		}
	}
}

// ── Database ─────────────────────────────────────────────────────────

func openDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(2)
	migration, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read migration: %w", err)
	}
	if _, err := conn.Exec(string(migration)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply migration: %w", err)
	}
	return conn, nil
}

// ── Session ──────────────────────────────────────────────────────────

func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (a *App) createAdminSession() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	_, err := a.db.Exec("INSERT INTO admin_sessions (token_hash, expires_at) VALUES (?, ?)",
		sha256Hash(token), time.Now().Add(7*24*time.Hour).UTC().Format(time.RFC3339))
	return token, err
}

func (a *App) validateAdminSession(r *http.Request) bool {
	cookie, err := r.Cookie("rm_admin")
	if err != nil {
		return false
	}
	var expiresAt string
	if a.db.QueryRow("SELECT expires_at FROM admin_sessions WHERE token_hash = ?",
		sha256Hash(cookie.Value)).Scan(&expiresAt) != nil {
		return false
	}
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(expires) {
		a.db.Exec("DELETE FROM admin_sessions WHERE token_hash = ?", sha256Hash(cookie.Value))
		return false
	}
	return true
}

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.validateAdminSession(r) {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

// ── Data Helpers ─────────────────────────────────────────────────────

func (a *App) getTournamentStatus() string {
	var status string
	a.db.QueryRow("SELECT status FROM tournament WHERE id = 1").Scan(&status)
	if status == "" {
		return "registration"
	}
	return status
}

func (a *App) getPlayers() []Player {
	rows, err := a.db.Query("SELECT id, name FROM players ORDER BY id")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var players []Player
	for rows.Next() {
		var p Player
		rows.Scan(&p.ID, &p.Name)
		players = append(players, p)
	}
	return players
}

func (a *App) getTeamByID(id int) *Team {
	var t Team
	var p1id, p2id int
	if a.db.QueryRow("SELECT id, player1_id, player2_id, name FROM teams WHERE id = ?", id).
		Scan(&t.ID, &p1id, &p2id, &t.Name) != nil {
		return nil
	}
	a.db.QueryRow("SELECT id, name FROM players WHERE id = ?", p1id).Scan(&t.Player1.ID, &t.Player1.Name)
	a.db.QueryRow("SELECT id, name FROM players WHERE id = ?", p2id).Scan(&t.Player2.ID, &t.Player2.Name)
	return &t
}

func (a *App) getTeams() []Team {
	rows, err := a.db.Query("SELECT id FROM teams ORDER BY id")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var teams []Team
	for rows.Next() {
		var id int
		rows.Scan(&id)
		if t := a.getTeamByID(id); t != nil {
			teams = append(teams, *t)
		}
	}
	return teams
}

func (a *App) getAllMatches() []Match {
	rows, err := a.db.Query(
		"SELECT id, round, position, seq, team1_id, team2_id, score1, score2, winner_id, status FROM matches ORDER BY seq, id")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var matches []Match
	for rows.Next() {
		var m Match
		var t1, t2, w sql.NullInt64
		rows.Scan(&m.ID, &m.Round, &m.Position, &m.Seq, &t1, &t2, &m.Score1, &m.Score2, &w, &m.Status)
		if t1.Valid {
			m.Team1 = a.getTeamByID(int(t1.Int64))
		}
		if t2.Valid {
			m.Team2 = a.getTeamByID(int(t2.Int64))
		}
		if w.Valid {
			m.Winner = a.getTeamByID(int(w.Int64))
		}
		matches = append(matches, m)
	}
	return matches
}

// ── Tournament Logic ─────────────────────────────────────────────────

func (a *App) generateBracket(players []Player) error {
	if len(players) != 8 {
		return fmt.Errorf("need exactly 8 players, got %d", len(players))
	}

	// Build a name→player map for fixed pairings.
	byName := make(map[string]Player)
	for _, p := range players {
		byName[strings.ToLower(p.Name)] = p
	}

	// Fixed teams and matchups.
	pairings := [][2]string{
		{"condeco", "maria"},   // team 1 (semi 1)
		{"jfig", "pp13"},      // team 2 (semi 1)
		{"gordo", "rosinha"},  // team 3 (semi 2)
		{"rv", "spadz"},       // team 4 (semi 2)
	}

	var teams []int64
	for _, pair := range pairings {
		p1, ok1 := byName[pair[0]]
		p2, ok2 := byName[pair[1]]
		if !ok1 || !ok2 {
			return fmt.Errorf("player not found: %s or %s", pair[0], pair[1])
		}
		name := p1.Name + " & " + p2.Name
		res, err := a.db.Exec("INSERT INTO teams (player1_id, player2_id, name) VALUES (?, ?, ?)",
			p1.ID, p2.ID, name)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		teams = append(teams, id)
	}

	// Create bracket: 4 teams → 2 semis → 3rd place + final = 4 matches.
	// Semi 1: team[0] vs team[1]
	a.db.Exec("INSERT INTO matches (round, position, team1_id, team2_id, status, seq) VALUES (1, 1, ?, ?, 'pending', 1)",
		teams[0], teams[1])
	// Semi 2: team[2] vs team[3]
	a.db.Exec("INSERT INTO matches (round, position, team1_id, team2_id, status, seq) VALUES (1, 2, ?, ?, 'pending', 2)",
		teams[2], teams[3])
	// 3rd place match (round 2, position 2): losers of semis
	a.db.Exec("INSERT INTO matches (round, position, status, seq) VALUES (2, 2, 'pending', 3)")
	// Final (round 2, position 1): winners of semis
	a.db.Exec("INSERT INTO matches (round, position, status, seq) VALUES (2, 1, 'pending', 4)")

	return nil
}

func (a *App) advanceWinner(matchID int, winnerID int) {
	var round, position int
	var t1ID, t2ID sql.NullInt64
	a.db.QueryRow("SELECT round, position, team1_id, team2_id FROM matches WHERE id = ?", matchID).
		Scan(&round, &position, &t1ID, &t2ID)

	// Only advance from semi-finals (round 1).
	if round != 1 {
		return
	}

	// Determine loser.
	var loserID int64
	if t1ID.Valid && int(t1ID.Int64) == winnerID {
		loserID = t2ID.Int64
	} else {
		loserID = t1ID.Int64
	}

	// Winner goes to the final (round 2, position 1).
	var finalID int
	if a.db.QueryRow("SELECT id FROM matches WHERE round = 2 AND position = 1").Scan(&finalID) == nil {
		if position == 1 {
			a.db.Exec("UPDATE matches SET team1_id = ? WHERE id = ?", winnerID, finalID)
		} else {
			a.db.Exec("UPDATE matches SET team2_id = ? WHERE id = ?", winnerID, finalID)
		}
	}

	// Loser goes to 3rd place match (round 2, position 2).
	var thirdID int
	if a.db.QueryRow("SELECT id FROM matches WHERE round = 2 AND position = 2").Scan(&thirdID) == nil {
		if position == 1 {
			a.db.Exec("UPDATE matches SET team1_id = ? WHERE id = ?", loserID, thirdID)
		} else {
			a.db.Exec("UPDATE matches SET team2_id = ? WHERE id = ?", loserID, thirdID)
		}
	}
}

// ── State ────────────────────────────────────────────────────────────

func (a *App) getState() TournamentState {
	state := TournamentState{
		Status:  a.getTournamentStatus(),
		Players: a.getPlayers(),
		Teams:   a.getTeams(),
		Matches: a.getAllMatches(),
	}
	if state.Players == nil {
		state.Players = []Player{}
	}
	if state.Teams == nil {
		state.Teams = []Team{}
	}
	if state.Matches == nil {
		state.Matches = []Match{}
	}

	// Rounds.
	for _, m := range state.Matches {
		if m.Round > state.Rounds {
			state.Rounds = m.Round
		}
	}

	// Current and next match (by seq).
	for i := range state.Matches {
		if state.Matches[i].Status == "in_progress" {
			state.CurrentMatch = &state.Matches[i]
			break
		}
	}
	if state.CurrentMatch == nil {
		for i := range state.Matches {
			if state.Matches[i].Status == "pending" && state.Matches[i].Team1 != nil && state.Matches[i].Team2 != nil {
				state.CurrentMatch = &state.Matches[i]
				a.db.Exec("UPDATE matches SET status = 'in_progress' WHERE id = ?", state.Matches[i].ID)
				state.CurrentMatch.Status = "in_progress"
				state.Matches[i].Status = "in_progress"
				break
			}
		}
	}
	if state.CurrentMatch != nil {
		for i := range state.Matches {
			if state.Matches[i].ID != state.CurrentMatch.ID &&
				state.Matches[i].Status == "pending" &&
				state.Matches[i].Team1 != nil && state.Matches[i].Team2 != nil {
				state.NextMatch = &state.Matches[i]
				break
			}
		}
	}

	// Champion.
	if state.Status == "in_progress" || state.Status == "finished" {
		for i := len(state.Matches) - 1; i >= 0; i-- {
			if state.Matches[i].Status == "completed" && state.Matches[i].Winner != nil {
				if state.Matches[i].Round == state.Rounds {
					state.Champion = state.Matches[i].Winner
				}
				break
			}
		}
	}

	return state
}

// ── Page Handlers ────────────────────────────────────────────────────

func (a *App) handlePublic(w http.ResponseWriter, r *http.Request) {
	state := a.getState()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, "public.html", state); err != nil {
		log.Printf("rm: render public: %v", err)
	}
}

func (a *App) handleBracket(w http.ResponseWriter, r *http.Request) {
	state := a.getState()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, "bracket.html", state); err != nil {
		log.Printf("rm: render bracket: %v", err)
	}
}

func (a *App) handleAdmin(w http.ResponseWriter, r *http.Request) {
	state := a.getState()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, "admin.html", state); err != nil {
		log.Printf("rm: render admin: %v", err)
	}
}

func (a *App) handleAdminLoginPage(w http.ResponseWriter, r *http.Request) {
	if a.validateAdminSession(r) {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	a.tmpl.ExecuteTemplate(w, "admin_login.html", nil)
}

func (a *App) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if strings.TrimSpace(r.FormValue("passphrase")) != a.passphrase {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.ExecuteTemplate(w, "admin_login.html", map[string]string{"Error": "Wrong passphrase"})
		return
	}
	token, err := a.createAdminSession()
	if err != nil {
		http.Error(w, "session failed", http.StatusInternalServerError)
		return
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{Name: "rm_admin", Value: token, Path: "/",
		MaxAge: 7 * 24 * 3600, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (a *App) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("rm_admin"); err == nil {
		a.db.Exec("DELETE FROM admin_sessions WHERE token_hash = ?", sha256Hash(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: "rm_admin", Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true, Expires: time.Unix(0, 0)})
	http.Redirect(w, r, "/", http.StatusFound)
}

// ── API Handlers ─────────────────────────────────────────────────────

func (a *App) handleAddPlayer(w http.ResponseWriter, r *http.Request) {
	if a.getTournamentStatus() != "registration" {
		http.Error(w, "tournament already started", http.StatusBadRequest)
		return
	}
	var input struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	result, err := a.db.Exec("INSERT INTO players (name) VALUES (?)", name)
	if err != nil {
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	id, _ := result.LastInsertId()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":%d,"name":%q}`, id, name)
}

func (a *App) handleRemovePlayer(w http.ResponseWriter, r *http.Request) {
	if a.getTournamentStatus() != "registration" {
		http.Error(w, "tournament already started", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	res, err := a.db.Exec("DELETE FROM players WHERE id = ?", id)
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

func (a *App) handleStartTournament(w http.ResponseWriter, r *http.Request) {
	if a.getTournamentStatus() != "registration" {
		http.Error(w, "tournament already started", http.StatusBadRequest)
		return
	}
	players := a.getPlayers()
	if len(players) != 8 {
		http.Error(w, fmt.Sprintf("need exactly 8 players, got %d", len(players)), http.StatusBadRequest)
		return
	}
	if err := a.generateBracket(players); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.db.Exec("UPDATE tournament SET status = 'in_progress' WHERE id = 1")
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"started":true}`)
}

func (a *App) handleResetTournament(w http.ResponseWriter, r *http.Request) {
	a.db.Exec("DELETE FROM matches")
	a.db.Exec("DELETE FROM teams")
	a.db.Exec("DELETE FROM players")
	a.db.Exec("UPDATE tournament SET status = 'registration' WHERE id = 1")
	for _, name := range defaultPlayers {
		a.db.Exec("INSERT INTO players (name) VALUES (?)", name)
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"reset":true}`)
}

func (a *App) handleRecordScore(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var input struct {
		Score1 int `json:"score1"`
		Score2 int `json:"score2"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if (input.Score1 != 3 && input.Score2 != 3) || (input.Score1 == 3 && input.Score2 == 3) {
		http.Error(w, "one team must reach exactly 3 to win", http.StatusBadRequest)
		return
	}
	if input.Score1 < 0 || input.Score2 < 0 || input.Score1 > 3 || input.Score2 > 3 {
		http.Error(w, "scores must be between 0 and 3", http.StatusBadRequest)
		return
	}

	var t1ID, t2ID sql.NullInt64
	var matchStatus string
	err = a.db.QueryRow("SELECT team1_id, team2_id, status FROM matches WHERE id = ?", id).
		Scan(&t1ID, &t2ID, &matchStatus)
	if err != nil {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}
	if matchStatus == "completed" {
		http.Error(w, "match already completed", http.StatusBadRequest)
		return
	}
	if !t1ID.Valid || !t2ID.Valid {
		http.Error(w, "match doesn't have both teams yet", http.StatusBadRequest)
		return
	}

	var winnerID int64
	if input.Score1 == 6 {
		winnerID = t1ID.Int64
	} else {
		winnerID = t2ID.Int64
	}

	a.db.Exec("UPDATE matches SET score1 = ?, score2 = ?, winner_id = ?, status = 'completed' WHERE id = ?",
		input.Score1, input.Score2, winnerID, id)

	a.advanceWinner(id, int(winnerID))

	// Check if all matches done.
	var remaining int
	a.db.QueryRow("SELECT COUNT(*) FROM matches WHERE status != 'completed'").Scan(&remaining)
	if remaining == 0 {
		a.db.Exec("UPDATE tournament SET status = 'finished' WHERE id = 1")
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"winner_id":%d}`, winnerID)
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request) {
	state := a.getState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

// ── Logging ──────────────────────────────────────────────────────────

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
