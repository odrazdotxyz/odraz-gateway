package editor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Kind says how a file type is opened.
type Kind struct {
	DocumentType string // ONLYOFFICE documentType: word, cell, slide, pdf
	Editable     bool   // false: open read-only
}

// Formats we offer. Editable ones are saved back in the same format; the others (old binary formats, PDF) would
// come back converted, so they open read-only.
var formats = map[string]Kind{
	"docx": {"word", true}, "odt": {"word", true}, "txt": {"word", true},
	"xlsx": {"cell", true}, "ods": {"cell", true}, "csv": {"cell", true},
	"pptx": {"slide", true}, "odp": {"slide", true},
	"doc": {"word", false}, "rtf": {"word", false}, "xls": {"cell", false}, "ppt": {"slide", false}, "pdf": {"pdf", false},
}

// KindOf returns how a file name would open, and false if the editor does not handle its type.
func KindOf(name string) (ext string, k Kind, ok bool) {
	ext = strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	k, ok = formats[ext]
	return
}

// ONLYOFFICE is one ONLYOFFICE Document Server.
type ONLYOFFICE struct {
	URL    *url.URL // as browsers and the gateway reach it
	Secret string   // the shared JWT secret
	Client *http.Client
}

// APIScript is the URL of the browser API script.
func (o *ONLYOFFICE) APIScript() string {
	return o.URL.String() + "/web-apps/apps/api/documents/api.js"
}

// Key derives the document key. The editor treats equal keys as the same document version, so it must change
// whenever the file's content does: it is a hash of where the file is and its version stamp (ETag).
func Key(acct, drive, filePath, version string) string {
	sum := sha256.Sum256([]byte(acct + "\x00" + drive + "\x00" + filePath + "\x00" + version))
	return "o" + hex.EncodeToString(sum[:])[:40] // [0-9a-z], well under the 128-character limit
}

// Params is what the editor needs to open one file.
type Params struct {
	FileName, Ext string
	Kind          Kind
	Edit          bool
	Key           string
	FileURL       string // where the editor downloads the file (a gateway URL)
	CallbackURL   string // where the editor reports saves
	UserID        string
	UserName      string
	Lang          string
}

// Config builds the editor configuration and signs it. The returned map is sent to the browser: it carries the
// signature ("token") but never the secret.
func (o *ONLYOFFICE) Config(p Params) (map[string]any, error) {
	mode := "view"
	if p.Edit && p.Kind.Editable {
		mode = "edit"
	}
	title := p.FileName
	if len(title) > 128 {
		title = title[:128]
	}
	cfg := map[string]any{
		"documentType": p.Kind.DocumentType,
		"type":         "desktop",
		"width":        "100%",
		"height":       "100%",
		"document": map[string]any{
			"fileType": p.Ext, "key": p.Key, "title": title, "url": p.FileURL,
			"permissions": map[string]any{"edit": mode == "edit", "download": true, "print": true, "comment": mode == "edit"},
		},
		"editorConfig": map[string]any{
			"mode": mode, "lang": p.Lang, "callbackUrl": p.CallbackURL,
			"user":          map[string]any{"id": p.UserID, "name": p.UserName},
			"customization": map[string]any{"autosave": true, "forcesave": false},
		},
	}
	tok, err := Sign(o.Secret, cfg)
	if err != nil {
		return nil, err
	}
	cfg["token"] = tok
	return cfg, nil
}

// Callback is the part of the editor's save notification we use.
type Callback struct {
	Key    string `json:"key"`
	Status int    `json:"status"`
	URL    string `json:"url"`
}

// Callback statuses (ONLYOFFICE callback handler).
const (
	StatusEditing    = 1 // someone connected or disconnected
	StatusReady      = 2 // the document is closed and ready to be saved
	StatusSaveError  = 3
	StatusNoChanges  = 4 // closed without changes
	StatusForceSaved = 6 // a force-save while still open
	StatusForceError = 7
)

// ParseCallback verifies and decodes a save notification. The editor signs it either in a "token" field of the
// body or in an Authorization: Bearer header; either way the signed content, not the unsigned body, is trusted.
func (o *ONLYOFFICE) ParseCallback(body []byte, authorization string) (Callback, error) {
	tok := ""
	var raw struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(body, &raw)
	if raw.Token != "" {
		tok = raw.Token
	} else if t, ok := strings.CutPrefix(authorization, "Bearer "); ok {
		tok = t
	}
	claims, err := Verify(o.Secret, tok)
	if err != nil {
		return Callback{}, err
	}
	if inner, ok := claims["payload"].(map[string]any); ok { // the header form wraps the body in "payload"
		claims = inner
	}
	b, _ := json.Marshal(claims)
	var cb Callback
	if err := json.Unmarshal(b, &cb); err != nil {
		return Callback{}, ErrToken
	}
	return cb, nil
}

// VerifyRequest checks the token the editor sends when it downloads a file from the gateway.
func (o *ONLYOFFICE) VerifyRequest(authorization string) error {
	t, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok {
		return ErrToken
	}
	_, err := Verify(o.Secret, t)
	return err
}

// Check asks the editor server whether it is up and whether our secret is accepted: the healthcheck endpoint
// answers "true", and a signed "version" command is refused (error 6) when the secret is wrong.
func (o *ONLYOFFICE) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	get, _ := http.NewRequestWithContext(ctx, http.MethodGet, o.URL.String()+"/healthcheck", nil)
	resp, err := o.Client.Do(get)
	if err != nil {
		return fmt.Errorf("cannot reach the editor server: %w", err)
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.TrimSpace(string(b)) != "true" {
		return fmt.Errorf("that does not look like a Document Server (healthcheck answered HTTP %d)", resp.StatusCode)
	}
	claims := map[string]any{"c": "version"}
	tok, _ := Sign(o.Secret, claims)
	payload, _ := json.Marshal(map[string]any{"c": "version", "token": tok})
	post, _ := http.NewRequestWithContext(ctx, http.MethodPost, o.URL.String()+"/command", strings.NewReader(string(payload)))
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("Authorization", "Bearer "+tok)
	resp, err = o.Client.Do(post)
	if err != nil {
		return fmt.Errorf("cannot reach the editor server: %w", err)
	}
	defer resp.Body.Close()
	var ans struct {
		Error   int    `json:"error"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&ans); err != nil {
		return fmt.Errorf("the editor server gave an unexpected answer (HTTP %d)", resp.StatusCode)
	}
	switch ans.Error {
	case 0:
		return nil
	case 6:
		return fmt.Errorf("the editor server rejected the secret")
	}
	return fmt.Errorf("the editor server answered error %d", ans.Error)
}
