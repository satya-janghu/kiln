// Package thermal holds thermal logic shared by the thermal step and `kiln thermal apply`
// (catalog step 12). All sysfs/procfs reads and writes go through sys.System.
package thermal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/runner"
	"github.com/satya-janghu/kiln/internal/state"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Paths and units used by the step and the boot helper.
const (
	ModprobePath    = "/etc/modprobe.d/kiln-thinkpad.conf"
	ConfigPath      = "/etc/thinkfan.yaml"
	FanPath         = "/proc/acpi/ibm/fan"
	FanControlParam = "/sys/module/thinkpad_acpi/parameters/fan_control"
	BootServicePath = "/etc/systemd/system/kiln-thinkfan-enable.service"
	BootServiceUnit = "kiln-thinkfan-enable.service"
	ThinkfanUnit    = "thinkfan.service"

	profilePath    = "/sys/firmware/acpi/platform_profile"
	choicesPath    = "/sys/firmware/acpi/platform_profile_choices"
	profileClass   = "/sys/class/platform-profile"
	amdPStatePath  = "/sys/devices/system/cpu/amd_pstate/status"
	eppGlob        = "/sys/devices/system/cpu/cpufreq/policy*/energy_performance_preference"
	hwmonNameGlob  = "/sys/class/hwmon/hwmon*/name"
	lockName       = "thermal"
	validateWindow = "10"
)

