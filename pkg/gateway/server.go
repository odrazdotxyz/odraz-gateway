// Package gateway is a small web gateway built on the Odraz Go SDK: it signs a user in
// (OIDC through the identity provider OpenCloud trusts, or an OpenCloud app token), keeps
// their tokens server side, and exposes the SDK's files API as JSON to a web page.
//
// It is a demo: sessions live in memory, and the SDK's in-memory SecretStore (meant for
// tests) holds the tokens. A real gateway would use an encrypted session store.
package gateway

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/odrazdotxyz/odraz-go-sdk/accounts"
	"github.com/odrazdotxyz/odraz-go-sdk/files"

	_ "github.com/odrazdotxyz/odraz-go-sdk/vendors/nextcloud" // registers the "nextcloud" adapter
	_ "github.com/odrazdotxyz/odraz-go-sdk/vendors/opencloud" // registers the "opencloud" adapter
)

//go:embed web
var webFS embed.FS

// Backend is one storage server the gateway can connect to. Several can be configured; a session
// can connect to any of them and the page shows their files together.
type Backend struct {
	ID     string // unique, e.g. "opencloud"; also the account ID
	Vendor string // SDK adapter: "opencloud" or "nextcloud"
	URL    string // e.g. https://localhost:9200
	// Insecure accepts this backend's self-signed certificate. Per backend and explicit: never a global switch.
	Insecure bool
}

// DemoLogin is a hook that signs a visitor in without a credential of their own, by returning the user name and
// secret (app token or app password) to use for a backend, or ok=false for none. The product binary does not set
// it, and then the route POST /auth/demo does not exist at all. The demo program sets it. Anyone who can reach
// the gateway gets in with it: demo only.
type DemoLogin func(b Backend) (username, secret string, ok bool)

// Config configures the gateway.
type Config struct {
	PublicURL string    // where browsers reach the gateway, e.g. http://localhost:8080 (OIDC redirect base)
	ClientID  string    // OIDC client registered in the identity provider for this gateway
	Backends  []Backend // at least one
	// WebDir, if set, serves the page (index.html, app.js, app.css, assets) from this folder on disk instead of the
	// copy compiled into the binary, so edits to the page show up on a browser refresh without restarting the
	// gateway. For development; leave it empty for the self-contained binary.
	WebDir string
	// DemoLogin, if set, enables "Try the demo" (POST /auth/demo). See DemoLogin.
	DemoLogin DemoLogin
	// Store keeps sessions and their tokens. Default: in memory (lost on restart). Use NewFileStore for a
	// persistent, encrypted one.
	Store      Store
	Listen     string // address to listen on for Run, default 127.0.0.1:8080
	TrustProxy bool   // behind a reverse proxy: take the client address from the last X-Forwarded-For entry
	Limits     Limits
	Logger     *slog.Logger // default: JSON lines to stderr
	// Office configures the document editor (see OfficeConfig). Leave it empty and users may set their own.
	Office OfficeConfig
}

// Server is the gateway's http.Handler.
type Server struct {
	cfg Config
	mux *http.ServeMux

	mu       sync.Mutex
	sessions map[string]*session

	store      Store
	persistent bool // the Store survives a restart
	log        *slog.Logger
	limits     Limits
	authLimit  rateLimiter
	apiLimit   rateLimiter

	routes []string // every API route pattern ("GET /api/v1/me"), for the test that keeps api/openapi.yaml in step

	editMu sync.Mutex
	edits  map[string]*editSession // open documents, by their unguessable id
}

const (
	cookieName = "odraz_session"
	sessionTTL = 12 * time.Hour
)

// account is one backend a session is connected to.
type account struct {
	backend Backend
	acc     accounts.Account
	method  string // "oidc" or "app-token" / "app-password"
	user    string
	handle  *accounts.Handle // for what is not files: the avatar
	client  files.Client
	drives  map[string]files.FS
}

type session struct {
	id      string
	mu      sync.Mutex
	store   accounts.SecretStore // this session's tokens, in the gateway's Store
	accts   map[string]*account  // by backend ID
	pending []byte               // the browser sign-in in progress: the SDK's saved OIDC session
	pendAcc accounts.Account
	pendBE  Backend
	office  officeSettings // the user's own editor server (memory only, like the session)
	created time.Time
}

