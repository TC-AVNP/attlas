package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	port         = 3000
	cookieName   = "attlas_session"
	cookieMaxAge = 86400 * 7 // 7 days
)

var (
	sessionSecret []byte
	distDir       string
	attlasDir     string
	oauthConfig   *OAuthConfig
)

// OAuth2 config loaded from ~/.attlas-server-config.json
type OAuthConfig struct {
	ClientID          string   `json:"google_oauth_client_id"`
	ClientSecret      string   `json:"google_oauth_client_secret"`
	AllowedEmails     []string `json:"allowed_emails"`
	AnthropicAdminKey string   `json:"anthropic_admin_key"` // F2: Anthropic cost_report API
}

// Known services
var knownServices = []Service{
	{ID: "terminal", Name: "Cloud Terminal", ServiceName: "ttyd", Command: "ttyd",
		Path: "/terminal/", Script: "install-terminal.sh"},
	{ID: "code-server", Name: "Cloud VS Code", ServiceName: "code-server", Command: "code-server",
		Path: "/code/", Script: "install-code-server.sh"},
	{ID: "openclaw", Name: "OpenClaw", ServiceName: "openclaw-gateway", Command: "openclaw",
		Path: "/openclaw/", Script: "install-openclaw.sh", CheckProcess: "openclaw-gateway"},
	{ID: "diary", Name: "Project Diary", ServiceName: "", Command: "hugo",
		Path: "/diary/", Script: "install-diary.sh"},
	{ID: "petboard", Name: "Petboard", ServiceName: "petboard", Command: "petboard",
		Path: "/petboard/", Script: "install-petboard.sh"},
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

// externalAPICacheTTL is the shared cache window for every handler
// that fans out to a rate-limited upstream (Anthropic cost_report,
// GCP BigQuery billing export, GCP Cloud Logging). The frontend polls
// some of these endpoints every 15–60s, so without caching the
// openclaw detail page alone would hit Anthropic ~4 times/minute and
// the infra detail page would hit Cloud Logging at the same rate —
// which is exactly how we started getting rate-limited.
//
// 15 minutes is short enough that "is anything on fire?" glances
// still see fresh-enough numbers, and long enough that even a busy
// day with multiple dashboards open never pushes us near any
// upstream's published rate limits. If you bump this and start
// seeing 429s again, this is the first knob to shrink.
const externalAPICacheTTL = 15 * time.Minute

// --- OAuth2 state tokens ---

// A state entry holds both the creation time (for expiry) and an optional
// return URL captured at /oauth2/login time. The callback handler uses the
// return URL to send the user back to the page they originally tried to
// visit — critical for flows like petboard's MCP /authorize, which must
// land back on a specific URL with its query params intact.
type stateEntry struct {
	createdAt time.Time
	returnTo  string
}

type stateStore struct {
	mu     sync.Mutex
	states map[string]stateEntry
}

var oauthStates = &stateStore{
	states: make(map[string]stateEntry),
}

func (ss *stateStore) generate(returnTo string) string {
	b := make([]byte, 32)
	rand.Read(b)
	state := hex.EncodeToString(b)

	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Clean expired states
	now := time.Now()
	for k, v := range ss.states {
		if now.Sub(v.createdAt) > 10*time.Minute {
			delete(ss.states, k)
		}
	}

	ss.states[state] = stateEntry{createdAt: now, returnTo: returnTo}
	return state
}

// validate consumes the state token and returns (returnTo, ok). The entry
// is single-use — it is deleted from the store regardless of whether the
// caller ends up redirecting to the return URL.
func (ss *stateStore) validate(state string) (string, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if entry, ok := ss.states[state]; ok {
		delete(ss.states, state)
		if time.Since(entry.createdAt) < 10*time.Minute {
			return entry.returnTo, true
		}
	}
	return "", false
}

// --- Public-path registry ---
//
// Any service that needs a subset of its paths to bypass alive-server's
// Google-OAuth gate drops a file into /etc/attlas-public-paths.d/. Each
// file is a newline-separated list of path prefixes; '#' starts a comment,
// blank lines are ignored. On startup and on SIGHUP the directory is
// re-read and an in-memory prefix list is atomically swapped in. The
// handleAuthVerify handler consults this list before doing any session
// check, returning 200 OK for matches so Caddy forward_auth lets the
// request through to the downstream service.
//
// This exists so services can implement their own auth for specific
// endpoints (e.g. petboard's MCP OAuth 2.1 flow, which needs the
// /.well-known/*, /token, /register, and /mcp paths to be reachable
// without a browser session) without having to modify the base Caddyfile.

const defaultPublicPathsDir = "/etc/attlas-public-paths.d"

type pathRegistry struct {
	mu       sync.RWMutex
	prefixes []string
}

var publicPathRegistry = &pathRegistry{}

func publicPathsDir() string {
	if dir := os.Getenv("ATTLAS_PUBLIC_PATHS_DIR"); dir != "" {
		return dir
	}
	return defaultPublicPathsDir
}

// load reads every *.conf file in the configured directory and rebuilds
// the in-memory prefix list. Called at startup and on SIGHUP. Safe to
// call concurrently with matches().
func (r *pathRegistry) load() {
	dir := publicPathsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Not an error — the directory may not exist yet if no service
		// has registered paths. Clear the list and move on.
		r.mu.Lock()
		r.prefixes = nil
		r.mu.Unlock()
		if !os.IsNotExist(err) {
			log.Printf("public-paths: read %s: %v", dir, err)
		}
		return
	}

	var prefixes []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("public-paths: read %s: %v", path, err)
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			// Strip '#' comments from anywhere on the line, then trim.
			if i := strings.Index(line, "#"); i >= 0 {
				line = line[:i]
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Require prefixes to be absolute paths so an accidental
			// empty or relative entry can't match everything.
			if !strings.HasPrefix(line, "/") {
				log.Printf("public-paths: ignoring non-absolute prefix %q in %s", line, path)
				continue
			}
			prefixes = append(prefixes, line)
		}
	}

	r.mu.Lock()
	r.prefixes = prefixes
	r.mu.Unlock()
	log.Printf("public-paths: loaded %d prefix(es) from %s", len(prefixes), dir)
}

// matches reports whether the given path starts with any registered
// public prefix.
func (r *pathRegistry) matches(path string) bool {
	if path == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// isSafeRelativePath reports whether a candidate return URL can safely be
// used as a redirect target. We only accept same-origin relative paths:
// must start with '/', must not start with '//' (protocol-relative), and
// must parse with an empty scheme and empty host. This prevents open
// redirects via a crafted return_to query param.
func isSafeRelativePath(p string) bool {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return false
	}
	if strings.ContainsAny(p, "\\\r\n") {
		return false
	}
	u, err := url.Parse(p)
	if err != nil {
		return false
	}
	return u.Scheme == "" && u.Host == ""
}

// --- Session ---

func loadOrCreateSecret() []byte {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".attlas-session-secret")

	data, err := os.ReadFile(path)
	if err == nil && len(data) >= 32 {
		return data
	}

	// Generate new secret
	secret := make([]byte, 32)
	rand.Read(secret)
	encoded := []byte(hex.EncodeToString(secret))
	os.WriteFile(path, encoded, 0600)
	return encoded
}

func loadOAuthConfig() *OAuthConfig {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".attlas-server-config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("WARNING: OAuth config not found at %s: %v", path, err)
		return nil
	}

	var config OAuthConfig
	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("WARNING: Failed to parse OAuth config: %v", err)
		return nil
	}

	if config.ClientID == "" || config.ClientSecret == "" {
		log.Printf("WARNING: OAuth config missing client_id or client_secret")
		return nil
	}

	log.Printf("OAuth2 configured with %d allowed email(s)", len(config.AllowedEmails))
	return &config
}

func makeSessionToken(email string) string {
	payload := fmt.Sprintf("%s:%d", email, time.Now().Unix())
	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))[:32]
	return fmt.Sprintf("%s:%s", payload, sig)
}

func verifySessionToken(token string) bool {
	lastColon := strings.LastIndex(token, ":")
	if lastColon < 0 {
		return false
	}
	payload := token[:lastColon]
	sig := token[lastColon+1:]

	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))[:32]

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return false
	}

	// Check expiry
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return false
	}
	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix()-ts <= cookieMaxAge
}

