package steps

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/run"
)

// doPower sets Debian's balanced profile: power-profiles-daemon with GNOME
// (its power menu uses it), tuned without (D5, D52). Never both: the
// packages don't conflict, so the other one is removed. TLP is never touched.
func doPower(c *Ctx) error {
	if c.F.GNOME {
		// Installing ppd first lets apt replace tuned-ppd (it Conflicts with
		// ppd) without ever leaving GNOME's dependency unmet.
		if err := c.Install("power-profiles-daemon"); err != nil {
			return err
		}
		if err := c.Purge("tuned-ppd", "tuned"); err != nil {
			return err
		}
		if err := c.Run("systemctl", "enable", "--now", "power-profiles-daemon.service"); err != nil {
			return fmt.Errorf("starting power-profiles-daemon failed: %w. See `journalctl -u power-profiles-daemon`", err)
		}
		// Started isn't ready: wait until it answers.
		err := pollFor(c, 30*time.Second, func() error {
			if !c.OK("powerprofilesctl", "get") {
				return errors.New("power-profiles-daemon doesn't answer")
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("%w within 30 seconds; see `journalctl -u power-profiles-daemon`", err)
		}
		if err := c.Run("powerprofilesctl", "set", "balanced"); err != nil {
			return fmt.Errorf("selecting the balanced power profile failed: %w", err)
		}
		if out, _ := c.Out("powerprofilesctl", "get"); strings.TrimSpace(out) != "balanced" {
			return fmt.Errorf("the power profile is %q, not balanced; run `powerprofilesctl set balanced`", strings.TrimSpace(out))
		}
		return nil
	}

	if err := c.Purge("power-profiles-daemon"); err != nil {
		return err
	}
	if err := c.Install("tuned"); err != nil {
		return err
	}
	if err := c.Run("systemctl", "enable", "--now", "tuned.service"); err != nil {
		return fmt.Errorf("starting tuned failed: %w. See `journalctl -u tuned`", err)
	}
	err := pollFor(c, 30*time.Second, func() error {
		if out, _ := c.Out("systemctl", "is-active", "tuned.service"); strings.TrimSpace(out) != "active" {
			return errors.New("tuned isn't running")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%w after 30 seconds; see `journalctl -u tuned`", err)
	}
	if err := c.Run("tuned-adm", "profile", "balanced"); err != nil {
		return fmt.Errorf("selecting tuned's balanced profile failed: %w", err)
	}
	out, _ := c.Out("tuned-adm", "active")
	if !slices.Contains(run.Scan(out), "Current active profile: balanced") {
		return fmt.Errorf("tuned's active profile isn't balanced (`tuned-adm active` says %q); run `tuned-adm profile balanced`", strings.TrimSpace(out))
	}
	return nil
}
