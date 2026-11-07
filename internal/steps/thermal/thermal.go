// Package thermal implements catalog step 12 `thermal` (docs/steps-catalog.md).
// kiln never writes platform_profile (ppd or tuned set it, D6); it verifies and
// reports. On ThinkPads it sets up thinkfan; the shared logic lives in internal/thermal.
package thermal

import (
	"context"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
	"github.com/satya-janghu/kiln/internal/thermal"
)

// Step is the `thermal` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "thermal" }
func (Step) Title() string       { return "Cooling and power profile" }
func (Step) Category() string    { return "power" }
func (Step) DependsOn() []string { return []string{"power"} }

func (Step) Packages() []step.PackageRef {
	return []step.PackageRef{
		{Name: "lm-sensors", Suite: "trixie", Component: "main", Repo: "debian"},
		{Name: "thinkfan", Suite: "trixie", Component: "main", Repo: "debian"},
	}
}

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "check_profile", Label: "Check the balanced platform profile", Kind: options.Bool, Default: true,
			Help: "Set by the power manager's balanced profile; kiln only verifies it and reports differences."},
		{Key: "thinkfan", Label: "thinkfan (ThinkPads)", Kind: options.Bool, Default: true,
			Help: "Only if this ThinkPad exposes fan control: the fan never stops and ramps up early."},
	}
}

// profileNotePrefix starts this step's platform-profile note.
const profileNotePrefix = "Platform profile: "

// reportProfile adds the platform-profile and AMD differences to the summary
// (kiln never writes these, so a difference is a note, not work to do) and
// returns the details for the step's status.
func reportProfile(env *step.Env, s sys.System) []string {
	r := thermal.VerifyProfile(s)
	if !r.OK {
		env.Notes.Add("%s%s. The power manager sets it; check `powerprofilesctl get` (with GNOME) or `tuned-adm active`.", profileNotePrefix, r.Detail)
	}
	detail := []string{r.Detail}
	d, notes := thermal.AMDReport(s, env.Facts)
	for _, n := range notes {
		env.Notes.Add("%s", n)
	}
	if d != "" {
		detail = append(detail, d)
	}
	return detail
}

func fileMatches(s sys.System, path string, want []byte) bool {
	b, err := s.ReadFile(path)
	return err == nil && string(b) == string(want)
}

// fanDone reports the ThinkPad part is in place: files match, thinkfan is
// installed, and it's running or the boot-time enable is pending.
func fanDone(ctx context.Context, env *step.Env, fan thermal.Fan) (bool, string) {
	s := env.Sys
	switch {
	case !fileMatches(s, thermal.ModprobePath, thermal.ModprobeContent()):
		return false, thermal.ModprobePath + " is missing or different"
	case !apt.Installed(ctx, s, "thinkfan"):
		return false, "thinkfan isn't installed"
	case !fileMatches(s, thermal.ConfigPath, thermal.ConfigContent(fan.Sensors)):
		return false, thermal.ConfigPath + " is missing or different"
	case thermal.ThinkfanActive(ctx, s):
		return true, "thinkfan running"
	case thermal.BootPending(ctx, s):
		return true, "thinkfan is enabled at the next boot"
	case thermal.BootEnabled(ctx, s):
		return false, "kiln's thinkfan boot helper didn't get thinkfan running; see `journalctl -b -u " + thermal.BootServiceUnit + "`"
	}
	return false, "thinkfan isn't running"
}

func fanWanted(env *step.Env, fan thermal.Fan) bool {
	return env.Opts.Bool("thinkfan") && fan.Applies
}

func (Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	var detail []string
	if !apt.Installed(ctx, env.Sys, "lm-sensors") {
		return step.Todo("lm-sensors isn't installed"), nil
	}
	if env.Opts.Bool("check_profile") {
		// Never Todo: a difference is reported as a note, every run.
		detail = append(detail, reportProfile(env, env.Sys)...)
	}
	if fan := thermal.ThinkPadFan(env.Sys, env.Facts); fanWanted(env, fan) {
		ok, d := fanDone(ctx, env, fan)
		if !ok {
			return step.Todo("%s", d), nil
		}
		detail = append(detail, d)
	}
	return step.Done(strings.Join(detail, "; ")), nil
}

