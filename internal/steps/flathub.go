package steps

import (
	"errors"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

const flathubURL = "https://dl.flathub.org/repo/flathub.flatpakrepo"

// doFlathub installs flatpak and adds the Flathub remote system-wide (catalog 15).
func doFlathub(c *Ctx) error {
	pkgs := []string{"flatpak"}
	if c.F.GNOME {
		pkgs = append(pkgs, "gnome-software-plugin-flatpak")
	}
	if err := c.Install(pkgs...); err != nil {
		return err
	}
	if err := c.Run("flatpak", "remote-add", "--system", "--if-not-exists", "flathub", flathubURL); err != nil {
		return errors.New("adding the Flathub remote failed; check the network connection (" + flathubURL + ") and run kiln again: " + err.Error())
	}
	out, err := c.Out("flatpak", "remotes", "--system")
	if err == nil {
		for _, l := range run.Scan(out) {
			if f := strings.Fields(l); len(f) > 0 && f[0] == "flathub" {
				return nil
			}
		}
	}
	return errors.New("`flatpak remotes --system` doesn't list flathub after adding it; run kiln again")
}
