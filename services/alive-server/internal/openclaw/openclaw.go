// Package openclaw owns the /api/services/openclaw detail endpoint:
// runtime status from systemd, task / session counts from the openclaw
// CLI, and a last-30-day Anthropic spend summary to match what the
// platform.claude.com cost page shows.
package openclaw

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"attlas-server/internal/auth"
	"attlas-server/internal/costs"
	"attlas-server/internal/util"
)

type Detail struct {
	Running         bool            `json:"running"`
	Uptime          string          `json:"uptime"`
	ActiveTasks     int             `json:"active_tasks"`
	Sessions        int             `json:"sessions"`
	TasksRun        int             `json:"tasks_run"`
	SpendLast30Days float64         `json:"spend_last_30_days"`
	SpendDaily      []costs.DayCost `json:"spend_daily"`
	BillingError    string          `json:"billing_error,omitempty"`
}

var (
	cacheMu      sync.Mutex
	cacheValue   Detail
	cacheExpires time.Time
)

// statusJSON is the shape we consume from `openclaw status --all --json`.
type statusJSON struct {
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

// HandleDetail is GET /api/services/openclaw.
func HandleDetail(w http.ResponseWriter, r *http.Request) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if time.Now().Before(cacheExpires) {
		util.SendJSON(w, cacheValue)
		return
	}

	detail := Detail{}

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
		var s statusJSON
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
		spend30, daily, err := costs.FetchAnthropicSpend(auth.AnthropicAdminKey(), time.Now().UTC().AddDate(0, 0, -30))
		if err != nil {
			detail.BillingError = err.Error()
		} else {
			detail.SpendLast30Days = spend30
			detail.SpendDaily = daily
		}
	} else {
		detail.BillingError = "anthropic admin key not configured"
	}

	cacheValue = detail
	cacheExpires = time.Now().Add(util.ExternalAPICacheTTL)
	util.SendJSON(w, detail)
}
