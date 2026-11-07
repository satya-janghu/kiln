// Package action defines the typed, declarative changes steps plan (rule 7).
package action

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Kind groups actions on the review screen.
type Kind string

const (
	Packages Kind = "Packages"
	Files    Kind = "Files"
	Services Kind = "Services"
	Firewall Kind = "Firewall"
	Commands Kind = "Commands"
)

// Action is one change.
type Action interface {
	Describe() string
	Kind() Kind
	Do(ctx context.Context, s sys.System) error
}

// RunAll executes actions in order, stopping at the first error.
func RunAll(ctx context.Context, s sys.System, acts []Action) error {
	for _, a := range acts {
		if err := a.Do(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", Short(a.Describe()), err)
		}
	}
	return nil
}

// Short trims an action description for error messages: the first clause, at
// most 90 characters, so the summary leads with the actual error (seen on the
// HP test machine: whole multi-line descriptions repeated before each error).
func Short(desc string) string {
	if i := strings.IndexAny(desc, ":;("); i > 20 {
		desc = desc[:i]
	}
	desc = strings.TrimSpace(desc)
	if r := []rune(desc); len(r) > 90 {
		desc = string(r[:89]) + "…"
	}
	return desc
}

// AptUpdate refreshes package lists.
type AptUpdate struct{}

func (AptUpdate) Describe() string                           { return "refresh package lists (apt-get update)" }
func (AptUpdate) Kind() Kind                                 { return Packages }
func (AptUpdate) Do(ctx context.Context, s sys.System) error { return apt.Update(ctx, s) }

// AptInstall installs packages.
type AptInstall struct {
	Pkgs         []string
	Suite        string
	NoRecommends bool
	Reinstall    bool
	Debconf      []string // preseed lines applied first
}

func (a AptInstall) Describe() string {
	s := "install " + strings.Join(a.Pkgs, " ")
	if a.Suite != "" {
		s += " (from " + a.Suite + ")"
	}
	if a.NoRecommends {
		s += " without recommended packages"
	}
	return s
}
func (AptInstall) Kind() Kind { return Packages }
func (a AptInstall) Do(ctx context.Context, s sys.System) error {
	if err := apt.Preseed(ctx, s, a.Debconf); err != nil {
		return err
	}
	return apt.Install(ctx, s, a.Pkgs, apt.InstallOpts{Suite: a.Suite, NoRecommends: a.NoRecommends, Reinstall: a.Reinstall})
}

// AptRemove removes packages if installed.
type AptRemove struct {
	Pkgs  []string
	Purge bool
}

func (a AptRemove) Describe() string { return "remove " + strings.Join(a.Pkgs, " ") }
func (AptRemove) Kind() Kind         { return Packages }
func (a AptRemove) Do(ctx context.Context, s sys.System) error {
	return apt.Remove(ctx, s, a.Pkgs, a.Purge)
}

// WriteFile is a managed write (rule 4).
type WriteFile struct {
	Path    string
	Content []byte
	Mode    fs.FileMode
	Owner   *sys.User
}

func (w WriteFile) Describe() string {
	m := w.Mode
	if m == 0 {
		m = 0o644
	}
	return fmt.Sprintf("write %s (%#o)", w.Path, m.Perm())
}
func (WriteFile) Kind() Kind { return Files }
func (w WriteFile) Do(_ context.Context, s sys.System) error {
	_, err := s.WriteFile(w.Path, w.Content, sys.WriteOpts{Mode: w.Mode, Owner: w.Owner})
	return err
}

// RemoveFile deletes a file (backed up first).
type RemoveFile struct{ Path string }

func (r RemoveFile) Describe() string                         { return "remove " + r.Path }
func (RemoveFile) Kind() Kind                                 { return Files }
func (r RemoveFile) Do(_ context.Context, s sys.System) error { return s.Remove(r.Path) }

// Symlink creates or fixes a symlink.
type Symlink struct {
	Target, Path string
	Owner        *sys.User
}

func (l Symlink) Describe() string { return "link " + l.Path + " → " + l.Target }
func (Symlink) Kind() Kind         { return Files }
func (l Symlink) Do(_ context.Context, s sys.System) error {
	return s.Symlink(l.Target, l.Path, l.Owner)
}

// Mkdir creates a directory.
type Mkdir struct {
	Path  string
	Mode  fs.FileMode
	Owner *sys.User
}

func (m Mkdir) Describe() string {
	return fmt.Sprintf("create directory %s (%#o)", m.Path, m.Mode.Perm())
}
func (Mkdir) Kind() Kind { return Files }
func (m Mkdir) Do(_ context.Context, s sys.System) error {
	return s.MkdirAll(m.Path, m.Mode, m.Owner)
}

// Systemctl runs one systemctl verb.
type Systemctl struct {
	Verb  string // enable, disable, mask, unmask, start, stop, restart, reload, daemon-reload, try-restart
	Units []string
	Now   bool
}

func (c Systemctl) Describe() string {
	s := "systemctl " + c.Verb
	if c.Now {
		s += " --now"
	}
	if len(c.Units) > 0 {
		s += " " + strings.Join(c.Units, " ")
	}
	return s
}
func (Systemctl) Kind() Kind { return Services }
func (c Systemctl) Do(ctx context.Context, s sys.System) error {
	args := []string{c.Verb}
	if c.Now {
		args = append(args, "--now")
	}
	args = append(args, c.Units...)
	_, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: args})
	return err
}

// Run executes a command.
type Run struct {
	Cmd  sys.Cmd
	Desc string // optional plain-language description
	K    Kind   // default Commands
}

func (r Run) Describe() string {
	if r.Desc != "" {
		return r.Desc
	}
	return r.Cmd.String()
}
func (r Run) Kind() Kind {
	if r.K == "" {
		return Commands
	}
	return r.K
}
func (r Run) Do(ctx context.Context, s sys.System) error {
	_, err := s.Run(ctx, r.Cmd)
	return err
}

// UserAddGroup adds a user to a group (adduser <user> <group>).
type UserAddGroup struct{ User, Group string }

func (u UserAddGroup) Describe() string { return "add " + u.User + " to group " + u.Group }
func (UserAddGroup) Kind() Kind         { return Commands }
func (u UserAddGroup) Do(ctx context.Context, s sys.System) error {
	_, err := s.Run(ctx, sys.Cmd{Name: "adduser", Args: []string{"--quiet", u.User, u.Group}})
	return err
}

// Custom is for steps that need runtime feedback. Desc must say exactly what it will try.
type Custom struct {
	Desc string
	K    Kind
	Fn   func(ctx context.Context, s sys.System) error
}

func (c Custom) Describe() string { return c.Desc }
func (c Custom) Kind() Kind {
	if c.K == "" {
		return Commands
	}
	return c.K
}
func (c Custom) Do(ctx context.Context, s sys.System) error { return c.Fn(ctx, s) }
