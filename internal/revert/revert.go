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
