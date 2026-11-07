// Package kernel implements catalog step 5b `kernel` (docs/steps-catalog.md, D38).
// The step's checkbox is the "backports kernel" choice and defaults to off.
// kiln never pins, holds or marks a kernel manual; the metapackage follows its suite.
package kernel

import (
	"context"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/steps/firmware"
	"github.com/satya-janghu/kiln/internal/sys"
)

const (
	suite   = "trixie-backports"
	image   = "linux-image-amd64"
	headers = "linux-headers-amd64"
)

// Step is the `kernel` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string       { return "kernel" }
func (Step) Title() string    { return "Newer kernel from trixie-backports" }
func (Step) Category() string { return "hardware" }
func (Step) DependsOn() []string {
	return []string{"apt-sources", firmware.ID}
}

// DefaultEnabled is false: the one deliberate exception to "everything on" (D38).
func (Step) DefaultEnabled(*facts.Facts) bool { return false }

func (Step) Packages() []step.PackageRef {
	out := []step.PackageRef{
		{Name: image, Suite: suite, Component: "main", Repo: "debian"},
		{Name: headers, Suite: suite, Component: "main", Repo: "debian"},
	}
	for _, p := range firmware.New().Packages() {
		if strings.HasPrefix(p.Name, "firmware-") {
			p.Suite = suite
			out = append(out, p)
		}
	}
	return out
}

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{{Key: "backports_firmware", Label: "Backports firmware", Kind: options.Bool, Default: true,
		Help: "Also reinstall the firmware packages chosen by the firmware step from trixie-backports (newer firmware)."}}
}

func backportsEnabled(env *step.Env) bool {
	// An unticked apt-sources is reported by the dependency check ("needs …").
	v, ok := env.All.Values["apt-sources.backports"].(bool)
	return ok && v
}

func (Step) Applies(env *step.Env) step.Applicability {
	if !backportsEnabled(env) {
		return step.No("backports disabled")
	}
	return step.Yes()
}

// policyVersions parses `apt-cache policy` into version → its sources and whether it's installed.
type policyVersion struct {
	Version   string
	Installed bool
	Sources   []string
}

func parsePolicy(out string) []policyVersion {
	var vs []policyVersion
	inTable := false
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "Version table:") {
			inTable = true
			continue
		}
		if !inTable || t == "" {
			continue
		}
		f := strings.Fields(t)
		installed := f[0] == "***"
		if installed {
			f = f[1:]
		}
		// Version lines are indented 5 (or carry ***); source lines are indented 8 and start with a priority.
		indent := len(l) - len(strings.TrimLeft(l, " "))
		if installed || indent <= 5 {
			if len(f) > 0 {
				vs = append(vs, policyVersion{Version: f[0], Installed: installed})
			}
			continue
		}
		if len(vs) > 0 {
			vs[len(vs)-1].Sources = append(vs[len(vs)-1].Sources, t)
		}
	}
	return vs
}

func fromBackports(v policyVersion) bool {
	for _, s := range v.Sources {
		if strings.Contains(s, " "+suite+"/") {
			return true
		}
	}
	return false
}

// InstalledFromBackports reports whether the installed linux-image-amd64 is a
// backports version, and that version. Any installed version carrying
// trixie-backports' "~bpo13" suffix counts, even once a newer backports upload
// has dropped it from the index. multimedia uses it for the headers suite.
func InstalledFromBackports(ctx context.Context, s sys.System) (bool, string) {
	if v := apt.InstalledVersion(ctx, s, image); strings.Contains(v, "~bpo13") {
		return true, v
	}
	for _, v := range parsePolicy(apt.Policy(ctx, s, image)) {
		if v.Installed {
			return fromBackports(v), v.Version
		}
	}
	return false, ""
}

func backportsCandidate(ctx context.Context, s sys.System) string {
	for _, v := range parsePolicy(apt.Policy(ctx, s, image)) {
		if fromBackports(v) {
			return v.Version
		}
	}
	return ""
}

