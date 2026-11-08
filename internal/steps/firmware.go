package steps

import (
	"fmt"
	"slices"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// firmwarePackages picks microcode and firmware for the detected hardware
// (non-free always, D52). firmware-misc-nonfree is installed separately,
// without Recommends.
func firmwarePackages(c *Ctx) (microcode, firmware []string) {
	switch c.F.CPUVendor {
	case "AMD":
		microcode = []string{"amd64-microcode"}
	case "Intel":
		microcode = []string{"intel-microcode"}
	}
	add := func(p ...string) {
		for _, x := range p {
			if !slices.Contains(firmware, x) {
				firmware = append(firmware, x)
			}
		}
	}
	if c.F.HasGPU("1002") {
		add("firmware-amd-graphics")
	}
	if c.F.HasGPU("8086") {
		add("firmware-intel-graphics", "firmware-intel-misc")
	}
	if c.F.HasGPU("10de") {
		add("firmware-nvidia-graphics")
	}
	for _, d := range c.F.NetDriver {
		switch {
		case d == "iwlwifi":
			add("firmware-iwlwifi")
		case strings.HasPrefix(d, "ath"):
			add("firmware-atheros")
		case strings.HasPrefix(d, "mt7"):
			add("firmware-mediatek")
		case d == "brcmfmac":
			add("firmware-brcm80211")
		case d == "r8169", d == "r8152", strings.HasPrefix(d, "rtw_"), strings.HasPrefix(d, "rtw89"), strings.HasPrefix(d, "rtl8"):
			add("firmware-realtek")
		}
	}
	// A Wi-Fi card missing its firmware has no interface, so match PCI vendors too.
	for _, w := range c.F.WiFiPCI {
		switch w.Vendor {
		case "8086":
			add("firmware-iwlwifi")
		case "14c3":
			add("firmware-mediatek")
		case "10ec":
			add("firmware-realtek")
		case "168c", "17cb":
			add("firmware-atheros")
		case "14e4":
			add("firmware-brcm80211")
		}
	}
	return microcode, firmware
}

func doFirmware(c *Ctx) error {
	const misc = "firmware-misc-nonfree"
	microcode, firmware := firmwarePackages(c)
	all := append(append([]string{misc}, microcode...), firmware...)

	newly := false
	for _, p := range all {
		if !run.Installed(c.S, p) {
			newly = true
		}
	}
	for _, p := range microcode {
		if !run.Installed(c.S, p) {
			c.Reboot("CPU microcode was installed (%s)", p)
		}
	}

	if err := c.InstallOpts(run.InstallOpts{NoRecommends: true}, misc); err != nil {
		return err
	}
	if err := c.Install(append(append([]string{}, microcode...), firmware...)...); err != nil {
		return err
	}
	if err := c.Install("isenkram-cli"); err != nil {
		return err
	}

	if newly {
		if err := c.Run("update-initramfs", "-u", "-k", "all"); err != nil {
			return fmt.Errorf("the firmware is installed, but rebuilding the initramfs failed (%w); this is usually a full /boot. Free space in /boot and run kiln again", err)
		}
		c.Reboot("new firmware or microcode was installed")
	}

	// List mode only: package names on stdout; the exit code doesn't matter
	// (debian13-notes, isenkram).
	out, _ := c.Exec(run.Cmd{Name: "isenkram-autoinstall-firmware", Args: []string{"-l"}, Quiet: true})
	if pkgs := strings.Fields(out); len(pkgs) > 0 {
		c.Note("Firmware still missing for loaded drivers: %s. Install it with `apt install %s` if you need that hardware.",
			strings.Join(pkgs, ", "), strings.Join(pkgs, " "))
	}

	// Check: everything selected is installed.
	var missing []string
	for _, p := range append(all, "isenkram-cli") {
		if !run.Installed(c.S, p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("these packages didn't install: %s; run `apt-get install %s` to see why", strings.Join(missing, ", "), strings.Join(missing, " "))
	}
	return nil
}
