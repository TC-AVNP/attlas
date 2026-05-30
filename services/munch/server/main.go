// munch — Meal-prep companion: pick a dish, fetch ingredients, shop, rate.
//
// Configuration via environment variables:
//
//	MUNCH_PORT           TCP port (default 7698)
//	MUNCH_DB             SQLite path (default /var/lib/munch/munch.db)
//	MUNCH_ANTHROPIC_KEY  Anthropic API key for ingredient fetching
package main

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
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

type App struct {
	db           *sql.DB
	tmpl         *template.Template
	anthropicKey string
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

type Dish struct {
	ID        int          `json:"id"`
	Name      string       `json:"name"`
	Servings  int          `json:"servings"`
	CreatedBy string       `json:"created_by"`
	CreatedAt string       `json:"created_at"`
	Ingredients []Ingredient `json:"ingredients,omitempty"`
	AvgRating *float64     `json:"avg_rating,omitempty"`
	NumRatings int         `json:"num_ratings,omitempty"`
}

type Ingredient struct {
	ID        int    `json:"id"`
	DishID    int    `json:"dish_id"`
	Name      string `json:"name"`
	Qty       string `json:"qty"`
	Unit      string `json:"unit"`
	SortOrder int    `json:"sort_order"`
}

type ShoppingSession struct {
	ID          int              `json:"id"`
	DishID      int              `json:"dish_id"`
	DishName    string           `json:"dish_name"`
	CreatedBy   string           `json:"created_by"`
	CreatedAt   string           `json:"created_at"`
	CompletedAt *string          `json:"completed_at,omitempty"`
	Items       []ShoppingItem   `json:"items"`
	Total       int              `json:"total"`
	Checked     int              `json:"checked"`
}

type ShoppingItem struct {
	IngredientID int    `json:"ingredient_id"`
	Name         string `json:"name"`
	Qty          string `json:"qty"`
	Unit         string `json:"unit"`
	IsChecked    bool   `json:"is_checked"`
}

type Rating struct {
	ID        int    `json:"id"`
	DishID    int    `json:"dish_id"`
	Rater     string `json:"rater"`
	Score     int    `json:"score"`
	CreatedAt string `json:"created_at"`
}

type RankedDish struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	AvgRating  float64 `json:"avg_rating"`
	NumRatings int     `json:"num_ratings"`
	LastCooked string  `json:"last_cooked"`
}

// --- Auth ---

func getAuthUser(r *http.Request) string {
	return r.Header.Get("X-Auth-User")
}

// --- Main ---

func main() {
	port := envOr("MUNCH_PORT", "7703")
	dbPath := envOr("MUNCH_DB", "/var/lib/munch/munch.db")
	anthropicKey := os.Getenv("MUNCH_ANTHROPIC_KEY")

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	funcMap := template.FuncMap{
		"json": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"inc": func(i int) int { return i + 1 },
		"deref": func(f *float64) float64 {
			if f == nil {
				return 0
			}
			return *f
		},
	}
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	app := &App{db: db, tmpl: tmpl, anthropicKey: anthropicKey}

	mux := http.NewServeMux()

	// Pages
	mux.HandleFunc("GET /", app.handleIndex)
	mux.HandleFunc("GET /shop/{id}", app.handleShopPage)

	// API — Dishes
	mux.HandleFunc("POST /api/dishes", app.handleCreateDish)
	mux.HandleFunc("GET /api/dishes/{id}", app.handleGetDish)
	mux.HandleFunc("DELETE /api/dishes/{id}", app.handleDeleteDish)

	// API — Shopping
	mux.HandleFunc("POST /api/dishes/{id}/shop", app.handleStartShopping)
	mux.HandleFunc("GET /api/shop/{id}", app.handleGetSession)
	mux.HandleFunc("PUT /api/shop/{id}/toggle/{ingredientId}", app.handleToggleCheck)

	// API — Ratings
	mux.HandleFunc("POST /api/dishes/{id}/rate", app.handleRate)
	mux.HandleFunc("GET /api/rankings", app.handleRankings)

	addr := "127.0.0.1:" + port
	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		log.Printf("munch: listening on %s", addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("munch: shutting down")
	srv.Close()
}

// --- Page handlers ---

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	email := getAuthUser(r)
	dishes := a.listDishes()
	rankings := a.getRankings()
	activeSessions := a.getActiveSessions(email)

	a.tmpl.ExecuteTemplate(w, "index.html", map[string]any{
		"Email":          email,
		"Dishes":         dishes,
		"Rankings":       rankings,
		"ActiveSessions": activeSessions,
	})
}

