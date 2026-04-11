package api

import (
	"net/http"

	"petboard/service"
)

// logEffort handles POST /api/projects/{slug}/effort.
//
// The wire contract accepts minutes directly (matching the DB column)
// rather than hours. MCP tools convert hours → minutes at the boundary;
// REST callers send whatever they store.
func (a *API) logEffort(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var body struct {
		Minutes   int64   `json:"minutes"`
		Note      *string `json:"note"`
		FeatureID *int64  `json:"feature_id"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	log, err := a.Svc.LogEffort(slug, service.LogEffortInput{
		Minutes:   body.Minutes,
		Note:      body.Note,
		FeatureID: body.FeatureID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, log)
}