func getSessionCookie(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func isAuthenticated(r *http.Request) bool {
	token := getSessionCookie(r)
	return token != "" && verifySessionToken(token)
}

// --- VM info ---

func gcpMeta(path string) string {
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest("GET", "http://metadata.google.internal/computeMetadata/v1/"+path, nil)
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := client.Do(req)
	if err != nil {
		return "unknown"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(body))
}

func getVMInfo() map[string]string {
	ip := gcpMeta("instance/network-interfaces/0/access-configs/0/external-ip")
	zoneRaw := gcpMeta("instance/zone")
	zone := zoneRaw
	if i := strings.LastIndex(zoneRaw, "/"); i >= 0 {
		zone = zoneRaw[i+1:]
	}
	name := gcpMeta("instance/name")
	mt := gcpMeta("instance/machine-type")
	if i := strings.LastIndex(mt, "/"); i >= 0 {
		mt = mt[i+1:]
	}
	return map[string]string{
		"name":         name,
		"zone":         zone,
		"external_ip":  ip,
		"machine_type": mt,
		"domain":       "attlas.uk",
	}
}

// --- System load (live CPU + memory for the main dashboard) ---

// SystemLoad is the /proc-sourced snapshot that the dashboard shows at
// the top of the home page. Everything here is read from Linux
// pseudo-files in microseconds, so no caching is needed and the poll
// loop of /api/status can call getSystemLoad on every request.
type SystemLoad struct {
	CPUCores      int     `json:"cpu_cores"`
	CPUPercent    int     `json:"cpu_percent"` // 0..100, delta-based from /proc/stat
	LoadAvg1      float64 `json:"load_avg_1"`
	LoadAvg5      float64 `json:"load_avg_5"`
	LoadAvg15     float64 `json:"load_avg_15"`
	MemTotalBytes uint64  `json:"mem_total_bytes"`
	MemUsedBytes  uint64  `json:"mem_used_bytes"`
	MemPercent    int     `json:"mem_percent"` // 0..100
}

// /proc/stat is a cumulative counter of jiffies since boot. Meaningful
// CPU utilization requires two samples: we remember the last one in a
// process-global and compute the delta vs "now". First call after
// process start returns 0 because there's nothing to delta against;
// every subsequent call covers the time since the previous call.
type cpuStatSample struct {
	total uint64
	idle  uint64
	at    time.Time
}

var (
	cpuSampleMu sync.Mutex
	cpuSample   cpuStatSample
)

// readCPUStat parses the aggregate "cpu" line from /proc/stat:
//
//	cpu  user nice system idle iowait irq softirq steal guest guest_nice
//
// Total = sum of all columns, idle = column 4 (index 3 after "cpu").
// Per Linux convention the idle column includes both idle and iowait;
// iowait is part of the 5th column and NOT counted as idle here, so
// high iowait correctly shows up as "CPU busy" on the dashboard.
func readCPUStat() (total, idle uint64, err error) {
	data, rerr := os.ReadFile("/proc/stat")
	if rerr != nil {
		return 0, 0, rerr
	}
	firstNL := strings.IndexByte(string(data), '\n')
	if firstNL < 0 {
		return 0, 0, fmt.Errorf("unexpected /proc/stat layout")
	}
	fields := strings.Fields(string(data[:firstNL]))
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, fmt.Errorf("unexpected /proc/stat header: %q", string(data[:firstNL]))
	}
	for i := 1; i < len(fields); i++ {
		v, perr := strconv.ParseUint(fields[i], 10, 64)
		if perr != nil {
			return 0, 0, perr
		}
		total += v
		if i == 4 { // idle column
			idle = v
		}
	}
	return total, idle, nil
}

func getCPUUtilization() int {
	cpuSampleMu.Lock()
	defer cpuSampleMu.Unlock()

	total, idle, err := readCPUStat()
	if err != nil {
		return 0
	}

	prev := cpuSample
	cpuSample = cpuStatSample{total: total, idle: idle, at: time.Now()}

	if prev.at.IsZero() || total <= prev.total {
		return 0 // first call after start, or counter reset
	}

	totalDelta := total - prev.total
	idleDelta := idle - prev.idle
	if idleDelta > totalDelta {
		idleDelta = totalDelta
	}
	if totalDelta == 0 {
		return 0
	}
	pct := int(100 * (totalDelta - idleDelta) / totalDelta)
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	return pct
}

// readMemInfo returns (total, available) in bytes. MemAvailable is the
// kernel's own estimate of "memory you can allocate without swapping",
// which is what we actually want for a "RAM in use" number — MemFree
// alone is misleading because Linux aggressively uses free RAM for
// page cache.
func readMemInfo() (total, available uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, perr := strconv.ParseUint(fields[1], 10, 64)
		if perr != nil {
			continue
		}
		v *= 1024 // /proc/meminfo values are in KB
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		}
	}
	return total, available
}

func getSystemLoad() SystemLoad {
	sl := SystemLoad{CPUCores: runtime.NumCPU()}
	sl.CPUPercent = getCPUUtilization()

	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			sl.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
			sl.LoadAvg5, _ = strconv.ParseFloat(fields[1], 64)
			sl.LoadAvg15, _ = strconv.ParseFloat(fields[2], 64)
		}
	}

	total, available := readMemInfo()
	sl.MemTotalBytes = total
	if total > 0 {
		var used uint64
		if available < total {
			used = total - available
		}
		sl.MemUsedBytes = used
		sl.MemPercent = int(100 * used / total)
	}

	return sl
}

// --- Claude status ---

func isClaudeInstalled() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// isClaudeLoggedIn asks the claude CLI itself, running as the interactive
// login user via sudo, instead of trying to read the user's .claude.json
// (which has 600 perms and lives in a different user's home). Requires
// /etc/sudoers.d/alive-svc-claude to allow alive-svc to run claude as
// agnostic-user without a password.
func isClaudeLoggedIn() bool {
	cmd := exec.Command("sudo", "-n", "-u", "agnostic-user", "-H", "claude", "auth", "status")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), `"loggedIn": true`)
}

// --- Service status ---

func runCmd(name string, args ...string) (string, bool) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

// runCmdCtx runs a command with a hard timeout. Used for subprocesses
// that might hang (openclaw gateway probes, systemctl on a stuck unit).
func runCmdCtx(timeout time.Duration, name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

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

// --- Dotfiles status (F1) ---

// DotfilesStatus is what the dashboard shows for the `dotfiles` row.
// Fields that can't be resolved (missing repo, systemd unit not installed,
// never run) come back as zero values and the Status field degrades to
// "unknown" — the frontend handles that.
type DotfilesStatus struct {
	LastSync        string `json:"last_sync"`         // ISO timestamp of last successful run
	LastExitStatus  int    `json:"last_exit_status"`  // 0 = ok
	HeadCommit      string `json:"head_commit"`       // short hash
	HeadCommittedAt string `json:"head_committed_at"` // ISO
	RemoteCommit    string `json:"remote_commit"`     // short hash
	Behind          int    `json:"behind"`            // commit count HEAD..origin/<branch>
	Status          string `json:"status"`            // "up-to-date" | "behind" | "error" | "unknown"
}

// dotfilesDir resolves the dotfiels repo relative to ATTLAS_DIR
// (attlas lives at $WORKSPACE/attlas, dotfiels at $WORKSPACE/dotfiels).
// The repo name retains the upstream "dotfiels" typo.
func dotfilesDir() string {
	return filepath.Join(filepath.Dir(attlasDir), "dotfiels")
}

func getDotfilesStatus() DotfilesStatus {
	status := DotfilesStatus{Status: "unknown"}

	dir := dotfilesDir()
	if _, err := os.Stat(dir); err != nil {
		return status
	}

	// systemctl show dotfiles-sync.service
	if out, ok := runCmdCtx(2*time.Second, "systemctl", "show", "dotfiles-sync.service",
		"-p", "ExecMainExitTimestamp", "-p", "ExecMainStatus"); ok {
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "ExecMainStatus=") {
				if v, err := strconv.Atoi(strings.TrimPrefix(line, "ExecMainStatus=")); err == nil {
					status.LastExitStatus = v
				}
			} else if strings.HasPrefix(line, "ExecMainExitTimestamp=") {
				raw := strings.TrimPrefix(line, "ExecMainExitTimestamp=")
				// systemd emits e.g. "Fri 2026-04-10 13:34:42 UTC"
				if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", raw); err == nil {
					status.LastSync = t.UTC().Format(time.RFC3339)
				}
			}
		}
	}

	// Detect branch (upstream is master — but derive rather than hardcode).
	branch, _ := runCmdCtx(2*time.Second, "git", "-C", dir, "symbolic-ref", "--short", "HEAD")
	if branch == "" {
		branch = "master"
	}

	if head, ok := runCmdCtx(2*time.Second, "git", "-C", dir, "rev-parse", "--short", "HEAD"); ok {
		status.HeadCommit = head
	}
	if committedAt, ok := runCmdCtx(2*time.Second, "git", "-C", dir, "log", "-1", "--format=%cI", "HEAD"); ok {
		status.HeadCommittedAt = committedAt
	}
	if remote, ok := runCmdCtx(2*time.Second, "git", "-C", dir, "rev-parse", "--short", "origin/"+branch); ok {
		status.RemoteCommit = remote
	}
	if behindOut, ok := runCmdCtx(2*time.Second, "git", "-C", dir, "rev-list", "--count", "HEAD..origin/"+branch); ok {
		if n, err := strconv.Atoi(behindOut); err == nil {
			status.Behind = n
		}
	}

	// Derive semantic status.
	if status.LastExitStatus != 0 && status.LastSync != "" {
		status.Status = "error"
	} else if status.Behind > 0 {
		status.Status = "behind"
	} else if status.HeadCommit != "" {
		status.Status = "up-to-date"
	}

	return status
}

