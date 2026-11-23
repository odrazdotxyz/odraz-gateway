package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
	"github.com/odrazdotxyz/odraz-go-sdk/files"

	"github.com/odrazdotxyz/demo/internal/editor"
)

// The document editor. The gateway is the editor's "host": the editor server (ONLYOFFICE Document Server, run
// by the user or operator, never bundled) downloads the file from the gateway, which reads it through the SDK,
// and reports saves to the gateway, which writes the result back through the SDK. Documents from any backend
// therefore open in the same editor. See docs/editor.md.

// OfficeConfig is the operator's editor setting. If URL is set it wins over every user's own setting.
type OfficeConfig struct {
	URL    string // the editor server, as browsers and the gateway reach it, e.g. https://office.example.com
	Secret string // its shared JWT secret
	// AllowPrivate lets a user-supplied editor URL point at private and loopback addresses (needed when the editor
	// runs in the same Docker network). The operator's own URL is never restricted.
	AllowPrivate bool
	// GatewayURL is where the EDITOR reaches this gateway, when that differs from PublicURL (Docker).
	GatewayURL string
	// Listen, if set, serves the two routes the editor calls (/editor/file, /editor/callback) on this address
	// instead of the main one, so nothing else is reachable from the editor's network. Use Server.OfficeHandler.
	Listen string
}

// officeSettings is one editor server and its secret.
type officeSettings struct{ URL, Secret string }

const (
	maxEditorFile   = 256 << 20 // largest document saved back
	editSessionTTL  = 12 * time.Hour
	editorCheckWait = 20 * time.Second
)

// editSession is one open document: it ties the editor's unauthenticated calls to the signed-in user, the file
// and the version the editor opened.
type editSession struct {
	id        string
	sess      *session
	acct      string
	drive     string
	path      string
	version   string // the file's version stamp when the editor opened it
	office    officeSettings
	allowPriv bool
	expires   time.Time

	mu       sync.Mutex
	state    string // "open", "saved", "unchanged", "conflict", "error"
	savedTo  string // the edited copy's path after a conflict
	message  string
	saves    int
	lastSave time.Time
}

func versionOf(e files.Entry) string {
	if e.ETag != "" {
		return e.ETag
	}
	return strconv.FormatInt(e.Size, 10) + "@" + e.ModTime.UTC().Format(time.RFC3339Nano)
}

// officeFor returns the editor setting that applies to this session: the operator's, else the user's own.
func (s *Server) officeFor(sess *session) (st officeSettings, locked, ok bool) {
	if s.cfg.Office.URL != "" {
		return officeSettings{URL: s.cfg.Office.URL, Secret: s.cfg.Office.Secret}, true, true
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.office, false, sess.office.URL != "" && sess.office.Secret != ""
}

func (s *Server) provider(st officeSettings, locked bool) (*editor.ONLYOFFICE, error) {
	u, err := editor.ParseURL(st.URL)
	if err != nil {
		return nil, err
	}
	return &editor.ONLYOFFICE{URL: u, Secret: st.Secret, Client: editor.NewClient(locked || s.cfg.Office.AllowPrivate, 60*time.Second)}, nil
}

// officeOrigin is the editor's origin (scheme://host) for the page's content security policy, "" if none.
func (s *Server) officeOrigin(r *http.Request) string {
	raw := s.cfg.Office.URL
	if raw == "" {
		sess := s.lookup(r)
		if sess == nil {
			return ""
		}
		sess.mu.Lock()
		raw = sess.office.URL
		sess.mu.Unlock()
	}
	u, err := editor.ParseURL(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// ---- settings: GET/PUT/DELETE /api/v1/editor, POST /api/v1/editor/check ----

func (s *Server) editorStatus(sess *session) map[string]any {
	st, locked, ok := s.officeFor(sess)
	out := map[string]any{"configured": ok, "locked": locked, "provider": "onlyoffice", "persistent": false}
	if st.URL != "" {
		out["url"] = st.URL
	}
	out["secretSet"] = st.Secret != "" // never the secret itself
	return out
}

func (s *Server) handleEditorGet(w http.ResponseWriter, r *http.Request, sess *session) {
	writeJSON(w, http.StatusOK, s.editorStatus(sess))
}

// PUT /api/v1/editor {url, secret}: the user's own editor server. An empty secret keeps the saved one.
func (s *Server) handleEditorPut(w http.ResponseWriter, r *http.Request, sess *session) {
	if s.cfg.Office.URL != "" {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "the editor is managed by your administrator", "code": "permission"})
		return
	}
	var in struct{ URL, Secret string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request body", "code": "invalid_path"})
		return
	}
	u, err := editor.ParseURL(in.URL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "code": "invalid_path"})
		return
	}
	sess.mu.Lock()
	secret := in.Secret
	if secret == "" {
		secret = sess.office.Secret
	}
	sess.mu.Unlock()
	if secret == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "give the editor's secret", "code": "invalid_path"})
		return
	}
	if !s.cfg.Office.AllowPrivate { // refuse private addresses up front, with a clear message; the dialer checks again
		if err := checkHostPublic(r.Context(), u.Hostname()); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "that address is private or internal and not allowed here", "code": "blocked"})
			return
		}
	}
	sess.mu.Lock()
	sess.office = officeSettings{URL: u.String(), Secret: secret}
	sess.mu.Unlock()
	writeJSON(w, http.StatusOK, s.editorStatus(sess))
}

