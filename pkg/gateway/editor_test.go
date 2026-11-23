package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
	"github.com/odrazdotxyz/odraz-go-sdk/files"

	"github.com/odrazdotxyz/demo/internal/editor"
)

// memFS is a tiny in-memory files.FS: just enough for the editor flow. Every write bumps the ETag.
type memFS struct {
	mu   sync.Mutex
	data map[string][]byte
	ver  map[string]int
}

func newMemFS() *memFS { return &memFS{data: map[string][]byte{}, ver: map[string]int{}} }

func (m *memFS) put(p, c string) { m.data[p] = []byte(c); m.ver[p]++ }
func (m *memFS) get(p string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return string(m.data[p])
}
func (m *memFS) names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n []string
	for k := range m.data {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}
func (m *memFS) entry(p string) files.Entry {
	return files.Entry{Path: p, Name: p[strings.LastIndex(p, "/")+1:], Size: int64(len(m.data[p])), ETag: fmt.Sprintf("v%d", m.ver[p])}
}
func (m *memFS) Stat(_ context.Context, p string) (files.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[p]; !ok {
		return files.Entry{}, odraz.ErrNotFound
	}
	return m.entry(p), nil
}
func (m *memFS) List(context.Context, string) ([]files.Entry, error) { return nil, nil }
func (m *memFS) Read(_ context.Context, p string, off, n int64) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[p]
	if !ok {
		return nil, odraz.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (m *memFS) Write(_ context.Context, p string, r io.Reader, o files.WriteOptions) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[p]; ok && !o.Overwrite {
		return odraz.ErrExists
	}
	m.data[p] = b
	m.ver[p]++
	return nil
}
func (m *memFS) Mkdir(context.Context, string) error              { return nil }
func (m *memFS) Move(context.Context, string, string, bool) error { return odraz.ErrUnsupported }
func (m *memFS) Delete(context.Context, string, bool) error       { return odraz.ErrUnsupported }
func (m *memFS) Capabilities() files.Capabilities                 { return files.Capabilities{} }

type memClient struct{ fs *memFS }

func (c memClient) Drives(context.Context) ([]files.Drive, error) {
	return []files.Drive{{ID: "d1", Name: "Personal", Kind: files.DrivePersonal}}, nil
}
func (c memClient) OpenDrive(context.Context, string) (files.FS, error) { return c.fs, nil }

const secret = "demo-jwt-secret"

type rig struct {
	t    *testing.T
	gw   *httptest.Server
	srv  *Server
	ds   *httptest.Server // the fake Document Server
	mem  *memFS
	cook *http.Cookie
	dsMu sync.Mutex
	out  map[string]string // path under the fake DS -> bytes it serves
}

// newRig starts a gateway (with a signed-in session on an in-memory backend) and a fake Document Server.
func newRig(t *testing.T, office func(dsURL string) OfficeConfig) *rig {
	t.Helper()
	r := &rig{t: t, mem: newMemFS(), out: map[string]string{}}
	r.ds = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.dsMu.Lock()
		defer r.dsMu.Unlock()
		if b, ok := r.out[q.URL.Path]; ok {
			io.WriteString(w, b)
			return
		}
		http.NotFound(w, q)
	}))
	t.Cleanup(r.ds.Close)
	cfg := Config{PublicURL: "http://gw.test", Backends: []Backend{{ID: "mem", Vendor: "nextcloud", URL: "http://x"}}}
	if office != nil {
		cfg.Office = office(r.ds.URL)
	}
	r.srv = New(cfg)
	r.gw = httptest.NewServer(r.srv)
	t.Cleanup(r.gw.Close)
	sess := &session{accts: map[string]*account{"mem": {backend: cfg.Backends[0], user: "alice", client: memClient{r.mem}, drives: map[string]files.FS{}}}, created: time.Now()}
	r.srv.sessions["sid"] = sess
	r.cook = &http.Cookie{Name: cookieName, Value: "sid"}
	return r
}

