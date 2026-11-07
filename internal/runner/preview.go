package runner

import (
	"context"
	"strings"

	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// PreviewStep is one step in the plan.
type PreviewStep struct {
	ID, Title, Category string
	Status              Status // Done here means "will change"
	Reason              string
	Actions             []string
	Ops                 []sys.Op
}

// Preview is the full plan (rule 7: same code path as a real run).
type Preview struct {
	Steps    []PreviewStep
	Warnings []string
	Reboot   []string
	Notes    []string
}

// Preview runs every step against a Recorder: reads are real, changes recorded.
func (r *Runner) Preview(ctx context.Context) *Preview {
	rec := sys.NewRecorder(r.Sys)
	r.results = map[string]*StepResult{}
	r.Notes = &step.Notes{} // fresh notes per preview
	pv := &Preview{}
	for _, st := range r.Steps {
		rec.SetStep(st.ID())
		before := len(rec.Ops())
		res, plan := r.runStep(ctx, st, rec, false)
		r.results[st.ID()] = res
		ops := rec.Ops()[before:]
		pv.Steps = append(pv.Steps, PreviewStep{ID: res.ID, Title: res.Title, Category: res.Category,
			Status: res.Status, Reason: res.Reason, Actions: plan, Ops: ops})
	}
	pv.Notes, pv.Warnings, pv.Reboot = r.Notes.All()
	return pv
}

// Group classifies a recorded op for the review tabs.
func Group(op sys.Op) string {
	switch op.Kind {
	case sys.OpWrite, sys.OpRemove, sys.OpRename, sys.OpMkdir, sys.OpSymlink:
		return "Files"
	case sys.OpFetch:
		return "Commands"
	}
	if op.Cmd == nil {
		return "Commands"
	}
	switch op.Cmd.Name {
	case "apt-get", "apt", "dpkg", "debconf-set-selections", "dpkg-reconfigure":
		return "Packages"
	case "systemctl":
		return "Services"
	}
	return "Commands"
}

// Packages returns the package names an op installs, if any.
func Packages(op sys.Op) []string {
	if op.Cmd == nil || op.Cmd.Name != "apt-get" {
		return nil
	}
	args := op.Cmd.Args
	for i, a := range args {
		if a == "install" {
			var out []string
			for _, p := range args[i+1:] {
				if !strings.HasPrefix(p, "-") {
					out = append(out, p)
				}
			}
			return out
		}
	}
	return nil
}
