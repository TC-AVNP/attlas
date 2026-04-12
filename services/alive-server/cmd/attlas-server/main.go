package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"attlas-server/internal/auth"
	"attlas-server/internal/config"
	"attlas-server/internal/costs"
	"attlas-server/internal/infra"
	"attlas-server/internal/openclaw"
	"attlas-server/internal/status"
	"attlas-server/internal/util"
)

const port = 3000

var (
	distDir   string
	attlasDir string
)

// Known services
var knownServices = []Service{
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
	// through splitsies-gateway (separate service, not listed here because
	// users never visit the gateway directly). The Path field accepts a
	// full URL — the dashboard's "open" link uses it as an href directly.
	{ID: "splitsies", Name: "Splitsies", ServiceName: "splitsies", Command: "splitsies",
		Path: "https://splitsies.attlas.uk/", Script: "install.sh"},
}

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

// --- Service status ---

// loadInstalledServices is a live binary lookup, not a cached state file.
// The previous .attlas-services.json cache invalidated only on first read,
// which made the dashboard show a stale snapshot if services were installed
// out-of-band (e.g. via `bash services/install.sh` from an SSH session).
// LookPath is microseconds; there is no reason to persist its result.
func loadInstalledServices() map[string]bool {
	state := make(map[string]bool)
	for _, svc := range knownServices {
		if _, err := exec.LookPath(svc.Command); err == nil {
			state[svc.ID] = true
		}
	}
	return state
}


// --- Terminal detail (tmux-backed ttyd sessions) ---
//
// All shells served by /terminal/ are wrapped in tmux via
// services/ttyd-tmux.sh. The tmux server uses a named socket
// (-L attlas) under /tmp/tmux-<uid>/attlas, owned by the SERVICE_USER
// the ttyd systemd unit runs as (agnostic-user by default). To read /
// write that socket the dashboard (running as alive-svc) shells out
// via `sudo -n -u agnostic-user tmux -L attlas …`, authorized by
// /etc/sudoers.d/alive-svc-tmux installed by install-terminal.sh.

const (
	terminalSocket      = "attlas"
	terminalSessionUser = "agnostic-user"
)

// Session names are echoed back from tmux and also passed to the
// client for the attach URL (/terminal/?arg=<name>). Limit them to a
// safe character class so neither the shell wrapper nor a malformed
// HTML attribute can be subverted.
var terminalSessionRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

type TerminalSession struct {
	Name      string `json:"name"`
	Windows   int    `json:"windows"`
	CreatedAt string `json:"created_at"`      // ISO
	Created   string `json:"created_rel"`     // "3 h ago"
	Attached  bool   `json:"attached"`
	Activity  string `json:"activity_rel"`    // last activity, relative
}

type TerminalDetail struct {
	Running  bool              `json:"running"`  // ttyd systemd unit
	Uptime   string            `json:"uptime"`   // ttyd unit uptime
	Sessions []TerminalSession `json:"sessions"`
	Error    string            `json:"error,omitempty"`
}

// tmuxCmd builds an `sudo -n -u agnostic-user tmux -L attlas …` call.
func tmuxCmd(ctx context.Context, args ...string) *exec.Cmd {
	full := append([]string{"-n", "-u", terminalSessionUser, "/usr/bin/tmux", "-L", terminalSocket}, args...)
	return exec.CommandContext(ctx, "sudo", full...)
}

