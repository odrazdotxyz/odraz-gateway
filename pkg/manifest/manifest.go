// Package manifest reads and writes a drive's scaffolding manifest: a small JSON file at
// /.odraz/manifest.json that says how a drive is organised, so every client that opens the drive agrees
// (for example which folder is the Photos section). It lives in the drive itself: no database, and it
// moves with the data.
//
// It is deliberately free of gateway code (it needs only the SDK's files.FS) so it can later move to an
// optional package of its own. The SDK core stays storage-only.
//
// Rules for readers and writers:
//   - A missing manifest is normal: use the defaults (no sections).
//   - A broken manifest, or one with a newer version than this code knows, is read as empty and must not
//     be written over (Update refuses), so an older client never destroys a newer client's data.
//   - Nothing else of ours may live outside the reserved /.odraz folder.
package manifest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
	"github.com/odrazdotxyz/odraz-go-sdk/files"
)

const (
	// Dir is the reserved folder for everything the system writes in a drive (manifest, thumbnails, ...).
	Dir = "/.odraz"
	// Path is the manifest file.
	Path = Dir + "/manifest.json"
	// Version is the format version this code reads and writes.
	Version = 1
	// MaxSize bounds what is read: a manifest is a few hundred bytes.
	MaxSize = 64 << 10
)

// Section names.
const Photos = "photos"

// Section maps a section of the app onto a folder of this drive.
type Section struct {
	Folder      string `json:"folder"`      // absolute path inside the drive, e.g. "/Pictures/Camera Roll"
	HideInFiles bool   `json:"hideInFiles"` // do not list the folder in the plain file views
}

// Manifest is the file's content.
type Manifest struct {
	Version  int                `json:"version"`
	Sections map[string]Section `json:"sections"`
}

// ErrUnreadable means the manifest exists but cannot be used (invalid JSON, or a newer version).
var ErrUnreadable = errors.New("manifest: unreadable or newer than this client")

func empty() Manifest { return Manifest{Version: Version, Sections: map[string]Section{}} }

// Read returns the drive's manifest. A missing manifest gives an empty one and no error. An unreadable one
// gives an empty manifest AND an error wrapping ErrUnreadable: browse with the empty one, but do not write.
func Read(ctx context.Context, fs files.FS) (Manifest, error) {
	st, err := fs.Stat(ctx, Path)
	if err != nil {
		if errors.Is(err, odraz.ErrNotFound) {
			return empty(), nil
		}
		return empty(), err
	}
	if st.IsDir || st.Size > MaxSize {
		return empty(), fmt.Errorf("%w: %s is not a small file", ErrUnreadable, Path)
	}
	rc, err := fs.Read(ctx, Path, 0, -1)
	if err != nil {
		return empty(), err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, MaxSize+1))
	if err != nil {
		return empty(), err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return empty(), fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if m.Version < 1 || m.Version > Version {
		return empty(), fmt.Errorf("%w: version %d", ErrUnreadable, m.Version)
	}
	if m.Sections == nil {
		m.Sections = map[string]Section{}
	}
	return m, nil
}

// Update reads the manifest, lets change edit it, and writes it back. If no sections are left the manifest
// file is removed (and the .odraz folder too when it is empty), so a drive the user has "unset" is clean again.
func Update(ctx context.Context, fs files.FS, change func(*Manifest) error) error {
	m, err := Read(ctx, fs)
	if err != nil {
		return err // includes ErrUnreadable: never overwrite what we cannot read
	}
	if err := change(&m); err != nil {
		return err
	}
	if len(m.Sections) == 0 {
		if err := fs.Delete(ctx, Path, false); err != nil && !errors.Is(err, odraz.ErrNotFound) {
			return err
		}
		if err := fs.Delete(ctx, Dir, false); err != nil && !errors.Is(err, odraz.ErrNotFound) && !errors.Is(err, odraz.ErrNotEmpty) {
			return err
		}
		return nil
	}
	m.Version = Version
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := fs.Mkdir(ctx, Dir); err != nil {
		return err
	}
	return files.Upload(ctx, fs, Path, bytes.NewReader(data), files.WriteOptions{Size: int64(len(data)), Overwrite: true})
}

// SetSection sets (or replaces) one section.
func SetSection(ctx context.Context, fs files.FS, name string, s Section) error {
	return Update(ctx, fs, func(m *Manifest) error {
		m.Sections[name] = s
		return nil
	})
}

// RemoveSection removes one section; removing the last one removes the manifest.
func RemoveSection(ctx context.Context, fs files.FS, name string) error {
	return Update(ctx, fs, func(m *Manifest) error {
		delete(m.Sections, name)
		return nil
	})
}
