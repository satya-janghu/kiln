package run

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// aptEnv makes apt and dpkg fully non-interactive (CLAUDE.md, architecture).
var aptEnv = []string{
	"DEBIAN_FRONTEND=noninteractive",
	"NEEDRESTART_MODE=a",
	"APT_LISTCHANGES_FRONTEND=none",
	"UCF_FORCE_CONFFOLD=1",
	"LC_ALL=C",
}

// aptOpts keep existing config files and wait for the dpkg lock.
var aptOpts = []string{
	"-o", "Dpkg::Options::=--force-confdef",
	"-o", "Dpkg::Options::=--force-confold",
	"-o", "DPkg::Lock::Timeout=600",
}

// WaitForApt waits for first-boot automatic updates (apt-daily and
// apt-daily-upgrade) to finish, so kiln never fights unattended-upgrades.
func (s *S) WaitForApt() error {
	deadline := time.Now().Add(60 * time.Minute)
	said := false
	for {
		out, _ := s.Out("systemctl", "is-active", "apt-daily.service", "apt-daily-upgrade.service")
		busy := false
		for _, l := range Scan(out) {
			if l == "active" || l == "activating" || l == "deactivating" {
				busy = true
			}
		}
		if !busy {
			return nil
		}
		if !said {
			s.Say("Waiting for Debian's automatic updates to finish (this is normal on a fresh install)...")
			said = true
		}
		if time.Now().After(deadline) {
			return errors.New("debian's automatic updates are still running after an hour; let them finish (systemctl status apt-daily-upgrade.service) and run kiln again")
		}
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// apt runs apt-get with kiln's environment, retrying while another package
// manager holds the lock. It never kills apt or dpkg.
func (s *S) apt(stdin string, args ...string) error {
	if err := s.WaitForApt(); err != nil {
		return err
	}
	full := append(append([]string{}, aptOpts...), args...)
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		_, err = s.Exec(Cmd{Name: "apt-get", Args: full, Env: aptEnv, Stdin: stdin})
		var ee *ExitError
		if err == nil || !errors.As(err, &ee) || !strings.Contains(ee.Output, "Could not get lock") {
			return err
		}
		if attempt == 3 {
			break
		}
		s.Say("Another program is using apt; waiting a minute and trying again...")
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-time.After(time.Minute):
		}
	}
	return err
}

// Update runs apt-get update.
func (s *S) Update() error {
	if err := s.apt("", "update"); err != nil {
		return fmt.Errorf("apt-get update failed; check the network connection and the files in /etc/apt/sources.list.d: %w", err)
	}
	return nil
}

// Install installs packages (with Recommends).
func (s *S) Install(pkgs ...string) error {
	return s.InstallOpts(InstallOpts{}, pkgs...)
}

// InstallOpts are optional install settings.
type InstallOpts struct {
	Suite        string // e.g. "trixie-backports"
	NoRecommends bool
}

// InstallOpts installs packages with options.
func (s *S) InstallOpts(o InstallOpts, pkgs ...string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := []string{"install", "-y"}
	if o.Suite != "" {
		args = append(args, "-t", o.Suite)
	}
	if o.NoRecommends {
		args = append(args, "--no-install-recommends")
	}
	args = append(args, pkgs...)
	if err := s.apt("", args...); err != nil {
		return fmt.Errorf("installing %s failed: %w", strings.Join(pkgs, " "), err)
	}
	return nil
}

// Purge removes packages that are installed; missing ones are ignored.
func (s *S) Purge(pkgs ...string) error {
	var present []string
	for _, p := range pkgs {
		if Installed(s, p) {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return nil
	}
	if err := s.apt("", append([]string{"purge", "-y"}, present...)...); err != nil {
		return fmt.Errorf("removing %s failed: %w", strings.Join(present, " "), err)
	}
	return nil
}

// FullUpgrade runs apt-get full-upgrade.
func (s *S) FullUpgrade() error {
	return s.apt("", "full-upgrade", "-y")
}

// AptGet runs any apt-get command with kiln's environment.
func (s *S) AptGet(args ...string) error { return s.apt("", args...) }

// Preseed feeds debconf answers ("pkg question type value" lines).
func (s *S) Preseed(lines ...string) error {
	_, err := s.Exec(Cmd{Name: "debconf-set-selections", Stdin: strings.Join(lines, "\n") + "\n", Quiet: true})
	return err
}

// Installed reports whether a package is installed.
func Installed(s *S, pkg string) bool {
	out, err := s.Out("dpkg-query", "-W", "-f=${db:Status-Abbrev}", pkg)
	return err == nil && strings.HasPrefix(out, "ii")
}

// Available reports whether apt can install a package.
func Available(s *S, pkg string) bool {
	out, err := s.Out("apt-cache", "policy", pkg)
	if err != nil {
		return false
	}
	for _, l := range Scan(out) {
		if strings.HasPrefix(l, "Candidate:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "Candidate:")) != "(none)"
		}
	}
	return false
}
