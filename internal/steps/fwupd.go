package steps

import (
	"errors"
	"fmt"

	"github.com/satya-janghu/kiln/internal/run"
)

// fwupd exit codes (fwupd 2.0.20, debian13-notes).
const (
	fwupdNothing = 2   // nothing to do
	fwupdOffline = 101 // network unreachable
)

func exitCode(err error) int {
	var ee *run.ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	if err != nil {
		return -1
	}
	return 0
}

// doFwupd installs fwupd and applies available firmware updates once. It
// never reboots: --no-reboot-check skips fwupdmgr's "Restart now?" prompt,
// and its stdin is never a terminal.
func doFwupd(c *Ctx) error {
	// The signed EFI binary is needed for capsule updates under Secure Boot.
	if err := c.Install("fwupd", "fwupd-amd64-signed", "udisks2"); err != nil {
		return err
	}

	c.Say("Refreshing firmware information from LVFS...")
	switch err := c.Run("fwupdmgr", "refresh", "--force"); exitCode(err) {
	case 0:
	case fwupdOffline:
		c.Note("Firmware updates were skipped: LVFS couldn't be reached. Run `fwupdmgr refresh && fwupdmgr update` later.")
		return nil
	default:
		return fmt.Errorf("refreshing firmware information failed: %w. Check the network and run kiln again", err)
	}

	// Without --json: with it, fwupdmgr exits 0 even when there's nothing
	// to do (field-checked on the test box, 2026-11-08).
	switch _, err := c.Out("fwupdmgr", "get-updates"); exitCode(err) {
	case 0:
	case fwupdNothing:
		c.Note("Firmware: no updates available for this machine's devices.")
		return nil
	default:
		return fmt.Errorf("listing firmware updates failed: %w. Run `fwupdmgr get-updates` to see why", err)
	}

	c.Say("Applying firmware updates...")
	switch err := c.Run("fwupdmgr", "update", "-y", "--no-reboot-check", "--no-device-prompt"); exitCode(err) {
	case 0:
		c.Note("Firmware updates were applied; BIOS updates finish installing at the next reboot.")
		c.Reboot("firmware updates were applied")
	case fwupdNothing:
		c.Note("Firmware: no updates available for this machine's devices.")
	default:
		// A device refusing (on battery, say) isn't a setup failure.
		c.Note("A firmware update couldn't be applied (%v). Common causes: running on battery, or a device that refuses updates. Run `fwupdmgr update` later.", err)
	}
	return nil
}
