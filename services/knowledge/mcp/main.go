package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func main() {
	dbPath := os.Getenv("KNOWLEDGE_DB")
	if dbPath == "" {
		dbPath = "/var/lib/knowledge/knowledge.db"
	}

	var err error
	db, err = sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&mode=ro")
	if err != nil {
		fmt.Fprintf(os.Stderr, "knowledge-mcp: open db: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			writeError(nil, -32700, "parse error: "+err.Error())
			continue
		}

		handleRequest(&req)
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func handleRequest(req *rpcRequest) {
	switch req.Method {
	case "initialize":
		writeResult(req.ID, map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "knowledge-mcp", "version": "1.0.0"},
		})
	case "notifications/initialized":
		// no response needed
	case "tools/list":
		writeResult(req.ID, map[string]any{"tools": toolDefs()})
	case "tools/call":
		handleToolCall(req)
	default:
		writeError(req.ID, -32601, "unknown method: "+req.Method)
	}
}

func toolDefs() []map[string]any {
	return []map[string]any{
		{
			"name":        "search_entries",
			"description": "Search knowledge base entries by keyword in title or content. Returns matching entries with their content.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search term (case-insensitive substring match against title and content)"},
					"view":  map[string]any{"type": "string", "enum": []string{"llm", "human"}, "description": "Which content view to return: 'llm' (detailed, for agents) or 'human' (concise, for people). Default: llm"},
				},
			},
		},
		{
			"name":        "get_entry",
			"description": "Get a single knowledge base entry by slug. Returns the full entry with its content, parent entries, and child entries.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"slug"},
				"properties": map[string]any{
					"slug": map[string]any{"type": "string", "description": "Entry slug (e.g. 'how-to-create-a-new-pet-project', 'universe')"},
					"view": map[string]any{"type": "string", "enum": []string{"llm", "human"}, "description": "Which content view to return: 'llm' (detailed, for agents) or 'human' (concise, for people). Default: llm"},
				},
			},
		},
		{
			"name":        "list_entries",
			"description": "List all knowledge base entries. Returns slugs, titles, and placeholder status (no content). Use get_entry to read a specific entry.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

func handleToolCall(req *rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeError(req.ID, -32602, "invalid params")
		return
	}

	var result string
	var err error

	switch params.Name {
	case "search_entries":
		var args struct {
			Query string `json:"query"`
			View  string `json:"view"`
		}
		json.Unmarshal(params.Arguments, &args)
		result, err = searchEntries(args.Query, resolveView(args.View))
	case "get_entry":
		var args struct {
			Slug string `json:"slug"`
			View string `json:"view"`
		}
		json.Unmarshal(params.Arguments, &args)
		result, err = getEntry(args.Slug, resolveView(args.View))
	case "list_entries":
		result, err = listEntries()
	default:
		writeToolResult(req.ID, "unknown tool: "+params.Name, true)
		return
	}

	if err != nil {
		writeToolResult(req.ID, "ERROR: "+err.Error(), true)
		return
	}
	writeToolResult(req.ID, result, false)
}

func resolveView(v string) string {
	if v == "human" {
		return "human"
	}
	return "llm"
}

// --- tool implementations ---

func searchEntries(query, view string) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	pattern := "%" + query + "%"
	contentCol := "content_llm"
	if view == "human" {
		contentCol = "content_human"
	}

	rows, err := db.Query(
		fmt.Sprintf(`SELECT slug, title, %s, placeholder FROM entries
		 WHERE title LIKE ? COLLATE NOCASE OR content_llm LIKE ? COLLATE NOCASE OR content_human LIKE ? COLLATE NOCASE
		 ORDER BY title`, contentCol),
		pattern, pattern, pattern)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var sb strings.Builder
	count := 0
	for rows.Next() {
		var slug, title, content string
		var placeholder int
		rows.Scan(&slug, &title, &content, &placeholder)
		count++

		status := ""
		if placeholder == 1 {
			status = " (placeholder)"
		}
		sb.WriteString(fmt.Sprintf("## %s%s\n", title, status))
		sb.WriteString(fmt.Sprintf("Slug: `%s` | View: %s\n\n", slug, view))
		if content != "" {
			sb.WriteString(content + "\n")
		}
		sb.WriteString("\n---\n\n")
	}

	if count == 0 {
		return fmt.Sprintf("No entries match '%s'.", query), nil
	}
	return fmt.Sprintf("Found %d entries matching '%s':\n\n%s", count, query, sb.String()), nil
}

