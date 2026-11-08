package steps

import (
	"time"

	"github.com/satya-janghu/kiln/internal/run"
)

func doCockpit(c *Ctx) error {
	// Recommends are kept (cockpit-storaged, cockpit-packagekit, bringing
	// network-manager, packagekit, udisks2). cockpit-networkmanager is named
	// too, since apt adds no Recommends to a package that's already installed.
	if err := c.InstallOpts(run.InstallOpts{Suite: "trixie-backports"}, "cockpit", "cockpit-networkmanager"); err != nil {
		return err
	}
	c.Note("Cockpit: https://%s:9090, with %s's password (root can't log in there).", addrForNotes(c), c.U.Name)

	// Check: the package enables and starts cockpit.socket.
	return pollFor(c, 15*time.Second, func() error { return unitUp(c, "cockpit.socket") })
}
