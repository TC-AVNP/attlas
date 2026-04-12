// Package services owns everything about the service registry that
// the attlas dashboard shows and controls:
//
//   - The knownServices list and Service struct
//   - Install / uninstall endpoints that shell out to
//     services/<id>/{install,uninstall}.sh under sudo
//   - The terminal detail endpoint + tmux helpers
//   - The claude-login + claude-code endpoints (which drive the
//     services/claude-login/claude-login-helper.py script)
//   - GetStatus(), which fills in the Installed/Running flags for
//     the /api/status payload
//
// The "services" this package talks to are the per-service folders
// under attlas/services/, NOT the Go packages under alive-server's
// internal/. If that naming collision is confusing later, rename
// to "registry" or "dashsvc".
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"attlas-server/internal/status"
	"attlas-server/internal/util"
)

// --- attlasDir injection ---

var attlasDir string

// SetAttlasDir tells this package where attlas/ lives so it can find
// services/<id>/install.sh (and the claude-login helper). Must be
// called once at startup.
func SetAttlasDir(dir string) { attlasDir = dir }

// --- Service type + registry ---

type Service struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ServiceName  string `json:"service"`
	Command      string `json:"command"`
	Path         string `json:"path"`
	Script       string `json:"script"`
	CheckProcess string `json:"check_process,omitempty"`
	Installed    bool   `json:"installed"`
	Running      bool   `json:"running"`
}

var known = []Service{
	{ID: "terminal", Name: "Cloud Terminal", ServiceName: "ttyd", Command: "ttyd",
		Path: "/terminal/", Script: "install.sh"},
	{ID: "code-server", Name: "Cloud VS Code", ServiceName: "code-server", Command: "code-server",
		Path: "/code/", Script: "install.sh"},
	{ID: "openclaw", Name: "OpenClaw", ServiceName: "openclaw-gateway", Command: "openclaw",
		Path: "/openclaw/", Script: "install.sh", CheckProcess: "openclaw-gateway"},
	{ID: "diary", Name: "Project Diary", ServiceName: "", Command: "hugo",
		Path: "/diary/", Script: "install.sh"},
	{ID: "petboard", Name: "Petboard", ServiceName: "petboard", Command: "petboard",
		Path: "/petboard/", Script: "install.sh"},
	{ID: "homelab-planner", Name: "Homelab Planner", ServiceName: "homelab-planner", Command: "homelab-planner",
		Path: "/homelab-planner/", Script: "install.sh"},
	// Splitsies lives on its own subdomain (splitsies.attlas.uk) routed
	// through splitsies-gateway (separate service, not listed here
	// because users never visit the gateway directly).
	{ID: "splitsies", Name: "Splitsies", ServiceName: "splitsies", Command: "splitsies",
		Path: "https://splitsies.attlas.uk/", Script: "install.sh"},
}

func findService(id string) *Service {
	for _, s := range known {
		if s.ID == id {
			return &s
		}
	}
	return nil
}

// loadInstalled is a live binary lookup (exec.LookPath), not a cached
// state file. See the long comment in the pre-refactor code for why.
func loadInstalled() map[string]bool {
	state := make(map[string]bool)
	for _, svc := range known {
		if _, err := exec.LookPath(svc.Command); err == nil {
			state[svc.ID] = true
		}
	}
	return state
}

// GetStatus fills in the Installed + Running flags for every service.
// Called by handleStatus in main to include the services list in the
// /api/status payload.
func GetStatus() []Service {
	installed := loadInstalled()
	var results []Service
	for _, svc := range known {
		s := svc
		s.Installed = installed[svc.ID]
		if s.Installed {
			if svc.CheckProcess != "" {
				_, s.Running = util.RunCmd("pgrep", "-f", svc.CheckProcess)
			} else if svc.ServiceName != "" {
				out, _ := util.RunCmd("systemctl", "is-active", svc.ServiceName)
				s.Running = out == "active"
			} else {
				s.Running = true
			}
		}
		results = append(results, s)
	}
	return results
}

