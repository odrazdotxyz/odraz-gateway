package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
	"github.com/odrazdotxyz/odraz-go-sdk/files"

	"github.com/odrazdotxyz/odraz-gateway/pkg/manifest"
)

// The Photos section is built from folders the user chose. Which folder it is lives in each drive's own
// manifest (/.odraz/manifest.json, see internal/manifest), so every client agrees and the gateway keeps no
// state of its own. Only personal drives can be mapped.

type photoDrive struct {
	a *account
	d files.Drive
}

// personalDrives lists the personal drives of every connected account.
func (s *Server) personalDrives(ctx context.Context, sess *session) ([]photoDrive, map[string]string) {
	var out []photoDrive
	errs := map[string]string{}
	for _, a := range sess.accounts() {
		ds, err := a.client.Drives(ctx)
		if err != nil {
			errs[a.backend.ID] = err.Error()
			continue
		}
		for _, d := range ds {
			if d.Kind == files.DrivePersonal && !d.ReadOnly {
				out = append(out, photoDrive{a, d})
			}
		}
	}
	return out, errs
}

type mappingJSON struct {
	Acct        string `json:"acct"`
	Drive       string `json:"drive"`
	Folder      string `json:"folder"`
	HideInFiles bool   `json:"hideInFiles"`
}

type problemJSON struct {
	Acct  string `json:"acct"`
	Drive string `json:"drive"`
	Error string `json:"error"`
}

// GET /api/v1/photos/config: where Photos is mapped, per personal drive. A drive whose manifest cannot be read
// (invalid, or from a newer client) is listed under "problems": it is browsed as if unmapped and never
// overwritten.
func (s *Server) handlePhotosConfig(w http.ResponseWriter, r *http.Request, sess *session) {
	drives, errs := s.personalDrives(r.Context(), sess)
	out := []mappingJSON{}
	problems := []problemJSON{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, pd := range drives {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fs, err := s.openFS(r.Context(), sess, pd.a.backend.ID, pd.d.ID)
			if err == nil {
				var m manifest.Manifest
				m, err = manifest.Read(r.Context(), fs)
				mu.Lock()
				defer mu.Unlock()
				if sec, ok := m.Sections[manifest.Photos]; ok && sec.Folder != "" {
					out = append(out, mappingJSON{pd.a.backend.ID, pd.d.ID, sec.Folder, sec.HideInFiles})
				}
				if err != nil {
					problems = append(problems, problemJSON{pd.a.backend.ID, pd.d.ID, err.Error()})
				}
				return
			}
			mu.Lock()
			defer mu.Unlock()
			problems = append(problems, problemJSON{pd.a.backend.ID, pd.d.ID, err.Error()})
		}()
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Acct+out[i].Drive < out[j].Acct+out[j].Drive })
	writeJSON(w, http.StatusOK, map[string]any{"mappings": out, "problems": problems, "errors": errs})
}

// cleanFolder validates a folder choice: inside the drive, not the drive root, not our reserved folder.
func cleanFolder(p string) (string, error) {
	c, err := files.CleanPath(p)
	if err != nil {
		return "", err
	}
	if c == "/" || c == manifest.Dir || strings.HasPrefix(c, manifest.Dir+"/") {
		return "", fmt.Errorf("%q cannot be a photos folder: %w", p, odraz.ErrInvalidPath)
	}
	return c, nil
}

// PUT /api/v1/photos/folder?acct=&drive=&folder=/X&hide=1: make /X this drive's Photos folder.
func (s *Server) handlePhotosSet(w http.ResponseWriter, r *http.Request, sess *session) {
	q := r.URL.Query()
	fs, err := s.fsFor(r, sess)
	if err != nil {
		writeError(w, err)
		return
	}
	folder, err := cleanFolder(q.Get("folder"))
	if err != nil {
		writeError(w, err)
		return
	}
	st, err := fs.Stat(r.Context(), folder)
	if err != nil {
		writeError(w, err)
		return
	}
	if !st.IsDir {
		writeError(w, fmt.Errorf("%s is a file, not a folder: %w", folder, odraz.ErrInvalidPath))
		return
	}
	err = manifest.SetSection(r.Context(), fs, manifest.Photos, manifest.Section{Folder: folder, HideInFiles: q.Get("hide") == "1"})
	reply(w, manifestErr(err))
}

