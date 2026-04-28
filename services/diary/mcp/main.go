package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// contentDir is resolved at startup from DIARY_CONTENT_DIR env or default.
var contentDir string

func main() {
	contentDir = os.Getenv("DIARY_CONTENT_DIR")
	if contentDir == "" {
		contentDir = filepath.Join(os.Getenv("HOME"), "iapetus/attlas/services/diary/content")
	}

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
			"serverInfo":      map[string]any{"name": "diary-mcp", "version": "1.0.0"},
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
			"name":        "search_by_project",
			"description": "Find diary entries tagged with a given project slug. Returns entry dates, titles, summaries, and lessons learned.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"project"},
				"properties": map[string]any{
					"project": map[string]any{"type": "string", "description": "Petboard project slug (e.g. terminal, petboard, diary)"},
				},
			},
		},
		{
			"name":        "search_by_keyword",
			"description": "Search diary entries for a keyword or phrase. Returns matching entries with context around each match.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search term (case-insensitive)"},
				},
			},
		},
		{
			"name":        "list_entries",
			"description": "List diary entries, optionally filtered by date range. Returns dates, titles, summaries, and project tags.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"from": map[string]any{"type": "string", "description": "Start date inclusive (YYYY-MM-DD)"},
					"to":   map[string]any{"type": "string", "description": "End date inclusive (YYYY-MM-DD)"},
				},
			},
		},
		{
			"name":        "get_entry",
			"description": "Get the full content of a single diary entry by date.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"date"},
				"properties": map[string]any{
					"date": map[string]any{"type": "string", "description": "Entry date (YYYY-MM-DD)"},
				},
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
	case "search_by_project":
		var args struct{ Project string }
		json.Unmarshal(params.Arguments, &args)
		result, err = searchByProject(args.Project)
	case "search_by_keyword":
		var args struct{ Query string }
		json.Unmarshal(params.Arguments, &args)
		result, err = searchByKeyword(args.Query)
	case "list_entries":
		var args struct {
			From string
			To   string
		}
		json.Unmarshal(params.Arguments, &args)
		result, err = listEntries(args.From, args.To)
	case "get_entry":
		var args struct{ Date string }
		json.Unmarshal(params.Arguments, &args)
		result, err = getEntry(args.Date)
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

// --- entry parsing ---

type entry struct {
	Date     string
	Title    string
	Summary  string
	Projects []string
	Body     string // full markdown body after frontmatter
}

var frontmatterRe = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n(.*)`)

func parseEntry(path string) (*entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	m := frontmatterRe.FindSubmatch(data)
	if m == nil {
		return nil, fmt.Errorf("no frontmatter in %s", path)
	}

	fm := string(m[1])
	body := string(m[2])

	e := &entry{Body: body}

	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "title:") {
			e.Title = strings.Trim(strings.TrimPrefix(line, "title:"), " \"")
		} else if strings.HasPrefix(line, "date:") {
			e.Date = strings.TrimSpace(strings.TrimPrefix(line, "date:"))
		} else if strings.HasPrefix(line, "summary:") {
			e.Summary = strings.Trim(strings.TrimPrefix(line, "summary:"), " \"")
		} else if strings.HasPrefix(line, "projects:") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "projects:"))
			raw = strings.Trim(raw, "[]")
			for _, p := range strings.Split(raw, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					e.Projects = append(e.Projects, p)
				}
			}
		}
	}
	return e, nil
}

func loadAllEntries() ([]*entry, error) {
	files, err := filepath.Glob(filepath.Join(contentDir, "2*.md"))
	if err != nil {
		return nil, err
	}

	var entries []*entry
	for _, f := range files {
		e, err := parseEntry(f)
		if err != nil {
			continue
		}
		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Date > entries[j].Date // newest first
	})
	return entries, nil
}

// --- tool implementations ---

func searchByProject(project string) (string, error) {
	if project == "" {
		return "", fmt.Errorf("project slug is required")
	}

	entries, err := loadAllEntries()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	count := 0
	for _, e := range entries {
		for _, p := range e.Projects {
			if strings.EqualFold(p, project) {
				count++
				sb.WriteString(fmt.Sprintf("## %s — %s\n", e.Date, e.Title))
				if e.Summary != "" {
					sb.WriteString(e.Summary + "\n")
				}
				// Extract lessons section if present
				if lessons := extractSection(e.Body, "Lessons"); lessons != "" {
					sb.WriteString("\n### Lessons\n" + lessons)
				}
				sb.WriteString("\n---\n\n")
				break
			}
		}
	}

	if count == 0 {
		return fmt.Sprintf("No diary entries found for project '%s'.", project), nil
	}
	return fmt.Sprintf("Found %d entries for project '%s':\n\n%s", count, project, sb.String()), nil
}

func searchByKeyword(query string) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	entries, err := loadAllEntries()
	if err != nil {
		return "", err
	}

	queryLower := strings.ToLower(query)
	var sb strings.Builder
	count := 0

	for _, e := range entries {
		fullText := strings.ToLower(e.Title + " " + e.Summary + " " + e.Body)
		if !strings.Contains(fullText, queryLower) {
			continue
		}
		count++
		sb.WriteString(fmt.Sprintf("## %s — %s\n", e.Date, e.Title))
		if e.Summary != "" {
			sb.WriteString(e.Summary + "\n")
		}

		// Find matching lines with context
		lines := strings.Split(e.Body, "\n")
		shown := 0
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), queryLower) && shown < 5 {
				start := i
				if start > 0 {
					start = i - 1
				}
				end := i + 2
				if end > len(lines) {
					end = len(lines)
				}
				for _, cl := range lines[start:end] {
					sb.WriteString("  " + cl + "\n")
				}
				sb.WriteString("\n")
				shown++
			}
		}
		sb.WriteString("---\n\n")
	}

	if count == 0 {
		return fmt.Sprintf("No diary entries match '%s'.", query), nil
	}
	return fmt.Sprintf("Found %d entries matching '%s':\n\n%s", count, query, sb.String()), nil
}

func listEntries(from, to string) (string, error) {
	entries, err := loadAllEntries()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	count := 0

	for _, e := range entries {
		if from != "" && e.Date < from {
			continue
		}
		if to != "" && e.Date > to {
			continue
		}
		count++
		projects := ""
		if len(e.Projects) > 0 {
			projects = " [" + strings.Join(e.Projects, ", ") + "]"
		}
		sb.WriteString(fmt.Sprintf("- **%s** %s%s\n", e.Date, e.Title, projects))
		if e.Summary != "" {
			sb.WriteString("  " + e.Summary + "\n")
		}
	}

	if count == 0 {
		return "No diary entries found in the specified range.", nil
	}
	return fmt.Sprintf("%d entries:\n\n%s", count, sb.String()), nil
}

func getEntry(date string) (string, error) {
	if date == "" {
		return "", fmt.Errorf("date is required")
	}

	// Validate date format
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", fmt.Errorf("invalid date format, use YYYY-MM-DD")
	}

	path := filepath.Join(contentDir, date+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no entry for %s", date)
	}

	return string(data), nil
}

// --- helpers ---

func extractSection(body, heading string) string {
	lines := strings.Split(body, "\n")
	var sb strings.Builder
	inSection := false

	for _, line := range lines {
		if inSection {
			// Stop at the next ## heading
			if strings.HasPrefix(line, "## ") {
				break
			}
			sb.WriteString(line + "\n")
		}
		if strings.Contains(strings.ToLower(line), "## "+strings.ToLower(heading)) {
			inSection = true
		}
	}

	return strings.TrimSpace(sb.String())
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