func (Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	var acts []action.Action
	f := env.Facts
	if !apt.Installed(ctx, env.Sys, "lm-sensors") {
		// sensors-detect isn't run: the hwmon drivers here load automatically (P3).
		acts = append(acts, action.AptInstall{Pkgs: []string{"lm-sensors"}})
	}
	if env.Opts.Bool("check_profile") {
		acts = append(acts, action.Custom{
			Desc: "check that the platform profile is balanced where it's offered (per-handler values if it reads custom), report amd-pstate and the CPU energy preference, and add any difference to the summary (kiln never writes these)",
			Fn: func(_ context.Context, s sys.System) error {
				reportProfile(env, s)
				return nil
			},
		})
	}
	if f.DMI.Vendor == "HP" || strings.EqualFold(f.DMI.Vendor, "Hewlett-Packard") {
		env.Notes.Add("Linux can't drive the fans on this HP; cooling follows the platform profile. Check the BIOS for *Fan Always On*.")
	}

	fan := thermal.ThinkPadFan(env.Sys, f)
	if env.Opts.Bool("thinkfan") && fan.ThinkPad && !fan.Applies {
		if fan.FanFile {
			env.Notes.Add("ThinkPad fan control: not applicable: no supported temperature sensor (k10temp or amdgpu) was found.")
		} else {
			env.Notes.Add("ThinkPad fan control: not applicable: this ThinkPad doesn't expose fan control.")
		}
	}
	if !fanWanted(env, fan) {
		return acts, nil
	}
	if ok, _ := fanDone(ctx, env, fan); ok {
		return acts, nil
	}
	acts = append(acts, action.WriteFile{Path: thermal.ModprobePath, Content: thermal.ModprobeContent(), Mode: 0o644})
	if !apt.Installed(ctx, env.Sys, "thinkfan") {
		acts = append(acts, action.AptInstall{Pkgs: []string{"thinkfan"}})
	}
	acts = append(acts, action.WriteFile{Path: thermal.ConfigPath, Content: thermal.ConfigContent(fan.Sensors), Mode: 0o644})

	if thermal.FanControlActive(env.Sys) {
		acts = append(acts, action.Custom{
			Desc: "under /run/kiln/thermal.lock, stop thinkfan if it's running, validate " + thermal.ConfigPath + " with `timeout 10 thinkfan -n -q -c " + thermal.ConfigPath +
				"` (exit 124 = valid), then enable and start thinkfan; if it's rejected, write `level auto` to " + thermal.FanPath + " and leave thinkfan disabled",
			K: action.Services,
			Fn: func(ctx context.Context, s sys.System) error {
				if !s.DryRun() {
					release, err := thermal.Lock(ctx, 2*time.Minute)
					if err != nil {
						return err
					}
					defer release()
				}
				if err := thermal.ValidateAndEnable(ctx, s); err != nil {
					return err
				}
				if thermal.BootEnabled(ctx, s) {
					_, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"disable", thermal.BootServiceUnit}})
					return err
				}
				return nil
			},
		})
		return acts, nil
	}
	// fan_control isn't active until thinkpad_acpi reloads with the new option:
	// leave thinkfan disabled and validate at the next boot.
	acts = append(acts,
		action.WriteFile{Path: thermal.BootServicePath, Content: thermal.BootServiceContent(), Mode: 0o644},
		action.Systemctl{Verb: "daemon-reload"},
		action.Systemctl{Verb: "enable", Units: []string{thermal.BootServiceUnit}},
	)
	env.Notes.Reboot("ThinkPad fan control (thinkfan) is enabled at the next boot")
	return acts, nil
}
