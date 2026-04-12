// Package mcp implements a minimal Model Context Protocol server over
// streamable HTTP at POST /mcp. We support the JSON-RPC 2.0 wire format
// directly — no SDK dependency — because the surface is small (initialize,
// tools/list, tools/call) and rolling our own keeps the binary tight.
//
// Tools mirror the petboard service layer one-to-one:
//
//   list_projects        - GET  /projects
//   get_project          - GET  /projects/:slug
//   create_project       - POST /projects
//   update_project       - PATCH /projects/:slug
//   add_feature          - POST /projects/:slug/features
//   set_feature_status   - PATCH /features/:id
//   update_feature       - PATCH /features/:id
//   log_effort           - POST /projects/:slug/effort
//
// All write operations also publish to the events.Broker so the canvas
// in the user's browser updates in real time.
package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"

	"petboard/events"
	"petboard/service"
)

// Handler bundles the dependencies the JSON-RPC handler needs.
type Handler struct {
	Svc    *service.Service
	Events *events.Broker
}

// ----- JSON-RPC envelope --------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ----- HTTP entry point ---------------------------------------------------

// ServeHTTP handles a single MCP JSON-RPC request. We do not implement
// the streaming side of MCP — every response is a one-shot JSON object,
// which is enough for synchronous tool calls.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "parse error: "+err.Error())
		return
	}

	switch req.Method {
	case "initialize":
		h.handleInitialize(w, &req)
	case "tools/list":
		h.handleToolsList(w, &req)
	case "tools/call":
		h.handleToolsCall(w, &req)
	case "notifications/initialized":
		// notification, no response
		w.WriteHeader(http.StatusOK)
	default:
		writeRPCError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

// ----- initialize ---------------------------------------------------------

func (h *Handler) handleInitialize(w http.ResponseWriter, req *rpcRequest) {
	writeRPCResult(w, req.ID, map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "petboard",
			"version": "0.1.0",
		},
	})
}

// ----- tools/list ---------------------------------------------------------

func (h *Handler) handleToolsList(w http.ResponseWriter, req *rpcRequest) {
	writeRPCResult(w, req.ID, map[string]any{
		"tools": toolDefinitions(),
	})
}

// toolDefinitions describes every petboard tool with a JSON schema for
// its arguments. The schemas are intentionally permissive — service
// layer enforces the real validation rules.
func toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "list_projects",
			"description": "List every project (with feature counts and total minutes).",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{
					"include_archived": map[string]any{"type": "boolean"},
				},
			},
		},
		{
			"name":        "get_project",
			"description": "Get one project by slug, including its features and effort log.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"slug"},
				"properties": map[string]any{
					"slug": map[string]any{"type": "string"},
				},
			},
		},
		{
			"name":        "create_project",
			"description": "Create a new project. The 'problem' field is mandatory and must frame the real-world pain the project addresses (not the solution).",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"name", "problem", "priority"},
				"properties": map[string]any{
					"name":        map[string]any{"type": "string"},
					"problem":     map[string]any{"type": "string"},
					"priority":    map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
					"description": map[string]any{"type": "string"},
					"color":       map[string]any{"type": "string"},
				},
			},
		},
		{
			"name":        "update_project",
			"description": "Patch fields of an existing project (slug-keyed).",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"slug"},
				"properties": map[string]any{
					"slug":        map[string]any{"type": "string"},
					"name":        map[string]any{"type": "string"},
					"problem":     map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
					"priority":    map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
					"color":       map[string]any{"type": "string"},
					"archived":    map[string]any{"type": "boolean"},
				},
			},
		},
		{
			"name":        "add_feature",
			"description": "Append a feature to a project's backlog.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"slug", "title"},
				"properties": map[string]any{
					"slug":        map[string]any{"type": "string"},
					"title":       map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
				},
			},
		},
		{
			"name":        "set_feature_status",
			"description": "Move a feature to a new status. Valid statuses: backlog, in_progress, done, dropped.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"feature_id", "status"},
				"properties": map[string]any{
					"feature_id": map[string]any{"type": "integer"},
					"status":     map[string]any{"type": "string", "enum": []string{"backlog", "in_progress", "done", "dropped"}},
				},
			},
		},
		{
			"name":        "update_feature",
			"description": "Patch fields of an existing feature (title and/or description).",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"feature_id"},
				"properties": map[string]any{
					"feature_id":  map[string]any{"type": "integer"},
					"title":       map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
				},
			},
		},
		{
			"name":        "log_effort",
			"description": "Record minutes of work against a project (optionally tied to a specific feature).",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"slug", "minutes"},
				"properties": map[string]any{
					"slug":       map[string]any{"type": "string"},
					"minutes":    map[string]any{"type": "integer", "minimum": 1},
					"note":       map[string]any{"type": "string"},
					"feature_id": map[string]any{"type": "integer"},
				},
			},
		},
	}
}

