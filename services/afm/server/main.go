// afm — Attlas File Manager.
//
// Web-based file manager for the homelab NAS. Browse, upload, preview,
// and download files from any device. Auth via alive-server forward_auth.
//
// Configuration via environment variables:
//
//	AFM_PORT    TCP port (default 7695)
//	AFM_DB      SQLite path (default /var/lib/afm/afm.db)
//	AFM_FILES   File storage root (default /var/lib/afm/files)
package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed templates/*.html
var templatesFS embed.FS

type FileEntry struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	IsDir      bool   `json:"is_dir"`
	Uploader   string `json:"uploader"`
	UploadedAt string `json:"uploaded_at"`
	ModTime    string `json:"mod_time"`
}

type App struct {
	db       *sql.DB
	tmpl     *template.Template
	filesDir string
}

func main() {
	port := envOr("AFM_PORT", "7695")
	dbPath := envOr("AFM_DB", "/var/lib/afm/afm.db")
	filesDir := envOr("AFM_FILES", "/home/agnostic-user/afm")

	// Ensure files directory exists.
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		log.Fatalf("create files dir: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}

	app := &App{db: db, tmpl: tmpl, filesDir: filesDir}

	mux := http.NewServeMux()
	mux.HandleFunc("/afm/", app.handleIndex)
	mux.HandleFunc("/afm/api/list", app.handleList)
	mux.HandleFunc("/afm/api/upload", app.handleUpload)
	mux.HandleFunc("/afm/api/preview", app.handlePreview)
	mux.HandleFunc("/afm/api/download", app.handleDownload)
	mux.HandleFunc("/afm/api/mkdir", app.handleMkdir)
	mux.HandleFunc("/afm/api/delete", app.handleDelete)
	mux.HandleFunc("/afm/api/move", app.handleMove)

	srv := &http.Server{
		Addr:    "127.0.0.1:" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("afm listening on 127.0.0.1:%s (files at %s)", port, filesDir)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func (app *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/afm/" && r.URL.Path != "/afm" {
		http.Redirect(w, r, "/afm/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	app.tmpl.ExecuteTemplate(w, "index.html", map[string]string{
		"FilesRoot": app.filesDir,
	})
}

func (app *App) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relPath := cleanPath(r.URL.Query().Get("path"))
	absPath := filepath.Join(app.filesDir, relPath)

	// Prevent directory traversal.
	if !strings.HasPrefix(absPath, app.filesDir) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			json.NewEncoder(w).Encode(map[string]interface{}{"entries": []FileEntry{}, "path": relPath})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	files := make([]FileEntry, 0, len(entries))
	var totalSize int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		var size int64
		if e.IsDir() {
			size = dirSize(filepath.Join(absPath, e.Name()))
		} else {
			size = info.Size()
		}
		totalSize += size
		fe := FileEntry{
			Name:    e.Name(),
			Size:    size,
			IsDir:   e.IsDir(),
			ModTime: info.ModTime().Format(time.RFC3339),
		}
		// Look up uploader from DB.
		var uploader, uploadedAt sql.NullString
		app.db.QueryRow("SELECT uploader, uploaded_at FROM files WHERE path = ? AND name = ?",
			relPath, e.Name()).Scan(&uploader, &uploadedAt)
		if uploader.Valid {
			fe.Uploader = uploader.String
		}
		if uploadedAt.Valid {
			fe.UploadedAt = uploadedAt.String
		}
		files = append(files, fe)
	}

	// Sort: directories first, then alphabetical.
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries":    files,
		"path":       relPath,
		"total_size": totalSize,
	})
}

func (app *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 512 MB max upload.
	r.ParseMultipartForm(512 << 20)

	relPath := cleanPath(r.FormValue("path"))
	absDir := filepath.Join(app.filesDir, relPath)
	if !strings.HasPrefix(absDir, app.filesDir) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(absDir, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	uploader := r.Header.Get("X-Auth-Email")
	if uploader == "" {
		uploader = "unknown"
	}

	files := r.MultipartForm.File["files"]
	uploaded := 0
	for _, fh := range files {
		src, err := fh.Open()
		if err != nil {
			continue
		}

		dstPath := filepath.Join(absDir, filepath.Base(fh.Filename))
		// Prevent traversal via filename.
		if !strings.HasPrefix(dstPath, absDir) {
			src.Close()
			continue
		}

		dst, err := os.Create(dstPath)
		if err != nil {
			src.Close()
			continue
		}

		written, _ := io.Copy(dst, src)
		dst.Close()
		src.Close()

		// Record metadata.
		app.db.Exec(`INSERT OR REPLACE INTO files (path, name, size, is_dir, uploader, uploaded_at)
			VALUES (?, ?, ?, 0, ?, datetime('now'))`,
			relPath, filepath.Base(fh.Filename), written, uploader)
		uploaded++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"uploaded": uploaded,
	})
}

func (app *App) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relPath := cleanPath(r.URL.Query().Get("path"))
	absPath := filepath.Join(app.filesDir, relPath)
	if !strings.HasPrefix(absPath, app.filesDir) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot preview directory", http.StatusBadRequest)
		return
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// For text-based files, return the content directly.
	if isTextMime(mimeType) || isTextExt(ext) {
		content, err := os.ReadFile(absPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type":    "text",
			"mime":    mimeType,
			"ext":     ext,
			"content": string(content),
			"size":    info.Size(),
		})
		return
	}

	// For images/audio/video, return a URL to stream directly.
	if isMediaMime(mimeType) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "media",
			"mime": mimeType,
			"ext":  ext,
			"url":  "/afm/api/download?path=" + relPath,
			"size": info.Size(),
		})
		return
	}

	// Binary/unknown — just offer download.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "binary",
		"mime": mimeType,
		"ext":  ext,
		"url":  "/afm/api/download?path=" + relPath,
		"size": info.Size(),
	})
}

func (app *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relPath := cleanPath(r.URL.Query().Get("path"))
	absPath := filepath.Join(app.filesDir, relPath)
	if !strings.HasPrefix(absPath, app.filesDir) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot download directory", http.StatusBadRequest)
		return
	}

	// Determine if this is a streaming request (for media previews) or a download.
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(absPath)))
	}

	http.ServeFile(w, r, absPath)
}

func (app *App) handleMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	relPath := cleanPath(req.Path)
	name := filepath.Base(req.Name)
	if name == "" || name == "." || name == ".." {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}

	absDir := filepath.Join(app.filesDir, relPath, name)
	if !strings.HasPrefix(absDir, app.filesDir) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(absDir, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	uploader := r.Header.Get("X-Auth-Email")
	app.db.Exec(`INSERT OR REPLACE INTO files (path, name, size, is_dir, uploader, uploaded_at)
		VALUES (?, ?, 0, 1, ?, datetime('now'))`, relPath, name, uploader)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (app *App) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	deleted := 0
	for _, p := range req.Paths {
		relPath := cleanPath(p)
		absPath := filepath.Join(app.filesDir, relPath)
		if !strings.HasPrefix(absPath, app.filesDir) || absPath == app.filesDir {
			continue
		}
		if err := os.RemoveAll(absPath); err != nil {
			continue
		}
		dir := filepath.Dir(relPath)
		name := filepath.Base(relPath)
		app.db.Exec("DELETE FROM files WHERE path = ? AND name = ?", dir, name)
		deleted++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": deleted})
}

func (app *App) handleMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Paths       []string `json:"paths"`
		Destination string   `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	destRel := cleanPath(req.Destination)
	destAbs := filepath.Join(app.filesDir, destRel)
	if !strings.HasPrefix(destAbs, app.filesDir) {
		http.Error(w, "invalid destination", http.StatusBadRequest)
		return
	}

	// Destination must be a directory.
	info, err := os.Stat(destAbs)
	if err != nil || !info.IsDir() {
		http.Error(w, "destination is not a folder", http.StatusBadRequest)
		return
	}

	moved := 0
	for _, p := range req.Paths {
		srcRel := cleanPath(p)
		srcAbs := filepath.Join(app.filesDir, srcRel)
		if !strings.HasPrefix(srcAbs, app.filesDir) || srcAbs == app.filesDir {
			continue
		}
		name := filepath.Base(srcRel)
		newAbs := filepath.Join(destAbs, name)

		// Don't move a folder into itself.
		if strings.HasPrefix(newAbs, srcAbs+"/") {
			continue
		}

		if err := os.Rename(srcAbs, newAbs); err != nil {
			continue
		}

		// Update DB metadata.
		srcDir := filepath.Dir(srcRel)
		app.db.Exec("UPDATE files SET path = ? WHERE path = ? AND name = ?",
			destRel, srcDir, name)
		moved++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"moved": moved})
}

// --- helpers ---

func migrate(db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, e := range entries {
		data, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if _, err := db.Exec(string(data)); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
	}
	return nil
}

func dirSize(path string) int64 {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func cleanPath(p string) string {
	p = filepath.Clean(p)
	p = strings.TrimPrefix(p, "/")
	if p == "." {
		return ""
	}
	// Reject any path that tries to escape.
	if strings.Contains(p, "..") {
		return ""
	}
	return p
}

func isTextMime(m string) bool {
	return strings.HasPrefix(m, "text/") || m == "application/json" ||
		m == "application/xml" || m == "application/javascript" ||
		m == "application/x-yaml" || m == "application/toml"
}

func isTextExt(ext string) bool {
	textExts := map[string]bool{
		".txt": true, ".md": true, ".go": true, ".py": true, ".js": true,
		".ts": true, ".html": true, ".css": true, ".json": true, ".yaml": true,
		".yml": true, ".toml": true, ".xml": true, ".sh": true, ".bash": true,
		".zsh": true, ".fish": true, ".sql": true, ".lua": true, ".rb": true,
		".rs": true, ".c": true, ".h": true, ".cpp": true, ".java": true,
		".kt": true, ".swift": true, ".r": true, ".csv": true, ".tsv": true,
		".log": true, ".conf": true, ".ini": true, ".env": true, ".gitignore": true,
		".dockerfile": true, ".makefile": true, ".mod": true, ".sum": true,
	}
	return textExts[ext]
}

func isMediaMime(m string) bool {
	return strings.HasPrefix(m, "image/") || strings.HasPrefix(m, "audio/") ||
		strings.HasPrefix(m, "video/")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