// New builds the gateway.
func New(cfg Config) *Server {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8080"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	if cfg.Store == nil {
		cfg.Store = NewMemoryStore()
	}
	s := &Server{cfg: cfg, store: cfg.Store, log: cfg.Logger, limits: cfg.Limits.withDefaults(), mux: http.NewServeMux(), sessions: map[string]*session{}, edits: map[string]*editSession{}}
	if p, ok := cfg.Store.(interface{ Persistent() bool }); ok {
		s.persistent = p.Persistent()
	}
	web, _ := fs.Sub(webFS, "web")
	if cfg.WebDir != "" {
		web = os.DirFS(cfg.WebDir)
	}
	s.mux.Handle("GET /", noStore(http.FileServerFS(web))) // the web UI: not part of the API
	s.handle("GET /healthz", s.handleHealth)

	s.handle("GET /auth/login", s.handleLogin)
	s.handle("GET /auth/callback", s.handleCallback)
	s.handle("POST /auth/apptoken", s.csrf(s.handleAppToken))
	if cfg.DemoLogin != nil {
		s.handle("POST /auth/demo", s.csrf(s.handleDemo))
	}
	s.handle("POST /auth/disconnect", s.csrf(s.handleDisconnect))
	s.handle("POST /auth/logout", s.csrf(s.handleLogout))

	s.handle("GET /api/v1/me", s.handleMe)
	s.handle("GET /api/v1/drives", s.authed(s.handleDrives))
	s.handle("GET /api/v1/list", s.authed(s.handleList))
	s.handle("GET /api/v1/file", s.authed(s.handleDownload))
	s.handle("GET /api/v1/thumb", s.authed(s.handleThumb))
	s.handle("GET /api/v1/zip", s.authed(s.handleZip))
	s.handle("GET /api/v1/avatar", s.authed(s.handleAvatar))
	s.handle("GET /api/v1/photos", s.authed(s.handlePhotos))
	s.handle("GET /api/v1/photos/config", s.authed(s.handlePhotosConfig))
	s.handle("PUT /api/v1/photos/folder", s.csrf(s.authed(s.handlePhotosSet)))
	s.handle("DELETE /api/v1/photos/folder", s.csrf(s.authed(s.handlePhotosUnset)))
	s.handle("PUT /api/v1/file", s.csrf(s.authed(s.handleUpload)))
	s.handle("POST /api/v1/mkdir", s.csrf(s.authed(s.handleMkdir)))
	s.handle("POST /api/v1/move", s.csrf(s.authed(s.handleMove)))
	s.handle("POST /api/v1/copy", s.csrf(s.authed(s.handleCopy)))
	s.handle("DELETE /api/v1/entry", s.csrf(s.authed(s.handleDelete)))

	s.handle("GET /api/v1/editor", s.authed(s.handleEditorGet))
	s.handle("PUT /api/v1/editor", s.csrf(s.authed(s.handleEditorPut)))
	s.handle("DELETE /api/v1/editor", s.csrf(s.authed(s.handleEditorDelete)))
	s.handle("POST /api/v1/editor/check", s.csrf(s.authed(s.handleEditorCheck)))
	s.handle("POST /api/v1/editor/session", s.csrf(s.authed(s.handleEditorSession)))
	s.handle("GET /api/v1/editor/session", s.authed(s.handleEditorState))
	if cfg.Office.Listen == "" { // the editor reaches us on the main address
		s.officeRoutes(s.mux)
	}
	return s
}

// handle registers a route and remembers its pattern.
func (s *Server) handle(pattern string, h http.HandlerFunc) {
	s.routes = append(s.routes, pattern)
	s.mux.HandleFunc(pattern, h)
}

// Routes lists the route patterns the gateway serves (method and path, not the web UI's file server).
func (s *Server) Routes() []string { return append([]string(nil), s.routes...) }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		script, frame := "'self'", "'none'"
		if o := s.officeOrigin(r); o != "" { // exactly the configured editor server may load its script and be framed
			script, frame = "'self' "+o, o
		}
		h.Set("Content-Security-Policy", "default-src 'self'; script-src "+script+"; frame-src "+frame+"; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
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
		if r.Header.Get("X-Requested-With") != "odraz" {
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
		if sess == nil || !sess.signedIn() {
			writeErr(w, http.StatusUnauthorized, "not signed in")
			return
		}
		next(w, r, sess)
	}
}

