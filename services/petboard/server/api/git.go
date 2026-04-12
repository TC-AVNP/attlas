package api

import (
	"net/http"

	"petboard/service"
)

// linkRepo handles POST /api/projects/{slug}/repos.
func (a *API) linkRepo(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var body struct {
		RepoPath       string  `json:"repo_path"`
		AuthorFilter   *string `json:"author_filter"`
		SessionGapMin  *int64  `json:"session_gap_min"`
		FirstCommitMin *int64  `json:"first_commit_min"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	repo, err := a.Svc.LinkRepo(slug, service.LinkRepoInput{
		RepoPath:       body.RepoPath,
		AuthorFilter:   body.AuthorFilter,
		SessionGapMin:  body.SessionGapMin,
		FirstCommitMin: body.FirstCommitMin,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	a.publish("repo.linked", map[string]any{"slug": slug, "repo_path": body.RepoPath})
	writeJSON(w, http.StatusCreated, repo)
}

// unlinkRepo handles DELETE /api/repos/{id}.
func (a *API) unlinkRepo(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid repo id"))
		return
	}
	if err := a.Svc.UnlinkRepo(id); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unlinked"})
}

// patchRepo handles PATCH /api/repos/{id} — update cursor without syncing.
func (a *API) patchRepo(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("invalid repo id"))
		return
	}
	var body struct {
		LastSyncedSHA *string `json:"last_synced_sha"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.LastSyncedSHA == nil {
		writeError(w, wrapInvalid("last_synced_sha is required"))
		return
	}
	if err := a.Svc.UpdateRepoSHA(id, *body.LastSyncedSHA); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// syncRepos handles POST /api/projects/{slug}/repos/sync.
func (a *API) syncRepos(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	results, err := a.Svc.SyncProjectRepos(slug)
	if err != nil {
		writeError(w, err)
		return
	}
	a.publish("repo.synced", map[string]any{"slug": slug})
	writeJSON(w, http.StatusOK, results)
}
