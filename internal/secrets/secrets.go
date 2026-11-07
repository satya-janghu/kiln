// Package secrets generates and stores secrets under /etc/kiln/secrets (0700/0600)
// and keeps the redaction list for logs. Secrets never reach logs, the summary,
// argv or state.json.
package secrets

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"sync"

	"github.com/satya-janghu/kiln/internal/sys"
)

// Dir holds stored secrets.
const Dir = "/etc/kiln/secrets"

// Store tracks secrets known to this run.
type Store struct {
	mu     sync.Mutex
	values map[string]string
	shown  map[string]bool // generated this run, to show once
	// OnAdd, if set, receives every secret value (the app wires it to the log redactor).
	OnAdd func(value string)
}

// NewStore returns an empty store.
func NewStore() *Store { return &Store{values: map[string]string{}, shown: map[string]bool{}} }

// Add registers a secret value for redaction.
func (s *Store) Add(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value != "" {
		s.values[name] = value
		if s.OnAdd != nil {
			s.OnAdd(value)
		}
	}
}

// Values returns all secret values (for sys.Cmd.Redact and log redaction).
func (s *Store) Values() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.values))
	for _, v := range s.values {
		out = append(out, v)
	}
	return out
}

// Get returns a registered secret.
func (s *Store) Get(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[name]
}

// Generated returns secrets generated during this run (shown once on screen).
func (s *Store) Generated() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for n := range s.shown {
		out[n] = s.values[n]
	}
	return out
}

const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// Generate returns a random password of n characters (no look-alike characters).
func Generate(n int) (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < n; i++ {
		x, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[x.Int64()])
	}
	return b.String(), nil
}

// GenerateAndRegister creates a secret and marks it to be shown once.
func (s *Store) GenerateAndRegister(name string, n int) (string, error) {
	v, err := Generate(n)
	if err != nil {
		return "", err
	}
	s.Add(name, v)
	s.mu.Lock()
	s.shown[name] = true
	s.mu.Unlock()
	return v, nil
}

// Path returns the file for a named secret.
func Path(name string) string { return filepath.Join(Dir, name) }

// Save writes a secret to its 0600 file (created with that mode, D41).
func Save(sy sys.System, name, value string) error {
	if err := sy.MkdirAll(Dir, 0o700, nil); err != nil {
		return err
	}
	_, err := sy.WriteFile(Path(name), []byte(value+"\n"), sys.WriteOpts{Mode: 0o600, NoBackup: true})
	return err
}

// Load reads a stored secret.
func Load(sy sys.System, name string) (string, error) {
	b, err := sy.ReadFile(Path(name))
	if err != nil {
		return "", fmt.Errorf("no stored secret %q: %w", name, err)
	}
	return strings.TrimRight(string(b), "\n"), nil
}

// Hash is the marker stored in state.json to detect a changed secret.
func Hash(value string) string {
	h := sha256.Sum256([]byte("kiln:" + value))
	return hex.EncodeToString(h[:8])
}
