package main

import (
	"bytes"
	"context"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"attlas-server/internal/auth"
	"attlas-server/internal/config"
	"attlas-server/internal/gcp"
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
		Path: "/terminal/", Script: "install-terminal.sh"},
	{ID: "code-server", Name: "Cloud VS Code", ServiceName: "code-server", Command: "code-server",
		Path: "/code/", Script: "install-code-server.sh"},
	{ID: "openclaw", Name: "OpenClaw", ServiceName: "openclaw-gateway", Command: "openclaw",
		Path: "/openclaw/", Script: "install-openclaw.sh", CheckProcess: "openclaw-gateway"},
	{ID: "diary", Name: "Project Diary", ServiceName: "", Command: "hugo",
		Path: "/diary/", Script: "install-diary.sh"},
	{ID: "petboard", Name: "Petboard", ServiceName: "petboard", Command: "petboard",
		Path: "/petboard/", Script: "install-petboard.sh"},
	{ID: "homelab-planner", Name: "Homelab Planner", ServiceName: "homelab-planner", Command: "homelab-planner",
		Path: "/homelab-planner/", Script: "install-homelab-planner.sh"},
	// Splitsies lives on its own subdomain (splitsies.attlas.uk) routed
	// through splitsies-gateway (separate service, not listed here because
	// users never visit the gateway directly). The Path field accepts a
	// full URL — the dashboard's "open" link uses it as an href directly.
	{ID: "splitsies", Name: "Splitsies", ServiceName: "splitsies", Command: "splitsies",
		Path: "https://splitsies.attlas.uk/", Script: "install-splitsies.sh"},
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
		util.SendJSON(w, openclawCacheValue)
		return
	}

	detail := OpenclawDetail{}

	// 1. systemd runtime status.
	if out, ok := util.RunCmdCtx(2*time.Second, "systemctl", "is-active", "openclaw-gateway"); ok {
		detail.Running = out == "active"
	}
	if out, ok := util.RunCmdCtx(2*time.Second, "systemctl", "show", "openclaw-gateway",
		"-p", "ActiveEnterTimestamp"); ok {
		raw := strings.TrimPrefix(strings.TrimSpace(out), "ActiveEnterTimestamp=")
		if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", raw); err == nil && !t.IsZero() {
			detail.Uptime = util.HumanDuration(time.Since(t))
		}
	}

	// 2. openclaw status --all --json via sudo -u openclaw-svc.
	//    Authorized via /etc/sudoers.d/alive-svc-openclaw.
	if out, ok := util.RunCmdCtx(2*time.Second, "sudo", "-n", "-u", "openclaw-svc", "-H",
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
	if auth.AnthropicAdminKey() != "" {
		spend30, daily, err := fetchAnthropicSpend(auth.AnthropicAdminKey(), time.Now().UTC().AddDate(0, 0, -30))
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
	openclawCacheExpires = time.Now().Add(util.ExternalAPICacheTTL)
	util.SendJSON(w, detail)
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
	projectID := gcp.Meta("project/project-id")
	if projectID == "unknown" {
		return nil, nil, fmt.Errorf("missing metadata (project=%s)", projectID)
	}

	token, terr := gcp.MetadataToken()
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
		util.SendJSON(w, infraCacheValue)
		return
	}

	now := time.Now().UTC()
	detail := InfrastructureDetail{
		Name:       gcp.Meta("instance/name"),
		ExternalIP: gcp.Meta("instance/network-interfaces/0/access-configs/0/external-ip"),
		InternalIP: gcp.Meta("instance/network-interfaces/0/ip"),
		Domain:     "attlas.uk",
	}

	zoneRaw := gcp.Meta("instance/zone") // "projects/<num>/zones/europe-west1-b"
	if i := strings.LastIndex(zoneRaw, "/"); i >= 0 {
		detail.Zone = zoneRaw[i+1:]
	} else {
		detail.Zone = zoneRaw
	}
	if idx := strings.LastIndex(detail.Zone, "-"); idx >= 0 {
		detail.Region = detail.Zone[:idx]
	}

	mtRaw := gcp.Meta("instance/machine-type")
	if i := strings.LastIndex(mtRaw, "/"); i >= 0 {
		detail.MachineType = mtRaw[i+1:]
	} else {
		detail.MachineType = mtRaw
	}

	// /proc/stat btime → OS boot time → current uptime.
	if bt := osBootTime(); !bt.IsZero() {
		detail.OSBootTime = bt.Format(time.RFC3339)
		detail.UptimeNow = util.HumanDuration(now.Sub(bt))
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
	infraCacheExpires = time.Now().Add(util.ExternalAPICacheTTL)
	util.SendJSON(w, detail)
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
		util.SendJSON(w, cloudSpendCacheValue)
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
	if auth.AnthropicAdminKey() != "" {
		spend, _, err := fetchAnthropicSpend(auth.AnthropicAdminKey(), monthStart)
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
	cloudSpendCacheExpires = time.Now().Add(util.ExternalAPICacheTTL)
	util.SendJSON(w, cs)
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
		projectID = gcp.Meta("project/project-id")
	}
	dataset := os.Getenv("BILLING_EXPORT_DATASET")
	if dataset == "" {
		dataset = "billing_export"
	}
	if projectID == "unknown" || projectID == "" {
		return 0, fmt.Errorf("missing project id")
	}

	token, err := gcp.MetadataToken()
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
		util.SendJSON(w, costsBreakdownCacheValue)
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

	if auth.AnthropicAdminKey() != "" {
		spend, _, aerr := fetchAnthropicSpend(auth.AnthropicAdminKey(), windowStart)
		if aerr != nil {
			cb.AnthropicError = aerr.Error()
		} else {
			cb.Anthropic = spend
		}
	} else {
		cb.AnthropicError = "anthropic admin key not configured"
	}

	costsBreakdownCacheValue = cb
	costsBreakdownCacheExpires = time.Now().Add(util.ExternalAPICacheTTL)
	util.SendJSON(w, cb)
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
		projectID = gcp.Meta("project/project-id")
	}
	dataset := os.Getenv("BILLING_EXPORT_DATASET")
	if dataset == "" {
		dataset = "billing_export"
	}
	if projectID == "unknown" || projectID == "" {
		return nil, nil, 0, fmt.Errorf("missing project id")
	}

	token, terr := gcp.MetadataToken()
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
	project := gcp.Meta("project/project-id")
	zoneRaw := gcp.Meta("instance/zone")
	zone := zoneRaw
	if i := strings.LastIndex(zoneRaw, "/"); i >= 0 {
		zone = zoneRaw[i+1:]
	}
	name := gcp.Meta("instance/name")

	if project == "unknown" || name == "unknown" {
		util.SendJSON(w, map[string]interface{}{"error": "metadata missing"})
		return
	}

	token, err := gcp.MetadataToken()
	if err != nil {
		util.SendJSON(w, map[string]interface{}{"error": fmt.Sprintf("metadata token: %v", err)})
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
		util.SendJSON(w, map[string]interface{}{"error": fmt.Sprintf("contact compute: %v", err)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		util.SendJSON(w, map[string]interface{}{"error": fmt.Sprintf("compute %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))})
		return
	}

	log.Printf("vm stop: requested by dashboard, instance %s/%s/%s", project, zone, name)
	util.SendJSON(w, map[string]interface{}{"success": true, "message": "stop requested — vm will be down in ~30s"})
}

// fetchInstanceCreationTimestamp hits the Compute Engine REST API for
// the current instance to get its creation timestamp (not exposed in
// the metadata server). Uses the metadata-server access token.
func fetchInstanceCreationTimestamp() (string, error) {
	project := gcp.Meta("project/project-id")
	zoneRaw := gcp.Meta("instance/zone")
	zone := zoneRaw
	if i := strings.LastIndex(zoneRaw, "/"); i >= 0 {
		zone = zoneRaw[i+1:]
	}
	name := gcp.Meta("instance/name")
	if project == "unknown" || name == "unknown" {
		return "", fmt.Errorf("metadata missing")
	}

	token, err := gcp.MetadataToken()
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

	script := filepath.Join(attlasDir, "services", svc.Script)
	if _, err := os.Stat(script); err != nil {
		util.SendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Script not found: %s", script)})
		return
	}

	// install-*.sh scripts require root (they write to /etc/systemd/system/),
	// so we run them via sudo. Authorized by /etc/sudoers.d/alive-svc-services.
	cmd := exec.Command("sudo", "-n", "bash", script)
	cmd.Dir = filepath.Join(attlasDir, "services")
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

	script := filepath.Join(attlasDir, "services", fmt.Sprintf("uninstall-%s.sh", svc.ID))
	if _, err := os.Stat(script); err != nil {
		util.SendJSON(w, map[string]interface{}{"error": fmt.Sprintf("Uninstall script not found: %s", script)})
		return
	}

	// uninstall-*.sh scripts require root (they touch /etc/systemd/system/
	// and /etc/caddy/conf.d/), so we run them via sudo.
	cmd := exec.Command("sudo", "-n", "bash", script)
	cmd.Dir = filepath.Join(attlasDir, "services")
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
	mux.HandleFunc("GET /api/services/openclaw", handleOpenclawDetail)
	mux.HandleFunc("GET /api/services/terminal", handleTerminalDetail)
	mux.HandleFunc("POST /api/services/terminal/kill", handleTerminalKill)
	mux.HandleFunc("GET /api/services/infrastructure", handleInfrastructureDetail)
	mux.HandleFunc("GET /api/services/splitsies", handleSplitsiesDetail)
	mux.HandleFunc("POST /api/services/splitsies/users", handleSplitsiesAddUser)
	mux.HandleFunc("PATCH /api/services/splitsies/users/{id}", handleSplitsiesPatchUser)
	mux.HandleFunc("DELETE /api/services/splitsies/users/{id}", handleSplitsiesRemoveUser)
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
