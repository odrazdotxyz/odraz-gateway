package gateway

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
	"github.com/odrazdotxyz/odraz-go-sdk/files"
)

const maxZipItems = 500

// zipItem is one selected file or folder: ?item=<acct>|<drive>|<path>.
type zipItem struct {
	fs   files.FS
	st   files.Entry
	name string // unique top-level name inside the archive
}

// GET /api/zip?item=acct|drive|path&item=...: the selected files and folders as one zip, streamed (nothing is
// buffered, so a big selection does not fill the gateway's memory). Items may come from different servers.
// Everything is checked before the first byte is sent, so a bad selection gets a normal error; a failure in
// the middle of the stream can only end the download early.
func (s *Server) handleZip(w http.ResponseWriter, r *http.Request, sess *session) {
	raw := r.URL.Query()["item"]
	if len(raw) == 0 || len(raw) > maxZipItems {
		writeError(w, fmt.Errorf("select between 1 and %d items: %w", maxZipItems, odraz.ErrInvalidPath))
		return
	}
	var items []zipItem
	used := map[string]bool{}
	for _, it := range raw {
		parts := strings.SplitN(it, "|", 3)
		if len(parts) != 3 {
			writeError(w, fmt.Errorf("bad item %q: %w", it, odraz.ErrInvalidPath))
			return
		}
		fs, err := s.openFS(r.Context(), sess, parts[0], parts[1])
		if err != nil {
			writeError(w, err)
			return
		}
		p, err := files.CleanPath(parts[2])
		if err != nil {
			writeError(w, err)
			return
		}
		st, err := fs.Stat(r.Context(), p)
		if err != nil {
			writeError(w, err)
			return
		}
		items = append(items, zipItem{fs: fs, st: st, name: uniqueName(used, st.Name)})
	}

	name := "odrive-files.zip"
	if len(items) == 1 {
		name = items[0].name + ".zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+encodeFilename(name))
	w.Header().Set("Cache-Control", "no-store")
	zw := zip.NewWriter(w)
	for _, it := range items {
		if err := addToZip(r, zw, it); err != nil {
			log.Printf("zip: %s: %v (download ended early)", it.st.Path, err)
			return // the response is already streaming: all we can do is stop (the client sees a truncated zip)
		}
	}
	if err := zw.Close(); err != nil {
		log.Printf("zip: closing: %v", err)
	}
}

// uniqueName makes a top-level name unique within the archive: two selected items called "Photos" from
// different servers become "Photos" and "Photos (2)".
func uniqueName(used map[string]bool, name string) string {
	if name == "" {
		name = "item"
	}
	n := name
	for i := 2; used[strings.ToLower(n)]; i++ {
		ext := path.Ext(name)
		n = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), i, ext)
	}
	used[strings.ToLower(n)] = true
	return n
}

// storeExts are already compressed: storing them is faster and no bigger.
var storeExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".heic": true,
	".mp4": true, ".m4v": true, ".mov": true, ".webm": true, ".mkv": true, ".zip": true, ".gz": true, ".7z": true, ".mp3": true, ".ogg": true}

func addToZip(r *http.Request, zw *zip.Writer, it zipItem) error {
	if !it.st.IsDir {
		return addFile(r, zw, it.fs, it.st, it.name)
	}
	root := it.st.Path
	return files.Walk(r.Context(), it.fs, root, func(e files.Entry) error {
		rel := strings.Trim(strings.TrimPrefix(e.Path, root), "/")
		name := it.name
		if rel != "" {
			name += "/" + rel
		}
		if e.IsDir {
			if rel != "" && strings.HasPrefix(e.Name, ".") {
				return files.SkipDir // .odraz and other hidden folders are ours, not the user's files
			}
			_, err := zw.CreateHeader(&zip.FileHeader{Name: name + "/", Modified: e.ModTime}) // keeps empty folders
			return err
		}
		return addFile(r, zw, it.fs, e, name)
	})
}

func addFile(r *http.Request, zw *zip.Writer, fs files.FS, e files.Entry, name string) error {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: e.ModTime}
	if storeExts[strings.ToLower(path.Ext(name))] {
		h.Method = zip.Store
	}
	rc, err := fs.Read(r.Context(), e.Path, 0, -1)
	if err != nil {
		return err
	}
	defer rc.Close()
	fw, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, rc)
	return err
}