func (s *Server) handleEditorDelete(w http.ResponseWriter, r *http.Request, sess *session) {
	if s.cfg.Office.URL != "" {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "the editor is managed by your administrator", "code": "permission"})
		return
	}
	sess.mu.Lock()
	sess.office = officeSettings{}
	sess.mu.Unlock()
	writeJSON(w, http.StatusOK, s.editorStatus(sess))
}

// POST /api/v1/editor/check: is the saved editor reachable and does it accept our secret?
func (s *Server) handleEditorCheck(w http.ResponseWriter, r *http.Request, sess *session) {
	st, locked, ok := s.officeFor(sess)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "no editor is set up"})
		return
	}
	p, err := s.provider(st, locked)
	if err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), editorCheckWait)
		defer cancel()
		err = p.Check(ctx)
	}
	if err != nil {
		msg := err.Error()
		if errors.Is(err, editor.ErrBlocked) {
			msg = "that address is private or internal and not allowed here"
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- opening a document: POST /api/v1/editor/session ----

func randomID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// gatewayBase is where the editor server reaches this gateway.
func (s *Server) gatewayBase() string {
	if s.cfg.Office.GatewayURL != "" {
		return strings.TrimRight(s.cfg.Office.GatewayURL, "/")
	}
	return strings.TrimRight(s.cfg.PublicURL, "/")
}

func (s *Server) handleEditorSession(w http.ResponseWriter, r *http.Request, sess *session) {
	st, locked, ok := s.officeFor(sess)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "no document editor is set up", "code": "unsupported"})
		return
	}
	var in struct{ Acct, Drive, Path string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request body", "code": "invalid_path"})
		return
	}
	p, err := s.provider(st, locked)
	if err != nil {
		writeError(w, err)
		return
	}
	fs, err := s.openFS(r.Context(), sess, in.Acct, in.Drive)
	if err != nil {
		writeError(w, err)
		return
	}
	filePath, err := files.CleanPath(in.Path)
	if err != nil {
		writeError(w, err)
		return
	}
	e, err := fs.Stat(r.Context(), filePath)
	if err != nil {
		writeError(w, err)
		return
	}
	ext, kind, supported := editor.KindOf(e.Name)
	if e.IsDir || !supported {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "the editor cannot open this kind of file", "code": "unsupported"})
		return
	}
	if e.Size > maxEditorFile {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "this file is too large for the editor", "code": "error"})
		return
	}
	readOnly := fs.Capabilities().ReadOnly
	if d := s.driveOf(r.Context(), sess, in.Acct, in.Drive); d != nil && d.ReadOnly {
		readOnly = true
	}

	a := sess.account(in.Acct)
	user := ""
	if a != nil {
		user = a.user
	}
	uid := sha256.Sum256([]byte(in.Acct + "\x00" + user))
	if user == "" {
		user = "User"
	}
	es := &editSession{id: randomID(), sess: sess, acct: in.Acct, drive: in.Drive, path: filePath, version: versionOf(e),
		office: st, allowPriv: locked || s.cfg.Office.AllowPrivate, expires: time.Now().Add(editSessionTTL), state: "open"}
	base := s.gatewayBase()
	cfg, err := p.Config(editor.Params{FileName: e.Name, Ext: ext, Kind: kind, Edit: !readOnly,
		Key: editor.Key(in.Acct, in.Drive, filePath, es.version), FileURL: base + "/editor/file/" + es.id,
		CallbackURL: base + "/editor/callback/" + es.id, UserID: hex.EncodeToString(uid[:8]), UserName: user, Lang: "en"})
	if err != nil {
		writeError(w, err)
		return
	}
	s.addEdit(es)
	mode := "view"
	if !readOnly && kind.Editable {
		mode = "edit"
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": es.id, "script": p.APIScript(), "mode": mode, "config": cfg})
}