// listTmuxSessions queries tmux for all sessions on the shared
// socket. Returns an empty slice (not an error) when tmux has no
// server running, which is the common case before anyone has opened
// /terminal/ for the first time since boot.
func listTmuxSessions() ([]TerminalSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Format: name|windows|created(unix)|attached(0/1)|last_activity(unix)
	fmtStr := "#{session_name}|#{session_windows}|#{session_created}|#{session_attached}|#{session_activity}"
	out, err := tmuxCmd(ctx, "list-sessions", "-F", fmtStr).CombinedOutput()
	if err != nil {
		// tmux exits 1 with "no server running on ..." when idle.
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

func handleTerminalDetail(w http.ResponseWriter, r *http.Request) {
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

func handleTerminalKill(w http.ResponseWriter, r *http.Request) {
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

// --- Infrastructure detail (daily VM uptime via Cloud Logging) ---

// VMUptimeSeries is one stacked series in the daily-uptime chart, keyed
// by VM name. If multiple instance_ids share a name (destroy/recreate
// cycles under terraform), their seconds are summed into the same
// series — that matches the user's mental model of "one VM called X".


func getServicesStatus() []Service {
	installed := loadInstalledServices()
	var results []Service
	for _, svc := range knownServices {
		s := svc
		s.Installed = installed[svc.ID]
		if s.Installed {
			if svc.CheckProcess != "" {
				_, s.Running = util.RunCmd("pgrep", "-f", svc.CheckProcess)
			} else if svc.ServiceName != "" {
				out, _ := util.RunCmd("systemctl", "is-active", svc.ServiceName)
				s.Running = out == "active"
			} else {
				s.Running = true // static service (no daemon)
			}
		}
		results = append(results, s)
	}
	return results
}


func handleStatus(w http.ResponseWriter, r *http.Request) {
	allowedEmails := auth.AllowedEmails()
	util.SendJSON(w, map[string]interface{}{
		"vm": status.VMInfo(),
		"user": map[string]interface{}{
			"email":          auth.GetSessionEmail(r),
			"allowed_emails": allowedEmails,
		},
		"claude": map[string]bool{
			"installed":     status.IsClaudeInstalled(),
			"authenticated": status.IsClaudeLoggedIn(),
		},
		"services":      getServicesStatus(),
		"dotfiles":      status.GetDotfilesStatus(),
		"domain_expiry": status.GetDomainExpiry(),
		"system_load":   status.GetSystemLoad(),
	})
}

func handleClaudeLogin(w http.ResponseWriter, r *http.Request) {
	if status.IsClaudeLoggedIn() {
		util.SendJSON(w, map[string]interface{}{"error": "Already logged in"})
		return
	}

	exec.Command("pkill", "-f", "claude-login-helper").Run()
	time.Sleep(1 * time.Second)

	for _, f := range []string{"/tmp/claude-login-url", "/tmp/claude-login-code", "/tmp/claude-login-result"} {
		os.Remove(f)
	}

	// The helper lives under services/claude-login/ alongside every
	// other service. Fall back to legacy locations (next-to-binary
	// or alive-server/) for old installs that haven't redeployed yet.
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

func handleClaudeCode(w http.ResponseWriter, r *http.Request) {
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

func handleInstallService(w http.ResponseWriter, r *http.Request) {
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

	// install.sh scripts require root (they write to /etc/systemd/system/),
	// so we run them via sudo. Authorized by /etc/sudoers.d/alive-svc-services.
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

func handleUninstallService(w http.ResponseWriter, r *http.Request) {
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

	// uninstall.sh scripts require root (they touch /etc/systemd/system/
	// and /etc/caddy/conf.d/), so we run them via sudo.
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

// --- Helpers ---

func findService(id string) *Service {
	for _, s := range knownServices {
		if s.ID == id {
			return &s
		}
	}
	return nil
}

func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	host, _, _ := strings.Cut(r.RemoteAddr, ":")
	return host
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	filePath := filepath.Join(distDir, filepath.Clean(path))

	if !strings.HasPrefix(filePath, distDir) {
		http.NotFound(w, r)
		return
	}

	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		contentType := mime.TypeByExtension(filepath.Ext(filePath))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		// Hashed assets are immutable; index.html must always revalidate.
		if strings.HasPrefix(path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFile(w, r, filePath)
		return
	}

	indexPath := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, indexPath)
		return
	}

	http.NotFound(w, r)
}

// --- Main ---

func main() {
	auth.Init(config.LoadOrCreateSecret(), config.Load())

	// Initial load of the public-path registry and a SIGHUP-triggered
	// reloader so services can `systemctl kill --signal=SIGHUP alive-server`
	// after installing or uninstalling to pick up their changes without
	// dropping existing sessions.
	auth.ReloadPublicPaths()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)
	go func() {
		for range sigCh {
			log.Printf("SIGHUP received — reloading public-path registry")
			auth.ReloadPublicPaths()
		}
	}()

	// Resolve paths
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)
	distDir = filepath.Join(execDir, "frontend", "dist")
	attlasDir = os.Getenv("ATTLAS_DIR")
	if attlasDir == "" {
		home, _ := os.UserHomeDir()
		attlasDir = filepath.Join(home, "attlas")
	}
	status.SetAttlasDir(attlasDir)

	if _, err := os.Stat(distDir); err != nil {
		wd, _ := os.Getwd()
		distDir = filepath.Join(wd, "frontend", "dist")
	}

	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("/api/auth/verify", auth.HandleAuthVerify)
	mux.HandleFunc("GET /oauth2/login", auth.HandleOAuth2Login)
	mux.HandleFunc("GET /oauth2/callback", auth.HandleOAuth2Callback)
	mux.HandleFunc("/logout", auth.HandleLogout)

	// API
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("POST /api/claude-login", handleClaudeLogin)
	mux.HandleFunc("POST /api/claude-login/code", handleClaudeCode)
	mux.HandleFunc("POST /api/install-service", handleInstallService)
	mux.HandleFunc("POST /api/uninstall-service", handleUninstallService)
	mux.HandleFunc("POST /api/dotfiles/sync", status.HandleDotfilesSync)
	mux.HandleFunc("GET /api/services/openclaw", openclaw.HandleDetail)
	mux.HandleFunc("GET /api/services/terminal", handleTerminalDetail)
	mux.HandleFunc("POST /api/services/terminal/kill", handleTerminalKill)
	mux.HandleFunc("GET /api/services/infrastructure", infra.HandleDetail)
	mux.HandleFunc("GET /api/services/splitsies", handleSplitsiesDetail)
	mux.HandleFunc("POST /api/services/splitsies/users", handleSplitsiesAddUser)
	mux.HandleFunc("PATCH /api/services/splitsies/users/{id}", handleSplitsiesPatchUser)
	mux.HandleFunc("DELETE /api/services/splitsies/users/{id}", handleSplitsiesRemoveUser)
	mux.HandleFunc("GET /api/cloud-spend", costs.HandleCloudSpend)
	mux.HandleFunc("GET /api/costs/breakdown", costs.HandleBreakdown)
	mux.HandleFunc("POST /api/vm/stop", infra.HandleStopVM)

	// Diary (Hugo static site)
	diaryDir := filepath.Join(attlasDir, "services", "diary", "public")
	mux.Handle("/diary/", http.StripPrefix("/diary/", http.FileServer(http.Dir(diaryDir))))

	// Static files (catch-all)
	mux.HandleFunc("/", serveStatic)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	log.Printf("Attlas server running on http://%s", addr)
	log.Printf("Serving frontend from %s", distDir)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func init() {
	mime.AddExtensionType(".js", "application/javascript")
	mime.AddExtensionType(".css", "text/css")
	mime.AddExtensionType(".svg", "image/svg+xml")
	mime.AddExtensionType(".json", "application/json")
	mime.AddExtensionType(".woff2", "font/woff2")
}
