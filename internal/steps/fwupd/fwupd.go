// Package fwupd implements catalog step 24 `firmware-updates` (docs/steps-catalog.md).
// This step can never reboot the machine: fwupdmgr runs with --no-reboot-check
// and stdin is never a TTY, so it returns before its "Restart now?" prompt,
// which is its only reboot path (fwupd 2.0.20 src/fu-util.c).
package fwupd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Step is the `firmware-updates` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "firmware-updates" }
func (Step) Title() string       { return "Firmware updates (fwupd)" }
func (Step) Category() string    { return "hardware" }
func (Step) DependsOn() []string { return []string{"apt-sources"} }

// packages: the signed EFI binary is needed under Secure Boot.
var packages = []string{"fwupd", "fwupd-amd64-signed", "udisks2"}

func (Step) Packages() []step.PackageRef {
	out := make([]step.PackageRef, 0, len(packages))
	for _, p := range packages {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "main", Repo: "debian"})
	}
	return out
}

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{{Key: "updates", Label: "fwupd updates", Kind: options.Bool, Default: true,
		Help: "Refresh LVFS metadata and apply available firmware updates. kiln never reboots; staged updates install at the next reboot."}}
}

// outcome remembers this run's result for the confirming Check. state.json
// has no field for it, so a new run starts without one (see the report).
var (
	mu      sync.Mutex
	outcome string // "" = not run; "ok", "nothing", "offline", "noted"
)

func setOutcome(v string) { mu.Lock(); outcome = v; mu.Unlock() }
func getOutcome() string  { mu.Lock(); defer mu.Unlock(); return outcome }

const (
	exitNothing = 2   // fwupd: nothing to do
	exitOffline = 101 // fwupd: network unreachable
)

func exitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var ee *sys.ExitError
	if errors.As(err, &ee) {
		return ee.Code, true
	}
	return 0, false
}

func fwupdmgr(args ...string) sys.Cmd {
	return sys.Cmd{Name: "fwupdmgr", Args: args, Timeout: 30 * time.Minute}
}

func rebootPending(ctx context.Context, s sys.System) bool {
	_, err := s.Query(ctx, sys.Cmd{Name: "fwupdmgr", Args: []string{"check-reboot-needed", "--json"}, Timeout: 2 * time.Minute})
	code, ok := exitCode(err)
	return ok && code == 0
}

func (Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	if ok, missing := apt.AllInstalled(ctx, env.Sys, packages); !ok {
		return step.Todo("not installed: %s", strings.Join(missing, ", ")), nil
	}
	if !env.Opts.Bool("updates") {
		return step.Done("fwupd installed (updates not requested)"), nil
	}
	switch getOutcome() {
	case "ok", "nothing":
		return step.Done("firmware is up to date or the update was applied"), nil
	case "offline":
		return step.Done("fwupd installed; LVFS couldn't be reached, so no updates were checked"), nil
	case "noted":
		return step.Done("fwupd installed; an update couldn't be applied (see the summary)"), nil
	}
	if rebootPending(ctx, env.Sys) {
		return step.Done("a firmware update is staged for the next reboot"), nil
	}
	return step.Todo("firmware updates haven't been checked in this run"), nil
}

func (Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	var acts []action.Action
	if ok, missing := apt.AllInstalled(ctx, env.Sys, packages); !ok {
		acts = append(acts, action.AptInstall{Pkgs: missing})
	}
	if !env.Opts.Bool("updates") {
		return acts, nil
	}
	acts = append(acts, action.Custom{
		Desc: "run `fwupdmgr refresh --force` (exit 101 = offline: skip with a note), `fwupdmgr get-updates --json` (exit 2 = nothing to do), " +
			"then `fwupdmgr update -y --no-reboot-check --no-device-prompt` (a refusal becomes a note), and add a reboot reason if `fwupdmgr check-reboot-needed` says one is staged; never reboots",
		K:  action.Commands,
		Fn: func(ctx context.Context, s sys.System) error { return update(ctx, env, s) },
	})
	return acts, nil
}

func update(ctx context.Context, env *step.Env, s sys.System) error {
	if s.DryRun() {
		for _, c := range []sys.Cmd{fwupdmgr("refresh", "--force"), fwupdmgr("update", "-y", "--no-reboot-check", "--no-device-prompt")} {
			if _, err := s.Run(ctx, c); err != nil {
				return err
			}
		}
		return nil
	}
	env.Say("refreshing firmware metadata from LVFS…")
	_, err := s.Run(ctx, fwupdmgr("refresh", "--force"))
	switch code, ok := exitCode(err); {
	case ok && code == 0:
	case ok && code == exitOffline:
		setOutcome("offline")
		env.Notes.Add("Firmware updates were skipped: LVFS couldn't be reached (no network). Run `sudo fwupdmgr refresh && sudo fwupdmgr update` later.")
		return nil
	default:
		return fmt.Errorf("could not refresh firmware metadata: %w. Check the network and run kiln again", err)
	}

	_, err = s.Query(ctx, fwupdmgr("get-updates", "--json"))
	switch code, ok := exitCode(err); {
	case ok && code == 0:
	case ok && code == exitNothing:
		setOutcome("nothing")
		return nil
	default:
		return fmt.Errorf("could not list firmware updates: %w. Run `sudo fwupdmgr get-updates` to see why, then run kiln again", err)
	}

	env.Say("applying firmware updates…")
	_, err = s.Run(ctx, fwupdmgr("update", "-y", "--no-reboot-check", "--no-device-prompt"))
	switch code, ok := exitCode(err); {
	case ok && (code == 0 || code == exitNothing):
		setOutcome("ok")
	default:
		setOutcome("noted")
		env.Notes.Add("A firmware update couldn't be applied (%v). Common causes: running on battery, or a device that refuses updates. Run `sudo fwupdmgr update` later.", err)
	}
	if rebootPending(ctx, s) {
		env.Notes.Reboot("firmware update staged")
	}
	return nil
}