func (s *Server) addEdit(es *editSession) {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	now := time.Now()
	for id, old := range s.edits {
		if now.After(old.expires) {
			delete(s.edits, id)
		}
	}
	s.edits[es.id] = es
}

func (s *Server) edit(id string) *editSession {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	es := s.edits[id]
	if es == nil || time.Now().After(es.expires) {
		return nil
	}
	return es
}

func (s *Server) forgetEdit(id string) {
	s.editMu.Lock()
	delete(s.edits, id)
	s.editMu.Unlock()
}

// live reports whether the user's gateway session is still in place: signing out ends what the editor may do.
func (s *Server) live(sess *session) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.sessions {
		if x == sess {
			return true
		}
	}
	return false
}

// GET /api/v1/editor/session?id=: how the save of an open document is going (the page polls it after closing).
func (s *Server) handleEditorState(w http.ResponseWriter, r *http.Request, sess *session) {
	es := s.edit(r.URL.Query().Get("id"))
	if es == nil || es.sess != sess {
		writeJSON(w, http.StatusOK, map[string]any{"state": "gone"})
		return
	}
	es.mu.Lock()
	defer es.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"state": es.state, "savedTo": es.savedTo, "message": es.message, "saves": es.saves})
}

// ---- the editor's own calls (no cookie: an unguessable id plus the editor's signed token) ----

// OfficeHandler serves only the routes the editor server calls. With OfficeConfig.Listen set, serve it there.
func (s *Server) OfficeHandler() http.Handler {
	m := http.NewServeMux()
	s.officeRoutes(m)
	return m
}

func (s *Server) officeRoutes(m *http.ServeMux) {
	reg := func(pattern string, h http.HandlerFunc) {
		if m == s.mux {
			s.routes = append(s.routes, pattern)
		}
		m.HandleFunc(pattern, h)
	}
	reg("GET /editor/file/{id}", s.handleEditorFile)
	reg("POST /editor/callback/{id}", s.handleEditorCallback)
}

func (s *Server) editorOf(w http.ResponseWriter, r *http.Request) (*editSession, *editor.ONLYOFFICE) {
	es := s.edit(r.PathValue("id"))
	if es == nil || !s.live(es.sess) {
		http.NotFound(w, r)
		return nil, nil
	}
	u, err := editor.ParseURL(es.office.URL)
	if err != nil {
		http.Error(w, "editor not configured", http.StatusServiceUnavailable)
		return nil, nil
	}
	return es, &editor.ONLYOFFICE{URL: u, Secret: es.office.Secret, Client: editor.NewClient(es.allowPriv, 120*time.Second)}
}