func (s *Server) lookup(r *http.Request) *session {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	s.mu.Lock()
	sess := s.sessions[c.Value]
	s.mu.Unlock()
	if sess == nil { // not in memory: after a restart, the Store may still have it
		sess = s.restore(r.Context(), c.Value)
	}
	if sess != nil && time.Since(sess.created) > sessionTTL {
		s.forget(r.Context(), c.Value)
		return nil
	}
	return sess
}

// forget removes a session from memory and from the Store.
func (s *Server) forget(ctx context.Context, id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	if err := s.store.Delete(ctx, id); err != nil {
		s.log.Error("session store: delete", "error", err.Error())
	}
}

// persist writes the session's durable state to the Store. A failure is logged and does not fail the request: the
// session keeps working from memory, and the next change tries again.
func (s *Server) persist(sess *session) {
	sess.mu.Lock()
	d := SessionData{ID: sess.id, Created: sess.created, OfficeURL: sess.office.URL, OfficeSecret: sess.office.Secret}
	for _, a := range sess.accts {
		d.Accounts = append(d.Accounts, AccountRecord{Backend: a.backend.ID, Account: a.acc, Method: a.method, User: a.user})
	}
	sort.Slice(d.Accounts, func(i, j int) bool { return d.Accounts[i].Backend < d.Accounts[j].Backend })
	if sess.pending != nil {
		d.Pending = &PendingLogin{Backend: sess.pendBE.ID, Account: sess.pendAcc, OIDC: sess.pending}
	}
	sess.mu.Unlock()
	if err := s.store.Save(context.Background(), &d); err != nil {
		s.log.Error("session store: save", "error", err.Error())
	}
}

// restore rebuilds a session from the Store after a restart: the tokens are already there, the live connections
// are opened again. A server that cannot be reached right now is left out of this session, not dropped from the Store.
func (s *Server) restore(ctx context.Context, id string) *session {
	d, err := s.store.Load(ctx, id)
	if err != nil || d == nil {
		if err != nil {
			s.log.Error("session store: load", "error", err.Error())
		}
		return nil
	}
	sess := &session{id: id, store: s.store.Secrets(id), accts: map[string]*account{}, created: d.Created,
		office: officeSettings{URL: d.OfficeURL, Secret: d.OfficeSecret}}
	for _, rec := range d.Accounts {
		be, ok := s.backend(rec.Backend)
		if !ok {
			continue // no longer configured
		}
		a, err := s.open(ctx, sess, be, rec.Account, rec.Method, rec.User)
		if err != nil {
			s.log.Warn("session restore: server not available", "backend", be.ID, "error", err.Error())
			continue
		}
		sess.accts[be.ID] = a
	}
	if d.Pending != nil {
		if be, ok := s.backend(d.Pending.Backend); ok {
			sess.pending, sess.pendAcc, sess.pendBE = d.Pending.OIDC, d.Pending.Account, be
		}
	}
	s.mu.Lock()
	if cur := s.sessions[id]; cur != nil { // a concurrent request restored it first
		s.mu.Unlock()
		return cur
	}
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess
}

// ensureSession returns the caller's session, creating one (and setting the cookie) if there is none.
// A session can hold several accounts, so signing in to a second backend keeps the first.
func (s *Server) ensureSession(w http.ResponseWriter, r *http.Request) *session {
	if sess := s.lookup(r); sess != nil {
		return sess
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.sessions { // forget expired sessions
		if time.Since(old.created) > sessionTTL {
			delete(s.sessions, id)
		}
	}
	_ = s.store.Sweep(context.Background(), time.Now().Add(-sessionTTL))
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	sess := &session{id: id, store: s.store.Secrets(id), accts: map[string]*account{}, created: time.Now()}
	s.sessions[id] = sess
	go s.persist(sess)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: len(s.cfg.PublicURL) > 5 && s.cfg.PublicURL[:5] == "https", MaxAge: int(sessionTTL / time.Second)})
	return sess
}

func (sess *session) signedIn() bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return len(sess.accts) > 0
}

// backend looks a configured backend up by ID.
func (s *Server) backend(id string) (Backend, bool) {
	for _, b := range s.cfg.Backends {
		if b.ID == id {
			return b, true
		}
	}
	return Backend{}, false
}

func (s *Server) dropSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.forget(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}
