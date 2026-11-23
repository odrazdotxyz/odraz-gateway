package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
	"github.com/odrazdotxyz/odraz-go-sdk/files"
)

// ---- JSON helpers and error mapping ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// statusFor maps the SDK's sentinel errors (always usable with errors.Is) to HTTP statuses.
func statusFor(err error) int {
	switch {
	case errors.Is(err, odraz.ErrAuth):
		return http.StatusUnauthorized
	case errors.Is(err, odraz.ErrBlockedByProxy):
		return http.StatusBadGateway
	case errors.Is(err, odraz.ErrInvalidPath):
		return http.StatusBadRequest
	case errors.Is(err, odraz.ErrPermission):
		return http.StatusForbidden
	case errors.Is(err, odraz.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, odraz.ErrExists), errors.Is(err, odraz.ErrNotEmpty):
		return http.StatusConflict
	case errors.Is(err, odraz.ErrUnsupported):
		return http.StatusNotImplemented
	case errors.Is(err, context.Canceled):
		return 499
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	}
	return http.StatusInternalServerError
}

// codeFor names the error kind so the page can react without parsing messages.
func codeFor(err error) string {
	for _, c := range []struct {
		err  error
		name string
	}{{odraz.ErrAuth, "auth"}, {odraz.ErrBlockedByProxy, "blocked_by_proxy"}, {odraz.ErrInvalidPath, "invalid_path"},
		{odraz.ErrPermission, "permission"}, {odraz.ErrNotFound, "not_found"}, {odraz.ErrExists, "exists"},
		{odraz.ErrNotEmpty, "not_empty"}, {odraz.ErrUnsupported, "unsupported"}} {
		if errors.Is(err, c.err) {
			return c.name
		}
	}
	return "error"
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, statusFor(err), map[string]any{"error": err.Error(), "code": codeFor(err)})
}

// ---- drives and entries ----

type driveJSON struct {
	Acct     string `json:"acct"`   // which connected backend it belongs to
	Vendor   string `json:"vendor"` // "opencloud" | "nextcloud"
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	ReadOnly bool   `json:"readOnly"`
	Used     *int64 `json:"used,omitempty"`
	Total    *int64 `json:"total,omitempty"` // absent when unlimited or unknown
}

// handleDrives lists the drives of every connected backend. A backend that fails is reported in
// "errors" and does not hide the others.
func (s *Server) handleDrives(w http.ResponseWriter, r *http.Request, sess *session) {
	out := []driveJSON{}
	errs := map[string]string{}
	for _, a := range sess.accounts() {
		ds, err := a.client.Drives(r.Context())
		if err != nil {
			errs[a.backend.ID] = err.Error()
			continue
		}
		for _, d := range ds {
			j := driveJSON{Acct: a.backend.ID, Vendor: a.backend.Vendor, ID: d.ID, Name: d.Name, Kind: string(d.Kind), ReadOnly: d.ReadOnly}
			if q := d.Quota; q != nil {
				if q.Used >= 0 {
					j.Used = &q.Used
				}
				if q.Total > 0 {
					j.Total = &q.Total
				}
			}
			out = append(out, j)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"drives": out, "errors": errs})
}

// accounts returns the connected accounts in the order the backends were configured.
func (sess *session) accounts() []*account {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	out := make([]*account, 0, len(sess.accts))
	for _, a := range sess.accts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].backend.ID < out[j].backend.ID })
	return out
}

type entryJSON struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime,omitempty"`
	Mime    string `json:"mime,omitempty"`
}

func toJSON(e files.Entry) entryJSON {
	j := entryJSON{Name: e.Name, Path: e.Path, IsDir: e.IsDir, Size: e.Size, Mime: contentType(e)}
	if !e.ModTime.IsZero() {
		j.ModTime = e.ModTime.UTC().Format(time.RFC3339)
	}
	return j
}

func contentType(e files.Entry) string {
	if e.IsDir {
		return ""
	}
	if t := mime.TypeByExtension(strings.ToLower(path.Ext(e.Name))); t != "" {
		return t
	}
	if e.MimeType != "" {
		return e.MimeType
	}
	return "application/octet-stream"
}

// fsFor returns the FS of the drive named by ?acct= and ?drive=, opened once per session.
func (s *Server) fsFor(r *http.Request, sess *session) (files.FS, error) {
	q := r.URL.Query()
	return s.openFS(r.Context(), sess, q.Get("acct"), q.Get("drive"))
}

