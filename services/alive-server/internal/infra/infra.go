// Package infra owns the infrastructure detail page + VM-stop endpoint.
// Reads GCE metadata, queries Cloud Monitoring for per-VM daily uptime,
// and talks to the Compute Engine REST API to stop the instance.
package infra

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"attlas-server/internal/gcp"
	"attlas-server/internal/util"
)

// VMUptimeSeries is one VM's daily-uptime bucket set, aligned to
// Detail.UptimeDays.
type VMUptimeSeries struct {
	Name         string  `json:"name"`
	TotalSeconds int64   `json:"total_seconds"`
	Daily        []int64 `json:"daily"`
}

// Detail is what GET /api/services/infrastructure returns.
type Detail struct {
	Name              string           `json:"name"`
	Zone              string           `json:"zone"`
	Region            string           `json:"region"`
	ExternalIP        string           `json:"external_ip"`
	InternalIP        string           `json:"internal_ip"`
	Domain            string           `json:"domain"`
	MachineType       string           `json:"machine_type"`
	CreationTimestamp string           `json:"creation_timestamp"`
	OSBootTime        string           `json:"os_boot_time"`
	UptimeNow         string           `json:"uptime_now"`
	UptimeDays        []string         `json:"uptime_days"`
	UptimeSeries      []VMUptimeSeries `json:"uptime_series"`
	TotalSecondsMonth int64            `json:"total_seconds_month"`
	EventsError       string           `json:"events_error,omitempty"`
}

var (
	cacheMu      sync.Mutex
	cacheValue   Detail
	cacheExpires time.Time
)

// fetchInstanceUptime queries Cloud Monitoring for the per-VM daily
// uptime metric. See the long-form comment in the previous monolith
// for why this replaced the Cloud Logging audit-event replay.
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

// osBootTime reads /proc/stat btime for the kernel-boot epoch.
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

// fetchInstanceCreationTimestamp hits the Compute Engine REST API for
// the current instance to get its creation timestamp.
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

// HandleDetail is GET /api/services/infrastructure.
func HandleDetail(w http.ResponseWriter, r *http.Request) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if time.Now().Before(cacheExpires) {
		util.SendJSON(w, cacheValue)
		return
	}

	now := time.Now().UTC()
	detail := Detail{
		Name:       gcp.Meta("instance/name"),
		ExternalIP: gcp.Meta("instance/network-interfaces/0/access-configs/0/external-ip"),
		InternalIP: gcp.Meta("instance/network-interfaces/0/ip"),
		Domain:     "attlas.uk",
	}

	zoneRaw := gcp.Meta("instance/zone")
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

	if bt := osBootTime(); !bt.IsZero() {
		detail.OSBootTime = bt.Format(time.RFC3339)
		detail.UptimeNow = util.HumanDuration(now.Sub(bt))
	}

	if ts, err := fetchInstanceCreationTimestamp(); err == nil {
		detail.CreationTimestamp = ts
	}

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)

	uDays, uSeries, err := fetchInstanceUptime(monthStart, tomorrow)
	if err != nil {
		detail.EventsError = err.Error()
	}
	detail.UptimeDays = uDays

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

	cacheValue = detail
	cacheExpires = time.Now().Add(util.ExternalAPICacheTTL)
	util.SendJSON(w, detail)
}

// HandleStopVM is POST /api/vm/stop. Sends an authenticated POST to
// the Compute Engine REST API. The API returns immediately with an
// Operation object; actual shutdown happens over the next ~30s, which
// is plenty of time for this HTTP response to complete before
// alive-server gets killed.
func HandleStopVM(w http.ResponseWriter, r *http.Request) {
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
