// Package upgrade implements catalog step 4 `upgrade` (docs/steps-catalog.md).
package upgrade

import (
	"context"
	"fmt"
	"strings"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Step is the `upgrade` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "upgrade" }
func (Step) Title() string       { return "Upgrade installed packages" }
func (Step) Category() string    { return "base" }
func (Step) DependsOn() []string { return []string{"apt-sources"} }

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{{Key: "full_upgrade", Label: "Full upgrade", Kind: options.Bool, Default: true,
		Help: "apt-get full-upgrade: installs every available update, including ones that add or remove packages."}}
}

func (Step) Applies(env *step.Env) step.Applicability {
	if !env.Opts.Bool("full_upgrade") {
		return step.No("full-upgrade is turned off in this step's options")
	}
	return step.Yes()
}

func (Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	// The simulation only means something against fresh package lists
	// (catalog step 4: "after an update in this run"); Plan refreshes them first.
	if !apt.UpdatedThisRun.Load() {
		return step.Todo("package lists haven't been refreshed in this run"), nil
	}
	lines, err := apt.SimulateUpgrade(ctx, env.Sys)
	if err != nil {
		return step.Todo("apt-get -s full-upgrade failed: %v", err), nil
	}
	if len(lines) > 0 {
		return step.Todo("%d package changes are waiting (%s)", len(lines), first(lines)), nil
	}
	return step.Done("every installed package is up to date"), nil
}

func first(lines []string) string {
	f := strings.Fields(lines[0])
	if len(f) < 2 {
		return lines[0]
	}
	s := f[0] + " " + f[1]
	if len(lines) > 1 {
		s += ", …"
	}
	return s
}

// kernelInSimulation reports whether a simulated upgrade installs a kernel image.
func kernelInSimulation(lines []string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, "Inst linux-image-") {
			return true
		}
	}
	return false
}

// runningBuildDiffers compares the running kernel's Debian build (`uname -v`,
// "... Debian <ver> ...") with the installed package of the same ABI, which
// catches same-ABI rebuilds.
func runningBuildDiffers(ctx context.Context, s sys.System, release string) (bool, string) {
	if release == "" {
		r, err := s.Query(ctx, sys.Cmd{Name: "uname", Args: []string{"-r"}})
		if err != nil {
			return false, ""
		}
		release = strings.TrimSpace(r.Stdout)
	}
	r, err := s.Query(ctx, sys.Cmd{Name: "uname", Args: []string{"-v"}})
	if err != nil {
		return false, ""
	}
	running := ""
	f := strings.Fields(r.Stdout)
	for i, w := range f {
		if w == "Debian" && i+1 < len(f) {
			running = f[i+1]
			break
		}
	}
	installed := apt.InstalledVersion(ctx, s, "linux-image-"+release)
	if running == "" || installed == "" || running == installed {
		return false, ""
	}
	return true, fmt.Sprintf("the running kernel is build %s, but %s is now installed", running, installed)
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	var newKernel bool
	return []action.Action{
		action.AptUpdate{},
		action.Custom{
			Desc: "simulate the upgrade (LC_ALL=C apt-get -s full-upgrade) to see whether it brings a new kernel",
			K:    action.Packages,
			Fn: func(ctx context.Context, s sys.System) error {
				lines, err := apt.SimulateUpgrade(ctx, s)
				if err != nil {
					return fmt.Errorf("the upgrade simulation failed: %w. Run `apt-get -s full-upgrade` to see why", err)
				}
				newKernel = kernelInSimulation(lines)
				return nil
			},
		},
		action.Custom{
			Desc: "upgrade every installed package (apt-get full-upgrade -y, keeping existing config files)",
			K:    action.Packages,
			Fn:   func(ctx context.Context, s sys.System) error { return apt.FullUpgrade(ctx, s) },
		},
		action.Custom{
			Desc: "check whether a reboot is needed (a new kernel in the upgrade, a rebuilt running kernel, or /run/reboot-required)",
			Fn: func(ctx context.Context, s sys.System) error {
				if newKernel {
					env.Notes.Reboot("the upgrade installed a new kernel")
				}
				if s.DryRun() {
					return nil
				}
				if differs, why := runningBuildDiffers(ctx, s, env.Facts.Kernel); differs {
					env.Notes.Reboot("%s", why)
				}
				if _, err := s.Stat("/run/reboot-required"); err == nil {
					env.Notes.Reboot("a package asked for a reboot (/run/reboot-required)")
				}
				return nil
			},
		},
	}, nil
}
