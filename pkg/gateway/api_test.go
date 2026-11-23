package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
)

func TestParseRange(t *testing.T) {
	const size = 100
	for _, c := range []struct {
		hdr            string
		start, length  int64
		partial, valid bool
	}{
		{"", 0, 100, false, true},
		{"bytes=0-9", 0, 10, true, true},
		{"bytes=10-", 10, 90, true, true},
		{"bytes=90-1000", 90, 10, true, true}, // end clamped to the file
		{"bytes=-5", 95, 5, true, true},
		{"bytes=-1000", 0, 100, true, true},
		{"bytes=100-", 0, 0, false, false}, // starts at EOF
		{"bytes=50-40", 0, 0, false, false},
		{"bytes=x-y", 0, 0, false, false},
		{"bytes=0-1,5-6", 0, 100, false, true}, // multi-range: whole file, allowed by the RFC
		{"items=0-1", 0, 100, false, true},     // unknown unit: ignored
	} {
		s, l, p, ok := parseRange(c.hdr, size)
		if ok != c.valid || (ok && (s != c.start || l != c.length || p != c.partial)) {
			t.Errorf("parseRange(%q) = %d,%d,%v,%v; want %d,%d,%v,%v", c.hdr, s, l, p, ok, c.start, c.length, c.partial, c.valid)
		}
	}
	if _, _, _, ok := parseRange("bytes=0-0", 0); ok {
		t.Error("any range of an empty file is unsatisfiable")
	}
}

func TestStatusFor(t *testing.T) {
	for _, c := range []struct {
		err  error
		want int
		code string
	}{
		{odraz.ErrAuth, 401, "auth"}, {odraz.ErrBlockedByProxy, 502, "blocked_by_proxy"}, {odraz.ErrInvalidPath, 400, "invalid_path"},
		{odraz.ErrPermission, 403, "permission"}, {odraz.ErrNotFound, 404, "not_found"}, {odraz.ErrExists, 409, "exists"},
		{odraz.ErrNotEmpty, 409, "not_empty"}, {odraz.ErrUnsupported, 501, "unsupported"},
		{context.DeadlineExceeded, 504, "error"}, {errors.New("boom"), 500, "error"},
		// wrapped errors still map (the SDK always wraps the original error)
		{fmt.Errorf("webdav: /x: %w: %w", odraz.ErrNotFound, errors.New("HTTP 404")), 404, "not_found"},
	} {
		if got := statusFor(c.err); got != c.want || codeFor(c.err) != c.code {
			t.Errorf("%v -> %d %q, want %d %q", c.err, got, codeFor(c.err), c.want, c.code)
		}
	}
}

func TestEncodeFilename(t *testing.T) {
	if got := encodeFilename("résumé — Élodie.txt"); got != "r%C3%A9sum%C3%A9%20%E2%80%94%20%C3%89lodie.txt" {
		t.Errorf("got %s", got)
	}
	if encodeFilename(`a"b;c.txt`) != "a%22b%3Bc.txt" {
		t.Error("quotes and semicolons must be escaped (header injection)")
	}
}

func TestIDTokenName(t *testing.T) {
	// header.payload.signature with payload {"name":"Demo Admin","preferred_username":"admin"}
	if got := idTokenName("e30.eyJuYW1lIjoiRGVtbyBBZG1pbiIsInByZWZlcnJlZF91c2VybmFtZSI6ImFkbWluIn0.sig"); got != "Demo Admin" {
		t.Errorf("got %q", got)
	}
	if idTokenName("") != "" || idTokenName("not-a-jwt") != "" {
		t.Error("garbage must give an empty name")
	}
}

