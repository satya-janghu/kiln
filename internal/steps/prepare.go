package steps

import (
	"fmt"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// doPrepare is catalog "Safety check and waiting for apt", after main.go's
// refusal checks: wait for first-boot updates, finish a half-done dpkg run,
// and install kiln's own prerequisites.
func doPrepare(c *Ctx) error {
	if err := c.WaitForApt(); err != nil {
		return err
	}

	// dpkg --audit always exits 0, so its output decides (debian13-notes).
	// The repair comes before any install: apt refuses to install while
	// dpkg is half-done.
	audit, _ := c.Out("dpkg", "--audit")
	if strings.TrimSpace(audit) != "" {
		c.Say("A previous package install was left half-done; finishing it first...")
		_, err := c.Exec(run.Cmd{Name: "dpkg", Args: []string{"--force-confdef", "--force-confold", "--configure", "-a"},
			Env: []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a", "APT_LISTCHANGES_FRONTEND=none", "UCF_FORCE_CONFFOLD=1", "LC_ALL=C"}})
		if err != nil {
			return fmt.Errorf("finishing the half-done package install (dpkg --configure -a) failed: %w. Run `dpkg --configure -a` yourself, fix what it reports, then run kiln again", err)
		}
		if err := c.AptGet("-f", "install", "-y"); err != nil {
			return fmt.Errorf("repairing broken packages (apt-get -f install) failed: %w. Run `apt-get -f install` yourself, fix what it reports, then run kiln again", err)
		}
	}

	var missing []string
	for _, p := range []string{"ca-certificates", "pciutils"} {
		if !run.Installed(c.S, p) {
			missing = append(missing, p)
		}
	}
	if err := c.Install(missing...); err != nil {
		return fmt.Errorf("%w. Check the network connection and run kiln again", err)
	}
	return nil
}
