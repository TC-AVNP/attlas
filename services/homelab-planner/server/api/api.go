// Package api implements the REST HTTP surface of homelab-planner.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"homelab-planner/service"
)

type API struct {
	Svc *service.Service
}

func (a *API) Register(mux *http.ServeMux) {
	// Steps
	mux.HandleFunc("GET /api/steps", a.listSteps)
	mux.HandleFunc("POST /api/steps", a.createStep)
	mux.HandleFunc("GET /api/steps/{id}", a.getStep)
	mux.HandleFunc("PATCH /api/steps/{id}", a.updateStep)
	mux.HandleFunc("DELETE /api/steps/{id}", a.deleteStep)

	// Checklist items (nested under steps for create)
	mux.HandleFunc("POST /api/steps/{id}/items", a.createItem)
	mux.HandleFunc("PATCH /api/items/{id}", a.updateItem)
	mux.HandleFunc("DELETE /api/items/{id}", a.deleteItem)

	// Item options (nested under items for create)
	mux.HandleFunc("POST /api/items/{id}/options", a.createOption)
	mux.HandleFunc("PATCH /api/options/{id}", a.updateOption)
	mux.HandleFunc("DELETE /api/options/{id}", a.deleteOption)

	// Build log (nested under steps for create)
	mux.HandleFunc("POST /api/steps/{id}/log", a.createLogEntry)
	mux.HandleFunc("PATCH /api/log/{id}", a.updateLogEntry)
	mux.HandleFunc("DELETE /api/log/{id}", a.deleteLogEntry)
}

// --- Steps -----------------------------------------------------------------

func (a *API) listSteps(w http.ResponseWriter, r *http.Request) {
	steps, err := a.Svc.ListSteps()
	if err != nil {
		writeError(w, err)
		return
	}
	if steps == nil {
		steps = []service.Step{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"steps": steps})
}

func (a *API) createStep(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	step, err := a.Svc.CreateStep(service.CreateStepInput{
		Title: body.Title, Description: body.Description,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, step)
}

func (a *API) getStep(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid step id"))
		return
	}
	detail, err := a.Svc.GetStep(id)
	if err != nil {
		writeError(w, err)
		return
	}
	if detail.Items == nil {
		detail.Items = []service.ChecklistItem{}
	}
	if detail.BuildLog == nil {
		detail.BuildLog = []service.BuildLogEntry{}
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *API) updateStep(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid step id"))
		return
	}
	var body struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Position    *int64  `json:"position"`
		Completed   *bool   `json:"completed"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	step, err := a.Svc.UpdateStep(id, service.UpdateStepInput{
		Title: body.Title, Description: body.Description,
		Position: body.Position, Completed: body.Completed,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, step)
}

func (a *API) deleteStep(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid step id"))
		return
	}
	if err := a.Svc.DeleteStep(id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Checklist Items -------------------------------------------------------

func (a *API) createItem(w http.ResponseWriter, r *http.Request) {
	stepID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid step id"))
		return
	}
	var body struct {
		Name        string `json:"name"`
		BudgetCents *int64 `json:"budget_cents"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	item, err := a.Svc.CreateItem(stepID, service.CreateItemInput{
		Name: body.Name, BudgetCents: body.BudgetCents,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) updateItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid item id"))
		return
	}
	var body struct {
		Name             *string            `json:"name"`
		BudgetCents      *int64             `json:"budget_cents"`
		ActualCostCents  *int64             `json:"actual_cost_cents"`
		Status           *service.ItemStatus `json:"status"`
		SelectedOptionID *int64             `json:"selected_option_id"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	item, err := a.Svc.UpdateItem(id, service.UpdateItemInput{
		Name: body.Name, BudgetCents: body.BudgetCents,
		ActualCostCents: body.ActualCostCents, Status: body.Status,
		SelectedOptionID: body.SelectedOptionID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid item id"))
		return
	}
	if err := a.Svc.DeleteItem(id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Item Options ----------------------------------------------------------

func (a *API) createOption(w http.ResponseWriter, r *http.Request) {
	itemID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid item id"))
		return
	}
	var body struct {
		Name       string `json:"name"`
		URL        string `json:"url"`
		PriceCents *int64 `json:"price_cents"`
		Notes      string `json:"notes"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	opt, err := a.Svc.CreateOption(itemID, service.CreateOptionInput{
		Name: body.Name, URL: body.URL, PriceCents: body.PriceCents, Notes: body.Notes,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, opt)
}

func (a *API) updateOption(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid option id"))
		return
	}
	var body struct {
		Name       *string `json:"name"`
		URL        *string `json:"url"`
		PriceCents *int64  `json:"price_cents"`
		Notes      *string `json:"notes"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	opt, err := a.Svc.UpdateOption(id, service.UpdateOptionInput{
		Name: body.Name, URL: body.URL, PriceCents: body.PriceCents, Notes: body.Notes,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, opt)
}

func (a *API) deleteOption(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid option id"))
		return
	}
	if err := a.Svc.DeleteOption(id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Build Log -------------------------------------------------------------

func (a *API) createLogEntry(w http.ResponseWriter, r *http.Request) {
	stepID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid step id"))
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	entry, err := a.Svc.CreateLogEntry(stepID, service.CreateLogEntryInput{Body: body.Body})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (a *API) updateLogEntry(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid log entry id"))
		return
	}
	var body struct {
		Body *string `json:"body"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	entry, err := a.Svc.UpdateLogEntry(id, service.UpdateLogEntryInput{Body: body.Body})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (a *API) deleteLogEntry(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid log entry id"))
		return
	}
	if err := a.Svc.DeleteLogEntry(id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
