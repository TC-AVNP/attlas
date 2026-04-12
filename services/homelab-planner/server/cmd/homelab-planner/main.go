// homelab-planner — personal homelab build tracker for attlas.
//
// Configuration via environment variables:
//
//   HOMELAB_PLANNER_DB          path to the sqlite database file
//   HOMELAB_PLANNER_PORT        TCP port to bind on 127.0.0.1
//   HOMELAB_PLANNER_STATIC_DIR  directory served at /homelab-planner/ (vite dist/)
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

	"homelab-planner/api"
	"homelab-planner/db"
	"homelab-planner/service"
)

const version = "0.1.0-dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: homelab-planner <serve|migrate|version>")
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
		fmt.Fprintf(os.Stderr, "homelab-planner: unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", envInt("HOMELAB_PLANNER_PORT", 7691), "HTTP listen port")
	dbPath := fs.String("db", envString("HOMELAB_PLANNER_DB", "/var/lib/homelab-planner/homelab-planner.db"), "SQLite path")
	staticDir := fs.String("static", envString("HOMELAB_PLANNER_STATIC_DIR", defaultStaticDir()), "React build directory")
	_ = fs.Parse(args)

	conn, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("homelab-planner: open db: %v", err)
	}
	defer conn.Close()

	svc := service.New(conn)
	apiHandler := &api.API{Svc: svc}

	inner := http.NewServeMux()
	apiHandler.Register(inner)
	inner.Handle("/", spaFileServer(*staticDir))

	outer := http.NewServeMux()
	outer.Handle("/homelab-planner/", http.StripPrefix("/homelab-planner", inner))
	outer.Handle("/homelab-planner", http.RedirectHandler("/homelab-planner/", http.StatusPermanentRedirect))

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           loggingMiddleware(outer),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("homelab-planner %s listening on http://%s (db=%s, static=%s)",
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
		log.Printf("homelab-planner: received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("homelab-planner: shutdown error: %v", err)
		}
	case err := <-errCh:
		if err != nil {
			log.Fatalf("homelab-planner: serve failed: %v", err)
		}
	}
}

func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dbPath := fs.String("db", envString("HOMELAB_PLANNER_DB", "/var/lib/homelab-planner/homelab-planner.db"), "SQLite path")
	_ = fs.Parse(args)

	conn, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("homelab-planner: open db: %v", err)
	}
	conn.Close()
	log.Printf("homelab-planner: migrations applied")
}

// --- static SPA server -----------------------------------------------------

func spaFileServer(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			http.Error(w,
				"homelab-planner: static directory not found at "+dir+
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
		http.Error(w, "homelab-planner: index.html missing in "+dir, http.StatusServiceUnavailable)
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
	case strings.HasSuffix(path, ".wasm"):
		w.Header().Set("Content-Type", "application/wasm")
	}
}

// --- middleware -------------------------------------------------------------

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

// --- env helpers ------------------------------------------------------------

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
	if stat, err := os.Stat("/usr/local/share/homelab-planner/dist"); err == nil && stat.IsDir() {
		return "/usr/local/share/homelab-planner/dist"
	}
	return "./web/dist"
}
