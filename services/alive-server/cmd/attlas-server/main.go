// Attlas dashboard — thin entrypoint. The real work lives in the
// internal/ packages; this file only loads config, wires the mux,
// and calls ListenAndServe.
package main

import (
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"attlas-server/internal/auth"
	"attlas-server/internal/config"
	"attlas-server/internal/costs"
	"attlas-server/internal/diary"
	"attlas-server/internal/infra"
	"attlas-server/internal/openclaw"
	"attlas-server/internal/services"
	"attlas-server/internal/status"
	"attlas-server/internal/util"
)

const port = 3000

var (
	distDir   string
	attlasDir string
)

// handleStatus is the main /api/status endpoint. Aggregates every
// piece of runtime info the dashboard shows on the home page.
func handleStatus(w http.ResponseWriter, r *http.Request) {
	util.SendJSON(w, map[string]interface{}{
		"vm": status.VMInfo(),
		"user": map[string]interface{}{
			"email":          auth.GetSessionEmail(r),
			"allowed_emails": auth.AllowedEmails(),
		},
		"claude": map[string]bool{
			"installed":     status.IsClaudeInstalled(),
			"authenticated": status.IsClaudeLoggedIn(),
		},
		"services":      services.GetStatus(),
		"dotfiles":      status.GetDotfilesStatus(),
		"domain_expiry": status.GetDomainExpiry(),
		"system_load":   status.GetSystemLoad(),
	})
}

// serveStatic serves the Vite-built SPA, falling back to index.html
// for paths that don't match a real file (so React Router deep links
// work).
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

func main() {
	auth.Init(config.LoadOrCreateSecret(), config.Load())

	// Initial load of the public-path registry and a SIGHUP-triggered
	// reloader so services can signal alive-server after installing
	// or uninstalling without dropping existing sessions.
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

	if _, err := os.Stat(distDir); err != nil {
		wd, _ := os.Getwd()
		distDir = filepath.Join(wd, "frontend", "dist")
	}

	// Inject paths into packages that need them.
	status.SetAttlasDir(attlasDir)
	services.SetAttlasDir(attlasDir)
	services.SetDistDir(distDir)
	diary.SetAttlasDir(attlasDir)

	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("/api/auth/verify", auth.HandleAuthVerify)
	mux.HandleFunc("GET /oauth2/login", auth.HandleOAuth2Login)
	mux.HandleFunc("GET /oauth2/callback", auth.HandleOAuth2Callback)
	mux.HandleFunc("/logout", auth.HandleLogout)

	// API
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("POST /api/claude-login", services.HandleClaudeLogin)
	mux.HandleFunc("POST /api/claude-login/code", services.HandleClaudeCode)
	mux.HandleFunc("POST /api/install-service", services.HandleInstall)
	mux.HandleFunc("POST /api/uninstall-service", services.HandleUninstall)
	mux.HandleFunc("POST /api/dotfiles/sync", status.HandleDotfilesSync)
	mux.HandleFunc("GET /api/services/openclaw", openclaw.HandleDetail)
	mux.HandleFunc("GET /api/services/terminal", services.HandleTerminalDetail)
	mux.HandleFunc("POST /api/services/terminal/kill", services.HandleTerminalKill)
	mux.HandleFunc("POST /api/services/terminal/rename", services.HandleTerminalRename)
	mux.HandleFunc("POST /api/services/terminal/describe", services.HandleTerminalDescribe)
	mux.HandleFunc("GET /api/services/infrastructure", infra.HandleDetail)
	mux.HandleFunc("GET /api/services/splitsies", handleSplitsiesDetail)
	mux.HandleFunc("POST /api/services/splitsies/users", handleSplitsiesAddUser)
	mux.HandleFunc("PATCH /api/services/splitsies/users/{id}", handleSplitsiesPatchUser)
	mux.HandleFunc("DELETE /api/services/splitsies/users/{id}", handleSplitsiesRemoveUser)
	mux.HandleFunc("GET /api/services/david-s-checklist", handleDavidDetail)
	mux.HandleFunc("POST /api/services/david-s-checklist/users", handleDavidAddUser)
	mux.HandleFunc("PATCH /api/services/david-s-checklist/users/{email}", handleDavidPatchUser)
	mux.HandleFunc("DELETE /api/services/david-s-checklist/users/{email}", handleDavidRemoveUser)
	mux.HandleFunc("GET /api/cloud-spend", costs.HandleCloudSpend)
	mux.HandleFunc("GET /api/costs/breakdown", costs.HandleBreakdown)
	mux.HandleFunc("POST /api/vm/stop", infra.HandleStopVM)
	mux.HandleFunc("GET /api/diary/effort", diary.HandleEffort)

	// Diary (Hugo static site)
	diaryDir := filepath.Join(attlasDir, "services", "diary", "public")
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
