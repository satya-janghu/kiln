package steps

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var sudoersUnsafe = regexp.MustCompile(`[^a-z0-9_-]`)

func doSudo(c *Ctx) error {
	u := c.U.Name
	if err := c.Install("sudo"); err != nil {
		return err
	}
	if err := c.Run("adduser", "--quiet", u, "sudo"); err != nil {
		return fmt.Errorf("adding %s to the sudo group failed: %w", u, err)
	}

	// sudoers.d skips names containing '.' or ending in '~' (sudoers(5)), so
	// the check copy is ignored by sudo and the real name has neither.
	name := sudoersUnsafe.ReplaceAllString(u, "_")
	path := "/etc/sudoers.d/90-" + name + "-nopasswd"
	check := "/etc/sudoers.d/.kiln-check-" + name
	content := "# Managed by kiln\n" + u + " ALL=(ALL:ALL) NOPASSWD: ALL\n"

	if err := c.Write(check, 0o440, content); err != nil {
		return err
	}
	if err := c.Run("visudo", "-c", "-q", "-f", check); err != nil {
		_ = c.Remove(check)
		return fmt.Errorf("the sudo rule didn't pass visudo, so nothing was installed: %w", err)
	}
	if err := os.Rename(check, path); err != nil {
		_ = c.Remove(check)
		return fmt.Errorf("could not put %s in place: %w", path, err)
	}
	c.Log("wrote %s", path)
	if err := c.Run("visudo", "-c", "-q"); err != nil {
		_ = c.Remove(path)
		return fmt.Errorf("the sudo policy didn't validate with kiln's rule, so %s was removed again: %w. Run `visudo -c` to see which file is wrong", path, err)
	}

	// Check: the rule is in effect for the user.
	out, err := c.Out("sudo", "-l", "-U", u)
	if err != nil || !strings.Contains(out, "NOPASSWD: ALL") {
		return fmt.Errorf("`sudo -l -U %s` doesn't show the passwordless rule; check %s and run kiln again", u, path)
	}
	c.Note("Log out and back in for %s's sudo group membership to take effect (sudo itself already works).", u)
	return nil
}
