// Package app loads everything a kiln command needs: logger, System, facts,
// state, options and the step list.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/logx"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/revert"
	"github.com/satya-janghu/kiln/internal/runner"
	"github.com/satya-janghu/kiln/internal/secrets"
	"github.com/satya-janghu/kiln/internal/state"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/steps/all"
	"github.com/satya-janghu/kiln/internal/sys"
)

// AllowApplyPath is created by install.sh (rule 1).
const AllowApplyPath = "/etc/kiln/allow-apply"

// App is a loaded kiln.
type App struct {
	Version  string
	Real     *sys.Real
	Log      *slog.Logger
	Red      *logx.Redactor
	Facts    *facts.Facts
	State    *state.State
	Defaults *options.Resolved
	Opts     *options.Resolved
	Steps    []step.Step
	Secrets  *secrets.Store
	closer   io.Closer
}

// Load gathers facts and resolves options: defaults < last run < (wizard edits later).
func Load(ctx context.Context, version string, writeLog bool) (*App, error) {
	a := &App{Version: version, Red: &logx.Redactor{}, Secrets: secrets.NewStore()}
	a.Secrets.OnAdd = a.Red.Add
	path := ""
	if writeLog && runtime.GOOS == "linux" && os.Geteuid() == 0 {
		path = logx.Path
	}
	a.Log, a.closer = logx.Open(path, a.Red)
	a.Log.Info("kiln start", "version", version, "args", fmt.Sprint(os.Args[1:]))
	a.Real = sys.NewReal(a.Log)
	a.Facts = facts.Gather(ctx, a.Real)
	st, err := state.Load(a.Real)
	if err != nil {
		a.Log.Warn("state", "err", err.Error())
	}
	a.State = st
	a.Steps = all.Steps()
	a.Defaults = options.New()
	for _, s := range a.Steps {
		a.Defaults.Declare(s.ID(), s.DefaultEnabled(a.Facts), s.Options(a.Facts))
	}
	a.Opts = a.Defaults.Clone()
	a.ApplyLastRun()
	if v, ok := a.Opts.Values["apt-sources.lock_wait"].(int); ok && v > 0 {
		apt.LockWait = time.Duration(v) * time.Minute
	}
	return a, nil
}

// ApplyLastRun overlays this machine's previous choices.
func (a *App) ApplyLastRun() {
	for id, on := range a.State.Choices.Steps {
		if _, ok := a.Opts.Enabled[id]; ok {
			a.Opts.Enabled[id] = on
		}
	}
	for k, v := range a.State.Choices.Options {
		if err := a.Opts.Set(k, v); err != nil {
			a.Log.Warn("ignoring saved option", "key", k, "err", err.Error())
		}
	}
}

// ResetToDefaults drops last-run choices (the wizard's `d` key).
func (a *App) ResetToDefaults() { a.Opts = a.Defaults.Clone() }

// SaveChoices stores the choices that differ from defaults (no secrets).
func (a *App) SaveChoices() {
	a.State.Choices.Steps = map[string]bool{}
	for id, on := range a.Opts.Enabled {
		if on != a.Defaults.Enabled[id] {
			a.State.Choices.Steps[id] = on
		}
	}
	a.State.Choices.Options = a.Opts.NonDefault(a.Defaults)
}

// Runner builds a runner over the real system.
func (a *App) Runner(events func(runner.Event)) *runner.Runner {
	return &runner.Runner{Steps: a.Steps, Facts: a.Facts, Opts: a.Opts, Sys: a.Real, Log: a.Log,
		State: a.State, Secrets: a.Secrets, KilnVersion: a.Version, Events: events}
}

// Gate reports whether this process may apply changes (rule 1).
func (a *App) Gate(force bool) (bool, string) {
	switch {
	case runtime.GOOS != "linux":
		return false, "kiln only changes Debian 13 machines; this is " + runtime.GOOS
	case os.Geteuid() != 0:
		return false, "run kiln as root: sudo kiln (or log in as root with su -)"
	case a.Facts.Container:
		return false, "kiln sets up real machines, not containers"
	}
	if force {
		return true, ""
	}
	if _, err := os.Stat(AllowApplyPath); err != nil {
		return false, "this machine isn't marked for setup (" + AllowApplyPath + " is missing). Install kiln with its one-line installer, or pass --i-know-this-is-a-fresh-machine"
	}
	return true, ""
}

// Prechecks is what screen 0 needs (D41).
type Prechecks struct {
	OtherKiln       int // pid of another running kiln, 0 if none
	Pending         *state.Pending
	Interrupted     *state.Run // last run not finished
	DpkgInterrupted bool
	StaleMasks      bool
	LowBattery      bool
	OnBattery       bool
}

// Check gathers the prechecks.
func (a *App) Check(ctx context.Context) Prechecks {
	p := Prechecks{Pending: a.State.Pending}
	if a.State.Run != nil && !a.State.Run.Finished {
		p.Interrupted = a.State.Run
	}
	p.DpkgInterrupted = apt.Interrupted(ctx, a.Real)
	p.StaleMasks = runner.StaleMasks(a.Real)
	p.OnBattery = !a.Facts.OnAC && len(a.Facts.Batteries) > 0
	p.LowBattery = p.OnBattery && a.Facts.BatteryPercent >= 0 && a.Facts.BatteryPercent < 30
	return p
}

// RevertPending undoes an interrupted risky change.
func (a *App) RevertPending(ctx context.Context) error { return revert.Undo(ctx, a.Real, a.State) }

// RepairDpkg finishes an interrupted dpkg run.
func (a *App) RepairDpkg(ctx context.Context) error { return apt.Repair(ctx, a.Real) }

// ClearStaleMasks removes runtime masks left by a crashed run.
func (a *App) ClearStaleMasks(ctx context.Context) error { return runner.ClearStale(ctx, a.Real) }

// AcknowledgeInterrupted marks the interrupted run as seen.
func (a *App) AcknowledgeInterrupted() {
	if a.State.Run != nil {
		a.State.Run.Finished = true
		a.State.Run.Result = "interrupted"
		_ = a.State.Save(a.Real)
	}
}

// LockRun takes the single-instance lock; pid>0 means another kiln holds it.
func (a *App) LockRun() (release func(), pid int, err error) {
	release, pid, err = runner.Lock("kiln")
	if err != nil && pid == 0 {
		pid = -1
	}
	return release, pid, err
}

// Close flushes the log.
func (a *App) Close() {
	a.Real.Cleanup()
	if a.closer != nil {
		_ = a.closer.Close()
	}
}

// RunID is a timestamp for unit names.
func RunID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(os.Getpid())
}
