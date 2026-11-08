package steps

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// Battery limit 70-80% (catalog 18, D56, debian13-notes 18).
const (
	batStart = "70"
	batEnd   = "80"

	batRule = "/etc/udev/rules.d/90-kiln-battery.rules"

	// HP's WMI BIOS interface (HPWMI_BIOS_GUID in the kernel's hp-wmi.c).
	hpWMIDevice = "/sys/bus/wmi/devices/5FB7F034-2C63-45E9-BE91-3D44E2C707E4" // #nosec G101 -- a WMI GUID, not a credential
	acpiCall    = "/proc/acpi/call"
	// HP WMI "battery charge operation" (command type 0x2B) through
	// \_SB.WMID.WMAA: signature "SECU", command (1 read, 2 write), type 0x2B,
	// 4 data bytes; the mode is the little-endian data word.
	hpRead      = `\_SB.WMID.WMAA 0 2 b53454355010000002b0000000400000000000000`
	hpChargeOff = `\_SB.WMID.WMAA 0 2 b53454355020000002b0000000400000000050000` // mode 0x0500
	hpChargeOn  = `\_SB.WMID.WMAA 0 2 b53454355020000002b0000000400000000000000` // mode 0x0000
	// A good answer: signature "PASS" and return code 0.
	hpAnswerOK = "{0x50, 0x41, 0x53, 0x53, 0x00, 0x00, 0x00, 0x00"

	hpModules = "/etc/modules-load.d/acpi_call.conf"
	hpScript  = "/usr/local/sbin/battery-limit"
	hpService = "/etc/systemd/system/battery-limit.service"
	hpTimer   = "/etc/systemd/system/battery-limit.timer"
)

func doBattery(c *Ctx) error {
	b := c.F.Battery
	switch {
	case run.Exists(b + "/charge_control_end_threshold"):
		return batThresholds(c, b)
	case run.Exists(hpWMIDevice):
		return batHP(c, b)
	}
	c.Note("Battery limit: %s has no charge-limit control kiln can use; use its BIOS/UEFI charge-limit option if it has one.", c.F.Machine())
	return nil
}

// batTee writes a value to a sysfs or procfs file.
func batTee(c *Ctx, path, value string) error {
	_, err := c.Exec(run.Cmd{Name: "tee", Args: []string{path}, Stdin: value + "\n", Quiet: true})
	return err
}

// batThresholds sets the kernel's charge thresholds now and, through a udev
// rule, whenever the battery appears. The hardware enforces them itself.
func batThresholds(c *Ctx, b string) error {
	name := filepath.Base(b)
	start, end := b+"/charge_control_start_threshold", b+"/charge_control_end_threshold"
	hasStart := run.Exists(start)

	write := func(startFirst bool) error {
		if hasStart && startFirst {
			if err := batTee(c, start, batStart); err != nil {
				return err
			}
		}
		if err := batTee(c, end, batEnd); err != nil {
			return err
		}
		if hasStart && !startFirst {
			return batTee(c, start, batStart)
		}
		return nil
	}
	// Start first (thinkpad_acpi rejects an end below the current start); if
	// the current end is below 70, the start write fails, so then end first.
	if err := write(true); err != nil {
		if err := write(false); err != nil {
			return fmt.Errorf("the battery driver refused the 70%%/80%% thresholds on %s: %w", name, err)
		}
	}

	rule := "# Written by kiln: keep " + name + " between 70% and 80% (D56)\n"
	if hasStart {
		rule += `SUBSYSTEM=="power_supply", ACTION=="add", KERNEL=="` + name + `", ATTR{charge_control_start_threshold}="` + batStart + `"` + "\n"
	}
	rule += `SUBSYSTEM=="power_supply", ACTION=="add", KERNEL=="` + name + `", ATTR{charge_control_end_threshold}="` + batEnd + `"` + "\n"
	if err := c.Write(batRule, 0o644, rule); err != nil {
		return err
	}
	if c.F.GNOME {
		c.Note("Battery limit: leave Settings → Power → Preserve Battery Health alone; turning it off resets the limit to 100%% until the next boot.")
	}

	gotEnd := strings.TrimSpace(run.Read(end))
	gotStart := strings.TrimSpace(run.Read(start))
	if gotEnd != batEnd || (hasStart && gotStart != batStart) {
		return fmt.Errorf("the battery driver reads back start %q and end %q instead of 70 and 80 on %s; use the BIOS/UEFI charge-limit option if it has one", gotStart, gotEnd, name)
	}
	return nil
}

