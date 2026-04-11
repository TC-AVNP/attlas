// petboard — personal project tracker service for attlas.
//
// One binary, three subcommands:
//
//   petboard serve    — run the HTTP server (REST + static SPA + later MCP)
//   petboard migrate  — apply SQLite schema migrations and exit
//   petboard version  — print the version string and exit
//
// Configuration is entirely via environment variables so the systemd
// unit can own it without a config file:
//
//   PETBOARD_DB          path to the sqlite database file
//   PETBOARD_PORT        TCP port to bind on 127.0.0.1
//   PETBOARD_STATIC_DIR  directory served at /petboard/ (vite dist/)
//
// See services/petboard/PLAN.md for the full design.
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

	"petboard/api"
	"petboard/db"
	"petboard/service"
)

const version = "0.1.0-dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: petboard <serve|migrate|version>")
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
		fmt.Fprintf(os.Stderr, "petboard: unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
}

// --- serve -------------------------------------------------------------

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", envInt("PETBOARD_PORT", 7690), "HTTP listen port (bound to 127.0.0.1)")
	dbPath := fs.String("db", envString("PETBOARD_DB", "/var/lib/petboard/petboard.db"), "SQLite path")
	staticDir := fs.String("static", envString("PETBOARD_STATIC_DIR", defaultStaticDir()), "React build directory (Vite dist/)")
	_ = fs.Parse(args)

	conn, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("petboard: open db: %v", err)
	}
	defer conn.Close()

	svc := service.New(conn)
	apiHandler := &api.API{Svc: svc}

	// Inner mux: routes as the downstream sees them AFTER we strip the
	// /petboard/ prefix.
	inner := http.NewServeMux()
	apiHandler.Register(inner)
	inner.Handle("/", spaFileServer(*staticDir))

	// Outer mux: Caddy proxies /petboard/* to us. Strip the prefix so
	// the inner mux matches plain /api/... and /.
	outer := http.NewServeMux()
	outer.Handle("/petboard/", http.StripPrefix("/petboard", inner))
	outer.Handle("/petboard", http.RedirectHandler("/petboard/", http.StatusPermanentRedirect))

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      loggingMiddleware(outer),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  90 * time.Second,
	}

	// Start the server in a goroutine so main can handle signals.
	errCh := make(chan error, 1)
	go func() {
		log.Printf("petboard %s listening on http://%s (db=%s, static=%s)",
			version, addr, *dbPath, *staticDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("petboard: received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("petboard: shutdown error: %v", err)
		}
	case err := <-errCh:
		if err != nil {
			log.Fatalf("petboard: serve failed: %v", err)
		}
	}
}

// --- migrate -----------------------------------------------------------

func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dbPath := fs.String("db", envString("PETBOARD_DB", "/var/lib/petboard/petboard.db"), "SQLite path")
	_ = fs.Parse(args)

	conn, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("petboard: open db: %v", err)
	}
	conn.Close()
	log.Printf("petboard: migrations applied")
}

// --- static SPA server -------------------------------------------------

// spaFileServer returns an http.Handler that serves static files from
// dir, falling back to dir/index.html for any path that isn't an
// existing file. That fallback is what lets client-side routes like
// /petboard/p/petboard deep-link correctly — the server treats the
// unknown path as a 200 with index.html, and React Router picks up.
//
// The MIME types for JS, CSS, and wasm are set explicitly because
// modernc.org/sqlite used to ship a wasm file and wait-staff browsers
// are picky; setting them unconditionally costs us nothing.
func spaFileServer(dir string) http.Handler {
	// If the static directory doesn't exist yet (e.g. running the
	// binary before the frontend has been built), surface a 503 with a
	// helpful message instead of a confusing 404.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			http.Error(w,
				"petboard: static directory not found at "+dir+
					" — build the frontend (cd web && npm run build) first",
				http.StatusServiceUnavailable)
			return
		}
		// Clean and normalize the request path so ../ attempts are
		// rejected by filepath.Join behavior plus the IsAbs check.
		reqPath := r.URL.Path
		if reqPath == "" || reqPath == "/" {
			serveIndex(w, r, dir)
			return
		}
		// Build the candidate file path rooted at dir. Reject any
		// resolved path that escapes dir.
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
		// Not a real file — fall through to index.html so the SPA router
		// can take it.
		serveIndex(w, r, dir)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, dir string) {
	index := filepath.Join(dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		http.Error(w, "petboard: index.html missing in "+dir, http.StatusServiceUnavailable)
		return
	}
	// index.html must never be cached — we want React to always pick
	// up the latest bundle references after a rebuild.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, index)
}

// setContentType applies a handful of MIME defaults for paths that
// http.ServeFile's built-in detection sometimes gets wrong on minimal
// systems.
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

// --- middleware --------------------------------------------------------

// loggingMiddleware writes a one-line access log per request. Keep it
// compact so the journalctl tail stays readable.
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

// --- env helpers -------------------------------------------------------

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

// defaultStaticDir guesses where the vite build output lives when no
// explicit path is supplied. Preference order:
//
//  1. ../../web/dist relative to the binary — useful during local
//     development when running `go run ./cmd/petboard` from server/.
//  2. /usr/local/share/petboard/dist — the install script's layout.
//  3. ./web/dist — fallback for ad-hoc runs.
func defaultStaticDir() string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "..", "..", "web", "dist")
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate
		}
	}
	if stat, err := os.Stat("/usr/local/share/petboard/dist"); err == nil && stat.IsDir() {
		return "/usr/local/share/petboard/dist"
	}
	return "./web/dist"
}
