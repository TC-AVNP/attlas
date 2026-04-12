// Package api implements the REST HTTP surface of splitsies.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"splitsies/events"
	"splitsies/service"
)

type API struct {
	Svc    *service.Service
	Events *events.Broker

	// Google OAuth config
	GoogleClientID     string
	GoogleClientSecret string
	BaseURL            string // e.g. "https://splitsies.attlas.uk" or "http://localhost:7691"
	LocalBypass        bool   // dev mode: auto-login as first admin
}

type contextKey string

const userKey contextKey = "user"

func (a *API) publish(eventType string, payload any) {
	if a.Events == nil {
		return
	}
	a.Events.Publish(events.Event{Type: eventType, Payload: payload})
}

func (a *API) Register(mux *http.ServeMux) {
	// Auth (no auth required)
	mux.HandleFunc("GET /api/auth/google", a.authGoogle)
	mux.HandleFunc("GET /api/auth/callback", a.authCallback)
	mux.HandleFunc("POST /api/auth/logout", a.authLogout)
	mux.HandleFunc("GET /api/auth/me", a.authMe)

	// Protected routes
	mux.Handle("GET /api/users", a.requireAuth(http.HandlerFunc(a.listUsers)))
	mux.Handle("POST /api/users", a.requireAdmin(http.HandlerFunc(a.addUser)))
	mux.Handle("DELETE /api/users/{id}", a.requireAdmin(http.HandlerFunc(a.removeUser)))

	mux.Handle("GET /api/groups", a.requireAuth(http.HandlerFunc(a.listGroups)))
	mux.Handle("POST /api/groups", a.requireAdmin(http.HandlerFunc(a.createGroup)))
	mux.Handle("GET /api/groups/{id}", a.requireAuth(http.HandlerFunc(a.getGroup)))
	mux.Handle("POST /api/groups/{id}/members", a.requireAdmin(http.HandlerFunc(a.addGroupMember)))

	mux.Handle("GET /api/categories", a.requireAuth(http.HandlerFunc(a.listCategories)))
	mux.Handle("POST /api/categories", a.requireAuth(http.HandlerFunc(a.createCategory)))

	mux.Handle("GET /api/groups/{id}/expenses", a.requireAuth(http.HandlerFunc(a.listExpenses)))
	mux.Handle("POST /api/groups/{id}/expenses", a.requireAuth(http.HandlerFunc(a.addExpense)))
	mux.Handle("DELETE /api/expenses/{id}", a.requireAuth(http.HandlerFunc(a.deleteExpense)))

	mux.Handle("GET /api/groups/{id}/settlements", a.requireAuth(http.HandlerFunc(a.listSettlements)))
	mux.Handle("POST /api/groups/{id}/settlements", a.requireAuth(http.HandlerFunc(a.addSettlement)))
	mux.Handle("DELETE /api/settlements/{id}", a.requireAuth(http.HandlerFunc(a.deleteSettlement)))

	mux.Handle("GET /api/groups/{id}/balances", a.requireAuth(http.HandlerFunc(a.getGroupBalances)))
	mux.Handle("GET /api/groups/{id}/suggested-payments", a.requireAuth(http.HandlerFunc(a.suggestPayments)))
	mux.Handle("GET /api/groups/{id}/timeline", a.requireAuth(http.HandlerFunc(a.getTimeline)))

	mux.Handle("GET /api/balances", a.requireAuth(http.HandlerFunc(a.getMyBalances)))
	mux.Handle("GET /api/overview", a.requireAuth(http.HandlerFunc(a.getOverview)))
	mux.Handle("GET /api/overview/{month}", a.requireAuth(http.HandlerFunc(a.getOverviewMonth)))

	if a.Events != nil {
		mux.Handle("GET /api/events", a.Events.Handler())
	}
}

// --- Middleware -------------------------------------------------------------

func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.authenticateRequest(r)
		if err != nil {
			if errors.Is(err, service.ErrUnauthorized) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
			return
		}
		ctx := context.WithValue(r.Context(), userKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *API) requireAdmin(next http.Handler) http.Handler {
	return a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Context().Value(userKey).(*service.User)
		if !user.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin required"})
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (a *API) authenticateRequest(r *http.Request) (*service.User, error) {
	// Dev mode: auto-login as first user
	if a.LocalBypass && r.Header.Get("X-Forwarded-For") == "" {
		return a.devUser()
	}

	cookie, err := r.Cookie("splitsies_session")
	if err != nil {
		return nil, service.ErrUnauthorized
	}
	return a.Svc.ValidateSession(cookie.Value)
}

func (a *API) devUser() (*service.User, error) {
	users, err := a.Svc.ListUsers()
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.IsAdmin && u.IsActive {
			return &u, nil
		}
	}
	// No admin yet — create one
	u, err := a.Svc.AddUser("dev@localhost", true)
	if err != nil {
		return nil, err
	}
	// Set name
	a.Svc.FindOrCreateUser("dev@localhost", "Dev User", "")
	u.Name = "Dev User"
	return u, nil
}