func (a *App) handleShopPage(w http.ResponseWriter, r *http.Request) {
	sessionID, _ := strconv.Atoi(r.PathValue("id"))
	if sessionID == 0 {
		http.NotFound(w, r)
		return
	}
	session := a.getSession(sessionID)
	if session == nil {
		http.NotFound(w, r)
		return
	}
	a.tmpl.ExecuteTemplate(w, "shop.html", map[string]any{
		"Session": session,
	})
}

// --- Dish API ---

func (a *App) handleCreateDish(w http.ResponseWriter, r *http.Request) {
	email := getAuthUser(r)
	var body struct {
		Name     string `json:"name"`
		Servings int    `json:"servings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Servings < 1 {
		body.Servings = 2
	}

	// Insert dish
	res, err := a.db.Exec("INSERT INTO dishes (name, servings, created_by) VALUES (?, ?, ?)",
		body.Name, body.Servings, email)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	dishID, _ := res.LastInsertId()

	// Fetch ingredients via Anthropic API
	ingredients, err := a.fetchIngredients(body.Name, body.Servings)
	if err != nil {
		log.Printf("ingredient fetch error: %v", err)
		// Still return the dish, just without ingredients
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": dishID, "error": "failed to fetch ingredients: " + err.Error()})
		return
	}

	// Insert ingredients
	for i, ing := range ingredients {
		a.db.Exec("INSERT INTO ingredients (dish_id, name, qty, unit, sort_order) VALUES (?, ?, ?, ?, ?)",
			dishID, ing.Name, ing.Qty, ing.Unit, i)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": dishID})
}

func (a *App) handleGetDish(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if id == 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	dish := a.getDish(id)
	if dish == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dish)
}

func (a *App) handleDeleteDish(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if id == 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	a.db.Exec("DELETE FROM dishes WHERE id = ?", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// --- Shopping API ---

func (a *App) handleStartShopping(w http.ResponseWriter, r *http.Request) {
	email := getAuthUser(r)
	dishID, _ := strconv.Atoi(r.PathValue("id"))
	if dishID == 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	res, err := a.db.Exec("INSERT INTO shopping_sessions (dish_id, created_by) VALUES (?, ?)",
		dishID, email)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	sessionID, _ := res.LastInsertId()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"session_id": sessionID})
}

func (a *App) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if id == 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	session := a.getSession(id)
	if session == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

func (a *App) handleToggleCheck(w http.ResponseWriter, r *http.Request) {
	sessionID, _ := strconv.Atoi(r.PathValue("id"))
	ingredientID, _ := strconv.Atoi(r.PathValue("ingredientId"))
	if sessionID == 0 || ingredientID == 0 {
		http.Error(w, "invalid ids", http.StatusBadRequest)
		return
	}

	// Check if already checked
	var count int
	a.db.QueryRow("SELECT COUNT(*) FROM shopping_checks WHERE session_id = ? AND ingredient_id = ?",
		sessionID, ingredientID).Scan(&count)

	if count > 0 {
		a.db.Exec("DELETE FROM shopping_checks WHERE session_id = ? AND ingredient_id = ?",
			sessionID, ingredientID)
	} else {
		a.db.Exec("INSERT INTO shopping_checks (session_id, ingredient_id) VALUES (?, ?)",
			sessionID, ingredientID)
	}

	// Check if all items are checked — if so, mark session complete
	session := a.getSession(sessionID)
	if session != nil && session.Checked == session.Total {
		a.db.Exec("UPDATE shopping_sessions SET completed_at = datetime('now') WHERE id = ?", sessionID)
	} else {
		a.db.Exec("UPDATE shopping_sessions SET completed_at = NULL WHERE id = ?", sessionID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"checked": count == 0})
}

// --- Rating API ---

func (a *App) handleRate(w http.ResponseWriter, r *http.Request) {
	dishID, _ := strconv.Atoi(r.PathValue("id"))
	if dishID == 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var body struct {
		Rater string `json:"rater"`
		Score int    `json:"score"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Rater) == "" {
		body.Rater = getAuthUser(r)
	}
	if body.Score < 0 || body.Score > 10 {
		http.Error(w, "score must be 0-10", http.StatusBadRequest)
		return
	}

	_, err := a.db.Exec("INSERT INTO ratings (dish_id, rater, score) VALUES (?, ?, ?)",
		dishID, body.Rater, body.Score)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (a *App) handleRankings(w http.ResponseWriter, r *http.Request) {
	rankings := a.getRankings()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rankings)
}