func (s *Server) openFS(ctx context.Context, sess *session, acct, id string) (files.FS, error) {
	if id == "" {
		return nil, fmt.Errorf("missing drive parameter: %w", odraz.ErrInvalidPath)
	}
	sess.mu.Lock()
	a := sess.accts[acct]
	var fs files.FS
	if a != nil {
		fs = a.drives[id]
	}
	sess.mu.Unlock()
	if a == nil {
		return nil, fmt.Errorf("not connected to %q: %w", acct, odraz.ErrNotFound)
	}
	if fs != nil {
		return fs, nil
	}
	fs, err := a.client.OpenDrive(ctx, id)
	if err != nil {
		return nil, err
	}
	sess.mu.Lock()
	a.drives[id] = fs
	sess.mu.Unlock()
	return fs, nil
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, sess *session) {
	fs, err := s.fsFor(r, sess)
	if err != nil {
		writeError(w, err)
		return
	}
	p := r.URL.Query().Get("path")
	if p == "" {
		p = "/"
	}
	cur, err := fs.Stat(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	es, err := fs.List(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	sort.Slice(es, func(i, j int) bool {
		if es[i].IsDir != es[j].IsDir {
			return es[i].IsDir
		}
		return strings.ToLower(es[i].Name) < strings.ToLower(es[j].Name)
	})
	out := make([]entryJSON, 0, len(es))
	for _, e := range es {
		out = append(out, toJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": cur.Path, "entries": out})
}

// ---- download (with HTTP ranges) and upload ----

// previewable types may be shown inline in the page; everything else downloads.
var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/svg+xml": true,
	"text/plain": true, "text/markdown": true, "text/csv": true, "application/json": true,
	"text/css": true, "text/javascript": true, "application/javascript": true, "text/html": true,
	"video/mp4": true, "video/webm": true, "video/ogg": true, // played in the Photos section

}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request, sess *session) {
	fs, err := s.fsFor(r, sess)
	if err != nil {
		writeError(w, err)
		return
	}
	p := r.URL.Query().Get("path")
	st, err := fs.Stat(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	if st.IsDir {
		writeError(w, fmt.Errorf("%s is a folder: %w", p, odraz.ErrInvalidPath))
		return
	}
	ct := contentType(st)
	base, _, _ := strings.Cut(ct, ";")
	disp := "attachment"
	if r.URL.Query().Get("inline") == "1" && inlineTypes[base] {
		disp = "inline"
		if base == "text/html" || base == "image/svg+xml" {
			// Active content from a file store must never run in our origin.
			w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
		}
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", disp+"; filename*=UTF-8''"+encodeFilename(st.Name))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, no-cache")
	if st.ETag != "" {
		w.Header().Set("ETag", st.ETag)
	}

	start, length, partial, ok := parseRange(r.Header.Get("Range"), st.Size)
	if !ok {
		w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(st.Size, 10))
		writeErr(w, http.StatusRequestedRangeNotSatisfiable, "range not satisfiable")
		return
	}
	rc, err := fs.Read(r.Context(), p, start, length)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, st.Size))
		w.WriteHeader(http.StatusPartialContent)
	}
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, rc) // a client that goes away mid-download is not an error worth reporting
	}
}

func encodeFilename(n string) string {
	var b strings.Builder
	for _, c := range []byte(n) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// parseRange handles a single "bytes=a-b", "bytes=a-" or "bytes=-n" range against size.
// It returns the start, the length to read, whether the answer is partial (206), and ok=false
// for an unsatisfiable range. No header means the whole file.
func parseRange(h string, size int64) (start, length int64, partial, ok bool) {
	if h == "" {
		return 0, size, false, true
	}
	spec, found := strings.CutPrefix(h, "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, size, false, true // unsupported forms: serve the whole file, as the RFC allows
	}
	a, b, _ := strings.Cut(spec, "-")
	switch {
	case a == "": // suffix: last n bytes
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false, false
		}
		if n > size {
			n = size
		}
		return size - n, n, true, size > 0
	default:
		s, err := strconv.ParseInt(a, 10, 64)
		if err != nil || s < 0 || s >= size {
			return 0, 0, false, false
		}
		e := size - 1
		if b != "" {
			v, err := strconv.ParseInt(b, 10, 64)
			if err != nil || v < s {
				return 0, 0, false, false
			}
			if v < e {
				e = v
			}
		}
		return s, e - s + 1, true, true
	}
}

