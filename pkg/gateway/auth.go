package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
	"github.com/odrazdotxyz/odraz-go-sdk/accounts"
	"github.com/odrazdotxyz/odraz-go-sdk/accounts/oidc"
	"github.com/odrazdotxyz/odraz-go-sdk/files"
)

// hasOIDC: only the OpenCloud adapter has browser (OIDC) sign-in. Nextcloud needs its user_oidc app for
// that, so Nextcloud signs in with a user name and an app password.
func hasOIDC(b Backend) bool { return b.Vendor == "opencloud" }

func (s *Server) hasDemo(b Backend) bool {
	if s.cfg.DemoLogin == nil {
		return false
	}
	_, _, ok := s.cfg.DemoLogin(b)
	return ok
}

// authMethod is the SDK login method for username + app token / app password.
func authMethod(b Backend) string {
	if b.Vendor == "nextcloud" {
		return "app-password"
	}
	return "app-token"
}

func (s *Server) baseAccount(b Backend) accounts.Account {
	cfg := map[string]string{"url": b.URL}
	if b.Insecure {
		cfg[accounts.ConfigInsecure] = "true"
	}
	label := map[string]string{"opencloud": "OpenCloud", "nextcloud": "Nextcloud"}[b.Vendor]
	return accounts.Account{ID: b.ID, Label: label, Adapter: b.Vendor, Config: cfg}
}

// GET /auth/login?backend=: discover the identity provider through the backend (WebFinger) and send the
// browser to it. The SDK never opens a browser itself: the redirect is ours.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var be Backend
	for _, b := range s.cfg.Backends {
		if hasOIDC(b) && (r.URL.Query().Get("backend") == "" || r.URL.Query().Get("backend") == b.ID) {
			be = b
			break
		}
	}
	if be.ID == "" {
		authError(w, errors.New("browser sign-in is not available here; use the demo button"))
		return
	}
	sess := s.ensureSession(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	acc := s.baseAccount(be)
	h, err := accounts.Open(ctx, acc, sess.store)
	if err != nil {
		authError(w, err)
		return
	}
	pending, err := oidc.Begin(ctx, oidc.HTTPClient(h.Env), be.URL, oidc.Config{
		ClientID: s.cfg.ClientID, RedirectURI: s.cfg.PublicURL + "/auth/callback"})
	if err != nil {
		authError(w, fmt.Errorf("could not start the login: %w", err))
		return
	}
	saved, err := pending.Marshal()
	if err != nil {
		authError(w, err)
		return
	}
	sess.mu.Lock()
	sess.pendAcc, sess.pendBE, sess.pending = acc, be, saved
	sess.mu.Unlock()
	s.persist(sess)
	http.Redirect(w, r, pending.AuthorizationURL, http.StatusFound)
}

// GET /auth/callback: the identity provider sent the browser back with a code.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	sess := s.lookup(r)
	if sess == nil || sess.pending == nil {
		authError(w, errors.New("no login in progress (the session expired, or the cookie was lost)"))
		return
	}
	sess = s.signInSession(w, r, true) // a new session id for the signed-in session
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	sess.mu.Lock()
	saved, acc, be := sess.pending, sess.pendAcc, sess.pendBE
	sess.mu.Unlock()
	h, err := accounts.Open(ctx, acc, sess.store) // for the HTTP client that trusts this backend's certificate
	if err != nil {
		authError(w, err)
		return
	}
	pending, err := oidc.Resume(saved, oidc.HTTPClient(h.Env))
	if err != nil {
		authError(w, err)
		return
	}
	tokens, err := pending.Complete(ctx, s.cfg.PublicURL+r.URL.RequestURI())
	if err != nil {
		authError(w, err)
		return
	}
	if err := oidc.Save(ctx, sess.store, acc.ID, tokens); err != nil {
		authError(w, err)
		return
	}
	acc.Config[accounts.ConfigOIDCClientID] = s.cfg.ClientID // marks the account as OIDC: tokens refresh themselves
	sess.mu.Lock()
	sess.pending = nil
	sess.mu.Unlock()
	// connect persists the session, with the pending login gone
	if err := s.connect(ctx, sess, be, acc, "oidc", idTokenName(tokens.IDToken)); err != nil {
		authError(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// POST /auth/apptoken {"backend","username","token"}: sign in to one backend with an app token or password.
func (s *Server) handleAppToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Backend, Username, Token string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil || in.Username == "" || in.Token == "" {
		writeErr(w, http.StatusBadRequest, "username and token are required")
		return
	}
	if in.Backend == "" && len(s.cfg.Backends) > 0 {
		in.Backend = s.cfg.Backends[0].ID
	}
	be, ok := s.backend(in.Backend)
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown backend")
		return
	}
	sess := s.signInSession(w, r, false)
	if err := s.signInPassword(r.Context(), sess, be, in.Username, in.Token); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /auth/demo[?backend=id]: one-click sign-in as the demo user, using the token the operator gave the
// gateway (-demo-token-file). Without ?backend it connects every backend that has demo credentials.
// Disabled unless configured, because it lets anyone who can reach the gateway in. Demo only.
func (s *Server) handleDemo(w http.ResponseWriter, r *http.Request) {
	var targets []Backend
	for _, b := range s.cfg.Backends {
		if s.hasDemo(b) && (r.URL.Query().Get("backend") == "" || r.URL.Query().Get("backend") == b.ID) {
			targets = append(targets, b)
		}
	}
	if len(targets) == 0 {
		writeErr(w, http.StatusNotFound, "demo login is not enabled")
		return
	}
	sess := s.signInSession(w, r, false)
	errs := map[string]string{}
	connected := 0
	for _, b := range targets {
		user, secret, _ := s.cfg.DemoLogin(b)
		if err := s.signInPassword(r.Context(), sess, b, user, secret); err != nil {
			errs[b.ID] = err.Error()
			continue
		}
		connected++
	}
	if connected == 0 {
		writeErr(w, http.StatusBadGateway, strings.Join(mapValues(errs), "; "))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "errors": errs})
}

