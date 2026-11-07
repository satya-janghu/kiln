// Package step defines the contract every catalog step implements
// (docs/architecture.md "Core interfaces"; behaviour in docs/steps-catalog.md).
package step

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/secrets"
	"github.com/satya-janghu/kiln/internal/state"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Applicability says whether a step fits this machine.
type Applicability struct {
	OK     bool
	Reason string // plain language, shown as "not applicable: <Reason>"
}

// Yes and No build Applicability values.
func Yes() Applicability             { return Applicability{OK: true} }
func No(reason string) Applicability { return Applicability{Reason: reason} }

// Status is the result of a read-only Check.
type Status struct {
	Done   bool
	Detail string // what differs (when not done) or what was found
}

// Done and Todo build Status values.
func Done(detail string) Status { return Status{Done: true, Detail: detail} }
func Todo(format string, a ...any) Status {
	return Status{Detail: fmt.Sprintf(format, a...)}
}

// Step is one catalog entry.
type Step interface {
	ID() string
	Title() string
	Category() string
	DependsOn() []string
	// Critical steps stop the whole run when they fail (only preflight).
	Critical() bool
	// DefaultEnabled is the checkbox default (everything on except D38's backports kernel).
	DefaultEnabled(f *facts.Facts) bool
	Options(f *facts.Facts) []options.Field
	Applies(env *Env) Applicability
	// Check is READ-ONLY: it may only read through env.Sys, env.Facts and env.State.
	Check(ctx context.Context, env *Env) (Status, error)
	Plan(ctx context.Context, env *Env) ([]action.Action, error)
	Apply(ctx context.Context, env *Env, plan []action.Action) error
	// Packages lists every package the step can declare, for /check-packages.
	Packages() []PackageRef
}

// PackageRef is a package a step may install.
type PackageRef struct {
	Name      string
	Suite     string // "trixie", "trixie-backports", or a vendor repo
	Component string
	Repo      string // debian, docker, github-cli, nvidia, extrepo:<name>
}

// Base supplies defaults; embed it in steps.
type Base struct{}

func (Base) DependsOn() []string                  { return nil }
func (Base) Critical() bool                       { return false }
func (Base) DefaultEnabled(*facts.Facts) bool     { return true }
func (Base) Options(*facts.Facts) []options.Field { return nil }
func (Base) Applies(*Env) Applicability           { return Yes() }
func (Base) Packages() []PackageRef               { return nil }
func (Base) Apply(ctx context.Context, env *Env, plan []action.Action) error {
	return action.RunAll(ctx, env.Sys, plan)
}

// Env is everything a step may use.
type Env struct {
	Facts   *facts.Facts
	Opts    options.View
	All     *options.Resolved
	Sys     sys.System
	Log     *slog.Logger
	Notes   *Notes
	Secrets *secrets.Store
	State   *state.State
	// Progress shows a short status line (the XP-style status bar).
	Progress func(text string)
	// Output receives live command output for the log pane.
	Output func(line string)
}

// User returns the target user (never nil after preflight).
func (e *Env) User() *sys.User { return e.Facts.User }

// Say reports progress if a sink is attached.
func (e *Env) Say(format string, a ...any) {
	if e.Progress != nil {
		e.Progress(fmt.Sprintf(format, a...))
	}
}

// Notes collects summary notes, review warnings and reboot reasons.
type Notes struct {
	mu       sync.Mutex
	notes    []string
	warnings []string
	reboot   []string
}

// Add records a summary note.
func (n *Notes) Add(format string, a ...any) {
	n.mu.Lock()
	defer n.mu.Unlock()
	msg := fmt.Sprintf(format, a...)
	for _, x := range n.notes {
		if x == msg {
			return
		}
	}
	n.notes = append(n.notes, msg)
}

// Warn records a warning for the review screen (shown before anything changes).
func (n *Notes) Warn(format string, a ...any) {
	n.mu.Lock()
	defer n.mu.Unlock()
	msg := fmt.Sprintf(format, a...)
	for _, x := range n.warnings {
		if x == msg {
			return
		}
	}
	n.warnings = append(n.warnings, msg)
}

// Reboot records why a reboot is needed.
func (n *Notes) Reboot(format string, a ...any) {
	n.mu.Lock()
	defer n.mu.Unlock()
	msg := fmt.Sprintf(format, a...)
	for _, x := range n.reboot {
		if x == msg {
			return
		}
	}
	n.reboot = append(n.reboot, msg)
}

// All returns copies of notes, warnings and reboot reasons.
func (n *Notes) All() (notes, warnings, reboot []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.notes...), append([]string(nil), n.warnings...), append([]string(nil), n.reboot...)
}

// Categories in display order.
var Categories = []string{"base", "hardware", "media", "apps", "power", "security", "dev"}

// CategoryTitle is the plain-language group name.
func CategoryTitle(c string) string {
	switch c {
	case "base":
		return "Base system"
	case "hardware":
		return "Hardware and firmware"
	case "media":
		return "Graphics and media"
	case "apps":
		return "Apps and services"
	case "power":
		return "Power, thermal and battery"
	case "security":
		return "Security and updates"
	case "dev":
		return "Developer tools"
	}
	return strings.ToUpper(c[:1]) + c[1:]
}

// SortedIDs returns step IDs sorted (helper for maps).
func SortedIDs(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
