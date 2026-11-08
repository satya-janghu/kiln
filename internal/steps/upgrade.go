package steps

import (
	"fmt"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// simulateUpgrade returns the Inst and Remv lines of `apt-get -s full-upgrade`.
func simulateUpgrade(c *Ctx) ([]string, error) {
	if err := c.WaitForApt(); err != nil {
		return nil, err
	}
	out, err := c.Exec(run.Cmd{Name: "apt-get", Args: []string{"-o", "DPkg::Lock::Timeout=600", "-s", "full-upgrade"},
		Env: []string{"LC_ALL=C"}, Quiet: true})
	if err != nil {
		return nil, fmt.Errorf("simulating the upgrade (apt-get -s full-upgrade) failed: %w", err)
	}
	var lines []string
	for _, l := range run.Scan(out) {
		if strings.HasPrefix(l, "Inst ") || strings.HasPrefix(l, "Remv ") {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func doUpgrade(c *Ctx) error {
	if err := c.Update(); err != nil {
		return err
	}
	before, err := simulateUpgrade(c)
	if err != nil {
		return err
	}
	for _, l := range before {
		if strings.HasPrefix(l, "Inst linux-image-") {
			c.Reboot("the upgrade installed a new kernel")
			break
		}
	}
	if err = c.FullUpgrade(); err != nil {
		return fmt.Errorf("the full upgrade (apt-get full-upgrade) failed: %w. Check the messages above, fix the package it names, and run kiln again", err)
	}
	if run.Exists("/run/reboot-required") {
		c.Reboot("a package asked for a reboot (/run/reboot-required)")
	}
	// Check: nothing is left to upgrade.
	left, err := simulateUpgrade(c)
	if err != nil {
		return err
	}
	if len(left) > 0 {
		return fmt.Errorf("%d package changes are still waiting after the upgrade (first: %s); run `apt-get full-upgrade` to see why", len(left), left[0])
	}
	return nil
}
