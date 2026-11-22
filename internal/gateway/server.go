// Package gateway is a small web gateway built on the Odraz Go SDK: it signs a user in
// (OIDC through the identity provider OpenCloud trusts, or an OpenCloud app token), keeps
// their tokens server side, and exposes the SDK's files API as JSON to a web page.
//
// It is a demo: sessions live in memory, and the SDK's in-memory SecretStore (meant for
// tests) holds the tokens. A real gateway would use an encrypted session store.
package gateway

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/odrazdotxyz/odraz-go-sdk/accounts"
	"github.com/odrazdotxyz/odraz-go-sdk/accounts/oidc"
	"github.com/odrazdotxyz/odraz-go-sdk/files"

	_ "github.com/odrazdotxyz/odraz-go-sdk/vendors/nextcloud" // registers the "nextcloud" adapter
	_ "github.com/odrazdotxyz/odraz-go-sdk/vendors/opencloud" // registers the "opencloud" adapter
)

//go:embed web
var webFS embed.FS

// Config configures the gateway.
type Config struct {
	PublicURL string // where browsers reach the gateway, e.g. http://localhost:8080 (OIDC redirect base)
	Backend   string // "opencloud" (default) or "nextcloud"
	ServerURL string // the backend, e.g. https://localhost:9200 (OpenCloud) or http://localhost:8082 (Nextcloud)
	ClientID  string // OIDC client registered in the identity provider for this gateway
	Insecure  bool   // accept OpenCloud's self-signed certificate
	DemoUser  string // with DemoToken: enables one-click "Try the demo" sign-in (demo only)
	DemoToken string
}

// Server is the gateway's http.Handler.
type Server struct {
	cfg Config
	mux *http.ServeMux

	mu       sync.Mutex
	sessions map[string]*session
}

const (
	cookieName = "odraz_demo"
	sessionTTL = 12 * time.Hour
)

type session struct {
	mu      sync.Mutex
	store   *accounts.MemoryStore
	acc     accounts.Account
	method  string // "oidc" or "app-token"
	user    string
	pending *oidc.Session // login in progress
	client  files.Client  // set once signed in
	drives  map[string]files.FS
	created time.Time
}

// New builds the gateway.
func New(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux(), sessions: map[string]*session{}}
	web, _ := fs.Sub(webFS, "web")
	s.mux.Handle("GET /", noStore(http.FileServerFS(web)))

	s.mux.HandleFunc("GET /auth/login", s.handleLogin)
	s.mux.HandleFunc("GET /auth/callback", s.handleCallback)
	s.mux.HandleFunc("POST /auth/apptoken", s.csrf(s.handleAppToken))
	s.mux.HandleFunc("POST /auth/demo", s.csrf(s.handleDemo))
	s.mux.HandleFunc("POST /auth/logout", s.csrf(s.handleLogout))

	s.mux.HandleFunc("GET /api/me", s.handleMe)
	s.mux.HandleFunc("GET /api/drives", s.authed(s.handleDrives))
	s.mux.HandleFunc("GET /api/list", s.authed(s.handleList))
	s.mux.HandleFunc("GET /api/file", s.authed(s.handleDownload))
	s.mux.HandleFunc("PUT /api/file", s.csrf(s.authed(s.handleUpload)))
	s.mux.HandleFunc("POST /api/mkdir", s.csrf(s.authed(s.handleMkdir)))
	s.mux.HandleFunc("POST /api/move", s.csrf(s.authed(s.handleMove)))
	s.mux.HandleFunc("POST /api/copy", s.csrf(s.authed(s.handleCopy)))
	s.mux.HandleFunc("DELETE /api/entry", s.csrf(s.authed(s.handleDelete)))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
	}
	s.mux.ServeHTTP(w, r)
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// csrf rejects state-changing requests that did not come from our own page: browsers
// cannot add a custom header to a cross-site form post.
func (s *Server) csrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Requested-With") != "odraz-demo" {
			writeErr(w, http.StatusForbidden, "missing X-Requested-With header")
			return
		}
		next(w, r)
	}
}

// authed runs next with the signed-in session, or answers 401.
func (s *Server) authed(next func(http.ResponseWriter, *http.Request, *session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.lookup(r)
		if sess == nil || sess.client == nil {
			writeErr(w, http.StatusUnauthorized, "not signed in")
			return
		}
		next(w, r, sess)
	}
}

func (s *Server) lookup(r *http.Request) *session {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[c.Value]
	if sess != nil && time.Since(sess.created) > sessionTTL {
		delete(s.sessions, c.Value)
		return nil
	}
	return sess
}

// newSession starts a fresh session (dropping any previous one) and sets the cookie.
func (s *Server) newSession(w http.ResponseWriter, r *http.Request) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, err := r.Cookie(cookieName); err == nil {
		delete(s.sessions, c.Value)
	}
	for id, old := range s.sessions { // forget expired sessions
		if time.Since(old.created) > sessionTTL {
			delete(s.sessions, id)
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	sess := &session{store: accounts.NewMemoryStore(), drives: map[string]files.FS{}, created: time.Now()}
	s.sessions[id] = sess
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: len(s.cfg.PublicURL) > 5 && s.cfg.PublicURL[:5] == "https", MaxAge: int(sessionTTL / time.Second)})
	return sess
}

func (s *Server) dropSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}