func (Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	ok, ver := InstalledFromBackports(ctx, env.Sys)
	if !ok {
		return step.Todo("%s isn't installed from %s", image, suite), nil
	}
	// Everything Plan installs from backports, so a part that failed (or an
	// option turned on later) is retried, not reported as unchanged.
	var parts []string
	if len(env.Facts.NVIDIA()) > 0 {
		parts = append(parts, headers)
	}
	if env.Opts.Bool("backports_firmware") {
		parts = append(parts, firmware.SelectFor(env.Facts, env.All.For(firmware.ID)).Firmware...)
	}
	for _, p := range parts {
		if !fromBackportsIfOffered(ctx, env.Sys, p) {
			return step.Todo("%s isn't installed from %s", p, suite), nil
		}
	}
	return step.Done("backports kernel metapackage " + ver + " installed"), nil
}

// fromBackportsIfOffered: pkg is installed from trixie-backports, or
// trixie-backports doesn't offer it (then trixie's version is what Plan gets).
func fromBackportsIfOffered(ctx context.Context, s sys.System, pkg string) bool {
	if strings.Contains(apt.InstalledVersion(ctx, s, pkg), "~bpo13") {
		return true
	}
	for _, v := range parsePolicy(apt.Policy(ctx, s, pkg)) {
		if fromBackports(v) {
			return false // offered, but not what's installed
		}
	}
	return true
}

// nvidiaDebianStock reports the multimedia step will use Debian's 550 driver.
// Option keys belong to the multimedia step; missing keys mean "not chosen".
func nvidiaDebianStock(env *step.Env) bool {
	if len(env.Facts.NVIDIA()) == 0 || !env.All.IsEnabled("multimedia") {
		return false
	}
	on, _ := env.All.Values["multimedia.nvidia"].(bool)
	src, _ := env.All.Values["multimedia.nvidia_source"].(string)
	return on && src == "debian-stock"
}

func (Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	pkgs := []string{image}
	if len(env.Facts.NVIDIA()) > 0 {
		pkgs = append(pkgs, headers) // DKMS modules build against the running kernel's headers
	}
	if nvidiaDebianStock(env) {
		env.Notes.Warn("The backports kernel (7.2) is selected, but this machine uses Debian's NVIDIA driver 550, whose newest build documents fixes only up to Linux 6.17. The driver may not build for 7.2; the 6.12 kernel stays in the boot menu until autoremove removes it.")
	}
	acts := []action.Action{action.AptInstall{Pkgs: pkgs, Suite: suite}}

	if env.Opts.Bool("backports_firmware") {
		sel := firmware.SelectFor(env.Facts, env.All.For(firmware.ID))
		var misc, rest []string
		for _, p := range sel.Firmware {
			if p == firmware.MiscFirmware {
				misc = append(misc, p)
			} else {
				rest = append(rest, p)
			}
		}
		if len(misc) > 0 {
			acts = append(acts, action.AptInstall{Pkgs: misc, Suite: suite, NoRecommends: true})
		}
		if len(rest) > 0 {
			acts = append(acts, action.AptInstall{Pkgs: rest, Suite: suite})
		}
		if len(sel.Firmware) > 0 {
			acts = append(acts, action.Run{
				Cmd:  sys.Cmd{Name: "update-initramfs", Args: []string{"-u", "-k", "all"}, Timeout: 20 * time.Minute},
				Desc: "rebuild the initramfs of every installed kernel (update-initramfs -u -k all) with the backports firmware",
			})
		}
	}
	acts = append(acts, action.Custom{
		Desc: "add the reboot reason \"new kernel <version>\"",
		Fn: func(ctx context.Context, s sys.System) error {
			ver := backportsCandidate(ctx, s)
			if !s.DryRun() {
				if ok, v := InstalledFromBackports(ctx, s); ok {
					ver = v
				}
			}
			if ver == "" {
				ver = "from " + suite
			}
			env.Notes.Reboot("new kernel %s", ver)
			return nil
		},
	})
	return acts, nil
}
