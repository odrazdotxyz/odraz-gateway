package editor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestJWT(t *testing.T) {
	tok, err := Sign("s3cret", map[string]any{"a": "b"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Verify("s3cret", tok)
	if err != nil || c["a"] != "b" {
		t.Fatalf("round trip: %v %v", c, err)
	}
	if _, err := Verify("other", tok); err == nil {
		t.Error("wrong secret accepted")
	}
	if _, err := Verify("", tok); err == nil {
		t.Error("empty secret accepted")
	}
	parts := strings.Split(tok, ".")
	tampered := parts[0] + "." + b64.EncodeToString([]byte(`{"a":"evil"}`)) + "." + parts[2]
	if _, err := Verify("s3cret", tampered); err == nil {
		t.Error("tampered payload accepted")
	}
	none := b64.EncodeToString([]byte(`{"alg":"none"}`)) + "." + parts[1] + "."
	if _, err := Verify("s3cret", none); err == nil {
		t.Error(`alg "none" accepted`)
	}
	exp, _ := Sign("s3cret", map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})
	if _, err := Verify("s3cret", exp); err == nil {
		t.Error("expired token accepted")
	}
}

func TestKey(t *testing.T) {
	k := Key("opencloud", "drive", "/a/ü b.docx", "etag-1")
	if len(k) > 128 || strings.Trim(k, "0123456789abcdefghijklmnopqrstuvwxyz") != "" {
		t.Errorf("key %q breaks the editor's rules", k)
	}
	if k == Key("opencloud", "drive", "/a/ü b.docx", "etag-2") {
		t.Error("a new version must give a new key")
	}
	if k == Key("nextcloud", "drive", "/a/ü b.docx", "etag-1") {
		t.Error("another backend must give another key")
	}
}

func TestKindOf(t *testing.T) {
	for name, want := range map[string]struct{ ok, edit bool }{
		"a.DOCX": {true, true}, "b.xlsx": {true, true}, "c.odp": {true, true}, "d.txt": {true, true}, "e.csv": {true, true},
		"f.pdf": {true, false}, "g.doc": {true, false}, "h.png": {false, false}, "noext": {false, false},
	} {
		_, k, ok := KindOf(name)
		if ok != want.ok || (ok && k.Editable != want.edit) {
			t.Errorf("%s: ok=%v editable=%v, want %+v", name, ok, k.Editable, want)
		}
	}
}

func TestIsBlocked(t *testing.T) {
	for ip, want := range map[string]bool{"127.0.0.1": true, "::1": true, "10.1.2.3": true, "192.168.0.5": true, "172.16.0.1": true,
		"169.254.169.254": true, "100.64.0.1": true, "0.0.0.0": true, "::ffff:127.0.0.1": true, "8.8.8.8": false, "2606:4700::1111": false} {
		if got := IsBlocked(netip.MustParseAddr(ip)); got != want {
			t.Errorf("IsBlocked(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestClientRefusesPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hi")) }))
	defer srv.Close()
	if _, err := NewClient(false, time.Second).Get(srv.URL); err == nil || !errors.Is(err, ErrBlocked) {
		t.Errorf("loopback reached with allowPrivate=false: %v", err)
	}
	resp, err := NewClient(true, time.Second).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestRedirectsNotFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://169.254.169.254/", 302) }))
	defer srv.Close()
	resp, err := NewClient(true, time.Second).Get(srv.URL)
	if err != nil || resp.StatusCode != 302 {
		t.Fatalf("got %v %v", resp, err)
	}
	resp.Body.Close()
}

