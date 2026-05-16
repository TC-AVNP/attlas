package api

import (
	"net/http"

	"homelab-planner/service"
)

// --- Wiki Pages --------------------------------------------------------------

func (a *API) listPages(w http.ResponseWriter, r *http.Request) {
	pages, err := a.Svc.ListPages()
	if err != nil {
		writeError(w, err)
		return
	}
	if pages == nil {
		pages = []service.PageSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": pages})
}

func (a *API) createPage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	page, err := a.Svc.CreatePage(service.CreatePageInput{
		Slug: body.Slug, Title: body.Title, Body: body.Body,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, page)
}

func (a *API) getPage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	page, err := a.Svc.GetPage(slug)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) updatePage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var body struct {
		Title *string `json:"title"`
		Body  *string `json:"body"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	page, err := a.Svc.UpdatePage(slug, service.UpdatePageInput{
		Title: body.Title, Body: body.Body,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// --- Journal -----------------------------------------------------------------

func (a *API) listJournal(w http.ResponseWriter, r *http.Request) {
	entries, err := a.Svc.ListJournal()
	if err != nil {
		writeError(w, err)
		return
	}
	if entries == nil {
		entries = []service.JournalSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (a *API) createJournalEntry(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Date  string `json:"date"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	entry, err := a.Svc.CreateJournalEntry(service.CreateJournalInput{
		Date: body.Date, Title: body.Title, Body: body.Body,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (a *API) getJournalEntry(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid journal entry id"))
		return
	}
	entry, err := a.Svc.GetJournalEntry(id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (a *API) updateJournalEntry(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid journal entry id"))
		return
	}
	var body struct {
		Date  *string `json:"date"`
		Title *string `json:"title"`
		Body  *string `json:"body"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	entry, err := a.Svc.UpdateJournalEntry(id, service.UpdateJournalInput{
		Date: body.Date, Title: body.Title, Body: body.Body,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (a *API) deleteJournalEntry(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid journal entry id"))
		return
	}
	if err := a.Svc.DeleteJournalEntry(id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
