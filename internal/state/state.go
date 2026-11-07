// Package state reads and writes /etc/kiln/state.json (docs/options-reference.md).
// It never holds secrets.
package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"time"

	"github.com/satya-janghu/kiln/internal/sys"
)

// Path is the state file.
const Path = "/etc/kiln/state.json"

// Battery records what the battery ladder accepted.
type Battery struct {
	Method  string `json:"method"` // thresholds, vendor:<name>, watch, none
	Battery string `json:"battery,omitempty"`
	Start   int    `json:"start,omitempty"`
	Stop    int    `json:"stop,omitempty"`
	Path    string `json:"path,omitempty"`
	Value   string `json:"value,omitempty"`
}

// Pending is a risky change in progress (architecture: "Risky changes").
type Pending struct {
	Step      string            `json:"step"`
	Desc      string            `json:"desc"`
	Snapshots map[string]string `json:"snapshots"` // path -> snapshot path ("" = did not exist)
	Restore   [][]string        `json:"restore"`   // commands (argv) run after files are restored
	Started   time.Time         `json:"started"`
}

// Run records the current or last run (D41).
type Run struct {
	Started  time.Time `json:"started"`
	Step     string    `json:"step"`
	Action   string    `json:"action"`
	Finished bool      `json:"finished"`
	Result   string    `json:"result,omitempty"`
}

// Choices are the wizard choices that differ from defaults.
type Choices struct {
	Steps   map[string]bool `json:"steps"`
	Options map[string]any  `json:"options"`
}

// State is the file's content.
type State struct {
	Version      int               `json:"version"`
	LastRun      time.Time         `json:"last_run"`
	KilnVersion  string            `json:"kiln_version"`
	Choices      Choices           `json:"choices"`
	Battery      *Battery          `json:"battery,omitempty"`
	PowerManager string            `json:"power_manager,omitempty"`
	Pending      *Pending          `json:"pending"`
	Run          *Run              `json:"run,omitempty"`
	SecretHashes map[string]string `json:"secret_hashes,omitempty"`
	Thinkfan     string            `json:"thinkfan,omitempty"`
}

// Load reads the state file; a missing file gives an empty state.
func Load(s sys.System) (*State, error) {
	st := &State{Version: 1, Choices: Choices{Steps: map[string]bool{}, Options: map[string]any{}}}
	b, err := s.ReadFile(Path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if uerr := json.Unmarshal(b, st); uerr != nil {
		return st, uerr
	}
	if st.Choices.Steps == nil {
		st.Choices.Steps = map[string]bool{}
	}
	if st.Choices.Options == nil {
		st.Choices.Options = map[string]any{}
	}
	return st, nil
}

// Save writes the state file atomically, 0600, without a backup.
func (st *State) Save(s sys.System) error {
	if s.DryRun() {
		return nil
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if merr := s.MkdirAll("/etc/kiln", 0o755, nil); merr != nil {
		return merr
	}
	_, err = s.WriteFile(Path, append(b, '\n'), sys.WriteOpts{Mode: 0o600, NoBackup: true})
	return err
}