// DELETE /api/v1/photos/folder?acct=&drive=: unmap Photos for this drive.
func (s *Server) handlePhotosUnset(w http.ResponseWriter, r *http.Request, sess *session) {
	fs, err := s.fsFor(r, sess)
	if err == nil {
		err = manifestErr(manifest.RemoveSection(r.Context(), fs, manifest.Photos))
	}
	reply(w, err)
}

// manifestErr turns "the manifest is from a newer client / broken" into a clear conflict, not a 500.
func manifestErr(err error) error {
	if errors.Is(err, manifest.ErrUnreadable) {
		return fmt.Errorf("this drive's %s cannot be read, so it was left alone (%v): %w", manifest.Path, err, odraz.ErrExists)
	}
	return err
}

type photoJSON struct {
	Acct    string `json:"acct"`
	Drive   string `json:"drive"`
	Vendor  string `json:"vendor"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime,omitempty"`
	Mime    string `json:"mime,omitempty"`
	Kind    string `json:"kind"` // "image" or "video"
}

const maxPhotos = 5000

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".heic": true, ".heif": true, ".bmp": true, ".tif": true, ".tiff": true, ".svg": true}
var videoExts = map[string]bool{".mp4": true, ".m4v": true, ".mov": true, ".webm": true, ".mkv": true, ".avi": true, ".3gp": true, ".ogv": true}

// mediaKind says whether a file is a picture or a video: by its type, then by its extension.
func mediaKind(e files.Entry) string {
	base, _, _ := strings.Cut(contentType(e), ";")
	switch {
	case strings.HasPrefix(base, "image/"):
		return "image"
	case strings.HasPrefix(base, "video/"):
		return "video"
	}
	switch ext := strings.ToLower(path.Ext(e.Name)); {
	case imageExts[ext]:
		return "image"
	case videoExts[ext]:
		return "video"
	}
	return ""
}

// GET /api/v1/photos: the pictures and videos under the folders chosen for Photos, from every mapped drive,
// newest first. Only pictures and videos: the other files of those folders are not part of Photos (they stay
// reachable in the file views, with "Show hidden").
func (s *Server) handlePhotos(w http.ResponseWriter, r *http.Request, sess *session) {
	drives, errs := s.personalDrives(r.Context(), sess)
	items := []photoJSON{}
	truncated := false
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, pd := range drives {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fs, err := s.openFS(r.Context(), sess, pd.a.backend.ID, pd.d.ID)
			var m manifest.Manifest
			if err == nil {
				m, _ = manifest.Read(r.Context(), fs) // unreadable: browsed as unmapped
			}
			sec, ok := m.Sections[manifest.Photos]
			if err != nil || !ok || sec.Folder == "" {
				if err != nil {
					mu.Lock()
					errs[pd.a.backend.ID] = err.Error()
					mu.Unlock()
				}
				return
			}
			var mine []photoJSON
			walkErr := files.Walk(r.Context(), fs, sec.Folder, func(e files.Entry) error {
				if e.IsDir {
					if e.Path != sec.Folder && strings.HasPrefix(e.Name, ".") {
						return files.SkipDir // .odraz and other hidden folders
					}
					return nil
				}
				kind := mediaKind(e)
				if kind == "" {
					return nil
				}
				j := photoJSON{Acct: pd.a.backend.ID, Drive: pd.d.ID, Vendor: pd.a.backend.Vendor, Name: e.Name, Path: e.Path, Size: e.Size, Mime: contentType(e), Kind: kind}
				if !e.ModTime.IsZero() {
					j.ModTime = e.ModTime.UTC().Format(time.RFC3339)
				}
				if len(mine) >= maxPhotos {
					return errTooMany
				}
				mine = append(mine, j)
				return nil
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(walkErr, errTooMany):
				truncated = true
			case walkErr != nil:
				errs[pd.a.backend.ID] = fmt.Sprintf("%s: %v", sec.Folder, walkErr)
			}
			items = append(items, mine...)
		}()
	}
	wg.Wait()
	sort.Slice(items, func(i, j int) bool {
		if items[i].ModTime != items[j].ModTime {
			return items[i].ModTime > items[j].ModTime
		}
		return items[i].Name < items[j].Name
	})
	if len(items) > maxPhotos {
		items, truncated = items[:maxPhotos], true
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "truncated": truncated, "errors": errs})
}

var errTooMany = errors.New("too many files")