func getEntry(slug, view string) (string, error) {
	if slug == "" {
		return "", fmt.Errorf("slug is required")
	}

	contentCol := "content_llm"
	if view == "human" {
		contentCol = "content_human"
	}

	var id int
	var title, content string
	var placeholder int
	err := db.QueryRow(
		fmt.Sprintf("SELECT id, title, %s, placeholder FROM entries WHERE slug = ?", contentCol),
		slug).Scan(&id, &title, &content, &placeholder)
	if err != nil {
		return "", fmt.Errorf("entry '%s' not found", slug)
	}

	var sb strings.Builder
	status := ""
	if placeholder == 1 {
		status = " (placeholder)"
	}
	sb.WriteString(fmt.Sprintf("# %s%s\n", title, status))
	sb.WriteString(fmt.Sprintf("Slug: `%s` | View: %s\n\n", slug, view))
	if content != "" {
		sb.WriteString(content + "\n\n")
	}

	// Parent entries (entries that link TO this one).
	parents, _ := db.Query(`
		SELECT e.slug, e.title FROM entries e
		JOIN links l ON e.id = l.source_id
		WHERE l.target_id = ? ORDER BY e.title`, id)
	if parents != nil {
		defer parents.Close()
		var parentList []string
		for parents.Next() {
			var ps, pt string
			parents.Scan(&ps, &pt)
			parentList = append(parentList, fmt.Sprintf("- %s (`%s`)", pt, ps))
		}
		if len(parentList) > 0 {
			sb.WriteString("## Parent entries\n")
			sb.WriteString(strings.Join(parentList, "\n") + "\n\n")
		}
	}

	// Child entries (entries this one links TO).
	children, _ := db.Query(`
		SELECT e.slug, e.title FROM entries e
		JOIN links l ON e.id = l.target_id
		WHERE l.source_id = ? ORDER BY e.title`, id)
	if children != nil {
		defer children.Close()
		var childList []string
		for children.Next() {
			var cs, ct string
			children.Scan(&cs, &ct)
			childList = append(childList, fmt.Sprintf("- %s (`%s`)", ct, cs))
		}
		if len(childList) > 0 {
			sb.WriteString("## Child entries\n")
			sb.WriteString(strings.Join(childList, "\n") + "\n")
		}
	}

	return sb.String(), nil
}

func listEntries() (string, error) {
	rows, err := db.Query("SELECT slug, title, placeholder FROM entries ORDER BY title")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var sb strings.Builder
	count := 0
	for rows.Next() {
		var slug, title string
		var placeholder int
		rows.Scan(&slug, &title, &placeholder)
		count++

		status := ""
		if placeholder == 1 {
			status = " [placeholder]"
		}
		sb.WriteString(fmt.Sprintf("- **%s** (`%s`)%s\n", title, slug, status))
	}

	if count == 0 {
		return "No entries in the knowledge base.", nil
	}
	return fmt.Sprintf("%d entries:\n\n%s", count, sb.String()), nil
}

// --- JSON-RPC output ---

func writeResult(id any, result any) {
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
	data, _ := json.Marshal(msg)
	fmt.Println(string(data))
}

func writeError(id any, code int, message string) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	}
	data, _ := json.Marshal(msg)
	fmt.Println(string(data))
}

func writeToolResult(id any, text string, isError bool) {
	writeResult(id, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": isError,
	})
}
