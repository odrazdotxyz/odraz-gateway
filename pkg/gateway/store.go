package gateway

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	odraz "github.com/odrazdotxyz/odraz-go-sdk"
	"github.com/odrazdotxyz/odraz-go-sdk/accounts"
)

// Store keeps what the gateway must remember about each signed-in browser session: which servers are connected
// (and the tokens for them), a sign-in in progress, and the user's own editor setting. The browser holds only an
// opaque session id. Live connections (HTTP clients, open drives) are rebuilt from this when needed.
//
// Two implementations: NewMemoryStore (development; everything is lost on restart) and NewFileStore (one file,
// encrypted and authenticated with AES-256-GCM under a key you provide; a single gateway instance). A Redis or
// Postgres store for several instances would implement the same interface.
type Store interface {
	// Load returns the session, or nil if there is none.
	Load(ctx context.Context, id string) (*SessionData, error)
	Save(ctx context.Context, d *SessionData) error
	Delete(ctx context.Context, id string) error
	// Secrets is the SDK secret store for one session's account tokens and passwords.
	Secrets(id string) accounts.SecretStore
	// Sweep forgets sessions created before cutoff.
	Sweep(ctx context.Context, cutoff time.Time) error
}

// SessionData is the persistent part of a session.
type SessionData struct {
	ID       string          `json:"id"`
	Created  time.Time       `json:"created"`
	Accounts []AccountRecord `json:"accounts,omitempty"`
	Pending  *PendingLogin   `json:"pending,omitempty"`
	// The user's own editor server. The secret is as sensitive as the tokens: it is only ever stored in a Store.
	OfficeURL    string `json:"officeUrl,omitempty"`
	OfficeSecret string `json:"officeSecret,omitempty"`
}

// AccountRecord is one connected server.
type AccountRecord struct {
	Backend string           `json:"backend"`
	Account accounts.Account `json:"account"`
	Method  string           `json:"method"`
	User    string           `json:"user"`
}

// PendingLogin is a browser sign-in in progress (the OIDC state and PKCE verifier).
type PendingLogin struct {
	Backend string           `json:"backend"`
	Account accounts.Account `json:"account"`
	OIDC    json.RawMessage  `json:"oidc"`
}

type stored struct {
	Data    SessionData                  `json:"data"`
	Secrets map[string]map[string][]byte `json:"secrets,omitempty"` // account id -> key -> value
}

type store struct {
	mu   sync.Mutex
	m    map[string]*stored
	path string // "" for memory only
	aead cipher.AEAD
}

// NewMemoryStore is a Store that lives in memory. For development and tests.
func NewMemoryStore() Store { return &store{m: map[string]*stored{}} }

// NewFileStore keeps sessions in path, encrypted with key (32 bytes). An existing file must open with this key or
// an error is returned, so a wrong key never silently starts an empty store. The file is written atomically with
// permissions 0600.
func NewFileStore(path string, key []byte) (Store, error) {
	if len(key) != 32 {
		return nil, errors.New("the session key must be 32 bytes (base64 of 32 random bytes, see ParseKey)")
	}
	blk, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(blk)
	st := &store{m: map[string]*stored{}, path: path, aead: aead}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return st, nil
	case err != nil:
		return nil, err
	}
	if len(b) < aead.NonceSize()+aead.Overhead() {
		return nil, fmt.Errorf("session file %s is too short", path)
	}
	plain, err := aead.Open(nil, b[:aead.NonceSize()], b[aead.NonceSize():], []byte("odraz-sessions-v1"))
	if err != nil {
		return nil, fmt.Errorf("session file %s cannot be opened with this key (wrong key, or the file was changed)", path)
	}
	if err := json.Unmarshal(plain, &st.m); err != nil {
		return nil, fmt.Errorf("session file %s: %w", path, err)
	}
	return st, nil
}

// ParseKey reads a base64 (standard or URL) encoded 32-byte key, for example from ODRAZ_SESSION_KEY.
// Make one with: openssl rand -base64 32
func ParseKey(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, errors.New("the session key must be base64 of exactly 32 bytes (openssl rand -base64 32)")
}

// flush writes the file. Called with mu held.
func (s *store) flush() error {
	if s.path == "" {
		return nil
	}
	plain, err := json.Marshal(s.m)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	out := s.aead.Seal(nonce, nonce, plain, []byte("odraz-sessions-v1"))
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".sessions-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

func (s *store) Load(_ context.Context, id string) (*SessionData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.m[id]
	if st == nil {
		return nil, nil
	}
	d := st.Data // a copy: callers must not alias the store's own slices
	d.Accounts = append([]AccountRecord(nil), st.Data.Accounts...)
	return &d, nil
}

func (s *store) Save(_ context.Context, d *SessionData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.m[d.ID]
	if st == nil {
		st = &stored{}
		s.m[d.ID] = st
	}
	st.Data = *d
	st.Data.Accounts = append([]AccountRecord(nil), d.Accounts...)
	return s.flush()
}

func (s *store) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return s.flush()
}

func (s *store) Sweep(_ context.Context, cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, st := range s.m {
		if st.Data.Created.Before(cutoff) {
			delete(s.m, id)
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return s.flush()
}

// Persistent reports whether sessions survive a restart.
func (s *store) Persistent() bool { return s.path != "" }

func (s *store) Secrets(id string) accounts.SecretStore { return &secrets{s: s, id: id} }

// secrets is the SDK's SecretStore over one session's slot. It never touches the SDK's test-only MemoryStore.
type secrets struct {
	s  *store
	id string
}

func (x *secrets) slot() *stored {
	st := x.s.m[x.id]
	if st == nil {
		st = &stored{Data: SessionData{ID: x.id, Created: time.Now()}}
		x.s.m[x.id] = st
	}
	if st.Secrets == nil {
		st.Secrets = map[string]map[string][]byte{}
	}
	return st
}

func (x *secrets) Put(_ context.Context, account, key string, value []byte) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	st := x.slot()
	if st.Secrets[account] == nil {
		st.Secrets[account] = map[string][]byte{}
	}
	st.Secrets[account][key] = append([]byte(nil), value...)
	return x.s.flush()
}

func (x *secrets) Get(_ context.Context, account, key string) ([]byte, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	if st := x.s.m[x.id]; st != nil {
		if v, ok := st.Secrets[account][key]; ok {
			return append([]byte(nil), v...), nil
		}
	}
	return nil, fmt.Errorf("secret %s/%s: %w", account, key, odraz.ErrNotFound)
}

func (x *secrets) Delete(_ context.Context, account string) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	if st := x.s.m[x.id]; st != nil {
		delete(st.Secrets, account)
	}
	return x.s.flush()
}
