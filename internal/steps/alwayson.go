package steps

import (
	"fmt"
	"os"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

const (
	logindAlwaysOn = "/etc/systemd/logind.conf.d/60-kiln-always-on.conf"
	dconfProfile   = "/etc/dconf/profile/user"
	dconfPowerKeys = "/etc/dconf/db/local.d/60-kiln-power"
	dconfPowerLock = "/etc/dconf/db/local.d/locks/60-kiln-power"
	gdmGreeter     = "/etc/gdm3/greeter.dconf-defaults"
	gsdPower       = "org/gnome/settings-daemon/plugins/power"
)

var sleepTargets = []string{"sleep.target", "suspend.target", "hibernate.target", "hybrid-sleep.target", "suspend-then-hibernate.target"}

const logindAlwaysOnContent = `# Managed by kiln
[Login]
HandleLidSwitch=ignore
HandleLidSwitchExternalPower=ignore
HandleLidSwitchDocked=ignore
HandleSuspendKey=ignore
HandleSuspendKeyLongPress=ignore
HandleHibernateKey=ignore
HandleHibernateKeyLongPress=ignore
IdleAction=ignore
`

// gnomePowerKeys are set system-wide and locked (D23, P10).
var gnomePowerKeys = [][2]string{
	{"sleep-inactive-ac-type", "'nothing'"},
	{"sleep-inactive-battery-type", "'nothing'"},
	{"power-button-action", "'interactive'"},
}

func doAlwaysOn(c *Ctx) error {
	if err := c.Run("systemctl", append([]string{"mask"}, sleepTargets...)...); err != nil {
		return fmt.Errorf("turning off sleep (systemctl mask) failed: %w", err)
	}
	if err := c.Write(logindAlwaysOn, 0o644, logindAlwaysOnContent); err != nil {
		return err
	}
	// logind is Type=notify-reload; a reload re-reads the config.
	if err := c.Run("systemctl", "reload", "systemd-logind"); err != nil {
		return fmt.Errorf("reloading systemd-logind failed: %w", err)
	}
	if c.F.GNOME {
		if err := gnomeAlwaysOn(c); err != nil {
			return err
		}
	}

	// Check.
	for _, t := range sleepTargets {
		if out, _ := c.Out("systemctl", "show", "-p", "LoadState", "--value", t); strings.TrimSpace(out) != "masked" {
			return fmt.Errorf("%s isn't masked; run `systemctl mask %s`", t, t)
		}
	}
	if c.F.GNOME {
		for _, k := range gnomePowerKeys {
			key := "/" + gsdPower + "/" + k[0]
			out, err := c.Exec(withQuiet(run.UserCmd(c.U, "dconf", "read", key)))
			if err != nil || strings.TrimSpace(out) != k[1] {
				return fmt.Errorf("GNOME's %s reads %q for %s, not %s; check %s and run `dconf update`", key, strings.TrimSpace(out), c.U.Name, k[1], dconfPowerKeys)
			}
		}
	}
	return nil
}

func withQuiet(cmd run.Cmd) run.Cmd { cmd.Quiet = true; return cmd }

func gnomeAlwaysOn(c *Ctx) error {
	// dconf update needs the dconf command, which GNOME doesn't always bring.
	if err := c.Install("dconf-cli"); err != nil {
		return err
	}
	// Debian ships no user profile; an existing one only gains the line.
	profile := "user-db:user\nsystem-db:local\n"
	if run.Exists(dconfProfile) {
		profile = run.Read(dconfProfile)
		has := false
		for _, l := range strings.Split(profile, "\n") {
			if strings.TrimSpace(l) == "system-db:local" {
				has = true
			}
		}
		if !has {
			if profile != "" && !strings.HasSuffix(profile, "\n") {
				profile += "\n"
			}
			profile += "system-db:local\n"
		}
	}
	if err := c.Write(dconfProfile, 0o644, profile); err != nil {
		return err
	}
	keys := "# Managed by kiln\n[" + gsdPower + "]\n"
	locks := ""
	for _, k := range gnomePowerKeys {
		keys += k[0] + "=" + k[1] + "\n"
		locks += "/" + gsdPower + "/" + k[0] + "\n"
	}
	if err := c.Write(dconfPowerKeys, 0o644, keys); err != nil {
		return err
	}
	if err := c.Write(dconfPowerLock, 0o644, locks); err != nil {
		return err
	}
	if err := c.Run("dconf", "update"); err != nil {
		return fmt.Errorf("`dconf update` failed: %w", err)
	}

	// The login screen: key-edit the ucf-managed greeter file, never rewrite it.
	if !run.Exists(gdmGreeter) {
		return nil
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(gdmGreeter); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := c.Write(gdmGreeter, mode, editGreeter(run.Read(gdmGreeter))); err != nil {
		return err
	}
	// Runs /usr/share/gdm/generate-config, then SIGHUP; sessions survive.
	if err := c.Run("systemctl", "reload", "gdm3"); err != nil {
		return fmt.Errorf("reloading the login screen (gdm3) failed: %w", err)
	}
	return nil
}

// editGreeter uncomments or inserts the two sleep-inactive keys under the
// power section header, leaving every other line as it is.
func editGreeter(content string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	header := "[" + gsdPower + "]"
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == header {
			start = i
			break
		}
	}
	if start < 0 {
		lines = append(lines, "", header)
		start = len(lines) - 1
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	var missing []string
	for _, k := range gnomePowerKeys[:2] {
		want := k[0] + "=" + k[1]
		found := false
		for i := start + 1; i < end; i++ {
			t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(lines[i]), "#"))
			if name, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(name) == k[0] {
				lines[i] = want
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		at := start + 1
		lines = append(lines[:at], append(missing, lines[at:]...)...)
	}
	return strings.Join(lines, "\n") + "\n"
}
