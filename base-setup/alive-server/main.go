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
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

type stateStore struct {
	mu     sync.Mutex
	states map[string]time.Time
}

var oauthStates = &stateStore{
	states: make(map[string]time.Time),
}

func (ss *stateStore) generate() string {
	b := make([]byte, 32)
	rand.Read(b)
	state := hex.EncodeToString(b)

	ss.mu.Lock()
	defer ss.mu.Unlock()

	// Clean expired states
	now := time.Now()
	for k, v := range ss.states {
		if now.Sub(v) > 10*time.Minute {
			delete(ss.states, k)
		}
	}

	ss.states[state] = now
	return state
}

func (ss *stateStore) validate(state string) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if t, ok := ss.states[state]; ok {
		delete(ss.states, state)
		return time.Since(t) < 10*time.Minute
	}
	return false
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

// --- Infrastructure detail (daily VM uptime via Cloud Logging) ---

type DayUptime struct {
	Date    string `json:"date"`    // YYYY-MM-DD (UTC)
	Seconds int64  `json:"seconds"` // 0 to 86400
}

type InfrastructureDetail struct {
	Name              string      `json:"name"`
	Zone              string      `json:"zone"`
	Region            string      `json:"region"`
	ExternalIP        string      `json:"external_ip"`
	InternalIP        string      `json:"internal_ip"`
	Domain            string      `json:"domain"`
	MachineType       string      `json:"machine_type"`
	CreationTimestamp string      `json:"creation_timestamp"` // ISO (from metadata)
	OSBootTime        string      `json:"os_boot_time"`       // ISO
	UptimeNow         string      `json:"uptime_now"`         // human-readable
	DailyUptime       []DayUptime `json:"daily_uptime"`
	TotalSecondsMonth int64       `json:"total_seconds_month"`
	EventsError       string      `json:"events_error,omitempty"`
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

// instanceEvent is a single start/stop audit entry for this VM.
type instanceEvent struct {
	timestamp time.Time
	method    string // "start" | "stop"
}

// fetchInstanceEvents pulls GCE audit-log start/stop/insert/delete
// entries for EVERY instance in the project, strictly after `since`.
// Sorted chronologically.
//
// No instance-name filter: this is a single-VM project and the user
// wants the uptime timeline to span all historical names (so the ~5h
// on "openclaw-vm" during Apr 8 morning shows up alongside the newer
// "simple-zombie" history). If this project ever grew multiple VMs
// we'd need to dedupe / merge overlapping intervals, but for now the
// single-VM invariant keeps the timeline clean.
//
// insert/delete are included so the replay knows when the VM actually
// existed — under terraform-managed infra the VM is destroyed and
// recreated during the day, and we don't want to credit a "not yet
// created" day with 24h of fake uptime.
func fetchInstanceEvents(since time.Time) ([]instanceEvent, error) {
	projectID := gcpMeta("project/project-id")
	if projectID == "unknown" {
		return nil, fmt.Errorf("missing metadata (project=%s)", projectID)
	}

	token, err := getMetadataToken()
	if err != nil {
		return nil, fmt.Errorf("metadata token: %v", err)
	}

	filter := fmt.Sprintf(
		`resource.type="gce_instance" AND (protoPayload.methodName="v1.compute.instances.start" OR protoPayload.methodName="v1.compute.instances.stop" OR protoPayload.methodName="v1.compute.instances.insert" OR protoPayload.methodName="v1.compute.instances.delete") AND timestamp>="%s"`,
		since.UTC().Format(time.RFC3339),
	)

	body := map[string]interface{}{
		"resourceNames": []string{"projects/" + projectID},
		"filter":        filter,
		"pageSize":      1000,
		"orderBy":       "timestamp asc",
	}
	bodyJSON, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://logging.googleapis.com/v2/entries:list", bytes.NewReader(bodyJSON))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("logging request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("logging %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result struct {
		Entries []struct {
			Timestamp    string `json:"timestamp"`
			ProtoPayload struct {
				MethodName string `json:"methodName"`
			} `json:"protoPayload"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode logging: %v", err)
	}

	events := make([]instanceEvent, 0, len(result.Entries))
	var parseFailures int
	var lastParseErr string
	for _, e := range result.Entries {
		t, err := parseLoggingTimestamp(e.Timestamp)
		if err != nil {
			parseFailures++
			lastParseErr = err.Error()
			continue
		}
		// Collapse GCE lifecycle verbs onto a running/not-running axis:
		//   insert → on  (VM created and running)
		//   delete → off (VM gone)
		//   start  → on
		//   stop   → off
		var method string
		switch {
		case strings.Contains(e.ProtoPayload.MethodName, "stop"),
			strings.Contains(e.ProtoPayload.MethodName, "delete"):
			method = "stop"
		default:
			method = "start"
		}
		events = append(events, instanceEvent{timestamp: t.UTC(), method: method})
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].timestamp.Before(events[j].timestamp)
	})
	log.Printf("infra: fetched %d raw entries, parsed %d events, %d parse failures (last err: %q)",
		len(result.Entries), len(events), parseFailures, lastParseErr)
	return events, nil
}

// parseLoggingTimestamp is tolerant of the variety of timestamp shapes
// Cloud Logging emits: integer seconds, 3-digit millis, 6-digit micros,
// 9-digit nanos, and occasionally no fractional part at all.
func parseLoggingTimestamp(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000000000Z07:00",
		"2006-01-02T15:04:05.000000Z07:00",
		"2006-01-02T15:04:05.000Z07:00",
	}
	var lastErr error
	for _, f := range formats {
		t, err := time.Parse(f, s)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

// uptimeInterval is a contiguous [start, end] period of VM ON state.
type uptimeInterval struct {
	start time.Time
	end   time.Time
}

// computeDailyUptime replays audit events into ON intervals and
// distributes them across each day of the current UTC month.
//
// Lifecycle events (insert/delete) are folded into the same start/stop
// stream upstream, so by this point every event is either "start"
// (running) or "stop" (not running).
//
// The "state before the first observed event" question is answered
// by a leading-STOP heuristic: if the first event in the window is a
// stop, the VM must have been running before, so we credit the
// pre-window period as ON. If the first event is a start (which on
// GCE is usually the very first insert for a brand-new VM), the VM
// didn't exist / was off before, and the pre-window period is NOT
// counted. This matches reality under terraform-managed infra where
// the VM can be created mid-month.
func computeDailyUptime(events []instanceEvent, now time.Time) ([]DayUptime, int64) {
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	// Anchor well before the month so `(anchor, first-stop)` covers
	// any pre-month ON period without accidental gaps.
	anchor := monthStart.AddDate(0, 0, -30)

	intervals := []uptimeInterval{}
	var openStart *time.Time
	state := "unknown"

	for i, ev := range events {
		if i == 0 {
			if ev.method == "stop" {
				// VM was running up until this stop.
				intervals = append(intervals, uptimeInterval{start: anchor, end: ev.timestamp})
				state = "off"
			} else { // start / insert
				// VM wasn't running before this event. Open a new
				// interval but do NOT backfill the pre-event period.
				t := ev.timestamp
				openStart = &t
				state = "on"
			}
			continue
		}
		if ev.method == "start" && state == "off" {
			t := ev.timestamp
			openStart = &t
			state = "on"
		} else if ev.method == "stop" && state == "on" {
			intervals = append(intervals, uptimeInterval{start: *openStart, end: ev.timestamp})
			openStart = nil
			state = "off"
		}
		// duplicate consecutive start/stop pairs are ignored
	}

	// Still-on state → ongoing interval ending at now.
	if state == "on" && openStart != nil {
		intervals = append(intervals, uptimeInterval{start: *openStart, end: now})
	}
	// Note: we used to have an "unknown" fallback here that assumed
	// the VM was on for the full lookback when no events fired. That
	// produced phantom uptime for days before the VM even existed, so
	// it's gone. If the events window is empty, the month stays zero.

	// Build daily buckets for each day of the current month up to "now".
	var (
		daily      []DayUptime
		totalSecs  int64
		maxDayUTC  = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	)
	for d := monthStart; !d.After(maxDayUTC); d = d.AddDate(0, 0, 1) {
		dayEnd := d.Add(24 * time.Hour)
		var secs int64
		for _, iv := range intervals {
			ovStart := iv.start
			if ovStart.Before(d) {
				ovStart = d
			}
			ovEnd := iv.end
			if ovEnd.After(dayEnd) {
				ovEnd = dayEnd
			}
			if ovEnd.After(ovStart) {
				secs += int64(ovEnd.Sub(ovStart).Seconds())
			}
		}
		daily = append(daily, DayUptime{Date: d.Format("2006-01-02"), Seconds: secs})
		totalSecs += secs
	}
	return daily, totalSecs
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

	// Daily uptime from Cloud Logging audit events.
	events, err := fetchInstanceEvents(now.AddDate(0, 0, -38)) // month + 7d anchor + headroom
	if err != nil {
		detail.EventsError = err.Error()
	}
	daily, totalSecs := computeDailyUptime(events, now)
	detail.DailyUptime = daily
	detail.TotalSecondsMonth = totalSecs

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
	if isAuthenticated(r) {
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/oauth2/login", http.StatusFound)
	}
}

func handleOAuth2Login(w http.ResponseWriter, r *http.Request) {
	if oauthConfig == nil {
		http.Error(w, "OAuth2 not configured", http.StatusInternalServerError)
		return
	}

	state := oauthStates.generate()
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

	// Validate state
	state := r.URL.Query().Get("state")
	if !oauthStates.validate(state) {
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
	http.Redirect(w, r, "/", http.StatusFound)
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
		http.ServeFile(w, r, filePath)
		return
	}

	indexPath := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		w.Header().Set("Content-Type", "text/html")
		http.ServeFile(w, r, indexPath)
		return
	}

	http.NotFound(w, r)
}

// --- Main ---

func main() {
	sessionSecret = loadOrCreateSecret()
	oauthConfig = loadOAuthConfig()

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
