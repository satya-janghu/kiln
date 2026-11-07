// Package revert implements verify-then-revert for risky changes
// (docs/architecture.md "Risky changes"; D28, D41).
package revert

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/state"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Root holds per-change snapshots.
const Root = "/var/lib/kiln/pending"

// Builtin marks a restore command that is a Go function a step registered,
// not a program: {Builtin, name, args...}. A restore step can then wait and
// poll (D41: "started" isn't "ready"), and a revert offered at the next start
// (it reads the same list from state.json) does exactly the same (D49).
const Builtin = "kiln:"

// BuiltinFunc is a registered restore step.
type BuiltinFunc func(ctx context.Context, s sys.System, args []string) error

var builtins = map[string]BuiltinFunc{}

// Register adds a restore step; call it from a package init.
func Register(name string, fn BuiltinFunc) { builtins[name] = fn }

// Begin snapshots paths (files or directories), records a pending entry in
// state.json and saves it before anything changes.
func Begin(ctx context.Context, s sys.System, st *state.State, step, desc string, paths []string, restore [][]string) error {
	if s.DryRun() {
		return nil
	}
	if st.Pending != nil && st.Pending.Step != step {
		return fmt.Errorf("an earlier change (%s) didn't finish and hasn't been reverted; restart kiln and answer its revert question first", st.Pending.Desc)
	}
	dir := filepath.Join(Root, step)
	_ = s.RemoveAll(dir)
	if err := s.MkdirAll(dir, 0o700, nil); err != nil {
		return err
	}
	p := &state.Pending{Step: step, Desc: desc, Snapshots: map[string]string{}, Restore: restore, Started: time.Now()}
	for i, path := range paths {
		fi, err := s.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			p.Snapshots[path] = ""
			continue
		}
		if err != nil {
			return err
		}
		snap := filepath.Join(dir, fmt.Sprintf("%02d.tar", i))
		rel := strings.TrimPrefix(path, "/")
		if _, err := s.Run(ctx, sys.Cmd{Name: "tar", Args: []string{"-C", "/", "--xattrs", "-cpf", snap, rel}}); err != nil {
			return fmt.Errorf("could not snapshot %s before changing it: %w", path, err)
		}
		_ = fi
		p.Snapshots[path] = snap
	}
	st.Pending = p
	return st.Save(s)
}

// Undo restores the snapshots and runs the restore commands.
func Undo(ctx context.Context, s sys.System, st *state.State) error {
	p := st.Pending
	if p == nil || s.DryRun() {
		return nil
	}
	var errs []string
	for path, snap := range p.Snapshots {
		if path == "/" || !filepath.IsAbs(path) {
			continue
		}
		if err := s.RemoveAll(path); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if snap == "" {
			continue
		}
		if _, err := s.Run(ctx, sys.Cmd{Name: "tar", Args: []string{"-C", "/", "--xattrs", "-xpf", snap}}); err != nil {
			errs = append(errs, fmt.Sprintf("restoring %s: %v", path, err))
		}
	}
	for _, argv := range p.Restore {
		if len(argv) == 0 {
			continue
		}
		if argv[0] == Builtin {
			fn := builtins[""]
			if len(argv) > 1 {
				fn = builtins[argv[1]]
			}
			if fn == nil {
				errs = append(errs, fmt.Sprintf("unknown restore step %q", strings.Join(argv, " ")))
				continue
			}
			if err := fn(ctx, s, argv[2:]); err != nil {
				errs = append(errs, err.Error())
			}
			continue
		}
		if _, err := s.Run(ctx, sys.Cmd{Name: argv[0], Args: argv[1:]}); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("revert of %q was incomplete: %s. Snapshots are kept in %s", p.Desc, strings.Join(errs, "; "), filepath.Join(Root, p.Step))
	}
	return Commit(s, st)
}

// Commit clears the pending entry and deletes its snapshots.
func Commit(s sys.System, st *state.State) error {
	if st.Pending == nil || s.DryRun() {
		return nil
	}
	step := st.Pending.Step
	st.Pending = nil
	if err := st.Save(s); err != nil {
		return err
	}
	return s.RemoveAll(filepath.Join(Root, step))
}

// Poll retries check until it succeeds or the deadline passes (D41: "started"
// isn't "ready"). It returns the last error.
func Poll(ctx context.Context, deadline time.Duration, every time.Duration, check func() error) error {
	end := time.Now().Add(deadline)
	var last error
	for {
		if last = check(); last == nil {
			return nil
		}
		if f := (final{}); errors.As(last, &f) {
			return f.err // a definite failure: polling longer won't change it
		}
		if time.Now().After(end) {
			return fmt.Errorf("still not ready after %s: %w", deadline, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
}

// Final wraps an error a Poll check returns when waiting longer can't help
// (for example, a unit that has already failed): Poll then returns err at once.
func Final(err error) error { return final{err} }

type final struct{ err error }

func (f final) Error() string { return f.err.Error() }
func (f final) Unwrap() error { return f.err }
