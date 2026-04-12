// Package gcp holds low-level helpers for talking to the GCP metadata
// server: a freeform field reader and a default-service-account OAuth
// token fetcher. Used by every other package that needs to call a GCP
// REST API without shelling out to gcloud.
package gcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Meta reads a single metadata-server field and returns the trimmed
// string body, or "unknown" on any error. The 3s timeout is deliberate:
// metadata reads should never block the dashboard, and anything past
// a few hundred ms means the metadata server is misbehaving.
func Meta(path string) string {
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

// MetadataToken fetches a short-lived OAuth access token for the VM's
// default service account via the metadata server. Used to call GCP
// REST APIs (Cloud Logging, Cloud Monitoring, BigQuery) without
// shelling out to gcloud.
func MetadataToken() (string, error) {
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
