// Package status holds every helper that feeds /api/status — VM
// metadata, live system load (CPU/memory/loadavg from /proc), Claude
// login state, dotfiles sync health, and domain-expiry scraping.
//
// Each exported function here is called on every /api/status tick
// from the dashboard's 10s poll loop, so they either read from
// in-process /proc (zero latency) or cache upstream lookups
// (whois is cached 6h, since attlas.uk's registry isn't going to
// move underneath us).
package status

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"attlas-server/internal/gcp"
	"attlas-server/internal/util"
)

// --- Attlas dir injection ---

var attlasDir string

// SetAttlasDir tells this package where the iapetus/attlas checkout
// lives. Used to resolve the sibling dotfiels repo path. Must be called
// once at startup.
func SetAttlasDir(dir string) { attlasDir = dir }

// --- VM info ---

func VMInfo() map[string]string {
	ip := gcp.Meta("instance/network-interfaces/0/access-configs/0/external-ip")
	zoneRaw := gcp.Meta("instance/zone")
	zone := zoneRaw
	if i := strings.LastIndex(zoneRaw, "/"); i >= 0 {
		zone = zoneRaw[i+1:]
	}
	name := gcp.Meta("instance/name")
	mt := gcp.Meta("instance/machine-type")
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

// --- System load ---

type SystemLoad struct {
	CPUCores      int     `json:"cpu_cores"`
	CPUPercent    int     `json:"cpu_percent"`
	LoadAvg1      float64 `json:"load_avg_1"`
	LoadAvg5      float64 `json:"load_avg_5"`
	LoadAvg15     float64 `json:"load_avg_15"`
	MemTotalBytes uint64  `json:"mem_total_bytes"`
	MemUsedBytes  uint64  `json:"mem_used_bytes"`
	MemPercent    int     `json:"mem_percent"`
}

type cpuStatSample struct {
	total uint64
	idle  uint64
	at    time.Time
}

var (
	cpuSampleMu sync.Mutex
	cpuSample   cpuStatSample
)

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
		if i == 4 {
			idle = v
		}
	}
	return total, idle, nil
}

func cpuUtilization() int {
	cpuSampleMu.Lock()
	defer cpuSampleMu.Unlock()

	total, idle, err := readCPUStat()
	if err != nil {
		return 0
	}

	prev := cpuSample
	cpuSample = cpuStatSample{total: total, idle: idle, at: time.Now()}

	if prev.at.IsZero() || total <= prev.total {
		return 0
	}
	dTotal := total - prev.total
	dIdle := idle - prev.idle
	if dTotal == 0 {
		return 0
	}
	busy := dTotal - dIdle
	pct := int(100 * busy / dTotal)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

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
		v *= 1024
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		}
	}
	return total, available
}

func GetSystemLoad() SystemLoad {
	sl := SystemLoad{CPUCores: runtime.NumCPU()}
	sl.CPUPercent = cpuUtilization()

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

// IsClaudeInstalled reports whether the `claude` CLI is on $PATH.
func IsClaudeInstalled() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// IsClaudeLoggedIn asks the claude CLI itself, running as the
// interactive login user via sudo, instead of trying to read the
// user's .claude.json (600 perms, different user). Requires
// /etc/sudoers.d/alive-svc-claude.
func IsClaudeLoggedIn() bool {
	cmd := exec.Command("sudo", "-n", "-u", "agnostic-user", "-H", "claude", "auth", "status")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), `"loggedIn": true`)
}

// --- Dotfiles ---

type DotfilesStatus struct {
	LastSync        string `json:"last_sync"`
	LastExitStatus  int    `json:"last_exit_status"`
	HeadCommit      string `json:"head_commit"`
	HeadCommittedAt string `json:"head_committed_at"`
	RemoteCommit    string `json:"remote_commit"`
	Behind          int    `json:"behind"`
	Status          string `json:"status"`
}

func dotfilesDir() string {
	return filepath.Join(filepath.Dir(attlasDir), "dotfiels")
}

