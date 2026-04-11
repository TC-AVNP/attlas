package api

import (
	"net/http"

	"petboard/service"
)

// listTodos handles GET /api/todos?include_completed=1.
func (a *API) listTodos(w http.ResponseWriter, r *http.Request) {
	includeCompleted := r.URL.Query().Get("include_completed") == "1"
	todos, err := a.Svc.ListTodos(includeCompleted)
	if err != nil {
		writeError(w, err)
		return
	}
	if todos == nil {
		todos = []service.Todo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"todos": todos})
}

// createTodo handles POST /api/todos.
func (a *API) createTodo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	t, err := a.Svc.CreateTodo(service.CreateTodoInput{Text: body.Text})
	if err != nil {
		writeError(w, err)
		return
	}
	a.publish("todo.created", map[string]any{"id": t.ID})
	writeJSON(w, http.StatusCreated, t)
}

// updateTodo handles PATCH /api/todos/{id}.
func (a *API) updateTodo(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("todo id must be numeric"))
		return
	}
	var body struct {
		Text      *string `json:"text"`
		Completed *bool   `json:"completed"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, err)
		return
	}
	t, err := a.Svc.UpdateTodo(id, service.UpdateTodoInput{
		Text:      body.Text,
		Completed: body.Completed,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	a.publish("todo.updated", map[string]any{"id": id})
	writeJSON(w, http.StatusOK, t)
}

// deleteTodo handles DELETE /api/todos/{id}.
func (a *API) deleteTodo(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, wrapInvalid("todo id must be numeric"))
		return
	}
	if err := a.Svc.DeleteTodo(id); err != nil {
		writeError(w, err)
		return
	}
	a.publish("todo.deleted", map[string]any{"id": id})
	w.WriteHeader(http.StatusNoContent)
}