// thumbTypes are the image types a thumbnail may have; anything else is treated as "no thumbnail" so a
// misbehaving server cannot make us serve active content from our origin.
var thumbTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// GET /api/v1/thumb?acct=&drive=&path=&size=N: a thumbnail rendered by the file's own server (OpenCloud,
// Nextcloud), at most size x size pixels. Where the server has none (the file type, or a backend without
// thumbnails) the answer is 501 and the page falls back to the original.
func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request, sess *session) {
	fs, err := s.fsFor(r, sess)
	if err != nil {
		writeError(w, err)
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size <= 0 {
		size = 256
	}
	size = min(max(size, 16), 1024)
	rc, ct, err := files.Preview(r.Context(), fs, r.URL.Query().Get("path"), size, size)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rc.Close()
	base, _, _ := strings.Cut(ct, ";")
	if !thumbTypes[strings.TrimSpace(strings.ToLower(base))] {
		writeError(w, fmt.Errorf("no thumbnail for this file: %w", odraz.ErrUnsupported))
		return
	}
	w.Header().Set("Content-Type", base)
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = io.Copy(w, rc)
}

// PUT /api/v1/file?drive=&path=&overwrite=1 with the raw file as the body. files.Upload creates
// missing parent folders and refuses to replace an existing file unless overwrite is set.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request, sess *session) {
	fs, err := s.fsFor(r, sess)
	if err != nil {
		writeError(w, err)
		return
	}
	q := r.URL.Query()
	size := r.ContentLength // -1 when unknown (chunked)
	if max := s.limits.MaxUploadBytes; size > max {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "the file is larger than this gateway accepts", "code": "too_large"})
		return
	} else if max > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, max) // also covers uploads with no declared length
	}
	err = files.Upload(r.Context(), fs, q.Get("path"), r.Body, files.WriteOptions{Size: size, Overwrite: q.Get("overwrite") == "1"})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

// ---- mkdir, move, copy, delete ----

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request, sess *session) {
	fs, err := s.fsFor(r, sess)
	if err == nil {
		err = fs.Mkdir(r.Context(), r.URL.Query().Get("path"))
	}
	reply(w, err)
}

// move and copy take from/to inside the drive named by acct/drive. With toAcct/toDrive the target is
// another drive, possibly on another backend: the bytes then go through the gateway (read from the
// source, write to the target), so it is slower than a move inside one server and not atomic.
func (s *Server) handleMove(w http.ResponseWriter, r *http.Request, sess *session) {
	s.moveOrCopy(w, r, sess, true)
}

func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request, sess *session) {
	s.moveOrCopy(w, r, sess, false)
}

func (s *Server) moveOrCopy(w http.ResponseWriter, r *http.Request, sess *session, move bool) {
	q := r.URL.Query()
	src, err := s.fsFor(r, sess)
	if err != nil {
		writeError(w, err)
		return
	}
	from, to, overwrite := q.Get("from"), q.Get("to"), q.Get("overwrite") == "1"
	toAcct, toDrive := q.Get("toAcct"), q.Get("toDrive")
	if toAcct == "" {
		toAcct = q.Get("acct")
	}
	if toDrive == "" {
		toDrive = q.Get("drive")
	}
	if toAcct == q.Get("acct") && toDrive == q.Get("drive") {
		if move {
			err = src.Move(r.Context(), from, to, overwrite)
		} else {
			err = files.Copy(r.Context(), src, from, to, overwrite)
		}
		reply(w, err)
		return
	}
	dst, err := s.openFS(r.Context(), sess, toAcct, toDrive)
	if err != nil {
		writeError(w, err)
		return
	}
	if err = copyAcross(r.Context(), src, from, dst, to, overwrite); err == nil && move {
		err = src.Delete(r.Context(), from, true)
	}
	reply(w, err)
}

// copyAcross copies a file or a whole folder from one FS to another.
func copyAcross(ctx context.Context, src files.FS, from string, dst files.FS, to string, overwrite bool) error {
	st, err := src.Stat(ctx, from)
	if err != nil {
		return err
	}
	if _, err := dst.Stat(ctx, to); err == nil && !overwrite {
		return fmt.Errorf("%s already exists: %w", to, odraz.ErrExists)
	}
	if !st.IsDir {
		rc, err := src.Read(ctx, from, 0, -1)
		if err != nil {
			return err
		}
		defer rc.Close()
		return files.Upload(ctx, dst, to, rc, files.WriteOptions{Size: st.Size, Overwrite: overwrite})
	}
	if err := dst.Mkdir(ctx, to); err != nil {
		return err
	}
	kids, err := src.List(ctx, from)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if err := copyAcross(ctx, src, path.Join(from, k.Name), dst, path.Join(to, k.Name), overwrite); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, sess *session) {
	fs, err := s.fsFor(r, sess)
	if err == nil {
		q := r.URL.Query()
		err = fs.Delete(r.Context(), q.Get("path"), q.Get("recursive") == "1")
	}
	reply(w, err)
}

func reply(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
