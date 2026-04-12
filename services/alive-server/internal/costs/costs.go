// Package costs owns the cloud-spend summary endpoint, the costs
// breakdown detail endpoint, and the upstream fetchers (Anthropic
// cost_report + GCP BigQuery billing export). Everything here caches
// for 15 minutes via util.ExternalAPICacheTTL because the upstreams
// are rate-limited.
package costs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"attlas-server/internal/auth"
	"attlas-server/internal/gcp"
	"attlas-server/internal/util"
)

// DayCost is a single day's USD figure, used for both Anthropic and
// GCP per-day series.
type DayCost struct {
	Date string  `json:"date"`
	USD  float64 `json:"usd"`
}

// --- /api/cloud-spend ---------------------------------------------------

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

// HandleCloudSpend is GET /api/cloud-spend.
func HandleCloudSpend(w http.ResponseWriter, r *http.Request) {
	cloudSpendCacheMu.Lock()
	defer cloudSpendCacheMu.Unlock()

	if time.Now().Before(cloudSpendCacheExpires) {
		util.SendJSON(w, cloudSpendCacheValue)
		return
	}

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	cs := CloudSpend{GCPSource: "bigquery billing export"}

	if v, err := fetchGCPSpendBigQuery(monthStart); err != nil {
		cs.GCPError = err.Error()
	} else {
		cs.GCPMTD = v
	}

	if auth.AnthropicAdminKey() != "" {
		spend, _, err := FetchAnthropicSpend(auth.AnthropicAdminKey(), monthStart)
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

// --- /api/costs/breakdown -----------------------------------------------

const windowDays = 30

type CategorySeries struct {
	Total30d float64   `json:"total_30d_usd"`
	AvgDaily float64   `json:"avg_daily_usd"`
	Daily    []DayCost `json:"daily"`
}

type Breakdown struct {
	VMCompute      CategorySeries `json:"vm_compute"`
	NetworkEgress  CategorySeries `json:"network_egress"`
	OtherGCP       float64        `json:"other_gcp_30d_usd"`
	Anthropic      float64        `json:"anthropic_30d_usd"`
	WindowDays     int            `json:"window_days"`
	WindowStart    string         `json:"window_start"`
	WindowEnd      string         `json:"window_end"`
	GCPError       string         `json:"gcp_error,omitempty"`
	AnthropicError string         `json:"anthropic_error,omitempty"`
}

var (
	breakdownCacheMu      sync.Mutex
	breakdownCacheValue   Breakdown
	breakdownCacheExpires time.Time
)

func HandleBreakdown(w http.ResponseWriter, r *http.Request) {
	breakdownCacheMu.Lock()
	defer breakdownCacheMu.Unlock()

	if time.Now().Before(breakdownCacheExpires) {
		util.SendJSON(w, breakdownCacheValue)
		return
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowStart := today.AddDate(0, 0, -windowDays)
	windowEnd := today

	cb := Breakdown{
		WindowDays:  windowDays,
		WindowStart: windowStart.Format("2006-01-02"),
		WindowEnd:   windowEnd.Format("2006-01-02"),
	}

	vm, egress, total, err := fetchGCPCategorizedCosts(windowStart, windowEnd)
	if err != nil {
		cb.GCPError = err.Error()
	}
	cb.VMCompute = buildSeries(vm, windowStart, windowEnd)
	cb.NetworkEgress = buildSeries(egress, windowStart, windowEnd)
	if err == nil {
		cb.OtherGCP = total - cb.VMCompute.Total30d - cb.NetworkEgress.Total30d
		if cb.OtherGCP < 0 {
			cb.OtherGCP = 0
		}
	}

	if auth.AnthropicAdminKey() != "" {
		spend, _, aerr := FetchAnthropicSpend(auth.AnthropicAdminKey(), windowStart)
		if aerr != nil {
			cb.AnthropicError = aerr.Error()
		} else {
			cb.Anthropic = spend
		}
	} else {
		cb.AnthropicError = "anthropic admin key not configured"
	}

	breakdownCacheValue = cb
	breakdownCacheExpires = time.Now().Add(util.ExternalAPICacheTTL)
	util.SendJSON(w, cb)
}

// --- BigQuery fetchers --------------------------------------------------

// fetchGCPSpendBigQuery returns the current project's month-to-date
// cost in USD from the Cloud Billing BigQuery export.
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
		if strings.Contains(rawStr, "does not match any table") {
			return 0, fmt.Errorf("waiting for first billing export (up to 24h after enabling in console)")
		}
		return 0, fmt.Errorf("bigquery %d: %s", resp.StatusCode, rawStr)
	}

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
		return 0, nil
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

// fetchGCPCategorizedCosts runs one BigQuery SELECT that classifies
// each row into vm_compute / network_egress / other. Returns per-day
// sums for the two headline categories plus the global total.
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

// buildSeries zero-fills a map of date→cost into a chronological
// array bounded by [start, end). Also computes total and average so
// the frontend can show headline numbers without recomputing.
func buildSeries(byDate map[string]float64, start, end time.Time) CategorySeries {
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

	return CategorySeries{
		Total30d: total,
		AvgDaily: avg,
		Daily:    daily,
	}
}

// --- Anthropic ----------------------------------------------------------

// FetchAnthropicSpend calls the Anthropic cost_report admin API and
// returns (total USD in window, last-7-days daily breakdown, error).
// Exported so the openclaw detail handler can call it too.
//
// Two surprises discovered while reverse-engineering this against
// the platform.claude.com cost page:
//
//  1. The response paginates at 7 buckets per page. We walk
//     `next_page` until `has_more` is false (capped at 10 pages /
//     70 days so a runaway account can't stall the dashboard).
//  2. The `amount` field is a string in CENTS, not dollars, despite
//     being reported with `currency: "USD"`. Divide by 100.
func FetchAnthropicSpend(adminKey string, startWindow time.Time) (float64, []DayCost, error) {
	now := time.Now().UTC()
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

	daily := make([]DayCost, 0, 7)
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		daily = append(daily, DayCost{Date: d, USD: byDate[d]})
	}

	return windowTotal, daily, nil
}
