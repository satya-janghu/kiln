// Package apt runs apt and dpkg the way rules 8 and 9 require: fully
// non-interactive, keeping existing config files, waiting for locks, never
// racing other package managers (D41).
package apt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/satya-janghu/kiln/internal/sys"
)

// LockWait is how long to wait for other package managers (option "dpkg lock wait").
var LockWait = 10 * time.Minute

// Output receives live apt output lines (set by the runner).
var Output func(line string)

// Env is the environment every apt/dpkg call gets.
func Env() []string {
	return []string{
		"DEBIAN_FRONTEND=noninteractive",
		"NEEDRESTART_MODE=a",
		"NEEDRESTART_SUSPEND=1", // restarts happen once at the end of the run (D41)
		"APT_LISTCHANGES_FRONTEND=none",
		"UCF_FORCE_CONFFOLD=1",
		"LC_ALL=C",
	}
}

func baseArgs() []string {
	return []string{
		"-y", "-q",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
		"-o", fmt.Sprintf("DPkg::Lock::Timeout=%d", int(LockWait.Seconds())),
	}
}

// WaitForAutomatic waits until apt-daily and apt-daily-upgrade aren't running.
func WaitForAutomatic(ctx context.Context, s sys.System) error {
	deadline := time.Now().Add(LockWait)
	announced := false
	for {
		busy := false
		for _, u := range []string{"apt-daily.service", "apt-daily-upgrade.service"} {
			r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-active", u}})
			st := strings.TrimSpace(r.Stdout)
			if st == "active" || st == "activating" || st == "deactivating" || st == "reloading" {
				busy = true
			}
		}
		if !busy {
			return nil
		}
		if !announced && Output != nil {
			Output("waiting for automatic updates to finish…")
			announced = true
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("automatic updates are still running after %s; wait for them to finish (journalctl -u apt-daily-upgrade) and run kiln again", LockWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// WaitDpkgUnlocked waits until no process holds the dpkg locks (e.g. a
// PackageKit transaction), up to LockWait.
func WaitDpkgUnlocked(ctx context.Context, s sys.System) error {
	if s.DryRun() {
		return nil
	}
	deadline := time.Now().Add(LockWait)
	for dpkgLocked() {
		if time.Now().After(deadline) {
			return fmt.Errorf("another program has held the package lock for %s; close GNOME Software or wait, then run kiln again", LockWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return nil
}

// lockBusy matches apt's and dpkg's "someone else holds the lock" messages.
func lockBusy(stderr string) bool {
	return strings.Contains(stderr, "Could not get lock") || strings.Contains(stderr, "is locked by another process") ||
		strings.Contains(stderr, "dpkg frontend lock") || strings.Contains(stderr, "Unable to acquire the dpkg")
}

func run(ctx context.Context, s sys.System, name string, args []string, stdin []byte) error {
	if !s.DryRun() {
		if err := WaitForAutomatic(ctx, s); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(LockWait)
	for {
		_, err := s.Run(ctx, sys.Cmd{Name: name, Args: args, Env: Env(), Stdin: stdin, Output: Output})
		if err == nil {
			return nil
		}
		var ee *sys.ExitError
		if errors.As(err, &ee) && lockBusy(ee.Stderr) && time.Now().Before(deadline) {
			if Output != nil {
				Output("another program is using apt; waiting…")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
			}
			continue
		}
		return fmt.Errorf("%w. Fix the problem shown above, then run kiln again", err)
	}
}

// UpdatedThisRun is set once `apt-get update` has succeeded in this process,
// so steps can tell whether the package lists are fresh (e.g. upgrade's Check).
var UpdatedThisRun atomic.Bool

// Update runs apt-get update.
func Update(ctx context.Context, s sys.System) error {
	if err := run(ctx, s, "apt-get", append(baseArgs(), "update"), nil); err != nil {
		return err
	}
	if !s.DryRun() {
		UpdatedThisRun.Store(true)
	}
	return nil
}

// InstallOpts controls Install.
type InstallOpts struct {
	Suite        string // "" or "trixie-backports"
	NoRecommends bool
	Reinstall    bool
}

// Install installs packages (a no-op for installed ones).
func Install(ctx context.Context, s sys.System, pkgs []string, o InstallOpts) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := baseArgs()
	if o.Suite != "" {
		args = append(args, "-t", o.Suite)
	}
	if o.NoRecommends {
		args = append(args, "--no-install-recommends")
	}
	if o.Reinstall {
		args = append(args, "--reinstall")
	}
	args = append(append(args, "install"), pkgs...)
	return run(ctx, s, "apt-get", args, nil)
}

// Remove removes (or purges) packages that are installed.
func Remove(ctx context.Context, s sys.System, pkgs []string, purge bool) error {
	var present []string
	for _, p := range pkgs {
		if Installed(ctx, s, p) {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return nil
	}
	verb := "remove"
	if purge {
		verb = "purge"
	}
	return run(ctx, s, "apt-get", append(append(baseArgs(), verb), present...), nil)
}

// FullUpgrade runs apt-get full-upgrade.
func FullUpgrade(ctx context.Context, s sys.System) error {
	return run(ctx, s, "apt-get", append(baseArgs(), "full-upgrade"), nil)
}

// Preseed feeds debconf-set-selections lines ("pkg question type value").
func Preseed(ctx context.Context, s sys.System, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	_, err := s.Run(ctx, sys.Cmd{Name: "debconf-set-selections", Env: Env(), Stdin: []byte(strings.Join(lines, "\n") + "\n")})
	return err
}

// Repair finishes an interrupted dpkg run (D41).
func Repair(ctx context.Context, s sys.System) error {
	if err := run(ctx, s, "dpkg", []string{"--force-confdef", "--force-confold", "--configure", "-a"}, nil); err != nil {
		return err
	}
	return run(ctx, s, "apt-get", append(append(baseArgs(), "-f"), "install"), nil)
}

// Interrupted reports whether a previous dpkg run was interrupted.
func Interrupted(ctx context.Context, s sys.System) bool {
	r, _ := s.Query(ctx, sys.Cmd{Name: "dpkg", Args: []string{"--audit"}, Env: []string{"LC_ALL=C"}})
	if strings.TrimSpace(r.Stdout) != "" {
		return true
	}
	ents, _ := s.ReadDir("/var/lib/dpkg/updates")
	for _, e := range ents {
		if strings.Trim(e.Name(), "0123456789") == "" {
			return true
		}
	}
	return false
}

// Installed reports whether a package is fully installed.
func Installed(ctx context.Context, s sys.System, pkg string) bool {
	r, err := s.Query(ctx, sys.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f=${db:Status-Abbrev}", pkg}, Env: []string{"LC_ALL=C"}})
	return err == nil && strings.HasPrefix(r.Stdout, "ii")
}

// InstalledVersion returns the installed version or "".
func InstalledVersion(ctx context.Context, s sys.System, pkg string) string {
	r, err := s.Query(ctx, sys.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f=${db:Status-Abbrev} ${Version}", pkg}, Env: []string{"LC_ALL=C"}})
	if err != nil || !strings.HasPrefix(r.Stdout, "ii") {
		return ""
	}
	f := strings.Fields(r.Stdout)
	return f[len(f)-1]
}

// AllInstalled reports whether every package is installed; it returns the missing ones.
func AllInstalled(ctx context.Context, s sys.System, pkgs []string) (bool, []string) {
	var missing []string
	for _, p := range pkgs {
		if !Installed(ctx, s, p) {
			missing = append(missing, p)
		}
	}
	return len(missing) == 0, missing
}

// Available reports whether apt has a candidate for pkg.
func Available(ctx context.Context, s sys.System, pkg string) bool {
	r, err := s.Query(ctx, sys.Cmd{Name: "apt-cache", Args: []string{"policy", pkg}, Env: []string{"LC_ALL=C"}})
	return err == nil && strings.Contains(r.Stdout, "Candidate:") && !strings.Contains(r.Stdout, "Candidate: (none)")
}

// Policy returns `apt-cache policy` output for pkg.
func Policy(ctx context.Context, s sys.System, pkg string) string {
	r, _ := s.Query(ctx, sys.Cmd{Name: "apt-cache", Args: []string{"policy", pkg}, Env: []string{"LC_ALL=C"}})
	return r.Stdout
}

// SimulateUpgrade returns the Inst/Remv lines of a simulated full-upgrade.
func SimulateUpgrade(ctx context.Context, s sys.System) ([]string, error) {
	r, err := s.Query(ctx, sys.Cmd{Name: "apt-get", Args: []string{"-s", "-q", "full-upgrade"}, Env: Env()})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(r.Stdout, "\n") {
		if strings.HasPrefix(l, "Inst ") || strings.HasPrefix(l, "Remv ") {
			out = append(out, l)
		}
	}
	return out, nil
}
