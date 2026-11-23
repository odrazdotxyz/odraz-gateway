package gateway

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer(cfg Config) (*Server, *bytes.Buffer) {
	var logs bytes.Buffer
	cfg.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	if cfg.Backends == nil {
		cfg.Backends = []Backend{{ID: "nextcloud", Vendor: "nextcloud", URL: "http://x"}}
	}
	return New(cfg), &logs
}

func TestHealthzAndRequestID(t *testing.T) {
	s, logs := testServer(Config{})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) || rec.Header().Get("X-Request-Id") == "" {
		t.Fatalf("healthz: %d %s %v", rec.Code, rec.Body, rec.Header())
	}
	if logs.Len() != 0 {
		t.Errorf("health checks must not fill the log: %s", logs)
	}
}

func TestAccessLogHasNoQueryAndNoSecrets(t *testing.T) {
	s, logs := testServer(Config{})
	req := httptest.NewRequest("GET", "/api/v1/list?path=/secret-plans&acct=x", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "sessionsecret"})
	req.Header.Set("Authorization", "Bearer tokensecret")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	out := logs.String()
	if !strings.Contains(out, `"path":"/api/v1/list"`) || !strings.Contains(out, `"status":401`) || !strings.Contains(out, rec.Header().Get("X-Request-Id")) {
		t.Errorf("log line: %s", out)
	}
	for _, leak := range []string{"secret-plans", "sessionsecret", "tokensecret"} {
		if strings.Contains(out, leak) {
			t.Errorf("the log leaks %q: %s", leak, out)
		}
	}
}

func TestRateLimits(t *testing.T) {
	s, _ := testServer(Config{Limits: Limits{AuthPerMinute: 3, APIPerMinute: 5}})
	h := s.Handler()
	codes := func(method, path string, n int) (got []int) {
		for i := 0; i < n; i++ {
			req := httptest.NewRequest(method, path, nil)
			req.RemoteAddr = "203.0.113.9:5555"
			req.Header.Set("X-Requested-With", "odraz")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			got = append(got, rec.Code)
		}
		return
	}
	got := codes("POST", "/auth/apptoken", 5)
	if got[2] == 429 || got[3] != 429 || got[4] != 429 {
		t.Errorf("auth limit of 3: %v", got)
	}
	got = codes("GET", "/api/v1/drives", 7)
	if got[4] == 429 || got[5] != 429 {
		t.Errorf("api limit of 5: %v", got)
	}
	// another client address is not affected, and the page itself is not limited
	req := httptest.NewRequest("GET", "/api/v1/drives", nil)
	req.RemoteAddr = "198.51.100.1:1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == 429 {
		t.Error("limit leaked across addresses")
	}
	for i := 0; i < 30; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.9:5555"
		h.ServeHTTP(rec, req)
		if rec.Code == 429 {
			t.Fatal("the page must not be rate limited")
		}
	}
}

func TestClientIPOnlyTrustsProxyWhenTold(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:99"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.7")
	if ip := (&Server{}).clientIP(r); ip != "10.0.0.1" {
		t.Errorf("untrusted proxy header used: %s", ip)
	}
	if ip := (&Server{cfg: Config{TrustProxy: true}}).clientIP(r); ip != "203.0.113.7" {
		t.Errorf("trusted proxy: %s (the last entry is the one the proxy added)", ip)
	}
}

func TestUploadLimit(t *testing.T) {
	r := newRig(t, nil)
	r.srv.limits.MaxUploadBytes = 10
	req, _ := http.NewRequest("PUT", r.gw.URL+"/api/v1/file?acct=mem&drive=d1&path=/big.bin", strings.NewReader(strings.Repeat("x", 100)))
	req.Header.Set("X-Requested-With", "odraz")
	req.AddCookie(r.cook)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Errorf("status %d, want 413", resp.StatusCode)
	}
	if got := r.mem.get("/big.bin"); got != "" {
		t.Errorf("an over-limit upload was stored: %q", got)
	}
}

func TestWithoutHookThereIsNoDemoRoute(t *testing.T) {
	s, _ := testServer(Config{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/auth/demo", nil)
	req.Header.Set("X-Requested-With", "odraz")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Errorf("demo route answered %d", rec.Code)
	}
	me := httptest.NewRecorder()
	s.Handler().ServeHTTP(me, httptest.NewRequest("GET", "/api/v1/me", nil))
	if strings.Contains(me.Body.String(), `"demo":true`) {
		t.Error("/api/v1/me advertises a demo login")
	}
}

// Session fixation: a session id that existed before sign-in must never become the signed-in session.
func TestSignInGetsAFreshSessionID(t *testing.T) {
	s, _ := testServer(Config{})
	planted := &session{id: "planted", accts: map[string]*account{}, created: time.Now(), pending: []byte("oidc-state"), pendBE: Backend{ID: "nextcloud"}}
	s.sessions["planted"] = planted
	req := httptest.NewRequest("POST", "/auth/apptoken", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "planted"})
	rec := httptest.NewRecorder()
	fresh := s.signInSession(rec, req, true)
	if fresh == planted || fresh.id == "planted" {
		t.Fatal("the planted session id was kept")
	}
	if _, ok := s.sessions["planted"]; ok {
		t.Error("the old session still exists, so the planted id still works")
	}
	if string(fresh.pending) != "oidc-state" || fresh.pendBE.ID != "nextcloud" {
		t.Error("a sign-in in progress must carry over")
	}
	var set *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			set = c
		}
	}
	if set == nil || set.Value != fresh.id || set.Value == "planted" || !set.HttpOnly {
		t.Errorf("cookie: %+v", set)
	}
	// without keepPending nothing is carried over
	s.sessions["p2"] = &session{id: "p2", accts: map[string]*account{}, created: time.Now(), pending: []byte("x")}
	req = httptest.NewRequest("POST", "/auth/apptoken", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "p2"})
	if f := s.signInSession(httptest.NewRecorder(), req, false); f.pending != nil {
		t.Error("pending login carried over when not asked")
	}
	// a session that is already signed in is kept: connecting a second server adds to it
	in := &session{id: "in", accts: map[string]*account{"x": {}}, created: time.Now()}
	s.sessions["in"] = in
	req = httptest.NewRequest("POST", "/auth/apptoken", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "in"})
	if got := s.signInSession(httptest.NewRecorder(), req, false); got != in {
		t.Error("a signed-in session must be kept")
	}
	// no cookie at all: a new session
	if f := s.signInSession(httptest.NewRecorder(), httptest.NewRequest("POST", "/auth/apptoken", nil), false); f == nil {
		t.Error("no session created")
	}
}