func (s *Server) handleEditorFile(w http.ResponseWriter, r *http.Request) {
	es, p := s.editorOf(w, r)
	if es == nil {
		return
	}
	if err := p.VerifyRequest(r.Header.Get("Authorization")); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	fs, err := s.openFS(r.Context(), es.sess, es.acct, es.drive)
	if err != nil {
		http.Error(w, "unavailable", http.StatusBadGateway)
		return
	}
	st, err := fs.Stat(r.Context(), es.path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rc, err := fs.Read(r.Context(), es.path, 0, -1)
	if err != nil {
		http.Error(w, "unavailable", http.StatusBadGateway)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size, 10))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleEditorCallback(w http.ResponseWriter, r *http.Request) {
	es, p := s.editorOf(w, r)
	if es == nil {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	cb, err := p.ParseCallback(body, r.Header.Get("Authorization"))
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	reply := func(code int) { writeJSON(w, http.StatusOK, map[string]any{"error": code}) }
	switch cb.Status {
	case editor.StatusEditing:
		reply(0)
	case editor.StatusNoChanges:
		es.set("unchanged", "", "")
		s.forgetEditLater(es)
		reply(0)
	case editor.StatusSaveError, editor.StatusForceError:
		es.set("error", "", "the editor could not save the document")
		reply(0)
	case editor.StatusReady, editor.StatusForceSaved:
		if err := s.saveBack(r.Context(), es, p, cb); err != nil {
			es.set("error", "", err.Error())
			reply(1) // the editor keeps the document and tells the user
			return
		}
		if cb.Status == editor.StatusReady {
			s.forgetEditLater(es)
		}
		reply(0)
	default:
		reply(0)
	}
}

// forgetEditLater keeps a finished edit session briefly so the page can read its final state.
func (s *Server) forgetEditLater(es *editSession) {
	time.AfterFunc(2*time.Minute, func() { s.forgetEdit(es.id) })
}

func (es *editSession) set(state, savedTo, msg string) {
	es.mu.Lock()
	es.state, es.savedTo, es.message = state, savedTo, msg
	es.mu.Unlock()
}

// saveBack downloads the edited document from the editor and writes it where it came from. The URL comes from the
// editor's signed message but is still required to be on the editor's own origin, so a forged or odd message
// cannot make the gateway fetch something else. If the file changed since the editor opened it, the edited
// document is written next to it as a copy instead of replacing the newer version.
func (s *Server) saveBack(ctx context.Context, es *editSession, p *editor.ONLYOFFICE, cb editor.Callback) error {
	if !editor.SameOrigin(p.URL, cb.URL) {
		return errors.New("the editor sent a download address outside its own server")
	}
	fs, err := s.openFS(ctx, es.sess, es.acct, es.drive)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cb.URL, nil)
	if tok, err := editor.Sign(p.Secret, map[string]any{"payload": map[string]any{"url": cb.URL}, "operation": "download"}); err == nil {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return fmt.Errorf("could not fetch the edited document: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("could not fetch the edited document (HTTP %d)", resp.StatusCode)
	}
	if resp.ContentLength > maxEditorFile {
		return errors.New("the edited document is too large")
	}
	body := io.LimitReader(resp.Body, maxEditorFile+1)
	size := resp.ContentLength

	target, overwrite, state, note := es.path, true, "saved", ""
	cur, err := fs.Stat(ctx, es.path)
	switch {
	case err == nil && versionOf(cur) != es.version:
		// Changed behind the editor's back: keep both.
		dir, name := files.Split(es.path)
		ext := path.Ext(name)
		target = joinPath(dir, strings.TrimSuffix(name, ext)+" (edited copy "+time.Now().Format("2006-01-02 1504")+")"+ext)
		overwrite, state = false, "conflict"
		note = "the file changed while you were editing, so your version was saved as a copy"
	case err != nil && !errors.Is(err, odraz.ErrNotFound):
		return err
	}
	if err := files.Upload(ctx, fs, target, body, files.WriteOptions{Size: size, Overwrite: overwrite}); err != nil {
		return err
	}
	if state == "saved" {
		if st, err := fs.Stat(ctx, es.path); err == nil {
			es.mu.Lock()
			es.version = versionOf(st) // later saves of the same open document compare against our own write
			es.mu.Unlock()
		}
	}
	es.mu.Lock()
	es.saves++
	es.lastSave = time.Now()
	es.mu.Unlock()
	saved := ""
	if state == "conflict" {
		saved = target
	}
	es.set(state, saved, note)
	return nil
}

func joinPath(dir, name string) string {
	if dir == "/" || dir == "" {
		return "/" + name
	}
	return dir + "/" + name
}

// driveOf finds one drive's description, to learn whether it is read-only.
func (s *Server) driveOf(ctx context.Context, sess *session, acct, id string) *files.Drive {
	a := sess.account(acct)
	if a == nil {
		return nil
	}
	ds, err := a.client.Drives(ctx)
	if err != nil {
		return nil
	}
	for i := range ds {
		if ds[i].ID == id {
			return &ds[i]
		}
	}
	return nil
}

func (sess *session) account(id string) *account {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.accts[id]
}