func GetDotfilesStatus() DotfilesStatus {
	s := DotfilesStatus{Status: "unknown"}
	dir := dotfilesDir()
	if _, err := os.Stat(dir); err != nil {
		return s
	}

	if out, ok := util.RunCmdCtx(2*time.Second, "systemctl", "show", "dotfiles-sync.service",
		"-p", "ExecMainExitTimestamp", "-p", "ExecMainStatus"); ok {
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "ExecMainStatus=") {
				if v, err := strconv.Atoi(strings.TrimPrefix(line, "ExecMainStatus=")); err == nil {
					s.LastExitStatus = v
				}
			} else if strings.HasPrefix(line, "ExecMainExitTimestamp=") {
				raw := strings.TrimPrefix(line, "ExecMainExitTimestamp=")
				if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", raw); err == nil {
					s.LastSync = t.UTC().Format(time.RFC3339)
				}
			}
		}
	}

	branch, _ := util.RunCmdCtx(2*time.Second, "git", "-C", dir, "symbolic-ref", "--short", "HEAD")
	if branch == "" {
		branch = "master"
	}
	if head, ok := util.RunCmdCtx(2*time.Second, "git", "-C", dir, "rev-parse", "--short", "HEAD"); ok {
		s.HeadCommit = head
	}
	if committedAt, ok := util.RunCmdCtx(2*time.Second, "git", "-C", dir, "log", "-1", "--format=%cI", "HEAD"); ok {
		s.HeadCommittedAt = committedAt
	}
	if remote, ok := util.RunCmdCtx(2*time.Second, "git", "-C", dir, "rev-parse", "--short", "origin/"+branch); ok {
		s.RemoteCommit = remote
	}
	if behindOut, ok := util.RunCmdCtx(2*time.Second, "git", "-C", dir, "rev-list", "--count", "HEAD..origin/"+branch); ok {
		if n, err := strconv.Atoi(behindOut); err == nil {
			s.Behind = n
		}
	}

	if s.LastExitStatus != 0 && s.LastSync != "" {
		s.Status = "error"
	} else if s.Behind > 0 {
		s.Status = "behind"
	} else if s.HeadCommit != "" {
		s.Status = "up-to-date"
	}
	return s
}

// HandleDotfilesSync is the POST /api/dotfiles/sync endpoint. Kicks
// the dotfiles-sync.service systemd unit via a no-prompt sudo. The
// sudoers drop-in at /etc/sudoers.d/alive-svc-dotfiles-sync authorises
// this exact invocation.
func HandleDotfilesSync(w http.ResponseWriter, r *http.Request) {
	cmd := exec.Command("sudo", "-n", "systemctl", "start", "dotfiles-sync.service")
	out, err := cmd.CombinedOutput()
	if err != nil {
		util.SendJSON(w, map[string]interface{}{"error": strings.TrimSpace(string(out))})
		return
	}
	util.SendJSON(w, map[string]interface{}{"success": true})
}

// --- Domain expiry ---

type DomainExpiry struct {
	ExpiresAt     string `json:"expires_at"`
	DaysRemaining int    `json:"days_remaining"`
	Severity      string `json:"severity"`
}

var (
	domainCacheMu      sync.Mutex
	domainCacheValue   DomainExpiry
	domainCacheExpires time.Time
	whoisExpiryRE      = regexp.MustCompile(`(?i)^\s*Expiry date:\s*(.+)$`)
)

func GetDomainExpiry() DomainExpiry {
	domainCacheMu.Lock()
	defer domainCacheMu.Unlock()
	if time.Now().Before(domainCacheExpires) {
		v := domainCacheValue
		if t, err := time.Parse("2006-01-02", v.ExpiresAt); err == nil {
			v.DaysRemaining = int(time.Until(t).Hours() / 24)
			v.Severity = domainSeverity(v.DaysRemaining)
		}
		return v
	}

	result := DomainExpiry{Severity: "unknown"}

	if _, err := exec.LookPath("whois"); err != nil {
		domainCacheValue = result
		domainCacheExpires = time.Now().Add(10 * time.Minute)
		return result
	}

	out, ok := util.RunCmdCtx(10*time.Second, "whois", "attlas.uk")
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