// --- Install / Uninstall ---

func HandleInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	svc := findService(body.ID)
	if svc == nil {
		util.SendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Unknown service: %s", body.ID)})
		return
	}

	script := filepath.Join(attlasDir, "services", svc.ID, svc.Script)
	if _, err := os.Stat(script); err != nil {
		util.SendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Script not found: %s", script)})
		return
	}

	cmd := exec.Command("sudo", "-n", "bash", script)
	cmd.Dir = filepath.Join(attlasDir, "services", svc.ID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		util.SendJSON(w, map[string]interface{}{"error": string(out)})
		return
	}

	exec.Command("sudo", "-n", "systemctl", "reload", "caddy").Run()
	util.SendJSON(w, map[string]interface{}{"success": true})
}

func HandleUninstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	svc := findService(body.ID)
	if svc == nil {
		util.SendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Unknown service: %s", body.ID)})
		return
	}

	script := filepath.Join(attlasDir, "services", svc.ID, "uninstall.sh")
	if _, err := os.Stat(script); err != nil {
		util.SendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Uninstall script not found: %s", script)})
		return
	}

	cmd := exec.Command("sudo", "-n", "bash", script)
	cmd.Dir = filepath.Join(attlasDir, "services", svc.ID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		util.SendJSON(w, map[string]interface{}{"error": string(out)})
		return
	}

	exec.Command("sudo", "-n", "systemctl", "reload", "caddy").Run()
	util.SendJSON(w, map[string]interface{}{"success": true})
}

// --- Terminal detail (tmux sessions) ---

const (
	terminalSocket      = "attlas"
	terminalSessionUser = "agnostic-user"
)

var terminalSessionRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

type TerminalSession struct {
	Name      string `json:"name"`
	Windows   int    `json:"windows"`
	CreatedAt string `json:"created_at"`
	Created   string `json:"created_rel"`
	Attached  bool   `json:"attached"`
	Activity  string `json:"activity_rel"`
}

type TerminalDetail struct {
	Running  bool              `json:"running"`
	Uptime   string            `json:"uptime"`
	Sessions []TerminalSession `json:"sessions"`
	Error    string            `json:"error,omitempty"`
}

func tmuxCmd(ctx context.Context, args ...string) *exec.Cmd {
	full := append([]string{"-n", "-u", terminalSessionUser, "/usr/bin/tmux", "-L", terminalSocket}, args...)
	return exec.CommandContext(ctx, "sudo", full...)
}

func listTmuxSessions() ([]TerminalSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	fmtStr := "#{session_name}|#{session_windows}|#{session_created}|#{session_attached}|#{session_activity}"
	out, err := tmuxCmd(ctx, "list-sessions", "-F", fmtStr).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no server running") {
			return []TerminalSession{}, nil
		}
		return nil, fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}

	var sessions []TerminalSession
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		windows, _ := strconv.Atoi(parts[1])
		createdUnix, _ := strconv.ParseInt(parts[2], 10, 64)
		attached, _ := strconv.Atoi(parts[3])
		activityUnix, _ := strconv.ParseInt(parts[4], 10, 64)

		s := TerminalSession{
			Name:     parts[0],
			Windows:  windows,
			Attached: attached > 0,
		}
		if createdUnix > 0 {
			t := time.Unix(createdUnix, 0).UTC()
			s.CreatedAt = t.Format(time.RFC3339)
			s.Created = util.HumanDuration(time.Since(t)) + " ago"
		}
		if activityUnix > 0 {
			s.Activity = util.HumanDuration(time.Since(time.Unix(activityUnix, 0))) + " ago"
		}
		sessions = append(sessions, s)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Name < sessions[j].Name
	})
	if sessions == nil {
		sessions = []TerminalSession{}
	}
	return sessions, nil
}

