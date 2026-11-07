// Package all lists every step in catalog order (docs/steps-catalog.md).
// The summary (catalog step 25) is the runner's final phase, not a step.
package all

import (
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/steps/aptsources"
	"github.com/satya-janghu/kiln/internal/steps/autoupdates"
	"github.com/satya-janghu/kiln/internal/steps/battery"
	"github.com/satya-janghu/kiln/internal/steps/cockpit"
	"github.com/satya-janghu/kiln/internal/steps/devtools"
	"github.com/satya-janghu/kiln/internal/steps/docker"
	"github.com/satya-janghu/kiln/internal/steps/fail2ban"
	"github.com/satya-janghu/kiln/internal/steps/firmware"
	"github.com/satya-janghu/kiln/internal/steps/fwupd"
	"github.com/satya-janghu/kiln/internal/steps/golang"
	"github.com/satya-janghu/kiln/internal/steps/kernel"
	"github.com/satya-janghu/kiln/internal/steps/mirrors"
	"github.com/satya-janghu/kiln/internal/steps/multimedia"
	"github.com/satya-janghu/kiln/internal/steps/netmigrate"
	"github.com/satya-janghu/kiln/internal/steps/node"
	"github.com/satya-janghu/kiln/internal/steps/power"
	"github.com/satya-janghu/kiln/internal/steps/preflight"
	"github.com/satya-janghu/kiln/internal/steps/python"
	"github.com/satya-janghu/kiln/internal/steps/repos"
	"github.com/satya-janghu/kiln/internal/steps/ssh"
	"github.com/satya-janghu/kiln/internal/steps/sudo"
	"github.com/satya-janghu/kiln/internal/steps/system"
	"github.com/satya-janghu/kiln/internal/steps/thermal"
	"github.com/satya-janghu/kiln/internal/steps/upgrade"
)

// Steps returns the catalog in run order.
func Steps() []step.Step {
	return []step.Step{
		preflight.New(),
		sudo.New(),
		aptsources.New(),
		mirrors.New(),
		upgrade.New(),
		firmware.New(),
		kernel.New(),
		netmigrate.New(),
		multimedia.New(),
		repos.New(),
		devtools.New(),
		system.New(),
		power.New(),
		thermal.New(),
		battery.New(),
		ssh.New(),
		fail2ban.New(),
		docker.New(),
		cockpit.New(),
		autoupdates.New(),
		node.New(),
		python.New(),
		golang.New(),
		fwupd.New(),
	}
}