// --- Anthropic API ---

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (a *App) fetchIngredients(dishName string, servings int) ([]Ingredient, error) {
	if a.anthropicKey == "" {
		return nil, fmt.Errorf("MUNCH_ANTHROPIC_KEY not set")
	}

	systemPrompt := `You are a cooking assistant. When given a dish name and serving count, return the full ingredient list as a JSON array. Each item must have: "name" (string), "qty" (string, e.g. "200", "1/2"), "unit" (string, e.g. "g", "ml", "tbsp", "pieces", or "" if unitless). Return ONLY the JSON array, no markdown, no explanation. Use metric units. Be specific about ingredient names (e.g. "extra virgin olive oil" not just "oil").`

	userMsg := fmt.Sprintf("Dish: %s\nServings: %d", dishName, servings)

	reqBody := anthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: userMsg},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.anthropicKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error %d: %s", resp.StatusCode, string(body))
	}

	var apiResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty response from API")
	}

	text := apiResp.Content[0].Text
	// Strip any markdown code fences the model might add
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var items []struct {
		Name string `json:"name"`
		Qty  string `json:"qty"`
		Unit string `json:"unit"`
	}
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		return nil, fmt.Errorf("parse ingredients JSON: %w (raw: %s)", err, text)
	}

	var ingredients []Ingredient
	for _, item := range items {
		ingredients = append(ingredients, Ingredient{
			Name: item.Name,
			Qty:  item.Qty,
			Unit: item.Unit,
		})
	}
	return ingredients, nil
}

// --- DB helpers ---

func (a *App) listDishes() []Dish {
	rows, _ := a.db.Query(`
		SELECT d.id, d.name, d.servings, d.created_by, d.created_at,
		       COALESCE(AVG(r.score), -1), COUNT(r.id)
		FROM dishes d
		LEFT JOIN ratings r ON r.dish_id = d.id
		GROUP BY d.id
		ORDER BY d.created_at DESC
	`)
	defer rows.Close()
	var out []Dish
	for rows.Next() {
		var d Dish
		var avg float64
		var numRatings int
		rows.Scan(&d.ID, &d.Name, &d.Servings, &d.CreatedBy, &d.CreatedAt, &avg, &numRatings)
		if avg >= 0 {
			d.AvgRating = &avg
		}
		d.NumRatings = numRatings
		out = append(out, d)
	}
	if out == nil {
		out = []Dish{}
	}
	return out
}

func (a *App) getDish(id int) *Dish {
	var d Dish
	err := a.db.QueryRow("SELECT id, name, servings, created_by, created_at FROM dishes WHERE id = ?", id).
		Scan(&d.ID, &d.Name, &d.Servings, &d.CreatedBy, &d.CreatedAt)
	if err != nil {
		return nil
	}
	d.Ingredients = a.getDishIngredients(id)

	var avg sql.NullFloat64
	a.db.QueryRow("SELECT AVG(score), COUNT(*) FROM ratings WHERE dish_id = ?", id).
		Scan(&avg, &d.NumRatings)
	if avg.Valid {
		d.AvgRating = &avg.Float64
	}

	return &d
}