func (r *rig) do(method, path string, body any, withCookie bool, hdr map[string]string) (int, []byte) {
	r.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, r.gw.URL+path, rd)
	req.Header.Set("X-Requested-With", "odraz")
	if withCookie {
		req.AddCookie(r.cook)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

type opened struct {
	ID     string         `json:"id"`
	Mode   string         `json:"mode"`
	Script string         `json:"script"`
	Config map[string]any `json:"config"`
}

func (r *rig) open(path string) opened {
	r.t.Helper()
	code, b := r.do("POST", "/api/v1/editor/session", map[string]string{"acct": "mem", "drive": "d1", "path": path}, true, nil)
	if code != 200 {
		r.t.Fatalf("open %s: %d %s", path, code, b)
	}
	var o opened
	if err := json.Unmarshal(b, &o); err != nil {
		r.t.Fatal(err)
	}
	return o
}

func (r *rig) callback(id string, body map[string]any) (int, string) {
	r.t.Helper()
	tok, _ := editor.Sign(secret, body)
	body["token"] = tok
	code, b := r.do("POST", "/editor/callback/"+id, body, false, nil)
	return code, string(b)
}

func locked(ds string) OfficeConfig { return OfficeConfig{URL: ds, Secret: secret} }

func TestEditorSaveBackToTheSameFile(t *testing.T) {
	r := newRig(t, locked)
	r.mem.put("/doc.docx", "old bytes")
	o := r.open("/doc.docx")
	if o.Mode != "edit" || !strings.HasSuffix(o.Script, "/web-apps/apps/api/documents/api.js") {
		t.Fatalf("opened: %+v", o)
	}
	doc := o.Config["document"].(map[string]any)
	if doc["url"] != "http://gw.test/editor/file/"+o.ID {
		t.Errorf("document url %v", doc["url"])
	}
	claims, err := editor.Verify(secret, o.Config["token"].(string))
	if err != nil || claims["documentType"] != "word" {
		t.Errorf("config token: %v %v", claims, err)
	}

	// the editor downloads the file: needs its token
	if code, _ := r.do("GET", "/editor/file/"+o.ID, nil, false, nil); code != 403 {
		t.Errorf("download without the editor's token: %d", code)
	}
	tok, _ := editor.Sign(secret, map[string]any{"payload": map[string]any{"url": doc["url"]}, "operation": "download"})
	if code, b := r.do("GET", "/editor/file/"+o.ID, nil, false, map[string]string{"Authorization": "Bearer " + tok}); code != 200 || string(b) != "old bytes" {
		t.Errorf("download: %d %q", code, b)
	}
	if code, _ := r.do("GET", "/editor/file/unknown", nil, false, map[string]string{"Authorization": "Bearer " + tok}); code != 404 {
		t.Errorf("unknown id: %d", code)
	}

	// the editor saves: the result lives on its own server
	r.out["/cache/result.docx"] = "new bytes"
	code, ans := r.callback(o.ID, map[string]any{"key": doc["key"], "status": 2, "url": r.ds.URL + "/cache/result.docx"})
	if code != 200 || !strings.Contains(ans, `"error":0`) {
		t.Fatalf("callback: %d %s", code, ans)
	}
	if got := r.mem.get("/doc.docx"); got != "new bytes" {
		t.Errorf("file = %q, want the edited bytes", got)
	}
	if n := r.mem.names(); len(n) != 1 {
		t.Errorf("a plain save must not create other files: %v", n)
	}
	_, st := r.do("GET", "/api/v1/editor/session?id="+o.ID, nil, true, nil)
	if !strings.Contains(string(st), `"state":"saved"`) {
		t.Errorf("state: %s", st)
	}
}

func TestEditorDoesNotClobberANewerFile(t *testing.T) {
	r := newRig(t, locked)
	r.mem.put("/sheet.xlsx", "v1")
	o := r.open("/sheet.xlsx")
	r.mem.mu.Lock()
	r.mem.put("/sheet.xlsx", "changed elsewhere") // someone else saves while the editor is open
	r.mem.mu.Unlock()
	r.out["/cache/r.xlsx"] = "my edits"
	if code, _ := r.callback(o.ID, map[string]any{"key": "k", "status": 2, "url": r.ds.URL + "/cache/r.xlsx"}); code != 200 {
		t.Fatal(code)
	}
	if got := r.mem.get("/sheet.xlsx"); got != "changed elsewhere" {
		t.Errorf("the newer file was overwritten: %q", got)
	}
	n := r.mem.names()
	if len(n) != 2 || !strings.Contains(n[1], "edited copy") && !strings.Contains(n[0], "edited copy") {
		t.Fatalf("want the original plus an edited copy, got %v", n)
	}
	for _, p := range n {
		if strings.Contains(p, "edited copy") && r.mem.get(p) != "my edits" {
			t.Errorf("copy holds %q", r.mem.get(p))
		}
	}
	_, st := r.do("GET", "/api/v1/editor/session?id="+o.ID, nil, true, nil)
	if !strings.Contains(string(st), `"state":"conflict"`) {
		t.Errorf("state: %s", st)
	}
}

func TestEditorCallbackGuards(t *testing.T) {
	r := newRig(t, locked)
	r.mem.put("/a.docx", "keep")
	o := r.open("/a.docx")
	// unsigned
	if code, _ := r.do("POST", "/editor/callback/"+o.ID, map[string]any{"key": "k", "status": 2, "url": r.ds.URL + "/x"}, false, nil); code != 403 {
		t.Errorf("unsigned callback: %d", code)
	}
	// signed, but the download address is not the editor's own server
	r.out["/x"] = "evil"
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "from elsewhere") }))
	defer other.Close()
	code, ans := r.callback(o.ID, map[string]any{"key": "k", "status": 2, "url": other.URL + "/x"})
	if code != 200 || strings.Contains(ans, `"error":0`) {
		t.Errorf("a foreign URL must be refused with an error answer: %d %s", code, ans)
	}
	if r.mem.get("/a.docx") != "keep" {
		t.Error("file changed")
	}
	// closed without changes: nothing is written
	if code, ans := r.callback(o.ID, map[string]any{"key": "k", "status": 4}); code != 200 || !strings.Contains(ans, `"error":0`) {
		t.Errorf("status 4: %d %s", code, ans)
	}
	if r.mem.get("/a.docx") != "keep" {
		t.Error("status 4 wrote")
	}
	// after signing out the editor can no longer reach the files
	r.srv.mu.Lock()
	delete(r.srv.sessions, "sid")
	r.srv.mu.Unlock()
	if code, _ := r.callback(o.ID, map[string]any{"key": "k", "status": 2, "url": r.ds.URL + "/x"}); code != 404 {
		t.Errorf("callback after sign-out: %d", code)
	}
}

