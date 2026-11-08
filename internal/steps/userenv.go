package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// The user's ~/.bashrc loads every ~/.bashrc.d/*.sh (catalog 12, step 6),
// so python and go can each drop in their own file.
const (
	bashrcMarker = "# kiln: load ~/.bashrc.d"
	bashrcLoader = bashrcMarker + "\n" +
		`if [ -d "$HOME/.bashrc.d" ]; then for f in "$HOME"/.bashrc.d/*.sh; do [ -r "$f" ] && . "$f"; done; unset f; fi` + "\n"
)

// ensureBashrc makes sure the user has a ~/.bashrc, copied from
// /etc/skel/.bashrc if there is none. It returns the current content.
func ensureBashrc(c *Ctx) (string, error) {
	u := c.U
	path := filepath.Join(u.Home, ".bashrc")
	fi, err := os.Lstat(path)
	if err == nil {
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("%s isn't a regular file, so kiln won't change it; replace it with a normal file and run kiln again", path)
		}
		return run.Read(path), nil
	}
	skel := run.Read("/etc/skel/.bashrc")
	if err := c.WriteAs(path, 0o644, u.UID, u.GID, skel); err != nil {
		return "", err
	}
	return skel, nil
}

// userDropIn makes ~/.bashrc load ~/.bashrc.d and writes ~/.bashrc.d/<name>
// (owned by the user).
func userDropIn(c *Ctx, name, content string) error {
	u := c.U
	cur, err := ensureBashrc(c)
	if err != nil {
		return err
	}
	if !strings.Contains(cur, bashrcMarker) {
		if cur != "" && !strings.HasSuffix(cur, "\n") {
			cur += "\n"
		}
		if err := c.WriteAs(filepath.Join(u.Home, ".bashrc"), 0o644, u.UID, u.GID, cur+"\n"+bashrcLoader); err != nil {
			return err
		}
	}
	dir := filepath.Join(u.Home, ".bashrc.d")
	// Created as the user, so the user owns it (WriteAs would create it as root).
	if err := c.AsUser(u, "mkdir", "-p", dir); err != nil {
		return fmt.Errorf("could not create %s: %w", dir, err)
	}
	return c.WriteAs(filepath.Join(dir, name), 0o644, u.UID, u.GID, "# Managed by kiln\n"+content)
}

// addrForNotes is the machine's LAN address for summary notes.
func addrForNotes(c *Ctx) string {
	if c.F.IP != "" {
		return c.F.IP
	}
	return "<lan-ip>"
}

// unitUp reports whether a unit is enabled and active.
func unitUp(c *Ctx, unit string) error {
	en, _ := c.Out("systemctl", "is-enabled", unit)
	ac, _ := c.Out("systemctl", "is-active", unit)
	en, ac = strings.TrimSpace(en), strings.TrimSpace(ac)
	if en != "enabled" || ac != "active" {
		return fmt.Errorf("%s is %s and %s, not enabled and active; run `systemctl enable --now %s` and check `journalctl -u %s`", unit, orUnknown(en), orUnknown(ac), unit, unit)
	}
	return nil
}

func orUnknown(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}