func (a *App) getDishIngredients(dishID int) []Ingredient {
	rows, _ := a.db.Query("SELECT id, dish_id, name, qty, unit, sort_order FROM ingredients WHERE dish_id = ? ORDER BY sort_order", dishID)
	defer rows.Close()
	var out []Ingredient
	for rows.Next() {
		var ing Ingredient
		rows.Scan(&ing.ID, &ing.DishID, &ing.Name, &ing.Qty, &ing.Unit, &ing.SortOrder)
		out = append(out, ing)
	}
	if out == nil {
		out = []Ingredient{}
	}
	return out
}

func (a *App) getSession(id int) *ShoppingSession {
	var s ShoppingSession
	var completedAt sql.NullString
	err := a.db.QueryRow(`
		SELECT s.id, s.dish_id, d.name, s.created_by, s.created_at, s.completed_at
		FROM shopping_sessions s JOIN dishes d ON d.id = s.dish_id
		WHERE s.id = ?`, id).
		Scan(&s.ID, &s.DishID, &s.DishName, &s.CreatedBy, &s.CreatedAt, &completedAt)
	if err != nil {
		return nil
	}
	if completedAt.Valid {
		s.CompletedAt = &completedAt.String
	}

	// Get ingredients with check status
	rows, _ := a.db.Query(`
		SELECT i.id, i.name, i.qty, i.unit,
		       CASE WHEN sc.ingredient_id IS NOT NULL THEN 1 ELSE 0 END
		FROM ingredients i
		LEFT JOIN shopping_checks sc ON sc.ingredient_id = i.id AND sc.session_id = ?
		WHERE i.dish_id = ?
		ORDER BY i.sort_order`, id, s.DishID)
	defer rows.Close()

	for rows.Next() {
		var item ShoppingItem
		var checked int
		rows.Scan(&item.IngredientID, &item.Name, &item.Qty, &item.Unit, &checked)
		item.IsChecked = checked == 1
		s.Items = append(s.Items, item)
		s.Total++
		if item.IsChecked {
			s.Checked++
		}
	}
	if s.Items == nil {
		s.Items = []ShoppingItem{}
	}
	return &s
}

func (a *App) getActiveSessions(email string) []ShoppingSession {
	rows, _ := a.db.Query(`
		SELECT s.id, s.dish_id, d.name, s.created_by, s.created_at
		FROM shopping_sessions s JOIN dishes d ON d.id = s.dish_id
		WHERE s.completed_at IS NULL
		ORDER BY s.created_at DESC
	`)
	defer rows.Close()
	var out []ShoppingSession
	for rows.Next() {
		var s ShoppingSession
		rows.Scan(&s.ID, &s.DishID, &s.DishName, &s.CreatedBy, &s.CreatedAt)

		// Count items
		a.db.QueryRow(`
			SELECT COUNT(*), COALESCE(SUM(CASE WHEN sc.ingredient_id IS NOT NULL THEN 1 ELSE 0 END), 0)
			FROM ingredients i
			LEFT JOIN shopping_checks sc ON sc.ingredient_id = i.id AND sc.session_id = ?
			WHERE i.dish_id = ?`, s.ID, s.DishID).Scan(&s.Total, &s.Checked)

		out = append(out, s)
	}
	if out == nil {
		out = []ShoppingSession{}
	}
	return out
}

func (a *App) getRankings() []RankedDish {
	rows, _ := a.db.Query(`
		SELECT d.id, d.name, AVG(r.score) as avg_score, COUNT(r.id), d.created_at
		FROM dishes d
		JOIN ratings r ON r.dish_id = d.id
		GROUP BY d.id
		ORDER BY avg_score DESC, COUNT(r.id) DESC
	`)
	defer rows.Close()
	var out []RankedDish
	for rows.Next() {
		var rd RankedDish
		rows.Scan(&rd.ID, &rd.Name, &rd.AvgRating, &rd.NumRatings, &rd.LastCooked)
		out = append(out, rd)
	}
	if out == nil {
		out = []RankedDish{}
	}
	return out
}