func TestParseURLAndSameOrigin(t *testing.T) {
	for _, bad := range []string{"", "ftp://x", "http://", "http://u:p@host", "//host", "javascript:alert(1)"} {
		if _, err := ParseURL(bad); err == nil {
			t.Errorf("ParseURL(%q) accepted", bad)
		}
	}
	u, err := ParseURL(" https://office.example.com/prefix/?x=1 ")
	if err != nil || u.String() != "https://office.example.com/prefix" {
		t.Fatalf("got %v %v", u, err)
	}
	if !SameOrigin(u, "https://office.example.com/cache/x.docx") {
		t.Error("same origin refused")
	}
	for _, bad := range []string{"http://office.example.com/x", "https://office.example.com.evil.net/x", "https://evil@office.example.com/x", "https://office.example.com:8443/x", "/x"} {
		if SameOrigin(u, bad) {
			t.Errorf("SameOrigin accepted %q", bad)
		}
	}
}

func TestParseCallback(t *testing.T) {
	o := &ONLYOFFICE{Secret: "k"}
	body := `{"key":"abc","status":2,"url":"http://x/y"}`
	// token in the body: the signed claims are the body fields
	tok, _ := Sign("k", map[string]any{"key": "abc", "status": 2, "url": "http://signed/y"})
	cb, err := o.ParseCallback([]byte(`{"key":"abc","status":2,"url":"http://evil/y","token":"`+tok+`"}`), "")
	if err != nil || cb.URL != "http://signed/y" {
		t.Errorf("body token: %+v %v (the signed URL, not the unsigned one, must win)", cb, err)
	}
	// token in the header: claims are wrapped in "payload"
	htok, _ := Sign("k", map[string]any{"payload": map[string]any{"key": "abc", "status": 4}, "operation": "callback"})
	cb, err = o.ParseCallback([]byte(body), "Bearer "+htok)
	if err != nil || cb.Status != 4 {
		t.Errorf("header token: %+v %v", cb, err)
	}
	if _, err := o.ParseCallback([]byte(body), ""); err == nil {
		t.Error("unsigned callback accepted")
	}
	bad, _ := Sign("other", map[string]any{"status": 2})
	if _, err := o.ParseCallback([]byte(body), "Bearer "+bad); err == nil {
		t.Error("callback signed with another secret accepted")
	}
}

func TestConfigSignedAndSecretFree(t *testing.T) {
	u, _ := ParseURL("http://office.test")
	o := &ONLYOFFICE{URL: u, Secret: "topsecret"}
	cfg, err := o.Config(Params{FileName: "a.docx", Ext: "docx", Kind: formats["docx"], Edit: true, Key: "k1", FileURL: "http://gw/f", CallbackURL: "http://gw/c", UserID: "u", UserName: "U", Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Verify("topsecret", cfg["token"].(string))
	if err != nil || claims["documentType"] != "word" {
		t.Fatalf("token: %v %v", claims, err)
	}
	if ec := cfg["editorConfig"].(map[string]any); ec["mode"] != "edit" || ec["callbackUrl"] != "http://gw/c" {
		t.Errorf("editorConfig: %v", ec)
	}
	ro, _ := o.Config(Params{FileName: "a.pdf", Ext: "pdf", Kind: formats["pdf"], Edit: true, Key: "k2"})
	if ro["editorConfig"].(map[string]any)["mode"] != "view" {
		t.Error("a pdf must open read-only")
	}
}

func TestCheck(t *testing.T) {
	const secret = "good"
	mux := http.NewServeMux()
	mux.HandleFunc("/healthcheck", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("true")) })
	mux.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		if _, err := Verify(secret, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); err != nil {
			w.Write([]byte(`{"error":6}`))
			return
		}
		w.Write([]byte(`{"error":0,"version":"9.0.0"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u, _ := ParseURL(srv.URL)
	if err := (&ONLYOFFICE{URL: u, Secret: secret, Client: NewClient(true, 5*time.Second)}).Check(context.Background()); err != nil {
		t.Errorf("good secret: %v", err)
	}
	if err := (&ONLYOFFICE{URL: u, Secret: "bad", Client: NewClient(true, 5*time.Second)}).Check(context.Background()); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Errorf("bad secret: %v", err)
	}
}
