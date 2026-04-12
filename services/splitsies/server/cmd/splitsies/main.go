// splitsies — expense splitting service for attlas.
//
// One binary, three subcommands:
//
//   splitsies serve    — run the HTTP server
//   splitsies migrate  — apply SQLite schema migrations and exit
//   splitsies version  — print the version string and exit
//
// Configuration is entirely via environment variables:
//
//   SPLITSIES_DB                 path to the sqlite database file
//   SPLITSIES_PORT               TCP port to bind on 127.0.0.1
//   SPLITSIES_STATIC_DIR         directory served at /splitsies/ (vite dist/)
//   SPLITSIES_GOOGLE_CLIENT_ID   Google OAuth client ID
//   SPLITSIES_GOOGLE_SECRET      Google OAuth client secret
//   SPLITSIES_BASE_URL           canonical base URL (e.g. https://splitsies.attlas.uk)
//   SPLITSIES_LOCAL_BYPASS       when "1", skip auth on loopback (dev only)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"splitsies/api"
	"splitsies/db"
	"splitsies/events"
	"splitsies/service"
)

const version = "0.1.0-dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: splitsies <serve|migrate|version>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "migrate":
		runMigrate(os.Args[2:])
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprintf(os.Stderr, "splitsies: unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", envInt("SPLITSIES_PORT", 7692), "HTTP listen port")
	dbPath := fs.String("db", envString("SPLITSIES_DB", "/var/lib/splitsies/splitsies.db"), "SQLite path")
	staticDir := fs.String("static", envString("SPLITSIES_STATIC_DIR", defaultStaticDir()), "React build directory")
	googleClientID := fs.String("google-client-id", envString("SPLITSIES_GOOGLE_CLIENT_ID", ""), "Google OAuth client ID")
	googleSecret := fs.String("google-secret", envString("SPLITSIES_GOOGLE_SECRET", ""), "Google OAuth client secret")
	baseURL := fs.String("base-url", envString("SPLITSIES_BASE_URL", "http://localhost:7692"), "Canonical base URL")
	localBypass := fs.Bool("local-bypass", envString("SPLITSIES_LOCAL_BYPASS", "") == "1", "skip auth on loopback (dev)")
	initialAdmin := fs.String("initial-admin", envString("SPLITSIES_INITIAL_ADMIN", ""), "email to bootstrap as the first admin if no admin exists yet")
	_ = fs.Parse(args)

	conn, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("splitsies: open db: %v", err)
	}
	defer conn.Close()

	svc := service.New(conn)

	// Bootstrap the first admin. No-op if any admin already exists — so
	// it's safe to keep this env var set in the systemd unit indefinitely.
	if *initialAdmin != "" {
		if err := svc.EnsureInitialAdmin(*initialAdmin); err != nil {
			log.Printf("splitsies: initial admin bootstrap failed: %v", err)
		}
	}
	broker := events.New()
	apiHandler := &api.API{
		Svc:                svc,
		Events:             broker,
		GoogleClientID:     *googleClientID,
		GoogleClientSecret: *googleSecret,
		BaseURL:            *baseURL,
		LocalBypass:        *localBypass,
	}
	if *localBypass {
		log.Printf("splitsies: WARNING — local bypass enabled; loopback requests skip auth")
	}

	mux := http.NewServeMux()
	apiHandler.Register(mux)
	mux.Handle("/", spaFileServer(*staticDir))

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("splitsies %s listening on http://%s (db=%s, static=%s)",
			version, addr, *dbPath, *staticDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("splitsies: received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("splitsies: shutdown error: %v", err)
		}
	case err := <-errCh:
		if err != nil {
			log.Fatalf("splitsies: serve failed: %v", err)
		}
	}
}

func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dbPath := fs.String("db", envString("SPLITSIES_DB", "/var/lib/splitsies/splitsies.db"), "SQLite path")
	_ = fs.Parse(args)

	conn, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("splitsies: open db: %v", err)
	}
	conn.Close()
	log.Printf("splitsies: migrations applied")
}

func spaFileServer(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			http.Error(w,
				"splitsies: static directory not found at "+dir+
					" — build the frontend (cd web && npm run build) first",
				http.StatusServiceUnavailable)
			return
		}
		reqPath := r.URL.Path
		if reqPath == "" || reqPath == "/" {
			serveIndex(w, r, dir)
			return
		}
		candidate := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(reqPath, "/")))
		rel, err := filepath.Rel(dir, candidate)
		if err != nil || strings.HasPrefix(rel, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			setContentType(w, candidate)
			http.ServeFile(w, r, candidate)
			return
		}
		serveIndex(w, r, dir)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, dir string) {
	index := filepath.Join(dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		http.Error(w, "splitsies: index.html missing in "+dir, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, index)
}

func setContentType(w http.ResponseWriter, path string) {
	switch {
	case strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".mjs"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(path, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingResponseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(lw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, lw.status, time.Since(start))
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *loggingResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *loggingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func defaultStaticDir() string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "..", "..", "web", "dist")
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate
		}
	}
	if stat, err := os.Stat("/usr/local/share/splitsies/dist"); err == nil && stat.IsDir() {
		return "/usr/local/share/splitsies/dist"
	}
	return "./web/dist"
}