// ----- tools/call ---------------------------------------------------------

func (h *Handler) handleToolsCall(w http.ResponseWriter, req *rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}

	result, err := h.dispatchTool(params.Name, params.Arguments)
	if err != nil {
		writeRPCResult(w, req.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ERROR: " + err.Error()},
			},
			"isError": true,
		})
		return
	}

	// Encode the result as JSON in a text content block — that's what
	// MCP clients expect for structured payloads.
	body, _ := json.MarshalIndent(result, "", "  ")
	writeRPCResult(w, req.ID, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(body)},
		},
	})
}

func (h *Handler) dispatchTool(name string, raw json.RawMessage) (any, error) {
	switch name {
	case "list_projects":
		var args struct {
			IncludeArchived bool `json:"include_archived"`
		}
		_ = json.Unmarshal(raw, &args)
		return h.Svc.ListProjects(args.IncludeArchived)

	case "get_project":
		var args struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return h.Svc.GetProject(args.Slug)

	case "create_project":
		var args struct {
			Name        string           `json:"name"`
			Problem     string           `json:"problem"`
			Priority    service.Priority `json:"priority"`
			Description *string          `json:"description"`
			Color       *string          `json:"color"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		p, err := h.Svc.CreateProject(service.CreateProjectInput{
			Name:        args.Name,
			Problem:     args.Problem,
			Priority:    args.Priority,
			Description: args.Description,
			Color:       args.Color,
		})
		if err == nil && h.Events != nil {
			h.Events.Publish(events.Event{
				Type:    "project.created",
				Payload: map[string]any{"slug": p.Slug, "id": p.ID},
			})
		}
		return p, err

	case "update_project":
		var args struct {
			Slug        string            `json:"slug"`
			Name        *string           `json:"name"`
			Problem     *string           `json:"problem"`
			Description *string           `json:"description"`
			Priority    *service.Priority `json:"priority"`
			Color       *string           `json:"color"`
			Archived    *bool             `json:"archived"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		p, err := h.Svc.UpdateProject(args.Slug, service.UpdateProjectInput{
			Name:        args.Name,
			Problem:     args.Problem,
			Description: args.Description,
			Priority:    args.Priority,
			Color:       args.Color,
			Archived:    args.Archived,
		})
		if err == nil && h.Events != nil {
			h.Events.Publish(events.Event{
				Type:    "project.updated",
				Payload: map[string]any{"slug": args.Slug},
			})
		}
		return p, err

	case "add_feature":
		var args struct {
			Slug        string  `json:"slug"`
			Title       string  `json:"title"`
			Description *string `json:"description"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		f, err := h.Svc.CreateFeature(args.Slug, service.CreateFeatureInput{
			Title:       args.Title,
			Description: args.Description,
		})
		if err == nil && h.Events != nil {
			h.Events.Publish(events.Event{
				Type:    "feature.created",
				Payload: map[string]any{"slug": args.Slug, "feature_id": f.ID},
			})
		}
		return f, err

	case "set_feature_status":
		var args struct {
			FeatureID int64          `json:"feature_id"`
			Status    service.Status `json:"status"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		status := args.Status
		f, err := h.Svc.UpdateFeature(args.FeatureID, service.UpdateFeatureInput{
			Status: &status,
		})
		if err == nil && h.Events != nil {
			h.Events.Publish(events.Event{
				Type:    "feature.status_changed",
				Payload: map[string]any{"feature_id": args.FeatureID, "status": args.Status},
			})
		}
		return f, err

	case "update_feature":
		var args struct {
			FeatureID   int64   `json:"feature_id"`
			Title       *string `json:"title"`
			Description *string `json:"description"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		f, err := h.Svc.UpdateFeature(args.FeatureID, service.UpdateFeatureInput{
			Title:       args.Title,
			Description: args.Description,
		})
		if err == nil && h.Events != nil {
			h.Events.Publish(events.Event{
				Type:    "feature.updated",
				Payload: map[string]any{"feature_id": args.FeatureID},
			})
		}
		return f, err

	case "log_effort":
		var args struct {
			Slug      string  `json:"slug"`
			Minutes   int64   `json:"minutes"`
			Note      *string `json:"note"`
			FeatureID *int64  `json:"feature_id"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		l, err := h.Svc.LogEffort(args.Slug, service.LogEffortInput{
			Minutes:   args.Minutes,
			Note:      args.Note,
			FeatureID: args.FeatureID,
		})
		if err == nil && h.Events != nil {
			h.Events.Publish(events.Event{
				Type:    "effort.logged",
				Payload: map[string]any{"slug": args.Slug, "minutes": args.Minutes},
			})
		}
		return l, err

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// ----- helpers ------------------------------------------------------------

func writeRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeRPCError(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	})
}
