// Package preflight implements catalog step 0 `preflight` (docs/steps-catalog.md).
// The repair part (pending revert, interrupted dpkg, stale masks, single
// instance) runs in the wizard's screen 0 before anything else (D41); this
// step re-validates the machine and installs kiln's own prerequisites.
package preflight

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// prerequisites are Priority: standard and may be missing (catalog intro).
var prerequisites = []string{"ca-certificates", "pciutils"}

// Step is the `preflight` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string       { return "preflight" }
func (Step) Title() string    { return "Check this machine" }
func (Step) Category() string { return "base" }
func (Step) Critical() bool   { return true }

func (Step) Packages() []step.PackageRef {
	var out []step.PackageRef
	for _, p := range prerequisites {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "main", Repo: "debian"})
	}
	return out
}

// validate returns a plain-language error for a machine kiln must not touch.
func validate(env *step.Env) error {
	f := env.Facts
	switch {
	case !f.Root:
		return errors.New("kiln must run as root. Run it with sudo, or as root via `su -`")
	case f.OS.ID != "debian" || f.OS.VersionID != "13":
		return fmt.Errorf("this is %q, but kiln only sets up Debian 13 (trixie)", f.OS.Pretty)
	case f.Arch != "amd64":
		return fmt.Errorf("this is a %s machine, but kiln only supports amd64", f.Arch)
	case f.Container:
		return errors.New("kiln sets up real machines, not containers")
	case f.User == nil:
		return errors.New(f.UserProblem)
	}
	return nil
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	if err := validate(env); err != nil {
		return step.Todo("%s", err.Error()), nil
	}
	if ok, missing := apt.AllInstalled(ctx, env.Sys, prerequisites); !ok {
		return step.Todo("kiln's prerequisites are missing: %s", strings.Join(missing, ", ")), nil
	}
	if err := internet(ctx, env); err != nil {
		return step.Todo("%s", err.Error()), nil
	}
	return step.Done(summary(env)), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	if err := validate(env); err != nil {
		return nil, err
	}
	f := env.Facts
	if f.ViaPlainSu {
		env.Notes.Add("kiln was started with plain `su`, so /usr/sbin wasn't on PATH; kiln added it for this run. Next time use `su -`.")
	}
	if f.Virt != "none" && f.Virt != "" {
		env.Notes.Warn("This looks like a virtual machine (%s). kiln is meant for bare metal; hardware steps may not apply.", f.Virt)
	}
	if f.SSHSession {
		env.Notes.Warn("You're connected over SSH. Network and firewall steps may disconnect you; kiln is meant to be run at the machine.")
	}
	var acts []action.Action
	if ok, missing := apt.AllInstalled(ctx, env.Sys, prerequisites); !ok {
		acts = append(acts, action.AptInstall{Pkgs: missing})
	}
	acts = append(acts, action.Custom{
		Desc: "check the internet connection (deb.debian.org over HTTP, github.com over HTTPS)",
		Fn: func(ctx context.Context, s sys.System) error {
			if s.DryRun() {
				return nil
			}
			return internet(ctx, env)
		},
	})
	return acts, nil
}

// internet reports precisely what's wrong (catalog step 0).
func internet(ctx context.Context, env *step.Env) error {
	if _, err := net.DefaultResolver.LookupHost(ctx, "deb.debian.org"); err != nil {
		return errors.New("can't resolve deb.debian.org: check the network and DNS (cable, Wi-Fi, router)")
	}
	if _, err := env.Sys.HTTPGet(ctx, "http://deb.debian.org/debian/", 4<<20); err != nil {
		return fmt.Errorf("can't reach deb.debian.org: %w. Check the cable or Wi-Fi", err)
	}
	if _, err := env.Sys.HTTPGet(ctx, "https://github.com/robots.txt", 1<<20); err != nil {
		var cerr *tls.CertificateVerificationError
		if errors.As(err, &cerr) || strings.Contains(err.Error(), "certificate") {
			return errors.New("HTTPS certificates can't be verified: install ca-certificates (apt install ca-certificates)")
		}
		return fmt.Errorf("can't reach github.com over HTTPS: %w", err)
	}
	return nil
}

func summary(env *step.Env) string {
	f := env.Facts
	return fmt.Sprintf("Debian %s %s, user %s", f.OS.VersionID, f.Arch, f.User.Name)
}