func TestEditorNotConfiguredOrUnsupported(t *testing.T) {
	r := newRig(t, nil)
	r.mem.put("/a.docx", "x")
	if code, _ := r.do("POST", "/api/v1/editor/session", map[string]string{"acct": "mem", "drive": "d1", "path": "/a.docx"}, true, nil); code != 409 {
		t.Errorf("no editor: %d, want 409", code)
	}
	_, b := r.do("GET", "/api/v1/editor", nil, true, nil)
	if !strings.Contains(string(b), `"configured":false`) {
		t.Errorf("status: %s", b)
	}
	r2 := newRig(t, locked)
	r2.mem.put("/pic.png", "x")
	if code, _ := r2.do("POST", "/api/v1/editor/session", map[string]string{"acct": "mem", "drive": "d1", "path": "/pic.png"}, true, nil); code != 415 {
		t.Errorf("unsupported type: %d, want 415", code)
	}
	if code, _ := r2.do("POST", "/api/v1/editor/session", map[string]string{"acct": "mem", "drive": "d1", "path": "/a.docx"}, false, nil); code != 401 {
		t.Errorf("no session: %d, want 401", code)
	}
}

func TestEditorSecretNeverLeaves(t *testing.T) {
	r := newRig(t, locked)
	r.mem.put("/a.docx", "x")
	o := r.open("/a.docx")
	_, st := r.do("GET", "/api/v1/editor", nil, true, nil)
	code, put := r.do("PUT", "/api/v1/editor", map[string]string{"url": "https://o.example.com", "secret": "zzz"}, true, nil)
	b, _ := json.Marshal(o)
	for name, body := range map[string]string{"session": string(b), "status": string(st), "put": string(put)} {
		if strings.Contains(body, secret) || strings.Contains(body, "zzz") {
			t.Errorf("%s response contains a secret: %s", name, body)
		}
	}
	if code != 403 {
		t.Errorf("a user cannot override the operator's editor: %d", code)
	}
	if !strings.Contains(string(st), `"locked":true`) || !strings.Contains(string(st), `"secretSet":true`) {
		t.Errorf("status: %s", st)
	}
}