func HandleTerminalDetail(w http.ResponseWriter, r *http.Request) {
	detail := TerminalDetail{Sessions: []TerminalSession{}}

	if out, ok := util.RunCmdCtx(2*time.Second, "systemctl", "is-active", "ttyd"); ok {
		detail.Running = out == "active"
	}
	if out, ok := util.RunCmdCtx(2*time.Second, "systemctl", "show", "ttyd",
		"-p", "ActiveEnterTimestamp"); ok {
		raw := strings.TrimPrefix(strings.TrimSpace(out), "ActiveEnterTimestamp=")
		if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", raw); err == nil && !t.IsZero() {
			detail.Uptime = util.HumanDuration(time.Since(t))
		}
	}

	sessions, err := listTmuxSessions()
	if err != nil {
		detail.Error = err.Error()
	} else {
		detail.Sessions = sessions
	}
	util.SendJSON(w, detail)
}

func HandleTerminalKill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	name := strings.TrimSpace(body.Name)
	if !terminalSessionRE.MatchString(name) {
		util.SendJSON(w, map[string]interface{}{"error": "Invalid session name"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := tmuxCmd(ctx, "kill-session", "-t", "="+name).CombinedOutput()
	if err != nil {
		util.SendJSON(w, map[string]interface{}{"error": strings.TrimSpace(string(out))})
		return
	}
	util.SendJSON(w, map[string]interface{}{"success": true})
}

// --- Claude login helpers ---

// distDir is needed for the legacy fallback path in HandleClaudeLogin.
// Set by main at startup via SetDistDir.
var distDir string

// SetDistDir tells the claude-login handler where the frontend dist
// lives (used only for a legacy helper fallback path).
func SetDistDir(dir string) { distDir = dir }

func HandleClaudeLogin(w http.ResponseWriter, r *http.Request) {
	if status.IsClaudeLoggedIn() {
		util.SendJSON(w, map[string]interface{}{"error": "Already logged in"})
		return
	}

	exec.Command("pkill", "-f", "claude-login-helper").Run()
	time.Sleep(1 * time.Second)

	for _, f := range []string{"/tmp/claude-login-url", "/tmp/claude-login-code", "/tmp/claude-login-result"} {
		os.Remove(f)
	}

	// Prefer the post-refactor location; fall back to legacy paths so
	// old installs keep working until they redeploy.
	helperPath := filepath.Join(attlasDir, "services", "claude-login", "claude-login-helper.py")
	if _, err := os.Stat(helperPath); err != nil {
		alt := filepath.Join(filepath.Dir(os.Args[0]), "claude-login-helper.py")
		if _, aerr := os.Stat(alt); aerr == nil {
			helperPath = alt
		} else {
			helperPath = filepath.Join(distDir, "..", "claude-login-helper.py")
		}
	}

	logFile, _ := os.Create("/tmp/claude-login-helper.log")
	cmd := exec.Command("python3", helperPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = nil
	cmd.Start()

	var authURL string
	for i := 0; i < 60; i++ {
		data, err := os.ReadFile("/tmp/claude-login-url")
		if err == nil {
			authURL = strings.TrimSpace(string(data))
			if authURL != "" {
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	if authURL != "" {
		util.SendJSON(w, map[string]interface{}{"url": authURL})
	} else {
		logData, _ := os.ReadFile("/tmp/claude-login-helper.log")
		snippet := string(logData)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		util.SendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Timed out waiting for URL. Log: %s", snippet)})
	}
}

func HandleClaudeCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Code == "" {
		util.SendJSON(w, map[string]interface{}{"error": "No code provided."})
		return
	}

	os.WriteFile("/tmp/claude-login-code", []byte(body.Code), 0644)

	var result string
	for i := 0; i < 30; i++ {
		data, err := os.ReadFile("/tmp/claude-login-result")
		if err == nil {
			result = strings.TrimSpace(string(data))
			if result != "" {
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	if result == "SUCCESS" {
		util.SendJSON(w, map[string]interface{}{"success": true})
	} else if result != "" {
		util.SendJSON(w, map[string]interface{}{"error": result})
	} else {
		util.SendJSON(w, map[string]interface{}{"error": "Timed out waiting for login result."})
	}
}
