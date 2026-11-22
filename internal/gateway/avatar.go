package gateway

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
)

// GET /api/avatar?acct=&size=N: the profile picture of the user signed in to that server, from the server
// itself (Nextcloud always has one; OpenCloud only when the user uploaded a photo, else 404). The page shows
// the vendor's icon when there is none.
func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request, sess *session) {
	sess.mu.Lock()
	a := sess.accts[r.URL.Query().Get("acct")]
	sess.mu.Unlock()
	if a == nil {
		writeError(w, fmt.Errorf("not connected: %w", odraz.ErrNotFound))
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	size = min(max(size, 16), 256)
	rc, ct, err := a.handle.Avatar(r.Context(), size)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rc.Close()
	base, _, _ := strings.Cut(ct, ";")
	if !thumbTypes[strings.TrimSpace(strings.ToLower(base))] { // never serve active content (svg) from our origin
		writeError(w, fmt.Errorf("no usable avatar: %w", odraz.ErrNotFound))
		return
	}
	w.Header().Set("Content-Type", base)
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = io.Copy(w, rc)
}