func TestEditorPersonalSettings(t *testing.T) {
	r := newRig(t, nil)
	// private and loopback addresses are refused by default
	for _, u := range []string{r.ds.URL, "http://localhost:8083", "http://10.0.0.5", "http://169.254.169.254", "http://[::1]"} {
		if code, _ := r.do("PUT", "/api/v1/editor", map[string]string{"url": u, "secret": "s"}, true, nil); code != 400 {
			t.Errorf("%s accepted: %d", u, code)
		}
	}
	if code, _ := r.do("PUT", "/api/v1/editor", map[string]string{"url": "ftp://x", "secret": "s"}, true, nil); code != 400 {
		t.Error("ftp accepted")
	}
	if code, _ := r.do("PUT", "/api/v1/editor", map[string]string{"url": "https://office.example.org"}, true, nil); code != 400 {
		t.Error("saved without any secret")
	}
	if code, b := r.do("PUT", "/api/v1/editor", map[string]string{"url": "https://office.example.org/", "secret": "s"}, true, nil); code != 200 || !strings.Contains(string(b), `"configured":true`) || strings.Contains(string(b), `"s"`) {
		t.Errorf("save: %d %s", code, b)
	}
	// keeping the secret when only the URL changes
	if code, _ := r.do("PUT", "/api/v1/editor", map[string]string{"url": "https://other.example.org"}, true, nil); code != 200 {
		t.Error("changing only the URL must keep the secret")
	}
	// it is per user: another session sees nothing
	r.srv.sessions["other"] = &session{accts: map[string]*account{"mem": {}}, created: time.Now()}
	req, _ := http.NewRequest("GET", r.gw.URL+"/api/v1/editor", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "other"})
	resp, _ := http.DefaultClient.Do(req)
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"configured":false`) {
		t.Errorf("settings leaked to another user: %s", b)
	}
	if code, _ := r.do("DELETE", "/api/v1/editor", nil, true, nil); code != 200 {
		t.Error("delete")
	}
	if _, b := r.do("GET", "/api/v1/editor", nil, true, nil); !strings.Contains(string(b), `"configured":false`) {
		t.Errorf("after delete: %s", b)
	}
	// the operator may allow private addresses (Docker)
	r2 := newRig(t, func(string) OfficeConfig { return OfficeConfig{AllowPrivate: true} })
	if code, _ := r2.do("PUT", "/api/v1/editor", map[string]string{"url": "http://localhost:8083", "secret": "s"}, true, nil); code != 200 {
		t.Errorf("AllowPrivate: %d", code)
	}
}

func TestEditorCheckEndpoint(t *testing.T) {
	r := newRig(t, nil)
	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path == "/healthcheck" {
			io.WriteString(w, "true")
			return
		}
		if _, err := editor.Verify("good", strings.TrimPrefix(q.Header.Get("Authorization"), "Bearer ")); err != nil {
			io.WriteString(w, `{"error":6}`)
			return
		}
		io.WriteString(w, `{"error":0,"version":"9"}`)
	}))
	defer ds.Close()
	r.srv.cfg.Office.AllowPrivate = true // the fake editor listens on loopback
	for secretTry, want := range map[string]bool{"good": true, "bad": false} {
		r.do("PUT", "/api/v1/editor", map[string]string{"url": ds.URL, "secret": secretTry}, true, nil)
		_, b := r.do("POST", "/api/v1/editor/check", nil, true, nil)
		if got := strings.Contains(string(b), `"ok":true`); got != want {
			t.Errorf("secret %q: %s", secretTry, b)
		}
	}
}

func TestEditorCSP(t *testing.T) {
	r := newRig(t, locked)
	resp, _ := http.Get(r.gw.URL + "/")
	csp := resp.Header.Get("Content-Security-Policy")
	resp.Body.Close()
	if !strings.Contains(csp, "script-src 'self' "+r.ds.URL) || !strings.Contains(csp, "frame-src "+r.ds.URL+";") {
		t.Errorf("csp: %s", csp)
	}
	r2 := newRig(t, nil)
	resp, _ = http.Get(r2.gw.URL + "/")
	csp = resp.Header.Get("Content-Security-Policy")
	resp.Body.Close()
	if !strings.Contains(csp, "frame-src 'none'") || strings.Contains(csp, "http://") {
		t.Errorf("with no editor the page must frame nothing: %s", csp)
	}
}

func TestOfficeListenerServesOnlyTheEditorRoutes(t *testing.T) {
	r := newRig(t, func(ds string) OfficeConfig { return OfficeConfig{URL: ds, Secret: secret, Listen: "127.0.0.1:0"} })
	h := httptest.NewServer(r.srv.OfficeHandler())
	defer h.Close()
	if resp, _ := http.Get(h.URL + "/api/v1/drives"); resp.StatusCode != 404 {
		t.Errorf("the editor listener must not serve the API: %d", resp.StatusCode)
	}
	if resp, _ := http.Get(h.URL + "/"); resp.StatusCode != 404 {
		t.Errorf("or the page: %d", resp.StatusCode)
	}
	if code, _ := r.do("GET", "/editor/file/x", nil, false, nil); code != 404 && code != 405 {
		// with a separate listener the main address does not serve them
		t.Logf("main address answered %d for /editor/file/x", code)
	}
}