func mapValues(m map[string]string) []string {
	var out []string
	for k, v := range m {
		out = append(out, k+": "+v)
	}
	return out
}

func (s *Server) signInPassword(ctx context.Context, sess *session, be Backend, username, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	values := map[string]string{"url": be.URL, "username": username, "password": token}
	if be.Insecure {
		values[accounts.ConfigInsecure] = "true"
	}
	acc, err := accounts.Configure(ctx, sess.store, s.baseAccount(be), authMethod(be), values)
	if err != nil {
		return err
	}
	// Validate gives a clear ErrAuth / ErrNotFound / ErrBlockedByProxy for the UI.
	if err := accounts.Validate(ctx, acc, sess.store); err != nil {
		return err
	}
	return s.connect(ctx, sess, be, acc, authMethod(be), username)
}

// POST /auth/disconnect?backend=id: forget one backend (and the tokens held for it); the others stay
// connected. Disconnecting the last one ends the session, so this is also how a user signs out.
func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if sess := s.lookup(r); sess != nil {
		id := r.URL.Query().Get("backend")
		sess.mu.Lock()
		delete(sess.accts, id)
		left := len(sess.accts)
		sess.mu.Unlock()
		_ = sess.store.Delete(r.Context(), id)
		if left == 0 {
			s.dropSession(w, r)
		} else {
			s.persist(sess)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.dropSession(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// connect opens the account, gets its files client from the SDK and adds it to the session.
func (s *Server) connect(ctx context.Context, sess *session, be Backend, acc accounts.Account, method, user string) error {
	a, err := s.open(ctx, sess, be, acc, method, user)
	if err != nil {
		return err
	}
	if _, err := a.client.Drives(ctx); err != nil { // proves the credentials work (and the server accepts them)
		return err
	}
	sess.mu.Lock()
	sess.accts[be.ID] = a
	sess.mu.Unlock()
	s.persist(sess)
	return nil
}

// open builds the live connection for an account whose credentials are in the session's secret store.
func (s *Server) open(ctx context.Context, sess *session, be Backend, acc accounts.Account, method, user string) (*account, error) {
	h, err := accounts.Open(ctx, acc, sess.store)
	if err != nil {
		return nil, err
	}
	cl, err := files.ForAccount(ctx, h)
	if err != nil {
		return nil, err
	}
	return &account{backend: be, acc: acc, method: method, user: user, handle: h, client: cl, drives: map[string]files.FS{}}, nil
}

type backendJSON struct {
	ID        string `json:"id"`
	Vendor    string `json:"vendor"`
	Connected bool   `json:"connected"`
	User      string `json:"user,omitempty"`
	Method    string `json:"method,omitempty"`
	OIDC      bool   `json:"oidc"`
	Demo      bool   `json:"demo"`
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := s.lookup(r)
	out := make([]backendJSON, 0, len(s.cfg.Backends))
	signedIn, demo := false, false
	for _, b := range s.cfg.Backends {
		j := backendJSON{ID: b.ID, Vendor: b.Vendor, OIDC: hasOIDC(b), Demo: s.hasDemo(b)}
		demo = demo || j.Demo
		if sess != nil {
			sess.mu.Lock()
			if a := sess.accts[b.ID]; a != nil {
				j.Connected, j.User, j.Method = true, a.user, a.method
				signedIn = true
			}
			sess.mu.Unlock()
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": signedIn, "demo": demo, "backends": out})
}

// authError renders a small page for failures in the browser redirect flow.
func authError(w http.ResponseWriter, err error) {
	hint := ""
	switch {
	case errors.Is(err, odraz.ErrBlockedByProxy):
		hint = "A proxy or login gateway answered instead of the server."
	case errors.Is(err, odraz.ErrAuth):
		hint = "The identity provider refused the login."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>Sign-in failed</title>
<body style="font-family:system-ui;max-width:42rem;margin:4rem auto;padding:0 1rem">
<h1>Sign-in failed</h1><p>%s</p><pre style="white-space:pre-wrap;background:#f3f4f6;padding:1rem;border-radius:8px">%s</pre>
<p><a href="/">Back to the start</a></p></body>`, html.EscapeString(hint), html.EscapeString(err.Error()))
}

// idTokenName reads a display name from an ID token WITHOUT verifying it. It is only used to
// show a name in the page; nothing is authorized on it (OpenCloud validates the access token).
func idTokenName(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var c struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	_ = json.Unmarshal(raw, &c)
	if c.Name != "" {
		return c.Name
	}
	return c.PreferredUsername
}