// Lock takes /run/kiln/thermal.lock, waiting up to wait for the boot helper (D41).
func Lock(ctx context.Context, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	for {
		release, pid, err := runner.Lock(lockName)
		if err == nil {
			return release, nil
		}
		busy := strings.Contains(err.Error(), "busy") // runner.Lock: another process holds it
		if !busy || time.Now().After(deadline) {
			if busy {
				who := "kiln's thinkfan boot helper"
				if pid > 0 {
					who += fmt.Sprintf(" (pid %d)", pid)
				}
				return nil, fmt.Errorf("%s is still running; wait a minute and run kiln again", who)
			}
			return nil, fmt.Errorf("could not take %s/%s.lock: %w", runner.LockDir, lockName, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func read(s sys.System, p string) (string, bool) {
	b, err := s.ReadFile(p)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func exists(s sys.System, p string) bool { _, err := s.Stat(p); return err == nil }

// --- platform profile and amd-pstate (verify and report only; kiln never writes them) ---

// ProfileReport is the read-only platform-profile verification.
type ProfileReport struct {
	OK     bool
	Detail string
}

// VerifyProfile checks that the platform profile is `performance` wherever it's offered.
// On kernels ≥ 6.14 the legacy file reads `custom` when handlers disagree; that
// counts as "not performance" and the per-handler values are reported.
func VerifyProfile(s sys.System) ProfileReport {
	choicesRaw, ok := read(s, choicesPath)
	if !ok {
		return ProfileReport{OK: true, Detail: "no ACPI platform profile on this machine"}
	}
	choices := strings.Fields(choicesRaw)
	cur, _ := read(s, profilePath)
	if !contains(choices, "performance") {
		return ProfileReport{OK: true, Detail: fmt.Sprintf("platform profile %s (performance isn't offered: %s)", cur, choicesRaw)}
	}
	if cur == "performance" {
		return ProfileReport{OK: true, Detail: "platform profile performance"}
	}
	if cur == "custom" {
		var per []string
		dirs, _ := s.Glob(profileClass + "/*")
		sort.Strings(dirs)
		for _, d := range dirs {
			name, _ := read(s, d+"/name")
			p, _ := read(s, d+"/profile")
			if name == "" {
				name = filepath.Base(d)
			}
			per = append(per, name+"="+p)
		}
		return ProfileReport{Detail: "platform profile reads custom (handlers disagree: " + strings.Join(per, ", ") + ")"}
	}
	return ProfileReport{Detail: "platform profile is " + orUnknown(cur) + ", expected performance"}
}

// AMDReport describes amd-pstate's mode and the per-policy EPP. notes lists
// differences from what the performance profile sets (EPP performance).
func AMDReport(s sys.System, f *facts.Facts) (detail string, notes []string) {
	if f.CPU.Vendor != "AMD" {
		return "", nil
	}
	status, ok := read(s, amdPStatePath)
	if !ok {
		return "amd-pstate isn't in use", nil
	}
	detail = "amd-pstate " + status
	paths, _ := s.Glob(eppGlob)
	sort.Strings(paths)
	values := map[string][]string{}
	for _, p := range paths {
		v, _ := read(s, p)
		values[v] = append(values[v], filepath.Base(filepath.Dir(p)))
	}
	if len(values) == 0 {
		return detail, nil
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	detail += ", EPP " + strings.Join(keys, "/")
	for _, k := range keys {
		if k != "performance" {
			notes = append(notes, fmt.Sprintf("CPU energy preference (EPP) is %q on %s, not performance; the power manager sets it with the performance profile (amd-pstate %s mode). kiln doesn't change the kernel command line.",
				k, strings.Join(values[k], ", "), status))
		}
	}
	return detail, notes
}

// --- ThinkPad fan control (thinkfan) ---

// Fan is the ThinkPad fan sub-part's applicability.
type Fan struct {
	ThinkPad bool     // vendor LENOVO with thinkpad_acpi loaded
	Applies  bool     // and the fan file plus usable sensors exist
	Sensors  []string // detected hwmon names, in config order
}

// sensorOrder is the catalog's sensor list; the config lists those detected.
var sensorOrder = []string{"k10temp", "amdgpu"}

// ThinkPadFan detects fan control and sensors at run time (fact status R).
func ThinkPadFan(s sys.System, f *facts.Facts) Fan {
	fan := Fan{ThinkPad: f.DMI.Vendor == "LENOVO" && f.ThinkpadACPI}
	if !fan.ThinkPad || !exists(s, FanPath) {
		return fan
	}
	names := map[string]bool{}
	files, _ := s.Glob(hwmonNameGlob)
	for _, p := range files {
		if n, ok := read(s, p); ok {
			names[n] = true
		}
	}
	for _, n := range sensorOrder {
		if names[n] {
			fan.Sensors = append(fan.Sensors, n)
		}
	}
	fan.Applies = len(fan.Sensors) > 0
	return fan
}

// ModprobeContent enables thinkpad_acpi fan writes.
func ModprobeContent() []byte {
	return []byte(sys.ManagedHeader("#") + "options thinkpad_acpi fan_control=1\n")
}

// ConfigContent is /etc/thinkfan.yaml: the fan never stops, reaches level 7
// (highest in-spec) from about 65–70 °C, and is disengaged only from 78 °C.
// The first detected sensor is required; later ones are optional.
func ConfigContent(sensors []string) []byte {
	var b strings.Builder
	b.WriteString(sys.ManagedHeader("#"))
	b.WriteString("sensors:\n")
	for i, n := range sensors {
		b.WriteString("  - hwmon: /sys/class/hwmon\n")
		b.WriteString("    name: " + n + "\n")
		b.WriteString("    indices: [1]\n")
		if i > 0 {
			b.WriteString("    optional: true\n")
		}
	}
	b.WriteString(`fans:
  - tpacpi: /proc/acpi/ibm/fan
levels:
  - [1, 0, 50]
  - [3, 45, 58]
  - [5, 55, 66]
  - [7, 63, 80]
  - ["level disengaged", 78, 32767]
`)
	return []byte(b.String())
}

// BootServiceContent runs `kiln thermal apply` once at boot, after which it disables itself.
func BootServiceContent() []byte {
	return []byte(sys.ManagedHeader("#") + `[Unit]
Description=kiln: validate and enable thinkfan once ThinkPad fan control is active

[Service]
Type=oneshot
ExecStart=/usr/local/bin/kiln thermal apply

[Install]
WantedBy=multi-user.target
`)
}

// FanControlActive reports thinkpad_acpi's fan_control parameter is Y.
func FanControlActive(s sys.System) bool {
	v, ok := read(s, FanControlParam)
	return ok && v == "Y"
}

func unitIs(ctx context.Context, s sys.System, verb, unit, want string) bool {
	r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{verb, unit}, Timeout: 30 * time.Second})
	return strings.TrimSpace(r.Stdout) == want
}

// ThinkfanActive reports thinkfan.service is running.
func ThinkfanActive(ctx context.Context, s sys.System) bool {
	return unitIs(ctx, s, "is-active", ThinkfanUnit, "active")
}

// BootEnabled reports the boot-time enable service is installed and enabled.
func BootEnabled(ctx context.Context, s sys.System) bool {
	return exists(s, BootServicePath) && unitIs(ctx, s, "is-enabled", BootServiceUnit, "enabled")
}

// BootFailed reports the boot-time enable service failed during this boot.
func BootFailed(ctx context.Context, s sys.System) bool {
	return unitIs(ctx, s, "is-failed", BootServiceUnit, "failed")
}

// BootPending reports the boot-time enable is still waiting for its first
// chance: the service is enabled, fan_control isn't Y yet (thinkpad_acpi hasn't
// reloaded with the new option) and the helper hasn't failed this boot.
func BootPending(ctx context.Context, s sys.System) bool {
	return BootEnabled(ctx, s) && !FanControlActive(s) && !BootFailed(ctx, s)
}

// ValidateAndEnable validates /etc/thinkfan.yaml and starts thinkfan. thinkfan
// has no config-test flag, so it runs in the foreground under `timeout 10`:
// exit 124 (still running when killed) means the config is valid. On any other
// exit the fan goes back to firmware control and thinkfan stays disabled.
// fan_control must already be Y.
func ValidateAndEnable(ctx context.Context, s sys.System) error {
	if !s.DryRun() && ThinkfanActive(ctx, s) {
		// two instances would fight over the fan
		if _, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"stop", ThinkfanUnit}}); err != nil {
			return err
		}
	}
	_, err := s.Run(ctx, sys.Cmd{Name: "timeout", Args: []string{validateWindow, "thinkfan", "-n", "-q", "-c", ConfigPath}, Timeout: time.Minute})
	var ee *sys.ExitError
	valid := s.DryRun() || (errors.As(err, &ee) && ee.Code == 124)
	if !valid {
		why := "it exited by itself"
		if err != nil {
			why = err.Error()
		}
		_, _ = s.Run(ctx, sys.Cmd{Name: "tee", Args: []string{FanPath}, Stdin: []byte("level auto\n"), Timeout: 15 * time.Second})
		_, _ = s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"disable", ThinkfanUnit}})
		return fmt.Errorf("thinkfan rejected %s (%s). The fan is back under firmware control and thinkfan stays off; check the sensors with `sensors` and fix the file, then run kiln again", ConfigPath, why)
	}
	if _, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"enable", "--now", ThinkfanUnit}}); err != nil {
		return err
	}
	if s.DryRun() {
		return nil
	}
	deadline := time.Now().Add(15 * time.Second)
	for !ThinkfanActive(ctx, s) {
		if time.Now().After(deadline) {
			_, _ = s.Run(ctx, sys.Cmd{Name: "tee", Args: []string{FanPath}, Stdin: []byte("level auto\n"), Timeout: 15 * time.Second})
			return errors.New("thinkfan didn't stay running; see `journalctl -u thinkfan`. The fan is back under firmware control")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil
}

// BootApply validates and enables thinkfan once fan_control is active (kiln thermal apply).
// The caller holds the thermal lock. On success it disables its own boot unit.
func BootApply(ctx context.Context, s sys.System, _ *facts.Facts, _ *state.State) error {
	if !exists(s, FanPath) {
		return fmt.Errorf("%s doesn't exist, so this machine doesn't expose ThinkPad fan control; nothing to do", FanPath)
	}
	if !exists(s, ConfigPath) {
		return fmt.Errorf("%s is missing; run `sudo kiln` again (thermal step)", ConfigPath)
	}
	// thinkpad_acpi is loaded by udev during boot; give it a moment.
	deadline := time.Now().Add(60 * time.Second)
	for !FanControlActive(s) {
		if s.DryRun() || time.Now().After(deadline) {
			return fmt.Errorf("ThinkPad fan control is still off (%s isn't Y); check %s and reboot", FanControlParam, ModprobePath)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if err := ValidateAndEnable(ctx, s); err != nil {
		return err
	}
	_, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"disable", BootServiceUnit}})
	return err
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func orUnknown(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}
