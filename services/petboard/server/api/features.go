package api

import (
	"net/http"

	"petboard/service"
)

// createFeature handles POST /api/projects/{slug}/features.
func (a *API) createFeature(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var body struct {
		Title       string  `json:"title"`
		Description *string `json:"description"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	f, err := a.Svc.CreateFeature(slug, service.CreateFeatureInput{
		Title:       body.Title,
		Description: body.Description,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

// updateFeature handles PATCH /api/features/{id}.
func (a *API) updateFeature(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("feature id must be numeric"))
		return
	}
	var body struct {
		Title       *string         `json:"title"`
		Description *string         `json:"description"`
		Status      *service.Status `json:"status"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	f, err := a.Svc.UpdateFeature(id, service.UpdateFeatureInput{
		Title:       body.Title,
		Description: body.Description,
		Status:      body.Status,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// deleteFeature handles DELETE /api/features/{id}.
func (a *API) deleteFeature(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("feature id must be numeric"))
		return
	}
	if err := a.Svc.DeleteFeature(id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
