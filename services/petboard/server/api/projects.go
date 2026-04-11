package api

import (
	"net/http"

	"petboard/service"
)

// listProjects handles GET /api/projects?include_archived=1.
func (a *API) listProjects(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("include_archived") == "1"
	projects, err := a.Svc.ListProjects(includeArchived)
	if err != nil {
		writeError(w, err)
		return
	}
	if projects == nil {
		projects = []service.Project{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

// createProject handles POST /api/projects.
func (a *API) createProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string            `json:"name"`
		Problem     string            `json:"problem"`
		Priority    service.Priority  `json:"priority"`
		Description *string           `json:"description"`
		Color       *string           `json:"color"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	p, err := a.Svc.CreateProject(service.CreateProjectInput{
		Name:        body.Name,
		Problem:     body.Problem,
		Priority:    body.Priority,
		Description: body.Description,
		Color:       body.Color,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// getProject handles GET /api/projects/{slug}.
func (a *API) getProject(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	detail, err := a.Svc.GetProject(slug)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// updateProject handles PATCH /api/projects/{slug}.
func (a *API) updateProject(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var body struct {
		Name        *string           `json:"name"`
		Problem     *string           `json:"problem"`
		Description *string           `json:"description"`
		Priority    *service.Priority `json:"priority"`
		Color       *string           `json:"color"`
		CanvasX     *float64          `json:"canvas_x"`
		CanvasY     *float64          `json:"canvas_y"`
		Archived    *bool             `json:"archived"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	detail, err := a.Svc.UpdateProject(slug, service.UpdateProjectInput{
		Name:        body.Name,
		Problem:     body.Problem,
		Description: body.Description,
		Priority:    body.Priority,
		Color:       body.Color,
		CanvasX:     body.CanvasX,
		CanvasY:     body.CanvasY,
		Archived:    body.Archived,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// deleteProject handles DELETE /api/projects/{slug}?hard=1.
func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	hard := r.URL.Query().Get("hard") == "1"
	if err := a.Svc.DeleteProject(slug, hard); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
