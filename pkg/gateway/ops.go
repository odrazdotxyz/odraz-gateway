package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Limits bound what one client can ask of the gateway. Zero values pick the defaults.
type Limits struct {
	MaxUploadBytes int64 // largest single upload (default 5 GiB); a larger Content-Length is refused with 413
	// AuthPerMinute caps sign-in attempts (the /auth/ routes) per client address (default 20). APIPerMinute caps
	// every other call per client address (default 6000, a photo gallery loads hundreds of thumbnails). Negative disables the limit.
	AuthPerMinute int
	APIPerMinute  int
}

func (l Limits) withDefaults() Limits {
	if l.MaxUploadBytes == 0 {
		l.MaxUploadBytes = 5 << 30
	}
	if l.AuthPerMinute == 0 {
		l.AuthPerMinute = 20
	}
	if l.APIPerMinute == 0 {
		l.APIPerMinute = 6000
	}
	return l
}

// rateLimiter is a fixed-window counter per key. It is small on purpose: a guard against floods and password
// guessing from one address, not a substitute for a reverse proxy's limits.
type rateLimiter struct {
	mu     sync.Mutex
	window time.Time
	count  map[string]int
}

func (r *rateLimiter) allow(key string, limit int) bool {
	if limit < 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if r.count == nil || now.Sub(r.window) >= time.Minute {
		r.window, r.count = now, map[string]int{}
	}
	r.count[key]++
	return r.count[key] <= limit
}

// clientIP is the address the request came from. Behind a reverse proxy set Config.TrustProxy so the address the
// proxy reports in X-Forwarded-For (its last entry, which the proxy itself appended) is used instead.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(c int) {
	if w.status == 0 {
		w.status = c
	}
	w.ResponseWriter.WriteHeader(c)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// guard wraps a handler with the request id, the access log and the rate limits. The log line has the method, the
// path WITHOUT its query (queries name files and can hold tokens), status, size, time and the request id, which is
// also returned in the X-Request-Id header. Cookies, bodies and credentials are never logged.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-Id", id)
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}

		ip := s.clientIP(r)
		limited := false
		switch {
		case r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/editor/"): // not a user's calls
		case strings.HasPrefix(r.URL.Path, "/auth/"):
			limited = !s.authLimit.allow(ip, s.limits.AuthPerMinute)
		case strings.HasPrefix(r.URL.Path, "/api/"):
			limited = !s.apiLimit.allow(ip, s.limits.APIPerMinute)
		}
		if limited {
			sw.Header().Set("Retry-After", "60")
			writeJSON(sw, http.StatusTooManyRequests, map[string]any{"error": "too many requests, try again in a minute", "code": "rate_limited"})
		} else {
			next.ServeHTTP(sw, r)
		}
		if r.URL.Path != "/healthz" {
			s.log.Info("request", "id", id, "method", r.Method, "path", r.URL.Path, "status", sw.status, "bytes", sw.bytes,
				"ms", time.Since(start).Milliseconds(), "ip", ip)
		}
	})
}

// Handler is the whole gateway as an http.Handler: Server plus request ids, logging, limits and /healthz.
func (s *Server) Handler() http.Handler {
	h := s.guard(s)
	if s.cfg.Middleware != nil {
		h = s.cfg.Middleware(h)
	}
	return h
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Run serves the gateway on cfg.Listen (and the editor-only listener on cfg.Office.Listen, if set) until ctx is
// cancelled, then shuts down gracefully. There is no overall read or write timeout because uploads and downloads
// can be large and long; slow clients are cut off by the header and idle timeouts, and the upload size limit.
func (s *Server) Run(ctx context.Context) error {
	srvs := []*http.Server{{Addr: s.cfg.Listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}}
	if s.cfg.Office.Listen != "" {
		srvs = append(srvs, &http.Server{Addr: s.cfg.Office.Listen, Handler: s.guard(s.OfficeHandler()), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute})
	}
	errc := make(chan error, len(srvs))
	for _, srv := range srvs {
		go func() {
			s.log.Info("listening", "addr", srv.Addr)
			errc <- srv.ListenAndServe()
		}()
	}
	var first error
	select {
	case <-ctx.Done():
	case first = <-errc:
	}
	sh, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range srvs {
		_ = srv.Shutdown(sh)
	}
	if first != nil && !errors.Is(first, http.ErrServerClosed) {
		return first
	}
	return nil
}