func handleDotfilesSync(w http.ResponseWriter, r *http.Request) {
	// Authorized via /etc/sudoers.d/alive-svc-dotfiles-sync.
	cmd := exec.Command("sudo", "-n", "systemctl", "start", "dotfiles-sync.service")
	out, err := cmd.CombinedOutput()
	if err != nil {
		sendJSON(w, map[string]interface{}{"error": strings.TrimSpace(string(out))})
		return
	}
	sendJSON(w, map[string]interface{}{"success": true})
}

// --- Domain expiry (F3) ---

type DomainExpiry struct {
	ExpiresAt     string `json:"expires_at"`     // ISO date
	DaysRemaining int    `json:"days_remaining"` // whole days from now
	Severity      string `json:"severity"`       // ok | warn | danger | unknown
}

var (
	domainCacheMu      sync.Mutex
	domainCacheValue   DomainExpiry
	domainCacheExpires time.Time
)

var whoisExpiryRE = regexp.MustCompile(`(?i)^\s*Expiry date:\s*(.+)$`)

func getDomainExpiry() DomainExpiry {
	domainCacheMu.Lock()
	defer domainCacheMu.Unlock()
	if time.Now().Before(domainCacheExpires) {
		// Refresh daysRemaining on every call even from cache — the underlying
		// date is stable but the countdown should tick down.
		v := domainCacheValue
		if t, err := time.Parse("2006-01-02", v.ExpiresAt); err == nil {
			v.DaysRemaining = int(time.Until(t).Hours() / 24)
			v.Severity = domainSeverity(v.DaysRemaining)
		}
		return v
	}

	result := DomainExpiry{Severity: "unknown"}

	// Tolerate `whois: command not found` on a fresh VM.
	if _, err := exec.LookPath("whois"); err != nil {
		domainCacheValue = result
		domainCacheExpires = time.Now().Add(10 * time.Minute)
		return result
	}

	out, ok := runCmdCtx(10*time.Second, "whois", "attlas.uk")
	if !ok {
		domainCacheValue = result
		domainCacheExpires = time.Now().Add(10 * time.Minute)
		return result
	}

	for _, line := range strings.Split(out, "\n") {
		m := whoisExpiryRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		raw := strings.TrimSpace(m[1])
		// Nominet format: DD-Mon-YYYY
		t, err := time.Parse("02-Jan-2006", raw)
		if err != nil {
			continue
		}
		result.ExpiresAt = t.Format("2006-01-02")
		result.DaysRemaining = int(time.Until(t).Hours() / 24)
		result.Severity = domainSeverity(result.DaysRemaining)
		break
	}

	domainCacheValue = result
	domainCacheExpires = time.Now().Add(6 * time.Hour)
	return result
}

func domainSeverity(days int) string {
	switch {
	case days > 60:
		return "ok"
	case days >= 30:
		return "warn"
	default:
		return "danger"
	}
}

// --- Openclaw detail (F2) ---

type DayCost struct {
	Date string  `json:"date"` // YYYY-MM-DD
	USD  float64 `json:"usd"`
}

type OpenclawDetail struct {
	Running          bool      `json:"running"`
	Uptime           string    `json:"uptime"`
	ActiveTasks      int       `json:"active_tasks"`
	Sessions         int       `json:"sessions"`           // lifetime, across agents
	TasksRun         int       `json:"tasks_run"`          // lifetime total
	SpendLast30Days  float64   `json:"spend_last_30_days"` // USD, matches platform.claude.com default cost view
	SpendDaily       []DayCost `json:"spend_daily"`        // last 7 days
	BillingError     string    `json:"billing_error,omitempty"`
}

var (
	openclawCacheMu      sync.Mutex
	openclawCacheValue   OpenclawDetail
	openclawCacheExpires time.Time
)

// openclawStatusJSON is the shape we actually consume out of
// `openclaw status --all --json`. Only the fields we care about.
type openclawStatusJSON struct {
	Tasks struct {
		Total    int `json:"total"`
		Active   int `json:"active"`
		ByStatus struct {
			Running int `json:"running"`
		} `json:"byStatus"`
	} `json:"tasks"`
	Sessions struct {
		Count int `json:"count"`
	} `json:"sessions"`
	Agents struct {
		TotalSessions int `json:"totalSessions"`
	} `json:"agents"`
}

func handleOpenclawDetail(w http.ResponseWriter, r *http.Request) {
	openclawCacheMu.Lock()
	defer openclawCacheMu.Unlock()

	if time.Now().Before(openclawCacheExpires) {
		sendJSON(w, openclawCacheValue)
		return
	}

	detail := OpenclawDetail{}

	// 1. systemd runtime status.
	if out, ok := runCmdCtx(2*time.Second, "systemctl", "is-active", "openclaw-gateway"); ok {
		detail.Running = out == "active"
	}
	if out, ok := runCmdCtx(2*time.Second, "systemctl", "show", "openclaw-gateway",
		"-p", "ActiveEnterTimestamp"); ok {
		raw := strings.TrimPrefix(strings.TrimSpace(out), "ActiveEnterTimestamp=")
		if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", raw); err == nil && !t.IsZero() {
			detail.Uptime = humanDuration(time.Since(t))
		}
	}

	// 2. openclaw status --all --json via sudo -u openclaw-svc.
	//    Authorized via /etc/sudoers.d/alive-svc-openclaw.
	if out, ok := runCmdCtx(2*time.Second, "sudo", "-n", "-u", "openclaw-svc", "-H",
		"/usr/bin/openclaw", "status", "--all", "--json"); ok {
		var s openclawStatusJSON
		if err := json.Unmarshal([]byte(out), &s); err == nil {
			detail.TasksRun = s.Tasks.Total
			detail.ActiveTasks = s.Tasks.Active
			if detail.ActiveTasks == 0 {
				detail.ActiveTasks = s.Tasks.ByStatus.Running
			}
			detail.Sessions = s.Sessions.Count
			if detail.Sessions == 0 {
				detail.Sessions = s.Agents.TotalSessions
			}
		}
	}

	// 3. Anthropic cost_report API (last 30 days to match the console).
	if oauthConfig != nil && oauthConfig.AnthropicAdminKey != "" {
		spend30, daily, err := fetchAnthropicSpend(oauthConfig.AnthropicAdminKey, time.Now().UTC().AddDate(0, 0, -30))
		if err != nil {
			detail.BillingError = err.Error()
		} else {
			detail.SpendLast30Days = spend30
			detail.SpendDaily = daily
		}
	} else {
		detail.BillingError = "anthropic admin key not configured"
	}

	openclawCacheValue = detail
	openclawCacheExpires = time.Now().Add(externalAPICacheTTL)
	sendJSON(w, detail)
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
			s.Created = humanDuration(time.Since(t)) + " ago"
		}
		if activityUnix > 0 {
			s.Activity = humanDuration(time.Since(time.Unix(activityUnix, 0))) + " ago"
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

	if out, ok := runCmdCtx(2*time.Second, "systemctl", "is-active", "ttyd"); ok {
		detail.Running = out == "active"
	}
	if out, ok := runCmdCtx(2*time.Second, "systemctl", "show", "ttyd",
		"-p", "ActiveEnterTimestamp"); ok {
		raw := strings.TrimPrefix(strings.TrimSpace(out), "ActiveEnterTimestamp=")
		if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", raw); err == nil && !t.IsZero() {
			detail.Uptime = humanDuration(time.Since(t))
		}
	}

	sessions, err := listTmuxSessions()
	if err != nil {
		detail.Error = err.Error()
	} else {
		detail.Sessions = sessions
	}
	sendJSON(w, detail)
}

