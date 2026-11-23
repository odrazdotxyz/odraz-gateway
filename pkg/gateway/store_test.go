package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odrazdotxyz/odraz-go-sdk/accounts"
)

func key(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestFileStoreIsEncryptedAndPersistent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sessions.bin")
	k := key(t)
	st, err := NewFileStore(path, k)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Secrets("s1").Put(ctx, "acct", accounts.SecretPassword, []byte("hunter2-token")); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(ctx, &SessionData{ID: "s1", Created: time.Now(), OfficeURL: "https://office.example", OfficeSecret: "office-jwt-secret"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"hunter2-token", "office-jwt-secret", "office.example", "s1"} {
		if bytes.Contains(raw, []byte(leak)) {
			t.Errorf("the file holds %q in the clear", leak)
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, want 0600", fi.Mode().Perm())
	}

	// a new process with the same key sees everything
	again, err := NewFileStore(path, k)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := again.Load(ctx, "s1")
	if d == nil || d.OfficeSecret != "office-jwt-secret" {
		t.Fatalf("after reopening: %+v", d)
	}
	if v, err := again.Secrets("s1").Get(ctx, "acct", accounts.SecretPassword); err != nil || string(v) != "hunter2-token" {
		t.Errorf("secret after reopening: %q %v", v, err)
	}
	if v, _ := again.Load(ctx, "nope"); v != nil {
		t.Error("unknown session found")
	}

	// the wrong key and a modified file are refused, not silently replaced by an empty store
	if _, err := NewFileStore(path, key(t)); err == nil {
		t.Error("wrong key accepted")
	}
	raw[len(raw)-1] ^= 1
	os.WriteFile(path, raw, 0o600)
	if _, err := NewFileStore(path, k); err == nil {
		t.Error("tampered file accepted")
	}
	if _, err := NewFileStore(path, []byte("short")); err == nil {
		t.Error("short key accepted")
	}
}

func TestStoreDeleteAndSweep(t *testing.T) {
	ctx := context.Background()
	for name, st := range map[string]Store{"memory": NewMemoryStore(), "file": func() Store { s, _ := NewFileStore(filepath.Join(t.TempDir(), "s"), key(t)); return s }()} {
		st.Save(ctx, &SessionData{ID: "old", Created: time.Now().Add(-48 * time.Hour)})
		st.Save(ctx, &SessionData{ID: "new", Created: time.Now()})
		st.Secrets("new").Put(ctx, "a", "k", []byte("v"))
		st.Sweep(ctx, time.Now().Add(-time.Hour))
		if d, _ := st.Load(ctx, "old"); d != nil {
			t.Errorf("%s: old session survived the sweep", name)
		}
		if d, _ := st.Load(ctx, "new"); d == nil {
			t.Errorf("%s: new session swept", name)
		}
		st.Delete(ctx, "new")
		if _, err := st.Secrets("new").Get(ctx, "a", "k"); err == nil {
			t.Errorf("%s: secret survived delete", name)
		}
	}
}

func TestParseKey(t *testing.T) {
	if _, err := ParseKey("not base64 !!"); err == nil {
		t.Error("garbage accepted")
	}
	if _, err := ParseKey("c2hvcnQ="); err == nil {
		t.Error("short key accepted")
	}
	if k, err := ParseKey("AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="); err != nil || len(k) != 32 {
		t.Errorf("good key: %v", err)
	}
}

// A session survives a gateway restart: the cookie still works, the connected server is still connected, the
// user's editor setting is still there, and signing out removes it for good.
func TestSessionSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sessions.bin")
	k := key(t)
	cfg := func(st Store) Config {
		return Config{PublicURL: "http://gw.test", Store: st, Backends: []Backend{{ID: "nextcloud", Vendor: "nextcloud", URL: "http://127.0.0.1:1"}}}
	}
	st1, _ := NewFileStore(path, k)
	// what a sign-in leaves behind: secrets in the store, the account record in the session
	st1.Secrets("sid").Put(ctx, "nextcloud", accounts.SecretUsername, []byte("alice"))
	st1.Secrets("sid").Put(ctx, "nextcloud", accounts.SecretPassword, []byte("app-password"))
	acc := accounts.Account{ID: "nextcloud", Adapter: "nextcloud", Config: map[string]string{"url": "http://127.0.0.1:1"}}
	st1.Save(ctx, &SessionData{ID: "sid", Created: time.Now(), OfficeURL: "https://office.example.org", OfficeSecret: "s3",
		Accounts: []AccountRecord{{Backend: "nextcloud", Account: acc, Method: "app-password", User: "alice"}}})

	// "restart": a new Server over a store re-read from the file
	st2, err := NewFileStore(path, k)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(cfg(st2)).Handler())
	defer srv.Close()
	get := func(p string) string {
		req, _ := http.NewRequest("GET", srv.URL+p, nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: "sid"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b strings.Builder
		buf := make([]byte, 4096)
		for n, _ := resp.Body.Read(buf); n > 0; n, _ = resp.Body.Read(buf) {
			b.Write(buf[:n])
		}
		return b.String()
	}
	if me := get("/api/v1/me"); !strings.Contains(me, `"authenticated":true`) || !strings.Contains(me, `"user":"alice"`) {
		t.Errorf("after a restart the session is gone: %s", me)
	}
	if ed := get("/api/v1/editor"); !strings.Contains(ed, `"url":"https://office.example.org"`) || !strings.Contains(ed, `"persistent":true`) || strings.Contains(ed, `"s3"`) {
		t.Errorf("editor setting after a restart: %s", ed)
	}

	// signing out removes it from the store, so another restart does not bring it back
	req, _ := http.NewRequest("POST", srv.URL+"/auth/logout", nil)
	req.Header.Set("X-Requested-With", "odraz")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "sid"})
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("logout: %v", err)
	}
	st3, _ := NewFileStore(path, k)
	if d, _ := st3.Load(ctx, "sid"); d != nil {
		t.Error("a signed-out session is still in the store")
	}
	// an expired session is not revived
	st3.Save(ctx, &SessionData{ID: "old", Created: time.Now().Add(-sessionTTL - time.Hour)})
	srv2 := httptest.NewServer(New(cfg(st3)).Handler())
	defer srv2.Close()
	req, _ = http.NewRequest("GET", srv2.URL+"/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "old"})
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if d, _ := st3.Load(ctx, "old"); d != nil {
		t.Error("an expired session was kept")
	}
}
