package manifest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
	"github.com/odrazdotxyz/odraz-go-sdk/files"
)

// memDrive is a tiny in-memory files.FS: just enough for the manifest (files and folders by path).
type memDrive struct {
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]bool
}

func newDrive() *memDrive {
	return &memDrive{files: map[string][]byte{}, dirs: map[string]bool{"/": true}}
}

func (d *memDrive) Stat(_ context.Context, p string) (files.Entry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if b, ok := d.files[p]; ok {
		return files.Entry{Path: p, Size: int64(len(b))}, nil
	}
	if d.dirs[p] {
		return files.Entry{Path: p, IsDir: true}, nil
	}
	return files.Entry{}, fmt.Errorf("%s: %w", p, odraz.ErrNotFound)
}
func (d *memDrive) List(context.Context, string) ([]files.Entry, error) { return nil, nil }
func (d *memDrive) Read(_ context.Context, p string, _, _ int64) (io.ReadCloser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	b, ok := d.files[p]
	if !ok {
		return nil, odraz.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (d *memDrive) Write(_ context.Context, p string, r io.Reader, _ files.WriteOptions) error {
	b, _ := io.ReadAll(r)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.files[p] = b
	return nil
}
func (d *memDrive) Mkdir(_ context.Context, p string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dirs[p] = true
	return nil
}
func (d *memDrive) Move(context.Context, string, string, bool) error { return nil }
func (d *memDrive) Delete(_ context.Context, p string, _ bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.files[p]; ok {
		delete(d.files, p)
		return nil
	}
	if d.dirs[p] {
		for f := range d.files {
			if strings.HasPrefix(f, p+"/") {
				return odraz.ErrNotEmpty
			}
		}
		delete(d.dirs, p)
		return nil
	}
	return odraz.ErrNotFound
}
func (d *memDrive) Capabilities() files.Capabilities { return files.Capabilities{} }

var ctx = context.Background()

func TestMissingManifestIsEmpty(t *testing.T) {
	m, err := Read(ctx, newDrive())
	if err != nil || len(m.Sections) != 0 || m.Version != Version {
		t.Fatalf("Read = %+v, %v", m, err)
	}
}

func TestSetReadRemoveRoundTrip(t *testing.T) {
	d := newDrive()
	if err := SetSection(ctx, d, Photos, Section{Folder: "/Pictures/Camera Roll", HideInFiles: true}); err != nil {
		t.Fatal(err)
	}
	m, err := Read(ctx, d)
	if err != nil || m.Sections[Photos] != (Section{Folder: "/Pictures/Camera Roll", HideInFiles: true}) {
		t.Fatalf("Read = %+v, %v", m, err)
	}
	if !strings.Contains(string(d.files[Path]), `"version": 1`) {
		t.Errorf("file = %s", d.files[Path])
	}
	// replacing keeps other sections
	if err := SetSection(ctx, d, "archive", Section{Folder: "/Old"}); err != nil {
		t.Fatal(err)
	}
	if err := SetSection(ctx, d, Photos, Section{Folder: "/Travel"}); err != nil {
		t.Fatal(err)
	}
	m, _ = Read(ctx, d)
	if m.Sections[Photos].Folder != "/Travel" || m.Sections["archive"].Folder != "/Old" {
		t.Errorf("sections = %+v", m.Sections)
	}
	// removing the last section removes the manifest and the empty .odraz folder
	if err := RemoveSection(ctx, d, Photos); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSection(ctx, d, "archive"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.files[Path]; ok || d.dirs[Dir] {
		t.Errorf("manifest left behind: files=%v dirs=%v", d.files, d.dirs)
	}
}

func TestUnreadableManifestIsNeverOverwritten(t *testing.T) {
	for name, content := range map[string]string{
		"invalid json":  "{not json",
		"newer version": `{"version": 2, "sections": {"photos": {"folder": "/x", "newField": true}}}`,
		"version zero":  `{"version": 0}`,
	} {
		d := newDrive()
		d.dirs[Dir] = true
		d.files[Path] = []byte(content)
		m, err := Read(ctx, d)
		if !errors.Is(err, ErrUnreadable) || len(m.Sections) != 0 {
			t.Errorf("%s: Read = %+v, %v; want empty + ErrUnreadable", name, m, err)
		}
		if err := SetSection(ctx, d, Photos, Section{Folder: "/Photos"}); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: SetSection = %v; must refuse", name, err)
		}
		if string(d.files[Path]) != content {
			t.Errorf("%s: the file was changed", name)
		}
	}
}

func TestOversizedManifestIsUnreadable(t *testing.T) {
	d := newDrive()
	d.files[Path] = bytes.Repeat([]byte("x"), MaxSize+10)
	if _, err := Read(ctx, d); !errors.Is(err, ErrUnreadable) {
		t.Errorf("Read = %v", err)
	}
}
