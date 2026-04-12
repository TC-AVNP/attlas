// splitsies-gateway — subdomain-level reverse proxy for splitsies.attlas.uk.
//
// Caddy terminates TLS for splitsies.attlas.uk and forwards everything to
// this gateway on 127.0.0.1:7700. The gateway dispatches requests to
// backend services based on a route registry. Services register
// themselves by dropping a route file in /etc/splitsies-gateway.d/.
//
// Route file format — one route per line:
//
//   # comments start with hash
//   <path_prefix> <backend_url>
//
// Example (splitsies itself):
//
//   /  http://127.0.0.1:7691
//
// Longest prefix wins. Send the gateway SIGHUP to reload routes without
// dropping connections.
//
// Configuration (env vars):
//
//   SPLITSIES_GATEWAY_PORT    listen port (default 7700)
//   SPLITSIES_GATEWAY_ROUTES  routes directory (default /etc/splitsies-gateway.d)
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const version = "0.1.0-dev"

// Route maps a URL path prefix to a backend.
type Route struct {
	Prefix  string
	Backend *url.URL
	Proxy   *httputil.ReverseProxy
}

// routesAtomic stores the current []Route behind atomic.Value so SIGHUP
// can swap the table without locking the request path.
type routesAtomic struct {
	v atomic.Value // holds []Route
}

func (r *routesAtomic) Load() []Route {
	v := r.v.Load()
	if v == nil {
		return nil
	}
	return v.([]Route)
}

func (r *routesAtomic) Store(rs []Route) { r.v.Store(rs) }

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	fs := flag.NewFlagSet("splitsies-gateway", flag.ExitOnError)
	port := fs.Int("port", envInt("SPLITSIES_GATEWAY_PORT", 7700), "listen port")
	routesDir := fs.String("routes", envString("SPLITSIES_GATEWAY_ROUTES", "/etc/splitsies-gateway.d"), "routes directory")
	showVersion := fs.Bool("version", false, "print version and exit")
	_ = fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Println(version)
		return
	}

	routes := &routesAtomic{}
	if err := reloadRoutes(routes, *routesDir); err != nil {
		log.Fatalf("gateway: load routes: %v", err)
	}

	handler := &gatewayHandler{routes: routes}

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           loggingMiddleware(handler),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// SIGHUP → reload routes without restart.
	hupCh := make(chan os.Signal, 1)
	signal.Notify(hupCh, syscall.SIGHUP)
	go func() {
		for range hupCh {
			log.Printf("gateway: received SIGHUP, reloading routes")
			if err := reloadRoutes(routes, *routesDir); err != nil {
				log.Printf("gateway: reload failed: %v", err)
			}
		}
	}()

	// SIGINT/SIGTERM → graceful shutdown.
	errCh := make(chan error, 1)
	go func() {
		log.Printf("splitsies-gateway %s listening on http://%s (routes=%s)",
			version, addr, *routesDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("gateway: received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("gateway: shutdown error: %v", err)
		}
	case err := <-errCh:
		if err != nil {
			log.Fatalf("gateway: serve failed: %v", err)
		}
	}
}

// --- Handler -------------------------------------------------------------

type gatewayHandler struct {
	routes *routesAtomic
}

func (h *gatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rts := h.routes.Load()
	for _, rt := range rts {
		if matches(r.URL.Path, rt.Prefix) {
			rt.Proxy.ServeHTTP(w, r)
			return
		}
	}
	http.Error(w, "no route matches", http.StatusBadGateway)
}

// matches reports whether path is covered by prefix. Prefix "/" matches
// everything; "/foo" matches "/foo" and "/foo/..." but not "/foobar".
func matches(path, prefix string) bool {
	if prefix == "/" {
		return true
	}
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}

// --- Route loading -------------------------------------------------------

// reloadRoutes reads every *.conf file in dir, parses routes, and atomically
// installs them. Returns an error if the directory can't be read or any
// file is malformed. A missing directory is treated as empty (no routes).
func reloadRoutes(store *routesAtomic, dir string) error {
	var parsed []Route

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			log.Printf("gateway: routes dir %s does not exist, no routes loaded", dir)
			store.Store(nil)
			return nil
		}
		return err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		rts, err := parseRouteFile(path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		parsed = append(parsed, rts...)
	}

	// Build reverse proxies and sort so longest prefix wins.
	for i := range parsed {
		parsed[i].Proxy = buildProxy(parsed[i].Backend)
	}
	sort.Slice(parsed, func(i, j int) bool {
		return len(parsed[i].Prefix) > len(parsed[j].Prefix)
	})

	for _, rt := range parsed {
		log.Printf("gateway: route %s → %s", rt.Prefix, rt.Backend)
	}
	store.Store(parsed)
	return nil
}

func parseRouteFile(path string) ([]Route, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var routes []Route
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("line %d: expected '<prefix> <backend>', got %q", lineNo, line)
		}
		prefix := fields[0]
		if !strings.HasPrefix(prefix, "/") {
			return nil, fmt.Errorf("line %d: prefix must start with /, got %q", lineNo, prefix)
		}
		u, err := url.Parse(fields[1])
		if err != nil {
			return nil, fmt.Errorf("line %d: bad backend URL %q: %w", lineNo, fields[1], err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("line %d: backend URL must include scheme and host, got %q", lineNo, fields[1])
		}
		routes = append(routes, Route{Prefix: prefix, Backend: u})
	}
	return routes, scanner.Err()
}

// buildProxy constructs a reverse proxy that forwards to backend while
// preserving the original host header (so backends that vhost-check don't
// reject the request) and setting X-Forwarded-* headers honestly.
func buildProxy(backend *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(backend)

	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		// Remember the original host so we can set X-Forwarded-Host.
		origHost := req.Host
		origProto := req.Header.Get("X-Forwarded-Proto")
		if origProto == "" {
			if req.TLS != nil {
				origProto = "https"
			} else {
				origProto = "http"
			}
		}
		origDirector(req)
		// Preserve the original Host for backends that care.
		req.Header.Set("X-Forwarded-Host", origHost)
		req.Header.Set("X-Forwarded-Proto", origProto)
		// Don't leak localhost:7691 as the apparent host.
		req.Host = backend.Host
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("gateway: proxy error for %s%s → %s: %v", r.Host, r.URL.Path, backend, err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}
	return proxy
}

// --- Middleware ----------------------------------------------------------

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingResponseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(lw, r)
		log.Printf("%s %s %s %d %s", r.Host, r.Method, r.URL.Path, lw.status, time.Since(start))
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

// --- Env helpers ---------------------------------------------------------

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
