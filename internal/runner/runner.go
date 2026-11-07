// Package runner walks the catalog in order (docs/architecture.md "Runner" and
// "Process model, ordering and interruption safety").
package runner

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/secrets"
	"github.com/satya-janghu/kiln/internal/state"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Status of a step in a run.
type Status string

const (
	Pending   Status = "pending"
	Running   Status = "running"
	Done      Status = "done"
	Unchanged Status = "unchanged"
	Failed    Status = "failed"
	Skipped   Status = "skipped"
)

// StepResult is one step's outcome.
type StepResult struct {
	ID, Title, Category string
	Status              Status
	Reason              string
	Duration            time.Duration
	byUser              bool
}

// EventKind classifies runner events.
type EventKind int

const (
	EvPhase     EventKind = iota // Text: what the runner is doing between steps
	EvStepStart                  // Index/Total/Step
	EvStepEnd                    // Result
	EvOutput                     // Text: one log line
	EvProgress                   // Text: short status-bar text
)

// Event is sent to the UI.
type Event struct {
	Kind   EventKind
	Index  int
	Total  int
	Step   string
	Title  string
	Text   string
	Result *StepResult
}

// Runner runs steps.
type Runner struct {
	Steps       []step.Step
	Facts       *facts.Facts
	Opts        *options.Resolved
	Sys         sys.System
	Log         *slog.Logger
	State       *state.State
	Secrets     *secrets.Store
	KilnVersion string
	Events      func(Event)

	Notes   *step.Notes
	stop    atomic.Bool
	results map[string]*StepResult
}

// Stop asks the runner to stop after the current step (Ctrl+C / F3).
func (r *Runner) Stop() { r.stop.Store(true) }

func (r *Runner) emit(e Event) {
	if r.Events != nil {
		r.Events(e)
	}
}

func (r *Runner) env(st step.Step, s sys.System) *step.Env {
	id, title := st.ID(), st.Title()
	return &step.Env{
		Facts: r.Facts, Opts: r.Opts.For(id), All: r.Opts, Sys: s, Log: r.Log.With("step", id),
		Notes: r.Notes, Secrets: r.Secrets, State: r.State,
		Progress: func(t string) { r.emit(Event{Kind: EvProgress, Step: id, Title: title, Text: t}) },
		Output:   func(l string) { r.emit(Event{Kind: EvOutput, Step: id, Title: title, Text: l}) },
	}
}

// blocked returns the title of a hard dependency that failed or was skipped
// by the user or by its own dependencies. "Not applicable" doesn't block.
func (r *Runner) blocked(st step.Step) string {
	for _, d := range st.DependsOn() {
		res, ok := r.results[d]
		if !ok {
			continue
		}
		if res.Status == Failed || (res.Status == Skipped && (res.byUser || strings.HasPrefix(res.Reason, "needs "))) {
			return res.Title
		}
	}
	return ""
}

// runStep evaluates one step against s (Real or Recorder).
func (r *Runner) runStep(ctx context.Context, st step.Step, s sys.System, apply bool) (res *StepResult, plan []string) {
	start := time.Now()
	res = &StepResult{ID: st.ID(), Title: st.Title(), Category: st.Category()}
	defer func() {
		if p := recover(); p != nil {
			r.Log.Error("panic", "step", st.ID(), "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			res.Status, res.Reason = Failed, fmt.Sprintf("kiln hit an internal error in this step (%v). The log has details: /var/log/kiln.log", p)
		}
		res.Duration = time.Since(start)
	}()
	env := r.env(st, s)
	if !r.Opts.IsEnabled(st.ID()) {
		res.Status, res.Reason, res.byUser = Skipped, "by you", true
		return res, nil
	}
	if a := st.Applies(env); !a.OK {
		res.Status, res.Reason = Skipped, "not applicable: "+a.Reason
		return res, nil
	}
	if dep := r.blocked(st); dep != "" {
		res.Status, res.Reason = Skipped, "needs "+dep
		return res, nil
	}
	stat, err := st.Check(ctx, env)
	if err != nil {
		res.Status, res.Reason = Failed, "could not check the current state: "+err.Error()
		return res, nil
	}
	if stat.Done {
		res.Status, res.Reason = Unchanged, stat.Detail
		return res, nil
	}
	acts, err := st.Plan(ctx, env)
	if err != nil {
		res.Status, res.Reason = Failed, err.Error()
		return res, nil
	}
	for _, a := range acts {
		plan = append(plan, a.Describe())
	}
	apt.Output = env.Output
	defer func() { apt.Output = nil }()
	if err := st.Apply(ctx, env, acts); err != nil {
		res.Status, res.Reason = Failed, err.Error()
		return res, plan
	}
	if !apply {
		res.Status, res.Reason = Done, stat.Detail
		return res, plan
	}
	after, cerr := st.Check(ctx, env)
	switch {
	case cerr != nil:
		res.Status, res.Reason = Failed, "could not confirm the change: "+cerr.Error()
	case !after.Done:
		res.Status, res.Reason = Failed, "the change didn't take effect: "+after.Detail
	default:
		res.Status, res.Reason = Done, after.Detail
	}
	return res, plan
}
