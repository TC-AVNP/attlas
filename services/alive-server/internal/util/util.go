// Package util holds small, dependency-free helpers used across the
// alive-server packages: shell command runners, a duration formatter,
// a JSON response helper, and the shared cache TTL constant.
package util

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ExternalAPICacheTTL is the shared cache window for every handler
// that fans out to a rate-limited upstream (Anthropic cost_report,
// BigQuery billing export, whois, Cloud Monitoring uptime).
// 15 minutes is tight enough that the dashboard shows fresh-ish data
// but loose enough that the dashboard surviving a cost-report spike
// is not a coin toss.
const ExternalAPICacheTTL = 15 * time.Minute

// RunCmd runs a command with no deadline. Returns trimmed combined
// output and a bool that is true iff the exit code was 0. Intended
// for commands that are near-instant (systemctl is-active, which
// command, git rev-parse) where a missing timeout would never bite.
func RunCmd(name string, args ...string) (string, bool) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

// RunCmdCtx runs a command with a hard timeout. Used for subprocesses
// that might hang (openclaw gateway probes, systemctl on a stuck unit).
func RunCmdCtx(timeout time.Duration, name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

// HumanDuration formats d as "Nd Nh", "Nh Nm", or "Nm" — whichever
// is the coarsest unit that still conveys the value. Negative durations
// render as an em-dash.
func HumanDuration(d time.Duration) string {
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

// SendJSON writes data as JSON with Content-Type set. Swallows encoder
// errors because by the time one happens the headers are on the wire
// and there's nothing useful to do about it.
func SendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}