// batHP uses HP's undocumented WMI charge command through acpi_call, with a
// small script and timer that are not part of kiln (D56).
func batHP(c *Ctx, b string) error {
	if c.F.SecureBoot {
		if err := enrollMOK(c); err != nil {
			return fmt.Errorf("kiln couldn't prepare signing for the acpi_call module, which Secure Boot needs; fix that and run kiln again: %w", err)
		}
	}
	// DKMS builds only for kernels whose headers are installed. The full
	// upgrade may have installed a newer kernel than the running one, so add
	// the running kernel's headers too when Debian still has them.
	running, _ := c.Out("uname", "-r")
	running = strings.TrimSpace(running)
	pkgs := []string{"linux-headers-amd64"}
	if running != "" && run.Available(c.S, "linux-headers-"+running) {
		pkgs = append(pkgs, "linux-headers-"+running)
	}
	if err := c.Install(append(pkgs, "acpi-call-dkms")...); err != nil {
		return err
	}
	// After a reboot (a new kernel, or the MOK enrollment), the timer's
	// script loads the module itself, so the limit starts on its own.
	afterReboot := false
	if err := c.Run("modprobe", "acpi_call"); err != nil {
		switch {
		case c.F.SecureBoot:
			afterReboot = true
			c.Reboot("the battery limit starts after the reboot, once the signing key is enrolled (choose Enroll MOK at the boot screen)")
		case running != newestKernel(c):
			afterReboot = true
			c.Reboot("the battery limit starts after the reboot into the new kernel")
		default:
			return fmt.Errorf("loading the acpi_call module failed; `dkms status` shows whether it was built. Fix that and run kiln again: %w", err)
		}
	}
	if afterReboot {
		return batHPFiles(c, b, false)
	}

	// Probe with a read call before relying on it.
	if err := batTee(c, acpiCall, hpRead); err != nil {
		return fmt.Errorf("writing to %s failed: %w", acpiCall, err)
	}
	answer := strings.TrimSpace(run.Read(acpiCall))
	c.Log("HP battery charge command (read) answered: %s", answer)
	if !strings.HasPrefix(answer, hpAnswerOK) {
		c.Note("Battery limit: %s doesn't answer HP's battery charge command; use its BIOS/UEFI charge-limit option if it has one.", c.F.Machine())
		return fmt.Errorf("this HP's firmware didn't accept the battery charge command (it answered %q), so no limit was set", answer)
	}

	return batHPFiles(c, b, true)
}

// batHPFiles writes the module autoload file, the script and the timer, and
// starts the timer. now: acpi_call is loaded, so run the script once and check.
func batHPFiles(c *Ctx, b string, now bool) error {
	if err := c.Write(hpModules, 0o644, "# Written by kiln: needed by "+hpScript+"\nacpi_call\n"); err != nil {
		return err
	}
	if err := c.Write(hpScript, 0o755, hpScriptText(b)); err != nil {
		return err
	}
	if err := c.Write(hpService, 0o644, `# Written by kiln (D56)
[Unit]
Description=Keep the battery between 70% and 80%

[Service]
Type=oneshot
ExecStart=`+hpScript+"\n"); err != nil {
		return err
	}
	if err := c.Write(hpTimer, 0o644, `# Written by kiln (D56)
[Unit]
Description=Check the battery charge limit every minute

[Timer]
OnBootSec=30s
OnUnitActiveSec=60s
AccuracySec=1s

[Install]
WantedBy=timers.target
`); err != nil {
		return err
	}
	steps := [][]string{{"daemon-reload"}, {"enable", "--now", "battery-limit.timer"}}
	if now {
		steps = append(steps, []string{"start", "battery-limit.service"})
	}
	for _, args := range steps {
		if err := c.Run("systemctl", args...); err != nil {
			return fmt.Errorf("`systemctl %s` failed; `journalctl -u battery-limit` shows why: %w", strings.Join(args, " "), err)
		}
	}
	c.Note("Battery limit: %s runs every minute (battery-limit.timer) and keeps the battery between 70%% and 80%% with HP's charge command; plugged in, it stays near 80%%.", hpScript)

	switch {
	case now && !run.Exists("/sys/module/acpi_call"):
		return errors.New("the acpi_call module isn't loaded; run `modprobe acpi_call` and kiln again")
	case !c.OK("systemctl", "is-active", "--quiet", "battery-limit.timer"):
		return errors.New("battery-limit.timer isn't active; `systemctl status battery-limit.timer` shows why")
	}
	return nil
}

// hpScriptText is the plain POSIX sh script the timer runs.
func hpScriptText(bat string) string {
	return `#!/bin/sh
# Written by kiln (D56). Keeps the battery between 70% and 80% on an HP laptop
# without a BIOS charge limit, through HP's WMI battery charge command (type
# 0x2B) and acpi_call. battery-limit.timer runs it every minute.
bat=` + bat + `
call=` + acpiCall + `

[ -e "$call" ] || modprobe acpi_call || exit 1
cap=$(cat "$bat/capacity") || exit 1

if [ "$cap" -gt 80 ]; then
	# Charging off (mode 0x0500).
	printf '%s\n' '` + hpChargeOff + `' > "$call"
elif [ "$cap" -le 70 ]; then
	# Normal charging (mode 0x0000).
	printf '%s\n' '` + hpChargeOn + `' > "$call"
fi
`
}
