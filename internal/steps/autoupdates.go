package steps

import (
	"fmt"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// autoUpdateFiles are written as-is. 20auto-upgrades (ucf) is never touched;
// 52kiln sorts after it and after 50unattended-upgrades.
var autoUpdateFiles = [][2]string{
	{"/etc/apt/apt.conf.d/52kiln-unattended-upgrades", `// Managed by kiln
APT::Periodic::Update-Package-Lists "always";
APT::Periodic::Unattended-Upgrade "always";
#clear Unattended-Upgrade::Origins-Pattern;
Unattended-Upgrade::Origins-Pattern {
    "origin=*";
};
Unattended-Upgrade::Automatic-Reboot "false";
`},
	{"/etc/systemd/system/apt-daily.timer.d/60-kiln.conf",
		"# Managed by kiln\n[Timer]\nOnCalendar=\nOnCalendar=*-*-* 06,18:00\nRandomizedDelaySec=15m\n"},
	{"/etc/systemd/system/apt-daily-upgrade.timer.d/60-kiln.conf",
		"# Managed by kiln\n[Timer]\nOnCalendar=\nOnCalendar=*-*-* 06,18:30\nRandomizedDelaySec=15m\n"},
	{"/etc/needrestart/conf.d/50-kiln.conf", "# Managed by kiln\n$nrconf{restart} = 'a';\n"},
}

func doAutoUpdates(c *Ctx) error {
	if err := c.Preseed("unattended-upgrades unattended-upgrades/enable_auto_updates boolean true"); err != nil {
		return fmt.Errorf("preseeding unattended-upgrades failed: %w", err)
	}
	if err := c.Install("unattended-upgrades", "apt-listchanges", "needrestart"); err != nil {
		return err
	}
	for _, f := range autoUpdateFiles {
		if err := c.Write(f[0], 0o644, f[1]); err != nil {
			return err
		}
	}
	if err := c.Run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := c.Run("systemctl", "restart", "apt-daily.timer", "apt-daily-upgrade.timer"); err != nil {
		return fmt.Errorf("restarting the update timers failed: %w", err)
	}

	// Check: apt sees kiln's values, and the timers run on the new schedule.
	out, err := c.Out("apt-config", "dump")
	if err != nil {
		return fmt.Errorf("`apt-config dump` failed: %w", err)
	}
	periodic := map[string]string{}
	var origins []string
	for _, l := range run.Scan(out) {
		k, v, ok := strings.Cut(l, " ")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSuffix(strings.TrimSpace(v), ";"), `"`)
		switch k {
		case "APT::Periodic::Update-Package-Lists", "APT::Periodic::Unattended-Upgrade":
			periodic[k] = v
		case "Unattended-Upgrade::Origins-Pattern::":
			origins = append(origins, v)
		}
	}
	for _, k := range []string{"APT::Periodic::Update-Package-Lists", "APT::Periodic::Unattended-Upgrade"} {
		if periodic[k] != "always" {
			return fmt.Errorf("apt's %s is %q, not \"always\"; another file in /etc/apt/apt.conf.d overrides kiln's", k, periodic[k])
		}
	}
	if len(origins) != 1 || origins[0] != "origin=*" {
		return fmt.Errorf("unattended-upgrades' Origins-Pattern is %q, not just \"origin=*\"; another file in /etc/apt/apt.conf.d adds to it after kiln's", strings.Join(origins, ", "))
	}
	for _, t := range [][2]string{{"apt-daily.timer", "06,18:00"}, {"apt-daily-upgrade.timer", "06,18:30"}} {
		out, _ := c.Out("systemctl", "show", "-p", "ActiveState", "-p", "TimersCalendar", t[0])
		if !strings.Contains(out, "ActiveState=active") {
			return fmt.Errorf("%s isn't active; see `systemctl status %s`", t[0], t[0])
		}
		if !strings.Contains(out, t[1]) {
			return fmt.Errorf("%s isn't scheduled at %s; check `systemctl cat %s`", t[0], t[1], t[0])
		}
	}

	c.Note("Updates run at about 06:00 and 18:00 and are skipped while on battery. Logs are in /var/log/unattended-upgrades/.")
	c.Note("Kernel updates need a reboot, which never happens automatically; /run/reboot-required shows when one is pending.")
	c.Note("A Docker update restarts containers only if they have a restart policy.")
	if c.Picked("nvidia") && len(c.F.NVIDIA()) > 0 {
		c.Note("NVIDIA driver updates (including a new driver branch) arrive automatically and also need a reboot.")
	}
	return nil
}
