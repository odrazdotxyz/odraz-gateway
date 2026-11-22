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

func (s *Server) baseAccount() accounts.Account {
	cfg := map[string]string{"url": s.cfg.ServerURL}
	if s.cfg.Insecure {
		cfg[accounts.ConfigInsecure] = "true"
	}
	if s.nextcloud() {
		return accounts.Account{ID: "demo", Label: "Nextcloud", Adapter: "nextcloud", Config: cfg}
	}
	return accounts.Account{ID: "demo", Label: "OpenCloud", Adapter: "opencloud", Config: cfg}
}

func (s *Server) nextcloud() bool { return s.cfg.Backend == "nextcloud" }

// oidcLogin: browser sign-in through the identity provider. Only the OpenCloud adapter has it:
// Nextcloud needs its user_oidc app for that, so the Nextcloud demo signs in with an app password.
func (s *Server) oidcLogin() bool { return !s.nextcloud() }

// authMethod is the SDK login method for username + app token / app password.
func (s *Server) authMethod() string {
	if s.nextcloud() {
		return "app-password"
	}
	return "app-token"
}

// GET /auth/login: discover the identity provider through OpenCloud (WebFinger) and send the
// browser to it. The SDK never opens a browser itself: the redirect is ours.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.oidcLogin() {
		authError(w, errors.New("browser sign-in is not available for this backend; use the demo button or an app password"))
		return
	}
	sess := s.newSession(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	acc := s.baseAccount()
	h, err := accounts.Open(ctx, acc, sess.store)
	if err != nil {
		authError(w, err)
		return
	}
	pending, err := oidc.Begin(ctx, oidc.HTTPClient(h.Env), s.cfg.ServerURL, oidc.Config{
		ClientID: s.cfg.ClientID, RedirectURI: s.cfg.PublicURL + "/auth/callback"})
	if err != nil {
		authError(w, fmt.Errorf("could not start the login: %w", err))
		return
	}
	sess.mu.Lock()
	sess.acc, sess.pending = acc, pending
	sess.mu.Unlock()
	http.Redirect(w, r, pending.AuthorizationURL, http.StatusFound)
}

// GET /auth/callback: the identity provider sent the browser back with a code.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	sess := s.lookup(r)
	if sess == nil || sess.pending == nil {
		authError(w, errors.New("no login in progress (the session expired, or the cookie was lost)"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tokens, err := sess.pending.Complete(ctx, s.cfg.PublicURL+r.URL.RequestURI())
	if err != nil {
		authError(w, err)
		return
	}
	if err := oidc.Save(ctx, sess.store, sess.acc.ID, tokens); err != nil {
		authError(w, err)
		return
	}
	sess.mu.Lock()
	sess.acc.Config[accounts.ConfigOIDCClientID] = s.cfg.ClientID // marks the account as OIDC: tokens refresh themselves
	sess.method, sess.user, sess.pending = "oidc", idTokenName(tokens.IDToken), nil
	sess.mu.Unlock()
	if err := s.connect(ctx, sess); err != nil {
		authError(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// POST /auth/apptoken {"username","token"}: sign in with an OpenCloud app token instead.
func (s *Server) handleAppToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Token string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil || in.Username == "" || in.Token == "" {
		writeErr(w, http.StatusBadRequest, "username and token are required")
		return
	}
	s.signInAppToken(w, r, in.Username, in.Token)
}

// POST /auth/demo: one-click sign-in as the demo user, using the app token the operator gave the
// gateway (-demo-token-file). Disabled unless configured, because it lets anyone who can reach the
// gateway in. For local and throwaway demos only.
func (s *Server) handleDemo(w http.ResponseWriter, r *http.Request) {
	if !s.demoEnabled() {
		writeErr(w, http.StatusNotFound, "demo login is not enabled")
		return
	}
	s.signInAppToken(w, r, s.cfg.DemoUser, s.cfg.DemoToken)
}

func (s *Server) demoEnabled() bool { return s.cfg.DemoUser != "" && s.cfg.DemoToken != "" }

func (s *Server) signInAppToken(w http.ResponseWriter, r *http.Request, username, token string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	sess := s.newSession(w, r)
	values := map[string]string{"url": s.cfg.ServerURL, "username": username, "password": token}
	if s.cfg.Insecure {
		values[accounts.ConfigInsecure] = "true"
	}
	acc, err := accounts.Configure(ctx, sess.store, s.baseAccount(), s.authMethod(), values)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Validate gives a clear ErrAuth / ErrNotFound / ErrBlockedByProxy for the UI.
	if err := accounts.Validate(ctx, acc, sess.store); err != nil {
		s.dropSession(w, r)
		writeError(w, err)
		return
	}
	sess.mu.Lock()
	sess.acc, sess.method, sess.user = acc, "app-token", username
	sess.mu.Unlock()
	if err := s.connect(ctx, sess); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.dropSession(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// connect opens the account and gets its files client from the SDK.
func (s *Server) connect(ctx context.Context, sess *session) error {
	h, err := accounts.Open(ctx, sess.acc, sess.store)
	if err != nil {
		return err
	}
	cl, err := files.ForAccount(ctx, h)
	if err != nil {
		return err
	}
	if _, err := cl.Drives(ctx); err != nil { // proves the credentials work (and OpenCloud accepts them)
		return err
	}
	sess.mu.Lock()
	sess.client = cl
	sess.mu.Unlock()
	return nil
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := s.lookup(r)
	if sess == nil || sess.client == nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "demo": s.demoEnabled(), "oidc": s.oidcLogin()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "method": sess.method, "user": sess.user})
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