func handleTerminalKill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	name := strings.TrimSpace(body.Name)
	if !terminalSessionRE.MatchString(name) {
		sendJSON(w, map[string]interface{}{"error": "Invalid session name"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := tmuxCmd(ctx, "kill-session", "-t", "="+name).CombinedOutput()
	if err != nil {
		sendJSON(w, map[string]interface{}{"error": strings.TrimSpace(string(out))})
		return
	}
	sendJSON(w, map[string]interface{}{"success": true})
}

// --- Infrastructure detail (daily VM uptime via Cloud Logging) ---

// VMUptimeSeries is one stacked series in the daily-uptime chart, keyed
// by VM name. If multiple instance_ids share a name (destroy/recreate
// cycles under terraform), their seconds are summed into the same
// series — that matches the user's mental model of "one VM called X".
type VMUptimeSeries struct {
	Name         string  `json:"name"`
	TotalSeconds int64   `json:"total_seconds"`
	Daily        []int64 `json:"daily"` // aligned to InfrastructureDetail.UptimeDays
}

type InfrastructureDetail struct {
	Name              string           `json:"name"`
	Zone              string           `json:"zone"`
	Region            string           `json:"region"`
	ExternalIP        string           `json:"external_ip"`
	InternalIP        string           `json:"internal_ip"`
	Domain            string           `json:"domain"`
	MachineType       string           `json:"machine_type"`
	CreationTimestamp string           `json:"creation_timestamp"` // ISO (from metadata)
	OSBootTime        string           `json:"os_boot_time"`       // ISO
	UptimeNow         string           `json:"uptime_now"`         // human-readable
	UptimeDays        []string         `json:"uptime_days"`        // chronological YYYY-MM-DD list
	UptimeSeries      []VMUptimeSeries `json:"uptime_series"`      // one per VM, Daily aligned to UptimeDays
	TotalSecondsMonth int64            `json:"total_seconds_month"`
	EventsError       string           `json:"events_error,omitempty"`
}

var (
	infraCacheMu      sync.Mutex
	infraCacheValue   InfrastructureDetail
	infraCacheExpires time.Time
)

// getMetadataToken fetches a short-lived OAuth access token for the
// VM's default service account via the metadata server. Used to call
// GCP REST APIs (Cloud Logging) without shelling out to gcloud.
func getMetadataToken() (string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest("GET",
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var data struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.AccessToken == "" {
		return "", fmt.Errorf("empty access token")
	}
	return data.AccessToken, nil
}

// fetchInstanceUptime queries Cloud Monitoring for the per-VM daily
// uptime metric over [monthStart, endExclusive), returning:
//
//	days   — chronological YYYY-MM-DD list covering the window, zero-filled
//	series — map[instance_name] -> []int64 seconds, one per day in days
//
// This replaces the old Cloud Logging audit-event replay, which was
// fundamentally wrong: it could only see API-initiated start/stop
// events, so anything that stopped the VM another way (guest OS
// shutdown, host maintenance reboot, preemption, crash) looked like
// "still running" to the replay, and days with no events defaulted
// to 24h of phantom uptime. For the attlas project this showed 168
// hours of Apr 1-7 uptime that never actually happened.
//
// compute.googleapis.com/instance/uptime is a DELTA metric Google
// scrapes every ~60 seconds. With ALIGN_DELTA + alignmentPeriod
// 86400s each point is exactly "seconds the VM was running during
// that UTC day". No guesswork, no replay, no assumptions.
//
// We intentionally do NOT use crossSeriesReducer so the response
// preserves per-VM series. Each series carries the VM name in
// metric.labels.instance_name (even for deleted VMs — the label
// persists with the time series forever). When two VM instances
// share a name — terraform destroy/recreate cycles produce this
// constantly — their daily seconds are merged into a single output
// series, matching the user's mental model of "one VM named X".
func fetchInstanceUptime(monthStart, endExclusive time.Time) (
	days []string, series map[string][]int64, err error,
) {
	projectID := gcpMeta("project/project-id")
	if projectID == "unknown" {
		return nil, nil, fmt.Errorf("missing metadata (project=%s)", projectID)
	}

	token, terr := getMetadataToken()
	if terr != nil {
		return nil, nil, fmt.Errorf("metadata token: %v", terr)
	}

	for d := monthStart; d.Before(endExclusive); d = d.AddDate(0, 0, 1) {
		days = append(days, d.Format("2006-01-02"))
	}
	dayIndex := make(map[string]int, len(days))
	for i, d := range days {
		dayIndex[d] = i
	}
	series = make(map[string][]int64)

	params := url.Values{}
	params.Set("filter", `metric.type="compute.googleapis.com/instance/uptime" AND resource.type="gce_instance"`)
	params.Set("interval.startTime", monthStart.Format(time.RFC3339))
	params.Set("interval.endTime", endExclusive.Format(time.RFC3339))
	params.Set("aggregation.alignmentPeriod", "86400s")
	params.Set("aggregation.perSeriesAligner", "ALIGN_DELTA")
	params.Set("view", "FULL")

	baseURL := fmt.Sprintf(
		"https://monitoring.googleapis.com/v3/projects/%s/timeSeries?%s",
		projectID, params.Encode(),
	)

	type respShape struct {
		TimeSeries []struct {
			Metric struct {
				Labels map[string]string `json:"labels"`
			} `json:"metric"`
			Points []struct {
				Interval struct {
					StartTime string `json:"startTime"`
					EndTime   string `json:"endTime"`
				} `json:"interval"`
				Value struct {
					DoubleValue float64 `json:"doubleValue"`
				} `json:"value"`
			} `json:"points"`
		} `json:"timeSeries"`
		NextPageToken string `json:"nextPageToken"`
	}

	client := &http.Client{Timeout: 8 * time.Second}
	const maxPages = 20
	pageToken := ""
	pagesFetched := 0
	totalSeries := 0
	for pagesFetched < maxPages {
		reqURL := baseURL
		if pageToken != "" {
			reqURL += "&pageToken=" + url.QueryEscape(pageToken)
		}
		req, _ := http.NewRequest("GET", reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, rerr := client.Do(req)
		if rerr != nil {
			return nil, nil, fmt.Errorf("monitoring request page %d: %v", pagesFetched+1, rerr)
		}
		if resp.StatusCode >= 400 {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return nil, nil, fmt.Errorf("monitoring %d on page %d: %s",
				resp.StatusCode, pagesFetched+1, strings.TrimSpace(string(raw)))
		}

		var page respShape
		if derr := json.NewDecoder(resp.Body).Decode(&page); derr != nil {
			resp.Body.Close()
			return nil, nil, fmt.Errorf("decode monitoring page %d: %v", pagesFetched+1, derr)
		}
		resp.Body.Close()

		for _, ts := range page.TimeSeries {
			name := ts.Metric.Labels["instance_name"]
			if name == "" {
				name = "unknown"
			}
			totalSeries++
			if _, exists := series[name]; !exists {
				series[name] = make([]int64, len(days))
			}
			for _, p := range ts.Points {
				if len(p.Interval.StartTime) < 10 {
					continue
				}
				dateStr := p.Interval.StartTime[:10]
				idx, ok := dayIndex[dateStr]
				if !ok {
					continue
				}
				series[name][idx] += int64(p.Value.DoubleValue)
			}
		}

		pagesFetched++
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}

	log.Printf("infra: monitoring returned %d time series over %d pages, merged into %d named VMs",
		totalSeries, pagesFetched, len(series))
	return days, series, nil
}

// osBootTime reads /proc/stat btime for the kernel-boot epoch. Used as
// a proxy for "current VM start", since on GCE an instance start = OS
// boot in nearly all cases.
func osBootTime() time.Time {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			if ts, err := strconv.ParseInt(strings.TrimPrefix(line, "btime "), 10, 64); err == nil {
				return time.Unix(ts, 0).UTC()
			}
		}
	}
	return time.Time{}
}

func handleInfrastructureDetail(w http.ResponseWriter, r *http.Request) {
	infraCacheMu.Lock()
	defer infraCacheMu.Unlock()

	if time.Now().Before(infraCacheExpires) {
		sendJSON(w, infraCacheValue)
		return
	}

	now := time.Now().UTC()
	detail := InfrastructureDetail{
		Name:       gcpMeta("instance/name"),
		ExternalIP: gcpMeta("instance/network-interfaces/0/access-configs/0/external-ip"),
		InternalIP: gcpMeta("instance/network-interfaces/0/ip"),
		Domain:     "attlas.uk",
	}

	zoneRaw := gcpMeta("instance/zone") // "projects/<num>/zones/europe-west1-b"
	if i := strings.LastIndex(zoneRaw, "/"); i >= 0 {
		detail.Zone = zoneRaw[i+1:]
	} else {
		detail.Zone = zoneRaw
	}
	if idx := strings.LastIndex(detail.Zone, "-"); idx >= 0 {
		detail.Region = detail.Zone[:idx]
	}

	mtRaw := gcpMeta("instance/machine-type")
	if i := strings.LastIndex(mtRaw, "/"); i >= 0 {
		detail.MachineType = mtRaw[i+1:]
	} else {
		detail.MachineType = mtRaw
	}

	// /proc/stat btime → OS boot time → current uptime.
	if bt := osBootTime(); !bt.IsZero() {
		detail.OSBootTime = bt.Format(time.RFC3339)
		detail.UptimeNow = humanDuration(now.Sub(bt))
	}

	// Creation timestamp isn't in the metadata server directly; query
	// the compute API with the metadata token.
	if ts, err := fetchInstanceCreationTimestamp(); err == nil {
		detail.CreationTimestamp = ts
	}

	// Per-VM daily uptime from Cloud Monitoring. Window is the current
	// UTC month so far, exclusive of tomorrow — today's bucket is
	// included but ~1-5 minutes behind real time (Monitoring's
	// scrape interval).
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)

	uDays, uSeries, err := fetchInstanceUptime(monthStart, tomorrow)
	if err != nil {
		detail.EventsError = err.Error()
	}
	detail.UptimeDays = uDays

	// Build per-VM entries + overall total. Sort by total desc so
	// the biggest contributors land first in both the chart's
	// stacking order and the legend.
	var totalMonth int64
	detail.UptimeSeries = make([]VMUptimeSeries, 0, len(uSeries))
	for name, daily := range uSeries {
		var seriesTotal int64
		for _, s := range daily {
			seriesTotal += s
		}
		totalMonth += seriesTotal
		detail.UptimeSeries = append(detail.UptimeSeries, VMUptimeSeries{
			Name:         name,
			TotalSeconds: seriesTotal,
			Daily:        daily,
		})
	}
	sort.Slice(detail.UptimeSeries, func(i, j int) bool {
		return detail.UptimeSeries[i].TotalSeconds > detail.UptimeSeries[j].TotalSeconds
	})
	detail.TotalSecondsMonth = totalMonth

	infraCacheValue = detail
	infraCacheExpires = time.Now().Add(externalAPICacheTTL)
	sendJSON(w, detail)
}

// --- Cloud spend (anthropic cost_report + gcp bigquery billing export) ---

// GCP cost comes from the Cloud Billing BigQuery export sitting at
// `<project>.billing_export.gcp_billing_export_v1_<account>`. The
// export is set up once by Terraform + a one-time console action;
// data lands with a ~24h delay, so on a fresh month the first day
// will show $0.00 until the export catches up. Everything after
// that is the exact number the official billing page would show.

type CloudSpend struct {
	AnthropicMTD   float64 `json:"anthropic_mtd_usd"`
	GCPMTD         float64 `json:"gcp_mtd_usd"`
	TotalMTD       float64 `json:"total_mtd_usd"`
	GCPSource      string  `json:"gcp_source"`
	GCPError       string  `json:"gcp_error,omitempty"`
	AnthropicError string  `json:"anthropic_error,omitempty"`
}

var (
	cloudSpendCacheMu      sync.Mutex
	cloudSpendCacheValue   CloudSpend
	cloudSpendCacheExpires time.Time
)

func handleCloudSpend(w http.ResponseWriter, r *http.Request) {
	cloudSpendCacheMu.Lock()
	defer cloudSpendCacheMu.Unlock()

	if time.Now().Before(cloudSpendCacheExpires) {
		sendJSON(w, cloudSpendCacheValue)
		return
	}

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	cs := CloudSpend{
		GCPSource: "bigquery billing export",
	}

	// GCP MTD from BigQuery billing export.
	if v, err := fetchGCPSpendBigQuery(monthStart); err != nil {
		cs.GCPError = err.Error()
	} else {
		cs.GCPMTD = v
	}

	// Anthropic MTD — deliberately NOT reusing the openclaw detail
	// cache because that one is scoped to Last 30 days to match the
	// console, while the combined card wants strict month-to-date.
	if oauthConfig != nil && oauthConfig.AnthropicAdminKey != "" {
		spend, _, err := fetchAnthropicSpend(oauthConfig.AnthropicAdminKey, monthStart)
		if err != nil {
			cs.AnthropicError = err.Error()
		} else {
			cs.AnthropicMTD = spend
		}
	} else {
		cs.AnthropicError = "anthropic admin key not configured"
	}

	cs.TotalMTD = cs.AnthropicMTD + cs.GCPMTD

	cloudSpendCacheValue = cs
	cloudSpendCacheExpires = time.Now().Add(externalAPICacheTTL)
	sendJSON(w, cs)
}

// fetchGCPSpendBigQuery runs a SQL query against the Cloud Billing
// BigQuery export and returns the current project's month-to-date
// cost in USD.
//
// The export table is created automatically by Google after billing
// export is enabled in the Cloud Console. The table name follows
// the pattern:
//
//	<project>.<dataset>.gcp_billing_export_v1_<BILLING_ACCOUNT_ID>
//
// with the billing account id dashes replaced by underscores. Rather
// than hard-code that id, we query with a wildcard:
//
//	`<project>.<dataset>.gcp_billing_export_v1_*`
//
// which BigQuery treats as a table-union across all daily shards of
// the export.
//
// Project id and dataset come from environment variables so this is
// reusable for projects with a different setup.
func fetchGCPSpendBigQuery(monthStart time.Time) (float64, error) {
	projectID := os.Getenv("BILLING_EXPORT_PROJECT")
	if projectID == "" {
		projectID = gcpMeta("project/project-id")
	}
	dataset := os.Getenv("BILLING_EXPORT_DATASET")
	if dataset == "" {
		dataset = "billing_export"
	}
	if projectID == "unknown" || projectID == "" {
		return 0, fmt.Errorf("missing project id")
	}

	token, err := getMetadataToken()
	if err != nil {
		return 0, fmt.Errorf("metadata token: %v", err)
	}

	sql := fmt.Sprintf(
		"SELECT SUM(cost) AS total "+
			"FROM `%s.%s.gcp_billing_export_v1_*` "+
			"WHERE project.id = '%s' "+
			"AND usage_start_time >= TIMESTAMP('%s')",
		projectID, dataset, projectID,
		monthStart.Format("2006-01-02 15:04:05"),
	)

	body, _ := json.Marshal(map[string]interface{}{
		"query":        sql,
		"useLegacySql": false,
		"timeoutMs":    10000,
	})

	reqURL := fmt.Sprintf("https://bigquery.googleapis.com/bigquery/v2/projects/%s/queries", projectID)
	req, _ := http.NewRequest("POST", reqURL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("bigquery: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		rawStr := strings.TrimSpace(string(raw))
		// "Table does not match" happens on a fresh setup before the
		// billing export has been enabled in the Cloud Console, or
		// during the first 24h before the first daily shard lands.
		// Surface a human-friendly message for the dashboard.
		if strings.Contains(rawStr, "does not match any table") {
			return 0, fmt.Errorf("waiting for first billing export (up to 24h after enabling in console)")
		}
		return 0, fmt.Errorf("bigquery %d: %s", resp.StatusCode, rawStr)
	}

	// BigQuery jobs.query response shape:
	//   { "jobComplete": true,
	//     "schema": { "fields": [ { "name": "total", ... } ] },
	//     "rows": [ { "f": [ { "v": "0.37" } ] } ] }
	// SUM of zero rows returns a single row with f[0].v = null.
	var result struct {
		JobComplete bool `json:"jobComplete"`
		Rows        []struct {
			F []struct {
				V interface{} `json:"v"`
			} `json:"f"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode bigquery: %v", err)
	}

	if !result.JobComplete {
		return 0, fmt.Errorf("bigquery job not complete")
	}
	if len(result.Rows) == 0 || len(result.Rows[0].F) == 0 {
		return 0, nil // no data yet — could be first day after export enable
	}
	switch v := result.Rows[0].F[0].V.(type) {
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, nil
		}
		return f, nil
	case nil:
		return 0, nil
	default:
		return 0, fmt.Errorf("unexpected bigquery cell type %T", v)
	}
}

// --- Costs breakdown (detail page) -----------------------------------
//
// Used by /services/details/costs. Goes deeper than /api/cloud-spend:
// instead of one GCP total, it classifies each billing-export row
// into one of three categories and returns a 30-day daily series for
// each of the two headline categories (VM compute and network
// egress). "Other" collapses to a single number (disk, IP, minor
// SKUs).
//
// The 30-day window is "last 30 completed UTC days" — today is
// excluded because the export lags ~24h and today's bucket is
// always partial. Callers that want MTD should keep using
// /api/cloud-spend.

const costsBreakdownWindowDays = 30

type CostCategorySeries struct {
	Total30d float64   `json:"total_30d_usd"`
	AvgDaily float64   `json:"avg_daily_usd"`
	Daily    []DayCost `json:"daily"`
}

type CostsBreakdown struct {
	VMCompute      CostCategorySeries `json:"vm_compute"`
	NetworkEgress  CostCategorySeries `json:"network_egress"`
	OtherGCP       float64            `json:"other_gcp_30d_usd"`
	Anthropic      float64            `json:"anthropic_30d_usd"`
	WindowDays     int                `json:"window_days"`
	WindowStart    string             `json:"window_start"` // YYYY-MM-DD
	WindowEnd      string             `json:"window_end"`   // YYYY-MM-DD, exclusive
	GCPError       string             `json:"gcp_error,omitempty"`
	AnthropicError string             `json:"anthropic_error,omitempty"`
}

var (
	costsBreakdownCacheMu      sync.Mutex
	costsBreakdownCacheValue   CostsBreakdown
	costsBreakdownCacheExpires time.Time
)

func handleCostsBreakdown(w http.ResponseWriter, r *http.Request) {
	costsBreakdownCacheMu.Lock()
	defer costsBreakdownCacheMu.Unlock()

	if time.Now().Before(costsBreakdownCacheExpires) {
		sendJSON(w, costsBreakdownCacheValue)
		return
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowStart := today.AddDate(0, 0, -costsBreakdownWindowDays)
	windowEnd := today

	cb := CostsBreakdown{
		WindowDays:  costsBreakdownWindowDays,
		WindowStart: windowStart.Format("2006-01-02"),
		WindowEnd:   windowEnd.Format("2006-01-02"),
	}

	vm, egress, total, err := fetchGCPCategorizedCosts(windowStart, windowEnd)
	if err != nil {
		cb.GCPError = err.Error()
	}
	// Always build zero-filled series so the chart renders an empty
	// axis instead of "no data yet" when the export is wired up but
	// the current window has no rows yet.
	cb.VMCompute = buildCostSeries(vm, windowStart, windowEnd)
	cb.NetworkEgress = buildCostSeries(egress, windowStart, windowEnd)
	if err == nil {
		cb.OtherGCP = total - cb.VMCompute.Total30d - cb.NetworkEgress.Total30d
		if cb.OtherGCP < 0 {
			// SUD credits can push "other" slightly negative if VM
			// compute classification misses a SKU edge case. Clamp
			// instead of showing a negative number on the UI.
			cb.OtherGCP = 0
		}
	}

	if oauthConfig != nil && oauthConfig.AnthropicAdminKey != "" {
		spend, _, aerr := fetchAnthropicSpend(oauthConfig.AnthropicAdminKey, windowStart)
		if aerr != nil {
			cb.AnthropicError = aerr.Error()
		} else {
			cb.Anthropic = spend
		}
	} else {
		cb.AnthropicError = "anthropic admin key not configured"
	}

	costsBreakdownCacheValue = cb
	costsBreakdownCacheExpires = time.Now().Add(externalAPICacheTTL)
	sendJSON(w, cb)
}

// fetchGCPCategorizedCosts runs ONE BigQuery SELECT that classifies
// each row into vm_compute / network_egress / other and returns
// per-day sums for the two headline categories plus the global total.
//
// Net cost = `cost + SUM(credits.amount)` because SUD and other
// credits appear as negative line items in a separate `credits`
// array; summing those in gives the bill-matching number instead of
// the inflated gross. Standard pattern from Google's own docs.
//
// Classification (keep in sync with backlog.md):
//
//	vm_compute     — any SKU with "instance core" or "instance ram"
//	                 in its description (E2/N2/etc.). Single-VM
//	                 project so this is effectively "simple-zombie".
//	network_egress — SKUs with "internet egress" in the description.
//	                 Destinations split into multiple SKUs (EMEA →
//	                 Americas, EMEA → APAC, ...) and all sum in.
//	other          — everything else. Returned as a single 30d
//	                 number, not a daily series.
//
// Anticipated SKU patterns are based on GCP docs; if the real export
// uses different substrings, update the CASE arms here and nothing
// else.
func fetchGCPCategorizedCosts(start, end time.Time) (
	vmCompute map[string]float64,
	networkEgress map[string]float64,
	total float64,
	err error,
) {
	projectID := os.Getenv("BILLING_EXPORT_PROJECT")
	if projectID == "" {
		projectID = gcpMeta("project/project-id")
	}
	dataset := os.Getenv("BILLING_EXPORT_DATASET")
	if dataset == "" {
		dataset = "billing_export"
	}
	if projectID == "unknown" || projectID == "" {
		return nil, nil, 0, fmt.Errorf("missing project id")
	}

	token, terr := getMetadataToken()
	if terr != nil {
		return nil, nil, 0, fmt.Errorf("metadata token: %v", terr)
	}

	sql := fmt.Sprintf(
		"SELECT "+
			"  DATE(usage_start_time) AS day, "+
			"  CASE "+
			"    WHEN LOWER(sku.description) LIKE '%%instance core%%' "+
			"      OR LOWER(sku.description) LIKE '%%instance ram%%' "+
			"      THEN 'vm_compute' "+
			"    WHEN LOWER(sku.description) LIKE '%%internet egress%%' "+
			"      THEN 'network_egress' "+
			"    ELSE 'other' "+
			"  END AS category, "+
			"  SUM(cost + IFNULL((SELECT SUM(c.amount) FROM UNNEST(credits) c), 0)) AS net_cost "+
			"FROM `%s.%s.gcp_billing_export_v1_*` "+
			"WHERE project.id = '%s' "+
			"  AND DATE(usage_start_time) >= DATE('%s') "+
			"  AND DATE(usage_start_time) < DATE('%s') "+
			"GROUP BY day, category",
		projectID, dataset, projectID,
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
	)

	body, _ := json.Marshal(map[string]interface{}{
		"query":        sql,
		"useLegacySql": false,
		"timeoutMs":    10000,
	})

	reqURL := fmt.Sprintf("https://bigquery.googleapis.com/bigquery/v2/projects/%s/queries", projectID)
	req, _ := http.NewRequest("POST", reqURL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, rerr := client.Do(req)
	if rerr != nil {
		return nil, nil, 0, fmt.Errorf("bigquery: %v", rerr)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		rawStr := strings.TrimSpace(string(raw))
		if strings.Contains(rawStr, "does not match any table") {
			return nil, nil, 0, fmt.Errorf("waiting for first billing export (up to 24h after enabling in console)")
		}
		return nil, nil, 0, fmt.Errorf("bigquery %d: %s", resp.StatusCode, rawStr)
	}

	var result struct {
		JobComplete bool `json:"jobComplete"`
		Rows        []struct {
			F []struct {
				V interface{} `json:"v"`
			} `json:"f"`
		} `json:"rows"`
	}
	if derr := json.NewDecoder(resp.Body).Decode(&result); derr != nil {
		return nil, nil, 0, fmt.Errorf("decode bigquery: %v", derr)
	}
	if !result.JobComplete {
		return nil, nil, 0, fmt.Errorf("bigquery job not complete")
	}

	vmCompute = make(map[string]float64)
	networkEgress = make(map[string]float64)
	for _, row := range result.Rows {
		if len(row.F) < 3 {
			continue
		}
		day, _ := row.F[0].V.(string)
		category, _ := row.F[1].V.(string)
		costStr, _ := row.F[2].V.(string)
		cost, perr := strconv.ParseFloat(costStr, 64)
		if perr != nil {
			continue
		}

		total += cost
		switch category {
		case "vm_compute":
			vmCompute[day] += cost
		case "network_egress":
			networkEgress[day] += cost
		}
	}

	return vmCompute, networkEgress, total, nil
}

// buildCostSeries zero-fills a map of date→cost into a chronological
// 30-entry array bounded by [start, end). Also computes total and
// average so the frontend can show them as headline numbers without
// recomputing.
func buildCostSeries(byDate map[string]float64, start, end time.Time) CostCategorySeries {
	daily := make([]DayCost, 0, 32)
	var total float64
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		v := byDate[key]
		total += v
		daily = append(daily, DayCost{Date: key, USD: v})
	}

	var avg float64
	if len(daily) > 0 {
		avg = total / float64(len(daily))
	}

	return CostCategorySeries{
		Total30d: total,
		AvgDaily: avg,
		Daily:    daily,
	}
}

// --- Stop VM ---
// User-triggered self-destruct. Calls the Compute Engine REST API
// directly with the metadata-server OAuth token. The API returns
// immediately with an Operation object; the actual shutdown happens
// over the next ~30 seconds, which is plenty of time for this HTTP
// response to complete before alive-server gets killed.
func handleStopVM(w http.ResponseWriter, r *http.Request) {
	project := gcpMeta("project/project-id")
	zoneRaw := gcpMeta("instance/zone")
	zone := zoneRaw
	if i := strings.LastIndex(zoneRaw, "/"); i >= 0 {
		zone = zoneRaw[i+1:]
	}
	name := gcpMeta("instance/name")

	if project == "unknown" || name == "unknown" {
		sendJSON(w, map[string]interface{}{"error": "metadata missing"})
		return
	}

	token, err := getMetadataToken()
	if err != nil {
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("metadata token: %v", err)})
		return
	}

	reqURL := fmt.Sprintf(
		"https://compute.googleapis.com/compute/v1/projects/%s/zones/%s/instances/%s/stop",
		project, zone, name,
	)
	req, _ := http.NewRequest("POST", reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("contact compute: %v", err)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("compute %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))})
		return
	}

	log.Printf("vm stop: requested by dashboard, instance %s/%s/%s", project, zone, name)
	sendJSON(w, map[string]interface{}{"success": true, "message": "stop requested — vm will be down in ~30s"})
}

// fetchInstanceCreationTimestamp hits the Compute Engine REST API for
// the current instance to get its creation timestamp (not exposed in
// the metadata server). Uses the metadata-server access token.
func fetchInstanceCreationTimestamp() (string, error) {
	project := gcpMeta("project/project-id")
	zoneRaw := gcpMeta("instance/zone")
	zone := zoneRaw
	if i := strings.LastIndex(zoneRaw, "/"); i >= 0 {
		zone = zoneRaw[i+1:]
	}
	name := gcpMeta("instance/name")
	if project == "unknown" || name == "unknown" {
		return "", fmt.Errorf("metadata missing")
	}

	token, err := getMetadataToken()
	if err != nil {
		return "", err
	}

	reqURL := fmt.Sprintf("https://compute.googleapis.com/compute/v1/projects/%s/zones/%s/instances/%s",
		project, zone, name)
	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("compute %d", resp.StatusCode)
	}
	var data struct {
		CreationTimestamp string `json:"creationTimestamp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	return data.CreationTimestamp, nil
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		return "—"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// fetchAnthropicSpend calls the Anthropic cost_report admin API and
// returns (total USD in window, last-7-days daily breakdown, error).
//
// Endpoint: GET https://api.anthropic.com/v1/organizations/cost_report
// Auth:     x-api-key: <admin_key>, anthropic-version: 2023-06-01
// Query:    starting_at=<ISO UTC>, bucket_width=1d[, page=<token>]
//
// Two surprises discovered while reverse-engineering this against
// the platform.claude.com cost page:
//
//  1. The response paginates at 7 buckets per page. We walk
//     `next_page` until `has_more` is false (capped at 10 pages /
//     70 days so a runaway account can't stall the dashboard).
//
//  2. The `amount` field is a string in CENTS, not dollars, despite
//     being reported with `currency: "USD"`. Cross-checking against
//     `usage_report/messages` + the published Haiku 4.5 token prices
//     shows the cost_report values are exactly 100x the dollar value
//     the console displays. We divide by 100 to convert back.
func fetchAnthropicSpend(adminKey string, startWindow time.Time) (float64, []DayCost, error) {
	now := time.Now().UTC()
	// Anchor at midnight UTC so bucket edges align with the API.
	startWindow = time.Date(startWindow.Year(), startWindow.Month(), startWindow.Day(), 0, 0, 0, 0, time.UTC)

	base := fmt.Sprintf(
		"https://api.anthropic.com/v1/organizations/cost_report?starting_at=%s&bucket_width=1d",
		url.QueryEscape(startWindow.Format(time.RFC3339)),
	)

	client := &http.Client{Timeout: 5 * time.Second}

	type costResult struct {
		Currency string `json:"currency"`
		Amount   string `json:"amount"`
	}
	type bucket struct {
		StartingAt string       `json:"starting_at"`
		Results    []costResult `json:"results"`
	}
	type page struct {
		Data     []bucket `json:"data"`
		HasMore  bool     `json:"has_more"`
		NextPage string   `json:"next_page"`
	}

	var windowTotal float64
	byDate := make(map[string]float64)

	pageURL := base
	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest("GET", pageURL, nil)
		req.Header.Set("x-api-key", adminKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, fmt.Errorf("contact anthropic: %v", err)
		}

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return 0, nil, fmt.Errorf("anthropic %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var p page
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			resp.Body.Close()
			return 0, nil, fmt.Errorf("decode anthropic response: %v", err)
		}
		resp.Body.Close()

		for _, b := range p.Data {
			t, err := time.Parse(time.RFC3339, b.StartingAt)
			if err != nil {
				continue
			}
			day := t.UTC().Format("2006-01-02")
			for _, r := range b.Results {
				if r.Currency != "USD" && r.Currency != "" {
					continue
				}
				v, err := strconv.ParseFloat(r.Amount, 64)
				if err != nil {
					continue
				}
				// cost_report amounts are in cents despite the "USD" label.
				v = v / 100
				byDate[day] += v
				windowTotal += v
			}
		}

		if !p.HasMore || p.NextPage == "" {
			break
		}
		pageURL = base + "&page=" + url.QueryEscape(p.NextPage)
	}

	// Build the last-7-day array in chronological order, filling gaps with 0.
	daily := make([]DayCost, 0, 7)
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		daily = append(daily, DayCost{Date: d, USD: byDate[d]})
	}

	return windowTotal, daily, nil
}

func getServicesStatus() []Service {
	installed := loadInstalledServices()
	var results []Service
	for _, svc := range knownServices {
		s := svc
		s.Installed = installed[svc.ID]
		if s.Installed {
			if svc.CheckProcess != "" {
				_, s.Running = runCmd("pgrep", "-f", svc.CheckProcess)
			} else if svc.ServiceName != "" {
				out, _ := runCmd("systemctl", "is-active", svc.ServiceName)
				s.Running = out == "active"
			} else {
				s.Running = true // static service (no daemon)
			}
		}
		results = append(results, s)
	}
	return results
}

// --- Error page ---

const errorPageTemplate = `<!DOCTYPE html>
<html>
<head>
    <title>Attlas — Access Denied</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: -apple-system, sans-serif; background: #1a1a2e; color: #eee;
               display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; }
        .box { max-width: 400px; width: 100%%; padding: 2rem; text-align: center; }
        h1 { color: #fc8181; font-size: 1.5rem; margin-bottom: 1rem; }
        p { color: #888; line-height: 1.6; }
        a { color: #5a67d8; }
    </style>
</head>
<body>
    <div class="box">
        <h1>Access Denied</h1>
        <p>%s</p>
        <p style="margin-top: 1.5rem;"><a href="/oauth2/login">Try again</a></p>
    </div>
</body>
</html>`

const accessDeniedPage = `<!DOCTYPE html>
<html>
<head>
    <title>KEEP AWAY</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: -apple-system, sans-serif; background: #0a0a0a; color: #ff0000;
               display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0;
               overflow: hidden; }
        .container { text-align: center; animation: pulse 1.5s ease-in-out infinite; }
        .icon { font-size: 8rem; margin-bottom: 1rem; filter: drop-shadow(0 0 30px #ff0000); }
        h1 { font-size: 4rem; font-weight: 900; letter-spacing: 0.3rem; text-transform: uppercase;
             text-shadow: 0 0 20px #ff0000, 0 0 40px #ff4444, 0 0 80px #ff0000; margin: 0.5rem 0; }
        .sub { font-size: 1.2rem; color: #ff4444; letter-spacing: 0.5rem; text-transform: uppercase; }
        .bar { height: 4px; background: repeating-linear-gradient(90deg, #ff0000 0px, #ff0000 20px, #000 20px, #000 40px);
               margin: 2rem auto; width: 80%%; max-width: 500px; }
        @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.7; } }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">&#9889;</div>
        <div class="bar"></div>
        <h1>KEEP AWAY</h1>
        <div class="bar"></div>
        <div class="sub">unauthorized access</div>
    </div>
</body>
</html>`

// --- Handlers ---

func handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	// Caddy forward_auth sends the original request URI in X-Forwarded-Uri.
	// Consult the public-path registry first: services register path
	// prefixes they want to handle with their own auth (or no auth), and
	// we wave those through without a session check.
	origURI := r.Header.Get("X-Forwarded-Uri")
	if publicPathRegistry.matches(origURI) {
		w.WriteHeader(http.StatusOK)
		return
	}

	// RFC 8414: when an OAuth issuer has a path component (e.g.
	// https://attlas.uk/petboard), clients construct the well-known
	// metadata URL as /.well-known/oauth-authorization-server/<path>.
	// That path is NOT under /petboard/* so Caddy won't route it to
	// petboard. We handle it here by redirecting to the service's
	// actual well-known endpoint. This makes Claude Code's MCP OAuth
	// discovery work for any subpath-based issuer without Caddy changes.
	if strings.HasPrefix(origURI, "/.well-known/oauth-authorization-server/") {
		svcPath := strings.TrimPrefix(origURI, "/.well-known/oauth-authorization-server")
		redirect := svcPath + "/.well-known/oauth-authorization-server"
		http.Redirect(w, r, redirect, http.StatusFound)
		return
	}
	if isAuthenticated(r) {
		w.WriteHeader(http.StatusOK)
		return
	}
	// Preserve the original URL across the OAuth round-trip so the user
	// lands back on the page they tried to visit instead of on /.
	loginURL := "/oauth2/login"
	if isSafeRelativePath(origURI) {
		loginURL += "?return_to=" + url.QueryEscape(origURI)
	}
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func handleOAuth2Login(w http.ResponseWriter, r *http.Request) {
	if oauthConfig == nil {
		http.Error(w, "OAuth2 not configured", http.StatusInternalServerError)
		return
	}

	// Capture the return URL from the ?return_to query param. Rejected
	// unless it's a safe same-origin relative path.
	returnTo := r.URL.Query().Get("return_to")
	if !isSafeRelativePath(returnTo) {
		returnTo = ""
	}

	state := oauthStates.generate(returnTo)
	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s&access_type=online&prompt=select_account",
		url.QueryEscape(oauthConfig.ClientID),
		url.QueryEscape("https://attlas.uk/oauth2/callback"),
		url.QueryEscape("openid email"),
		url.QueryEscape(state),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func handleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	if oauthConfig == nil {
		http.Error(w, "OAuth2 not configured", http.StatusInternalServerError)
		return
	}

	// Validate state and recover the captured return URL in one step.
	state := r.URL.Query().Get("state")
	returnTo, ok := oauthStates.validate(state)
	if !ok {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Invalid or expired login request. Please try again.")
		return
	}

	// Check for error from Google
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Google sign-in was cancelled or failed.")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "No authorization code received from Google.")
		return
	}

	// Exchange code for token
	tokenResp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"code":          {code},
		"client_id":     {oauthConfig.ClientID},
		"client_secret": {oauthConfig.ClientSecret},
		"redirect_uri":  {"https://attlas.uk/oauth2/callback"},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Failed to contact Google. Please try again.")
		return
	}
	defer tokenResp.Body.Close()

	var tokenData struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&tokenData)
	if tokenData.AccessToken == "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Failed to authenticate with Google.")
		return
	}

	// Get user info
	userReq, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	userReq.Header.Set("Authorization", "Bearer "+tokenData.AccessToken)
	userResp, err := http.DefaultClient.Do(userReq)
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Failed to get user info from Google.")
		return
	}
	defer userResp.Body.Close()

	var userInfo struct {
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
	}
	json.NewDecoder(userResp.Body).Decode(&userInfo)

	if userInfo.Email == "" || !userInfo.VerifiedEmail {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, errorPageTemplate, "Could not verify your email address.")
		return
	}

	// Check against allowed emails
	allowed := false
	for _, e := range oauthConfig.AllowedEmails {
		if strings.EqualFold(e, userInfo.Email) {
			allowed = true
			break
		}
	}
	if !allowed {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, accessDeniedPage)
		return
	}

	// Create session
	sessionToken := makeSessionToken(userInfo.Email)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	})

	// Prefer the captured return URL, falling back to / if missing or
	// unsafe (validated defensively here even though generate() also
	// screens it).
	dest := "/"
	if isSafeRelativePath(returnTo) {
		dest = returnTo
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/oauth2/login", http.StatusFound)
}

