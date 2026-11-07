// Package cockpit implements catalog step 18 `cockpit` (docs/steps-catalog.md).
package cockpit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

const (
	backportsSuite = "trixie-backports"
	socket         = "cockpit.socket"
)

// Step is the `cockpit` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string       { return "cockpit" }
func (Step) Title() string    { return "Cockpit web console" }
func (Step) Category() string { return "apps" }

// DependsOn lists hard dependencies only. Cockpit's networking page can edit
// only NetworkManager interfaces; kiln leaves networking as installed (D52).
func (Step) DependsOn() []string { return []string{"apt-sources"} }

func (Step) Packages() []step.PackageRef {
	return []step.PackageRef{
		{Name: "cockpit", Suite: backportsSuite, Component: "main", Repo: "debian"},
		{Name: "cockpit", Suite: "trixie", Component: "main", Repo: "debian"},
		{Name: "cockpit-networkmanager", Suite: backportsSuite, Component: "main", Repo: "debian"},
		{Name: "cockpit-networkmanager", Suite: "trixie", Component: "main", Repo: "debian"},
	}
}

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{{Key: "backports", Label: "Cockpit from trixie-backports", Kind: options.Bool, Default: true,
		Help: "Cockpit 371 from backports instead of 337 from trixie. Falls back to trixie when backports is off."}}
}

// fromBackports reports whether this run installs from backports: the option
// is on and apt-sources has backports enabled.
func fromBackports(env *step.Env) bool {
	bp, ok := env.All.Values["apt-sources.backports"].(bool)
	return env.Opts.Bool("backports") && ok && bp
}

func isBackportsVersion(v string) bool { return strings.Contains(v, "~bpo") }

func unitState(ctx context.Context, s sys.System, verb, unit string) string {
	r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{verb, unit}})
	return strings.TrimSpace(r.Stdout)
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	v := apt.InstalledVersion(ctx, env.Sys, "cockpit")
	if v == "" {
		return step.Todo("cockpit isn't installed"), nil
	}
	if fromBackports(env) && !isBackportsVersion(v) {
		return step.Todo("cockpit %s is from trixie, not trixie-backports", v), nil
	}
	if !apt.Installed(ctx, env.Sys, "cockpit-networkmanager") {
		return step.Todo("cockpit-networkmanager (the networking page) isn't installed"), nil
	}
	if st := unitState(ctx, env.Sys, "is-enabled", socket); st != "enabled" {
		return step.Todo("%s isn't enabled (%s)", socket, st), nil
	}
	if st := unitState(ctx, env.Sys, "is-active", socket); st != "active" {
		return step.Todo("%s isn't active (%s)", socket, st), nil
	}
	return step.Done("Cockpit " + v + " listening on port 9090"), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	var acts []action.Action
	suite := ""
	if fromBackports(env) {
		suite = backportsSuite
	} else if v := apt.InstalledVersion(ctx, env.Sys, "cockpit"); isBackportsVersion(v) {
		env.Notes.Add("Cockpit %s from trixie-backports stays installed; kiln doesn't downgrade it to trixie's version.", v)
	}
	// Recommends are kept: cockpit-networkmanager is the networking page (D37).
	// It's named too, because apt installs no Recommends for a package that's
	// already installed, and Check requires it.
	acts = append(acts, action.AptInstall{Pkgs: []string{"cockpit", "cockpit-networkmanager"}, Suite: suite})
	env.Notes.Warn("Cockpit's recommended packages pull in network-manager, packagekit and udisks2.")
	acts = append(acts, action.Custom{
		Desc: "check that " + socket + " (installed and started by the package) is enabled and active, waiting up to 15 s",
		K:    action.Services,
		Fn: func(ctx context.Context, s sys.System) error {
			if s.DryRun() {
				return nil
			}
			deadline := time.Now().Add(15 * time.Second)
			for {
				if unitState(ctx, s, "is-enabled", socket) == "enabled" && unitState(ctx, s, "is-active", socket) == "active" {
					return nil
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("%s isn't enabled and active after installing cockpit; run `systemctl enable --now %s` and check `journalctl -u %s`", socket, socket, socket)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
				}
			}
		},
	})

	ip := env.Facts.PrimaryIPv4()
	if ip == "" {
		ip = "<lan-ip>"
	}
	env.Notes.Add("Cockpit: https://%s:9090", ip)
	env.Notes.Add("Cockpit listens on port 9090 on every network this machine is connected to; kiln doesn't set up a firewall (D50).")
	env.Notes.Add("Log in to Cockpit with %s's password. root is blocked (/etc/cockpit/disallowed-users).", env.User().Name)
	return acts, nil
}