func TestGuards(t *testing.T) {
	s := New(Config{PublicURL: "http://localhost:8080", ClientID: "x", Backends: []Backend{{ID: "opencloud", Vendor: "opencloud", URL: "https://localhost:9200"}}})
	srv := httptest.NewServer(s)
	defer srv.Close()
	do := func(method, path string, hdr map[string]string) int {
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := do("GET", "/api/v1/drives", nil); c != 401 {
		t.Errorf("unauthenticated API call: %d, want 401", c)
	}
	if c := do("DELETE", "/api/v1/entry?drive=x&path=/a", nil); c != 403 {
		t.Errorf("state change without the CSRF header: %d, want 403", c)
	}
	if c := do("PUT", "/api/v1/file?drive=x&path=/a", map[string]string{"X-Requested-With": "odraz"}); c != 401 {
		t.Errorf("state change without a session: %d, want 401", c)
	}
	if c := do("GET", "/", nil); c != 200 {
		t.Errorf("index: %d", c)
	}
	if c := do("GET", "/assets/odrive-mark.svg", nil); c != 200 {
		t.Errorf("embedded asset: %d", c)
	}
	if c := do("GET", "/auth/callback", nil); c != 502 {
		t.Errorf("callback without a login in progress: %d, want the error page", c)
	}
}

func TestDemoLoginDisabledByDefault(t *testing.T) {
	srv := New(Config{})
	req := httptest.NewRequest("POST", "/auth/demo", nil)
	req.Header.Set("X-Requested-With", "odraz")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 404 && rec.Code != 405 { // no such route: the page handler answers
		t.Fatalf("status = %d, want 404/405 when no demo login hook is set", rec.Code)
	}
	// Without the hook the route is not registered at all, whatever the backends hold.
	if New(Config{Backends: []Backend{{ID: "nextcloud", Vendor: "nextcloud", URL: "http://x"}}}).hasDemo(Backend{ID: "nextcloud"}) {
		t.Error("a gateway without the hook must not offer a demo login")
	}
}

// Nextcloud has no browser (OIDC) sign-in: /api/v1/me must say so per backend, and say which have demo credentials.
func TestMeListsBackends(t *testing.T) {
	s := New(Config{PublicURL: "http://localhost:8080", Backends: []Backend{
		{ID: "opencloud", Vendor: "opencloud", URL: "https://localhost:9200"},
		{ID: "nextcloud", Vendor: "nextcloud", URL: "http://localhost:8082"},
	}, DemoLogin: func(b Backend) (string, string, bool) { return "alice", "x", b.ID == "nextcloud" }})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/me", nil))
	var me struct {
		Authenticated, Demo bool
		Backends            []struct {
			ID, Vendor            string
			OIDC, Demo, Connected bool
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Authenticated || !me.Demo || len(me.Backends) != 2 {
		t.Fatalf("me = %+v", me)
	}
	if b := me.Backends[0]; b.ID != "opencloud" || !b.OIDC || b.Demo || b.Connected {
		t.Errorf("opencloud = %+v", b)
	}
	if b := me.Backends[1]; b.ID != "nextcloud" || b.OIDC || !b.Demo || b.Connected {
		t.Errorf("nextcloud = %+v", b)
	}
	if authMethod(s.cfg.Backends[1]) != "app-password" || s.baseAccount(s.cfg.Backends[1]).Adapter != "nextcloud" {
		t.Error("nextcloud must use the app-password method and the nextcloud adapter")
	}
}

func TestUniqueName(t *testing.T) {
	used := map[string]bool{}
	var got []string
	for _, n := range []string{"Photos", "photos", "a.txt", "A.txt", "Photos", ""} {
		got = append(got, uniqueName(used, n))
	}
	want := []string{"Photos", "photos (2)", "a.txt", "A (2).txt", "Photos (3)", "item"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("name %d = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

// With WebDir the page comes from disk and an edit shows on the very next request: no restart.
func TestWebDirServesFromDisk(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	get := func(s *Server) string {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		return rec.Body.String()
	}
	write("<p>one</p>")
	live := New(Config{WebDir: dir, Backends: []Backend{{ID: "opencloud", Vendor: "opencloud", URL: "https://x"}}})
	if got := get(live); !strings.Contains(got, "one") {
		t.Fatalf("page = %q", got)
	}
	write("<p>two</p>")
	if got := get(live); !strings.Contains(got, "two") {
		t.Errorf("an edit must show without a restart, got %q", got)
	}
	if got := get(New(Config{Backends: []Backend{{ID: "opencloud", Vendor: "opencloud", URL: "https://x"}}})); !strings.Contains(got, "Odraz demo") {
		t.Errorf("without WebDir the embedded page is served, got %.80q", got)
	}
}
