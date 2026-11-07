// Package battery implements catalog step 13 `battery` (docs/steps-catalog.md).
// The method ladder lives in internal/battery, shared with `kiln battery`.
package battery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/battery"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Step is the `battery` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "battery" }
func (Step) Title() string       { return "Battery charge limit" }
func (Step) Category() string    { return "power" }
func (Step) DependsOn() []string { return []string{"preflight"} }

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "target_stop", Label: "Stop charging at (%)", Kind: options.Int, Default: 80,
			Help: "kiln tries values below 90, nearest to this first, and keeps the first one the hardware accepts.",
			Validate: func(v any) error {
				if n, _ := v.(int); n < 1 || n >= battery.MaxStop {
					return fmt.Errorf("use a value from 1 to %d", battery.MaxStop-1)
				}
				return nil
			}},
		{Key: "start_gap", Label: "Start charging this many points lower", Kind: options.Int, Default: 5,
			Help: "start = stop − gap, where the hardware supports a start value.",
			Validate: func(v any) error {
				if n, _ := v.(int); n < 0 || n > 50 {
					return errors.New("use 0 to 50")
				}
				return nil
			}},
		{Key: "software_fallback", Label: "Software fallback (inhibit-charge watcher)", Kind: options.Bool, Default: true,
			Help: "Only used when the battery has no thresholds or vendor mode but offers charge_behaviour inhibit-charge."},
	}
}

func (Step) Applies(env *step.Env) step.Applicability {
	if len(env.Facts.Batteries) == 0 {
		return step.No("no battery")
	}
	return step.Yes()
}

func opts(env *step.Env) battery.Options {
	return battery.Options{Stop: env.Opts.Int("target_stop"), Gap: env.Opts.Int("start_gap"), Software: env.Opts.Bool("software_fallback")}
}

type unitFile struct {
	path    string
	content []byte
}

func persistence() []unitFile {
	return []unitFile{
		{battery.ServicePath, battery.ServiceContent()},
		{battery.TimerPath, battery.TimerContent()},
		{battery.UdevRulePath, battery.UdevRuleContent()},
	}
}

func fileMatches(s sys.System, path string, want []byte) bool {
	b, err := s.ReadFile(path)
	return err == nil && string(b) == string(want)
}

func unitState(ctx context.Context, s sys.System, verb, unit string) string {
	r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{verb, unit}, Timeout: 30 * time.Second})
	return strings.TrimSpace(r.Stdout)
}

func (Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	ok, detail := battery.Verify(env.Sys, env.Facts, env.State.Battery, opts(env))
	if !ok {
		return step.Todo("%s", detail), nil
	}
	for _, u := range persistence() {
		if !fileMatches(env.Sys, u.path, u.content) {
			return step.Todo("%s is missing or different", u.path), nil
		}
	}
	for _, unit := range []string{battery.ServiceUnit, battery.TimerUnit} {
		if st := unitState(ctx, env.Sys, "is-enabled", unit); st != "enabled" {
			return step.Todo("%s isn't enabled", unit), nil
		}
	}
	if env.State.Battery.Method == battery.MethodWatch {
		if !fileMatches(env.Sys, battery.WatchServicePath, battery.WatchServiceContent()) {
			return step.Todo("%s is missing or different", battery.WatchServicePath), nil
		}
		if st := unitState(ctx, env.Sys, "is-active", battery.WatchServiceUnit); st != "active" {
			return step.Todo("%s isn't running", battery.WatchServiceUnit), nil
		}
	}
	return step.Done(detail), nil
}

func ladderDesc(env *step.Env, o battery.Options) string {
	predicted := battery.Predict(env.Sys, env.Facts, o)
	var what string
	switch {
	case predicted == battery.MethodThresholds:
		what = fmt.Sprintf("write a matching UPower limit to %s (then systemd-hwdb update, udevadm trigger -s power_supply, systemctl try-restart upower.service), then set charge_control thresholds, trying stop %s with start = stop − %d, verifying each read-back",
			battery.HwdbPath, battery.CandidatesText(o.Stop), o.Gap)
	case strings.HasPrefix(predicted, "vendor:"):
		what = "set the vendor fixed charge mode (" + strings.TrimPrefix(predicted, "vendor:") + ") and verify the read-back"
	case predicted == battery.MethodWatch:
		what = fmt.Sprintf("set up the software fallback: install and start %s, which inhibits charging at %d%% and returns to auto at %d%%", battery.WatchServiceUnit, o.Stop, o.Stop-o.Gap)
	default:
		what = "probe every charge-limit method; none is exposed, so the summary will point at the BIOS/UEFI options"
	}
	return "under /run/kiln/battery.lock, run the charge-limit ladder: " + what + "; fall back down the ladder if that fails, and record the result in /etc/kiln/state.json"
}

func (Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	o := opts(env)
	acts := []action.Action{action.Custom{
		Desc: ladderDesc(env, o),
		K:    action.Commands,
		Fn: func(ctx context.Context, s sys.System) error {
			if !s.DryRun() {
				release, err := battery.Lock(ctx, 2*time.Minute)
				if err != nil {
					return err
				}
				defer release()
			}
			out, err := battery.Run(ctx, s, env.Facts, o, func(format string, a ...any) {
				msg := fmt.Sprintf(format, a...)
				env.Log.Info(msg)
				if env.Output != nil {
					env.Output(msg)
				}
			})
			if err != nil {
				return err
			}
			valuesChanged := env.State.Battery == nil || *env.State.Battery != out.Rec
			if !s.DryRun() {
				// Saved now, not only at the end of the run: the helpers
				// (`kiln battery apply|watch`) read it as soon as they start.
				rec := out.Rec
				env.State.Battery = &rec
				env.Log.Info("battery limit accepted", "result", battery.Describe(&rec))
				if err := env.State.Save(s); err != nil {
					return fmt.Errorf("the charge limit is set, but /etc/kiln/state.json couldn't be saved: %w", err)
				}
			}
			if out.Rec.Method == battery.MethodWatch {
				if err := battery.EnsureWatch(ctx, s, env.Facts, valuesChanged); err != nil {
					return fmt.Errorf("could not start the software charge limit: %w", err)
				}
			} else if err := battery.StopWatch(ctx, s, env.Facts); err != nil {
				return err
			}
			for _, n := range out.Notes {
				env.Notes.Add("%s", n)
			}
			if out.Rec.Method == battery.MethodThresholds && env.Facts.GNOME {
				env.Notes.Add("Battery limit is managed by kiln. Leave GNOME's *Preserve Battery Health* switch alone; turning it **off** resets the limit to 100%% until kiln re-applies it (within 15 minutes).")
			}
			return nil
		},
	}}
	for _, u := range persistence() {
		acts = append(acts, action.WriteFile{Path: u.path, Content: u.content, Mode: 0o644})
	}
	acts = append(acts,
		action.Systemctl{Verb: "daemon-reload"},
		action.Systemctl{Verb: "enable", Units: []string{battery.ServiceUnit}},
		action.Systemctl{Verb: "enable", Now: true, Units: []string{battery.TimerUnit}},
	)
	return acts, nil
}
