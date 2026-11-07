package runner

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Summary is the result of a real run.
type Summary struct {
	Results  []StepResult
	Notes    []string
	Reboot   []string
	Started  time.Time
	Duration time.Duration
	Stopped  bool // stopped early (by the user, a failed critical step or an internal error)
}

// Count returns how many results have a status.
func (s *Summary) Count(st Status) int {
	n := 0
	for _, r := range s.Results {
		if r.Status == st {
			n++
		}
	}
	return n
}

// safe runs fn, turning a panic into a log entry and a note.
func (r *Runner) safe(what string, fn func()) {
	defer func() {
		if p := recover(); p != nil {
			r.Log.Error("panic", "in", what, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			r.Notes.Add("kiln hit an internal error while %s (%v); see /var/log/kiln.log", what, p)
		}
	}()
	fn()
}

// Run executes the steps for real with the full lifecycle (D41). The
// end-of-run tasks (needrestart, restoring automatic updates, releasing the
// inhibitor, saving state) always run, even after a panic.
func (r *Runner) Run(ctx context.Context) *Summary {
	sum := &Summary{Started: time.Now()}
	r.results = map[string]*StepResult{}
	r.Notes = &step.Notes{}
	lc := newLifecycle(r)
	r.safe("starting the run", func() { lc.begin(ctx) })
	r.safe("running the steps", func() { r.steps(ctx, lc, sum) })
	r.safe("finishing the run", func() { lc.end(ctx) })
	sum.Notes, _, sum.Reboot = r.Notes.All()
	sum.Duration = time.Since(sum.Started)
	r.safe("saving the summary", func() { lc.finish(ctx, sum) })
	return sum
}

func (r *Runner) steps(ctx context.Context, lc *lifecycle, sum *Summary) {
	total := len(r.Steps)
	stopReason := ""
	defer func() {
		if p := recover(); p != nil {
			sum.Stopped = true
			panic(p) // handled by safe(); end/finish still run
		}
	}()
	for i, st := range r.Steps {
		if stopReason == "" && r.stop.Load() {
			stopReason = "stopped by you before it started"
		}
		if stopReason != "" {
			sum.Stopped = true
			res := StepResult{ID: st.ID(), Title: st.Title(), Category: st.Category(), Status: Skipped, Reason: stopReason}
			sum.Results = append(sum.Results, res)
			r.emit(Event{Kind: EvStepEnd, Index: i, Total: total, Step: st.ID(), Title: st.Title(), Result: &res})
			continue
		}
		r.emit(Event{Kind: EvStepStart, Index: i, Total: total, Step: st.ID(), Title: st.Title()})
		lc.record(st.ID(), st.Title())
		res, _ := r.runStep(ctx, st, r.Sys, true)
		r.results[st.ID()] = res
		sum.Results = append(sum.Results, *res)
		r.Log.Info("step", "id", res.ID, "status", string(res.Status), "reason", res.Reason, "took", res.Duration.Round(time.Second).String())
		r.emit(Event{Kind: EvStepEnd, Index: i, Total: total, Step: st.ID(), Title: st.Title(), Result: res})
		if res.Status == Failed && st.Critical() {
			stopReason = "not run, because " + res.Title + " failed"
			r.Notes.Add("The run stopped because %q failed: %s", res.Title, res.Reason)
		}
	}
}

// Text renders the summary for /var/log/kiln-summary.txt. Secrets are redacted.
func (s *Summary) Text(hostname string, secrets []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "kiln summary for %s, %s (took %s)\n", hostname, s.Started.Format(time.RFC1123), s.Duration.Round(time.Second))
	fmt.Fprintf(&b, "%d done, %d unchanged, %d failed, %d skipped\n\n", s.Count(Done), s.Count(Unchanged), s.Count(Failed), s.Count(Skipped))
	for _, r := range s.Results {
		line := fmt.Sprintf("[%-9s] %s", r.Status, r.Title)
		if r.Reason != "" && r.Status != Done {
			line += ": " + r.Reason
		}
		b.WriteString(line + "\n")
	}
	if len(s.Notes) > 0 {
		b.WriteString("\nNotes\n")
		for _, n := range s.Notes {
			b.WriteString(" - " + n + "\n")
		}
	}
	if len(s.Reboot) > 0 {
		b.WriteString("\nReboot needed: " + strings.Join(s.Reboot, "; ") + "\n")
	} else {
		b.WriteString("\nNo reboot needed.\n")
	}
	if s.Stopped {
		b.WriteString("\nThe run stopped early. Run `sudo kiln` again to finish; every step is safe to repeat.\n")
	}
	return sys.Redact(b.String(), secrets)
}
