package steps

import (
	"fmt"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

const (
	zramConf    = "/etc/systemd/zram-generator.conf.d/60-kiln.conf"
	sysctlConf  = "/etc/sysctl.d/60-kiln.conf"
	journalConf = "/etc/systemd/journald.conf.d/60-kiln.conf"
)

// sysctlValues are written to sysctlConf and checked live afterwards.
var sysctlValues = [][2]string{
	{"vm.swappiness", "180"},
	{"fs.inotify.max_user_watches", "524288"},
	{"fs.inotify.max_user_instances", "1024"},
}

func zramActive() bool { return strings.Contains(run.Read("/proc/swaps"), "/dev/zram0") }

func doSystem(c *Ctx) error {
	// zram swap as big as RAM; the installer's disk swap stays as overflow.
	if err := c.Install("systemd-zram-generator"); err != nil {
		return err
	}
	if err := c.Write(zramConf, 0o644, "# Managed by kiln\n[zram0]\nzram-size = ram\ncompression-algorithm = zstd\n"); err != nil {
		return err
	}
	if err := c.Run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	zramLater := false
	if zramActive() {
		// Resizing a zram device in use means swapping it off; leave that to the boot.
		zramLater = true
		c.Note("zram swap was already on, so its new size (equal to RAM) applies after the next reboot.")
	} else if err := c.Run("systemctl", "start", "dev-zram0.swap"); err != nil {
		return fmt.Errorf("zram swap didn't start: %w. See `journalctl -u systemd-zram-setup@zram0`", err)
	}

	// sysctl: swappiness for zram, inotify limits for editors and file watchers.
	var b strings.Builder
	b.WriteString("# Managed by kiln\n")
	for _, kv := range sysctlValues {
		b.WriteString(kv[0] + " = " + kv[1] + "\n")
	}
	if err := c.Write(sysctlConf, 0o644, b.String()); err != nil {
		return err
	}
	if err := c.Run("sysctl", "-q", "-p", sysctlConf); err != nil {
		return fmt.Errorf("loading %s failed: %w", sysctlConf, err)
	}

	// journald: cap the journal at 500 MB.
	if err := c.Write(journalConf, 0o644, "# Managed by kiln\n[Journal]\nSystemMaxUse=500M\n"); err != nil {
		return err
	}
	if err := c.Run("systemctl", "restart", "systemd-journald"); err != nil {
		return fmt.Errorf("restarting the journal (systemd-journald) failed: %w", err)
	}

	// Time sync with Debian's default, systemd-timesyncd.
	if err := c.Install("systemd-timesyncd"); err != nil {
		return err
	}
	if err := c.Run("timedatectl", "set-ntp", "true"); err != nil {
		return fmt.Errorf("turning on network time sync failed: %w", err)
	}

	// Check.
	if !zramLater && !zramActive() {
		return fmt.Errorf("/dev/zram0 isn't in use as swap; see `journalctl -u systemd-zram-setup@zram0`")
	}
	for _, kv := range sysctlValues {
		if got, _ := c.Out("sysctl", "-n", kv[0]); strings.TrimSpace(got) != kv[1] {
			return fmt.Errorf("%s is %q, not %s; another file in /etc/sysctl.d may override %s", kv[0], strings.TrimSpace(got), kv[1], sysctlConf)
		}
	}
	if got, _ := c.Out("timedatectl", "show", "-p", "NTP", "--value"); strings.TrimSpace(got) != "yes" {
		return fmt.Errorf("network time sync is still off; run `timedatectl set-ntp true` and check `systemctl status systemd-timesyncd`")
	}
	return nil
}