func currentUser(r *http.Request) *service.User {
	return r.Context().Value(userKey).(*service.User)
}

// --- Users -----------------------------------------------------------------

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.Svc.ListUsers()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (a *API) addUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email   string `json:"email"`
		IsAdmin bool   `json:"is_admin"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	u, err := a.Svc.AddUser(body.Email, body.IsAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publish("user.added", u)
	writeJSON(w, http.StatusCreated, u)
}

func (a *API) removeUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid user id"))
		return
	}
	if err := a.Svc.RemoveUser(id); err != nil {
		writeError(w, err)
		return
	}
	a.publish("user.removed", map[string]int64{"user_id": id})
	w.WriteHeader(http.StatusNoContent)
}

// --- Groups ----------------------------------------------------------------

func (a *API) listGroups(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	groups, err := a.Svc.ListGroupsForUser(user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if groups == nil {
		groups = []service.GroupDetail{}
	}
	writeJSON(w, http.StatusOK, groups)
}

func (a *API) createGroup(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		PhotoURL    string `json:"photo_url"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	g, err := a.Svc.CreateGroup(user.ID, service.CreateGroupInput{
		Name:        body.Name,
		Description: body.Description,
		PhotoURL:    body.PhotoURL,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	a.publish("group.created", g)
	writeJSON(w, http.StatusCreated, g)
}

func (a *API) getGroup(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid group id"))
		return
	}
	g, err := a.Svc.GetGroup(id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (a *API) addGroupMember(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	groupID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid group id"))
		return
	}
	var body struct {
		UserID int64 `json:"user_id"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := a.Svc.AddGroupMember(groupID, user.ID, body.UserID); err != nil {
		writeError(w, err)
		return
	}
	a.publish("group.member_added", map[string]int64{"group_id": groupID, "user_id": body.UserID})
	g, _ := a.Svc.GetGroup(groupID)
	writeJSON(w, http.StatusOK, g)
}

// --- Categories ------------------------------------------------------------

func (a *API) listCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := a.Svc.ListCategories()
	if err != nil {
		writeError(w, err)
		return
	}
	if cats == nil {
		cats = []service.Category{}
	}
	writeJSON(w, http.StatusOK, cats)
}

func (a *API) createCategory(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	c, err := a.Svc.CreateCategory(body.Name, user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// --- Expenses --------------------------------------------------------------

func (a *API) listExpenses(w http.ResponseWriter, r *http.Request) {
	groupID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid group id"))
		return
	}
	category := r.URL.Query().Get("category")
	search := r.URL.Query().Get("search")
	exps, err := a.Svc.ListExpenses(groupID, category, search)
	if err != nil {
		writeError(w, err)
		return
	}
	if exps == nil {
		exps = []service.Expense{}
	}
	writeJSON(w, http.StatusOK, exps)
}

func (a *API) addExpense(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	groupID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid group id"))
		return
	}
	var body struct {
		PaidBy      int64  `json:"paid_by"`
		Amount      int64  `json:"amount"`
		Description string `json:"description"`
		CategoryID  *int64 `json:"category_id"`
		SplitType   string `json:"split_type"`
		Splits      []struct {
			UserID int64 `json:"user_id"`
			Amount int64 `json:"amount"`
		} `json:"splits"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	splits := make([]service.SplitInput, len(body.Splits))
	for i, s := range body.Splits {
		splits[i] = service.SplitInput{UserID: s.UserID, Amount: s.Amount}
	}
	exp, err := a.Svc.AddExpense(user.ID, service.AddExpenseInput{
		GroupID:     groupID,
		PaidBy:      body.PaidBy,
		Amount:      body.Amount,
		Description: body.Description,
		CategoryID:  body.CategoryID,
		SplitType:   body.SplitType,
		Splits:      splits,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	a.publish("expense.created", exp)
	writeJSON(w, http.StatusCreated, exp)
}

func (a *API) deleteExpense(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid expense id"))
		return
	}
	if err := a.Svc.DeleteExpense(user.ID, id); err != nil {
		writeError(w, err)
		return
	}
	a.publish("expense.deleted", map[string]int64{"expense_id": id})
	w.WriteHeader(http.StatusNoContent)
}

// --- Settlements -----------------------------------------------------------

func (a *API) listSettlements(w http.ResponseWriter, r *http.Request) {
	groupID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid group id"))
		return
	}
	// Use timeline to get settlements
	entries, err := a.Svc.GetTimeline(groupID, "", "")
	if err != nil {
		writeError(w, err)
		return
	}
	// Filter to settlements only
	var settlements []service.TimelineEntry
	for _, e := range entries {
		if e.Type == "settlement" {
			settlements = append(settlements, e)
		}
	}
	if settlements == nil {
		settlements = []service.TimelineEntry{}
	}
	writeJSON(w, http.StatusOK, settlements)
}

func (a *API) addSettlement(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	groupID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid group id"))
		return
	}
	var body struct {
		FromUser int64 `json:"from_user"`
		ToUser   int64 `json:"to_user"`
		Amount   int64 `json:"amount"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	st, err := a.Svc.AddSettlement(user.ID, service.AddSettlementInput{
		GroupID:  groupID,
		FromUser: body.FromUser,
		ToUser:   body.ToUser,
		Amount:   body.Amount,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	a.publish("settlement.created", st)
	writeJSON(w, http.StatusCreated, st)
}

func (a *API) deleteSettlement(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid settlement id"))
		return
	}
	if err := a.Svc.DeleteSettlement(user.ID, id); err != nil {
		writeError(w, err)
		return
	}
	a.publish("settlement.deleted", map[string]int64{"settlement_id": id})
	w.WriteHeader(http.StatusNoContent)
}

// --- Balances --------------------------------------------------------------

func (a *API) getMyBalances(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	total, balances, err := a.Svc.GetBalancesForUser(user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if balances == nil {
		balances = []service.PairBalance{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total_net": total,
		"balances":  balances,
	})
}

func (a *API) getGroupBalances(w http.ResponseWriter, r *http.Request) {
	groupID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid group id"))
		return
	}
	nets, err := a.Svc.GetGroupBalances(groupID)
	if err != nil {
		writeError(w, err)
		return
	}

	// Enrich with user names
	type balanceEntry struct {
		UserID   int64  `json:"user_id"`
		UserName string `json:"user_name"`
		Net      int64  `json:"net"`
	}
	var entries []balanceEntry
	for uid, net := range nets {
		var name string
		a.Svc.DB.QueryRow(`SELECT COALESCE(NULLIF(name, ''), email) FROM users WHERE id = ?`, uid).Scan(&name)
		entries = append(entries, balanceEntry{UserID: uid, UserName: name, Net: net})
	}
	if entries == nil {
		entries = []balanceEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (a *API) suggestPayments(w http.ResponseWriter, r *http.Request) {
	groupID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid group id"))
		return
	}
	payments, err := a.Svc.SuggestPayments(groupID)
	if err != nil {
		writeError(w, err)
		return
	}
	if payments == nil {
		payments = []service.SuggestedPayment{}
	}
	writeJSON(w, http.StatusOK, payments)
}

// --- Timeline --------------------------------------------------------------

func (a *API) getTimeline(w http.ResponseWriter, r *http.Request) {
	groupID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid group id"))
		return
	}
	category := r.URL.Query().Get("category")
	search := r.URL.Query().Get("search")
	entries, err := a.Svc.GetTimeline(groupID, category, search)
	if err != nil {
		writeError(w, err)
		return
	}
	if entries == nil {
		entries = []service.TimelineEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// --- Overview --------------------------------------------------------------

func (a *API) getOverview(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	months, err := a.Svc.GetMonthlyOverview(user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if months == nil {
		months = []service.MonthSummary{}
	}
	writeJSON(w, http.StatusOK, months)
}

func (a *API) getOverviewMonth(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	month := r.PathValue("month")
	detail, err := a.Svc.GetMonthDetail(user.ID, month)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// --- Shared helpers --------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, service.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, service.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
	case errors.Is(err, service.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
	default:
		log.Printf("api: unexpected error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}

func decodeBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return wrapInvalid("malformed JSON body: " + err.Error())
	}
	return nil
}

func wrapInvalid(msg string) error {
	return &wrappedInvalid{msg: msg}
}

type wrappedInvalid struct{ msg string }

func (e *wrappedInvalid) Error() string { return "invalid input: " + e.msg }
func (e *wrappedInvalid) Unwrap() error { return service.ErrInvalidInput }

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}
