// Package repos implements catalog step 8 `repos` (docs/steps-catalog.md):
// extrepo repositories (Google Chrome by default) and Flathub.
package repos

import (
	"context"
	"fmt"
	"regexp"
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
	extrepoConfig = "/etc/extrepo/config.yaml"
	sourcesDir    = "/etc/apt/sources.list.d"
	chromeDefault = "/etc/default/google-chrome"
	flathubURL    = "https://dl.flathub.org/repo/flathub.flatpakrepo"
)

// known holds what the catalog says about specific extrepo repositories.
// Other names are enabled with the policy `extrepo search` reports, and no package.
var known = map[string]struct {
	Policy  string
	Package string
	Desktop bool // its packages need a desktop: applies only when GNOME is detected
}{
	"google_chrome": {Policy: "non-free", Package: "google-chrome-stable", Desktop: true},
}

var flatpakPackages = []string{"flatpak", "gnome-software-plugin-flatpak"}

// Step is the `repos` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "repos" }
func (Step) Title() string       { return "Extra repositories (extrepo, Flathub)" }
func (Step) Category() string    { return "apps" }
func (Step) DependsOn() []string { return []string{"apt-sources"} }

func (Step) Packages() []step.PackageRef {
	out := []step.PackageRef{{Name: "extrepo", Suite: "trixie", Component: "main", Repo: "debian"}}
	for name, k := range known {
		out = append(out, step.PackageRef{Name: k.Package, Suite: "stable", Component: "main", Repo: "extrepo:" + name})
	}
	for _, p := range flatpakPackages {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "main", Repo: "debian"})
	}
	return out
}

var (
	nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
	itemRE = regexp.MustCompile(`^(#\s*)?-\s*(\S+)\s*$`) // "- main" or "# - non-free"
)

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "extrepo", Label: "extrepo repositories", Kind: options.List, Default: []string{"google_chrome"},
			Help: "Names as `extrepo search` shows them. Repositories for desktop apps need GNOME.",
			Validate: func(v any) error {
				l, _ := v.([]string)
				for _, n := range l {
					if !nameRE.MatchString(n) {
						return fmt.Errorf("%q isn't a valid extrepo repository name", n)
					}
				}
				return nil
			}},
		{Key: "flathub", Label: "Flathub", Kind: options.Bool, Default: true,
			Help: "Flatpak with the Flathub remote and GNOME Software's Flatpak plugin. Needs GNOME."},
	}
}

// chosen returns the extrepo repos that apply on this machine, and the ones that don't.
func chosen(env *step.Env) (use, needDesktop []string) {
	for _, n := range env.Opts.List("extrepo") {
		if known[n].Desktop && !env.Facts.GNOME {
			needDesktop = append(needDesktop, n)
			continue
		}
		use = append(use, n)
	}
	return use, needDesktop
}

func flathub(env *step.Env) bool { return env.Opts.Bool("flathub") && env.Facts.GNOME }

func (Step) Applies(env *step.Env) step.Applicability {
	use, _ := chosen(env)
	if len(use) == 0 && !flathub(env) {
		if !env.Facts.GNOME {
			return step.No("no GNOME desktop installed (the chosen repositories are for desktop apps)")
		}
		return step.No("no repositories chosen")
	}
	return step.Yes()
}

func sourcesPath(name string) string { return sourcesDir + "/extrepo_" + name + ".sources" }

// repoEnabled reports whether extrepo's sources file exists with every stanza enabled.
func repoEnabled(s sys.System, name string) bool {
	b, err := s.ReadFile(sourcesPath(name))
	if err != nil {
		return false
	}
	sts := apt.ParseDeb822(string(b))
	if len(sts) == 0 {
		return false
	}
	for _, st := range sts {
		if !strings.EqualFold(strings.TrimSpace(st.Get("Enabled")), "yes") {
			return false
		}
	}
	return true
}