func getSessionEmail(r *http.Request) string {
	token := getSessionCookie(r)
	if token == "" {
		return ""
	}
	lastColon := strings.LastIndex(token, ":")
	if lastColon < 0 {
		return ""
	}
	payload := token[:lastColon]
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	var allowedEmails []string
	if oauthConfig != nil {
		allowedEmails = oauthConfig.AllowedEmails
	}
	sendJSON(w, map[string]interface{}{
		"vm": getVMInfo(),
		"user": map[string]interface{}{
			"email":          getSessionEmail(r),
			"allowed_emails": allowedEmails,
		},
		"claude": map[string]bool{
			"installed":     isClaudeInstalled(),
			"authenticated": isClaudeLoggedIn(),
		},
		"services":      getServicesStatus(),
		"dotfiles":      getDotfilesStatus(),
		"domain_expiry": getDomainExpiry(),
		"system_load":   getSystemLoad(),
	})
}

func handleClaudeLogin(w http.ResponseWriter, r *http.Request) {
	if isClaudeLoggedIn() {
		sendJSON(w, map[string]interface{}{"error": "Already logged in"})
		return
	}

	exec.Command("pkill", "-f", "claude-login-helper").Run()
	time.Sleep(1 * time.Second)

	for _, f := range []string{"/tmp/claude-login-url", "/tmp/claude-login-code", "/tmp/claude-login-result"} {
		os.Remove(f)
	}

	helperPath := filepath.Join(filepath.Dir(os.Args[0]), "claude-login-helper.py")
	if _, err := os.Stat(helperPath); err != nil {
		helperPath = filepath.Join(distDir, "..", "claude-login-helper.py")
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
		sendJSON(w, map[string]interface{}{"url": authURL})
	} else {
		logData, _ := os.ReadFile("/tmp/claude-login-helper.log")
		snippet := string(logData)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Timed out waiting for URL. Log: %s", snippet)})
	}
}

func handleClaudeCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Code == "" {
		sendJSON(w, map[string]interface{}{"error": "No code provided."})
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
		sendJSON(w, map[string]interface{}{"success": true})
	} else if result != "" {
		sendJSON(w, map[string]interface{}{"error": result})
	} else {
		sendJSON(w, map[string]interface{}{"error": "Timed out waiting for login result."})
	}
}

func handleInstallService(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	svc := findService(body.ID)
	if svc == nil {
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Unknown service: %s", body.ID)})
		return
	}

	script := filepath.Join(attlasDir, "services", svc.Script)
	if _, err := os.Stat(script); err != nil {
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Script not found: %s", script)})
		return
	}

	// install-*.sh scripts require root (they write to /etc/systemd/system/),
	// so we run them via sudo. Authorized by /etc/sudoers.d/alive-svc-services.
	cmd := exec.Command("sudo", "-n", "bash", script)
	cmd.Dir = filepath.Join(attlasDir, "services")
	out, err := cmd.CombinedOutput()
	if err != nil {
		sendJSON(w, map[string]interface{}{"error": string(out)})
		return
	}

	exec.Command("sudo", "-n", "systemctl", "reload", "caddy").Run()
	sendJSON(w, map[string]interface{}{"success": true})
}

func handleUninstallService(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	svc := findService(body.ID)
	if svc == nil {
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Unknown service: %s", body.ID)})
		return
	}

	script := filepath.Join(attlasDir, "services", fmt.Sprintf("uninstall-%s.sh", svc.ID))
	if _, err := os.Stat(script); err != nil {
		sendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Uninstall script not found: %s", script)})
		return
	}

	// uninstall-*.sh scripts require root (they touch /etc/systemd/system/
	// and /etc/caddy/conf.d/), so we run them via sudo.
	cmd := exec.Command("sudo", "-n", "bash", script)
	cmd.Dir = filepath.Join(attlasDir, "services")
	out, err := cmd.CombinedOutput()
	if err != nil {
		sendJSON(w, map[string]interface{}{"error": string(out)})
		return
	}

	exec.Command("sudo", "-n", "systemctl", "reload", "caddy").Run()
	sendJSON(w, map[string]interface{}{"success": true})
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

func sendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
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
	sessionSecret = loadOrCreateSecret()
	oauthConfig = loadOAuthConfig()

	// Initial load of the public-path registry and a SIGHUP-triggered
	// reloader so services can `systemctl kill --signal=SIGHUP alive-server`
	// after installing or uninstalling to pick up their changes without
	// dropping existing sessions.
	publicPathRegistry.load()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)
	go func() {
		for range sigCh {
			log.Printf("SIGHUP received — reloading public-path registry")
			publicPathRegistry.load()
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

	if _, err := os.Stat(distDir); err != nil {
		wd, _ := os.Getwd()
		distDir = filepath.Join(wd, "frontend", "dist")
	}

	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("/api/auth/verify", handleAuthVerify)
	mux.HandleFunc("GET /oauth2/login", handleOAuth2Login)
	mux.HandleFunc("GET /oauth2/callback", handleOAuth2Callback)
	mux.HandleFunc("/logout", handleLogout)

	// API
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("POST /api/claude-login", handleClaudeLogin)
	mux.HandleFunc("POST /api/claude-login/code", handleClaudeCode)
	mux.HandleFunc("POST /api/install-service", handleInstallService)
	mux.HandleFunc("POST /api/uninstall-service", handleUninstallService)
	mux.HandleFunc("POST /api/dotfiles/sync", handleDotfilesSync)
	mux.HandleFunc("GET /api/services/openclaw", handleOpenclawDetail)
	mux.HandleFunc("GET /api/services/terminal", handleTerminalDetail)
	mux.HandleFunc("POST /api/services/terminal/kill", handleTerminalKill)
	mux.HandleFunc("GET /api/services/infrastructure", handleInfrastructureDetail)
	mux.HandleFunc("GET /api/cloud-spend", handleCloudSpend)
	mux.HandleFunc("GET /api/costs/breakdown", handleCostsBreakdown)
	mux.HandleFunc("POST /api/vm/stop", handleStopVM)

	// Diary (Hugo static site)
	diaryDir := filepath.Join(attlasDir, "diary", "public")
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
