// Package diary reads diary markdown entries from the Hugo content
// directory and extracts total effort hours from the summary field.
package diary

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"attlas-server/internal/util"
)

var attlasDir string

// SetAttlasDir tells this package where the iapetus/attlas checkout lives.
func SetAttlasDir(dir string) { attlasDir = dir }

// effortRe matches "~Xh" or "~X.Xh" in the summary line.
var effortRe = regexp.MustCompile(`~(\d+(?:\.\d+)?)h\b`)

// frontmatterRe splits YAML frontmatter from body.
var frontmatterRe = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n`)

type effortResult struct {
	TotalHours float64 `json:"total_hours"`
	Sessions   int     `json:"sessions"`
}

var (
	cache      *effortResult
	cacheTime  time.Time
	cacheMu    sync.Mutex
	cacheTTL   = 5 * time.Minute
)

func getEffort() effortResult {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if cache != nil && time.Since(cacheTime) < cacheTTL {
		return *cache
	}

	contentDir := filepath.Join(attlasDir, "services", "diary", "content")
	entries, err := os.ReadDir(contentDir)
	if err != nil {
		if cache != nil {
			return *cache
		}
		return effortResult{}
	}

	var total float64
	var sessions int

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "_") {
			continue
		}
		// Only match date-named entries (YYYY-MM-DD.md)
		if len(name) != 13 { // "2026-04-08.md"
			continue
		}

		data, err := os.ReadFile(filepath.Join(contentDir, name))
		if err != nil {
			continue
		}

		fm := frontmatterRe.FindSubmatch(data)
		if fm == nil {
			continue
		}

		// Extract summary line from frontmatter
		for _, line := range strings.Split(string(fm[1]), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "summary:") {
				m := effortRe.FindStringSubmatch(line)
				if m != nil {
					h, _ := strconv.ParseFloat(m[1], 64)
					total += h
					sessions++
				}
				break
			}
		}
	}

	result := effortResult{TotalHours: total, Sessions: sessions}
	cache = &result
	cacheTime = time.Now()
	return result
}

// HandleEffort serves GET /api/diary/effort.
func HandleEffort(w http.ResponseWriter, r *http.Request) {
	util.SendJSON(w, getEffort())
}