func chromeDefaultsOK(s sys.System) bool {
	b, err := s.ReadFile(chromeDefault)
	if err != nil {
		return false
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	return have[`repo_add_once="false"`] && have[`repo_reenable_on_distupgrade="false"`]
}

func flathubListed(ctx context.Context, s sys.System) bool {
	r, err := s.Query(ctx, sys.Cmd{Name: "flatpak", Args: []string{"remotes", "--system"}})
	if err != nil {
		return false
	}
	for _, l := range strings.Split(r.Stdout, "\n") {
		if f := strings.Fields(l); len(f) > 0 && f[0] == "flathub" {
			return true
		}
	}
	return false
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	use, _ := chosen(env)
	if len(use) > 0 && !apt.Installed(ctx, env.Sys, "extrepo") {
		return step.Todo("extrepo isn't installed"), nil
	}
	for _, n := range use {
		if !repoEnabled(env.Sys, n) {
			return step.Todo("extrepo repository %s isn't enabled", n), nil
		}
		if p := known[n].Package; p != "" && !apt.Installed(ctx, env.Sys, p) {
			return step.Todo("%s isn't installed", p), nil
		}
		if n == "google_chrome" && !chromeDefaultsOK(env.Sys) {
			return step.Todo("%s doesn't stop Chrome from adding its own duplicate source", chromeDefault), nil
		}
	}
	if flathub(env) {
		if ok, missing := apt.AllInstalled(ctx, env.Sys, flatpakPackages); !ok {
			return step.Todo("not installed yet: %s", strings.Join(missing, ", ")), nil
		}
		if !flathubListed(ctx, env.Sys) {
			return step.Todo("the flathub remote isn't configured"), nil
		}
	}
	return step.Done(fmt.Sprintf("%d extrepo repositories, Flathub %v", len(use), flathub(env))), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	use, needDesktop := chosen(env)
	if len(needDesktop) > 0 {
		env.Notes.Add("Skipped extrepo repositories for desktop apps (no GNOME desktop installed): %s", strings.Join(needDesktop, ", "))
	}
	if env.Opts.Bool("flathub") && !env.Facts.GNOME {
		env.Notes.Add("Flathub: not applicable: no GNOME desktop installed.")
	}
	var acts []action.Action
	if len(use) > 0 {
		if !apt.Installed(ctx, env.Sys, "extrepo") {
			acts = append(acts, action.AptInstall{Pkgs: []string{"extrepo"}})
		}
		var pkgs []string
		enabling := false
		for _, n := range use {
			if p := known[n].Package; p != "" {
				if !apt.Installed(ctx, env.Sys, p) {
					pkgs = append(pkgs, p)
				}
			} else {
				env.Notes.Add("extrepo repository %s is enabled; kiln doesn't know which of its packages you want, so install them yourself.", n)
			}
			if n == "google_chrome" && repoEnabled(env.Sys, n) && !chromeDefaultsOK(env.Sys) {
				return nil, fmt.Errorf("%s exists but %s is missing the keys extrepo's first enable writes (repo_add_once, repo_reenable_on_distupgrade). Run `extrepo disable google_chrome`, delete that .sources file, and run kiln again so extrepo sets it up from scratch",
					sourcesPath(n), chromeDefault)
			}
			if !repoEnabled(env.Sys, n) {
				acts = append(acts, enableRepo(n))
				enabling = true
			}
		}
		if enabling || len(pkgs) > 0 {
			acts = append(acts, action.AptUpdate{})
		}
		if len(pkgs) > 0 {
			acts = append(acts, action.AptInstall{Pkgs: pkgs})
		}
	}
	if flathub(env) {
		if ok, missing := apt.AllInstalled(ctx, env.Sys, flatpakPackages); !ok {
			acts = append(acts, action.AptInstall{Pkgs: missing})
		}
		if !flathubListed(ctx, env.Sys) {
			acts = append(acts, action.Run{Desc: "add the flathub remote (flatpak remote-add --system --if-not-exists flathub " + flathubURL + ")",
				Cmd: sys.Cmd{Name: "flatpak", Args: []string{"remote-add", "--system", "--if-not-exists", "flathub", flathubURL}, Timeout: 5 * time.Minute}})
		}
	}
	return acts, nil
}

// enableRepo makes sure the repo's policy is enabled in extrepo's config, then
// runs `extrepo enable` and lets its post-enable commands finish.
func enableRepo(name string) action.Action {
	policy := known[name].Policy
	pdesc := "the policy `extrepo search` reports"
	if policy != "" {
		pdesc = "`" + policy + "`"
	}
	return action.Custom{
		Desc: fmt.Sprintf("make sure %s is in enabled_policies in %s (key-level edit, backed up), then run `extrepo enable %s` and wait for its post-enable commands (about 10 s)", pdesc, extrepoConfig, name),
		K:    action.Packages,
		Fn: func(ctx context.Context, s sys.System) error {
			pols := []string{policy}
			if policy == "" {
				if s.DryRun() {
					pols = nil
				} else {
					var err error
					if pols, err = searchPolicies(ctx, s, name); err != nil {
						return err
					}
				}
			}
			for _, p := range pols {
				if err := ensurePolicy(s, p); err != nil {
					return err
				}
			}
			// stdin is /dev/null; extrepo only sleeps through its countdown, it never reads stdin.
			if _, err := s.Run(ctx, sys.Cmd{Name: "extrepo", Args: []string{"enable", name}, Timeout: 10 * time.Minute}); err != nil {
				return fmt.Errorf("extrepo couldn't enable %s: %w", name, err)
			}
			return nil
		},
	}
}

// searchPolicies reads a repo's license policies from `extrepo search` (YAML dump).
func searchPolicies(ctx context.Context, s sys.System, name string) ([]string, error) {
	r, err := s.Query(ctx, sys.Cmd{Name: "extrepo", Args: []string{"search", "^" + regexp.QuoteMeta(name) + "$"}, Timeout: 2 * time.Minute})
	if err != nil {
		return nil, fmt.Errorf("extrepo has no repository called %q (%w); check the name with `extrepo search` and fix the repos option", name, err)
	}
	var out []string
	in, inPolicies := false, false
	for _, l := range strings.Split(r.Stdout, "\n") {
		if strings.HasPrefix(l, "Found ") {
			in = strings.TrimSpace(l) == "Found "+name+":"
			continue
		}
		if !in {
			continue
		}
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "policy:"):
			out = append(out, strings.Trim(strings.TrimSpace(strings.TrimPrefix(l, "policy:")), `'"`))
			inPolicies = false
		case strings.HasPrefix(l, "policies:"):
			inPolicies = true
		case inPolicies && strings.HasPrefix(l, " ") && strings.Contains(t, ":"):
			_, v, _ := strings.Cut(t, ":")
			out = append(out, strings.Trim(strings.TrimSpace(v), `'"`))
		case l != "" && !strings.HasPrefix(l, " "):
			inPolicies = false
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("extrepo has no repository called %q; check the name with `extrepo search` and fix the repos option", name)
	}
	return out, nil
}

// ensurePolicy adds or uncomments "- <policy>" under enabled_policies:.
func ensurePolicy(s sys.System, policy string) error {
	b, err := s.ReadFile(extrepoConfig)
	if err != nil {
		if s.DryRun() {
			return nil // extrepo isn't installed yet in the preview
		}
		return fmt.Errorf("can't read %s (%w); reinstall extrepo", extrepoConfig, err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "enabled_policies:") {
			start = i
			break
		}
	}
	if start < 0 {
		return fmt.Errorf("%s has no enabled_policies: list; restore it from the extrepo package (apt-get install --reinstall extrepo)", extrepoConfig)
	}
	last, commented := start, -1
	for i := start + 1; i < len(lines); i++ {
		m := itemRE.FindStringSubmatch(lines[i])
		if m == nil {
			break
		}
		if m[2] == policy {
			if m[1] == "" {
				return nil // already enabled
			}
			commented = i
		}
		last = i
	}
	if commented >= 0 {
		lines[commented] = "- " + policy
	} else {
		lines = append(lines[:last+1], append([]string{"- " + policy}, lines[last+1:]...)...)
	}
	_, err = s.WriteFile(extrepoConfig, []byte(strings.Join(lines, "\n")+"\n"), sys.WriteOpts{Mode: 0o644})
	return err
}
