// Package python implements catalog step 22 `python` (docs/steps-catalog.md):
// Debian's Python plus uv, an automation venv, Playwright Chromium and uv tools.
package python

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
	"github.com/satya-janghu/kiln/internal/userenv"
)

var aptPackages = []string{"python3", "python3-venv", "python3-dev", "pipx", "libxml2-dev", "libxslt1-dev", "libffi-dev"}

// venvPackages is the owner's list; every one has cp313 or pure wheels for linux x86_64.
var venvPackages = []string{
	"requests", "httpx[http2]", "aiohttp", "tenacity", "beautifulsoup4", "lxml", "selectolax", "parsel",
	"scrapy", "playwright", "selenium", "pandas", "pyyaml", "python-dotenv", "rich", "typer", "ipython",
}

const (
	uvSourcePipx      = "pipx"
	uvSourceInstaller = "installer"
	// uvInstallerURL is the pinned official installer (docs.astral.sh/uv, uv 0.13.7).
	uvInstallerURL = "https://astral.sh/uv/0.13.7/install.sh"
)

// Step is the `python` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "python" }
func (Step) Title() string       { return "Python, uv and Playwright" }
func (Step) Category() string    { return "dev" }
func (Step) DependsOn() []string { return []string{"devtools"} }

func (Step) Packages() []step.PackageRef {
	out := make([]step.PackageRef, 0, len(aptPackages))
	for _, p := range aptPackages {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "main", Repo: "debian"})
	}
	return out
}

var (
	toolRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	sepRE  = regexp.MustCompile(`[-_.]+`)
)

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "venv_path", Label: "Automation venv path", Kind: options.String, Default: "~/.venvs/automation",
			Help: "Where the automation virtual environment lives. ~ is your home directory.",
			Validate: func(v any) error {
				s, _ := v.(string)
				if !strings.HasPrefix(s, "~/") && !strings.HasPrefix(s, "/") {
					return errors.New("use an absolute path or one starting with ~/")
				}
				if strings.ContainsAny(s, "\n'\"`$\\") {
					return errors.New("the path can't contain quotes, $, backslashes or line breaks")
				}
				return nil
			}},
		{Key: "uv_source", Label: "uv source", Kind: options.Choice, Default: uvSourcePipx,
			Help: "uv isn't packaged in Debian. pipx installs it from PyPI; the official installer is a pinned script.",
			Choices: []options.ChoiceItem{
				{Value: uvSourcePipx, Label: "pipx install uv"},
				{Value: uvSourceInstaller, Label: "uv's official installer (pinned)"},
			}},
		{Key: "playwright", Label: "Playwright Chromium", Kind: options.Bool, Default: true,
			Help: "Installs Chromium for Playwright. Its system libraries are installed by kiln's apt helper."},
		{Key: "uv_tools", Label: "uv tools", Kind: options.List, Default: []string{"ruff", "pre-commit", "httpie"},
			Help: "Installed with `uv tool install`, one per call, into ~/.local/bin.",
			Validate: func(v any) error {
				l, _ := v.([]string)
				for _, t := range l {
					if !toolRE.MatchString(t) {
						return fmt.Errorf("%q isn't a valid package name", t)
					}
				}
				return nil
			}},
	}
}

type paths struct {
	home, localBin, uv, venv, venvRel, pyvenv, playwright, cacheDir, dropIn string
}

func pathsFor(env *step.Env) paths {
	u := env.User()
	v := env.Opts.String("venv_path")
	rel := ""
	if strings.HasPrefix(v, "~/") {
		rel = strings.TrimPrefix(v, "~/")
		v = filepath.Join(u.Home, rel)
	} else if r, err := filepath.Rel(u.Home, v); err == nil && !strings.HasPrefix(r, "..") {
		rel = r
	}
	v = filepath.Clean(v)
	return paths{
		home:       u.Home,
		localBin:   filepath.Join(u.Home, ".local", "bin"),
		uv:         filepath.Join(u.Home, ".local", "bin", "uv"),
		venv:       v,
		venvRel:    rel,
		pyvenv:     filepath.Join(v, "pyvenv.cfg"),
		playwright: filepath.Join(v, "bin", "playwright"),
		cacheDir:   filepath.Join(u.Home, ".cache", "kiln"),
		dropIn:     filepath.Join(userenv.BashrcD(u), "50-kiln-python.sh"),
	}
}

func dropInContent(p paths) []byte {
	act := `"$HOME/` + p.venvRel + `/bin/activate"`
	if p.venvRel == "" {
		act = `"` + p.venv + `/bin/activate"`
	}
	return []byte(sys.ManagedHeader("#") +
		`case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) PATH="$HOME/.local/bin:$PATH" ;; esac` + "\n" +
		`alias pyauto='. ` + act + `'` + "\n")
}

// normalize is PEP 503 name normalisation, minus extras ("httpx[http2]" → "httpx").
func normalize(name string) string {
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	return sepRE.ReplaceAllString(strings.ToLower(name), "-")
}

func exists(s sys.System, p string) bool { _, err := s.Stat(p); return err == nil }

// installedInVenv parses `uv pip list` (columns: Package Version, then a dashed rule).
func installedInVenv(ctx context.Context, env *step.Env, p paths) map[string]bool {
	out := map[string]bool{}
	if !exists(env.Sys, p.uv) || !exists(env.Sys, p.pyvenv) {
		return out
	}
	r, err := env.Sys.Query(ctx, sys.Cmd{Name: p.uv, Args: []string{"pip", "list", "--python", p.venv}, As: env.User()})
	if err != nil {
		return out
	}
	for _, l := range strings.Split(r.Stdout, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || strings.HasPrefix(f[0], "-") || f[0] == "Package" {
			continue
		}
		out[normalize(f[0])] = true
	}
	return out
}

func missingVenvPackages(ctx context.Context, env *step.Env, p paths) []string {
	have := installedInVenv(ctx, env, p)
	var missing []string
	for _, pkg := range venvPackages {
		if !have[normalize(pkg)] {
			missing = append(missing, pkg)
		}
	}
	return missing
}

// uvTools parses `uv tool list`: tool lines look like "ruff v0.x.y", binaries like "- ruff".
func uvTools(ctx context.Context, env *step.Env, p paths) map[string]bool {
	out := map[string]bool{}
	if !exists(env.Sys, p.uv) {
		return out
	}
	r, err := env.Sys.Query(ctx, sys.Cmd{Name: p.uv, Args: []string{"tool", "list"}, As: env.User()})
	if err != nil {
		return out
	}
	for _, l := range strings.Split(r.Stdout, "\n") {
		f := strings.Fields(l)
		if len(f) >= 1 && !strings.HasPrefix(l, "-") && !strings.HasPrefix(l, " ") {
			out[normalize(f[0])] = true
		}
	}
	return out
}

// missingDeps runs Playwright's dry run (exit 0: nothing missing; exit 1: a list).
func missingDeps(ctx context.Context, s sys.System, u *sys.User, p paths) (pkgs []string, ok bool, err error) {
	r, qerr := s.Query(ctx, sys.Cmd{Name: p.playwright, Args: []string{"install-deps", "--dry-run", "chromium"}, As: u, Timeout: 5 * time.Minute})
	if qerr == nil {
		return nil, true, nil
	}
	var ee *sys.ExitError
	if !errors.As(qerr, &ee) || ee.Code != 1 {
		return nil, false, fmt.Errorf("playwright install-deps --dry-run failed: %w", qerr)
	}
	in := false
	for _, l := range strings.Split(r.Stdout+"\n"+r.Stderr, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "Missing system dependencies"):
			in = true
			continue
		case !in:
			continue
		case t == "":
			if len(pkgs) > 0 {
				return pkgs, false, nil
			}
			continue
		case l == t: // not indented: the list has ended
			return pkgs, false, nil
		}
		pkgs = append(pkgs, strings.Fields(t)[0])
	}
	if len(pkgs) == 0 {
		return nil, false, errors.New("playwright reported missing system dependencies but kiln couldn't read the list; run `playwright install-deps --dry-run chromium` in the venv to see it")
	}
	return pkgs, false, nil
}

func chromiumInstalled(s sys.System, home string) bool {
	m, _ := s.Glob(filepath.Join(home, ".cache", "ms-playwright", "chromium-*"))
	return len(m) > 0
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	u := env.User()
	p := pathsFor(env)
	if ok, missing := apt.AllInstalled(ctx, env.Sys, aptPackages); !ok {
		return step.Todo("not installed yet: %s", strings.Join(missing, ", ")), nil
	}
	if !exists(env.Sys, p.uv) {
		return step.Todo("uv isn't installed in %s", p.localBin), nil
	}
	if !exists(env.Sys, p.pyvenv) {
		return step.Todo("the venv %s doesn't exist", p.venv), nil
	}
	if m := missingVenvPackages(ctx, env, p); len(m) > 0 {
		return step.Todo("missing in the venv: %s", strings.Join(m, ", ")), nil
	}
	if env.Opts.Bool("playwright") {
		if _, ok, err := missingDeps(ctx, env.Sys, u, p); err != nil || !ok {
			return step.Todo("Playwright's Chromium system libraries aren't all installed"), nil
		}
		if !chromiumInstalled(env.Sys, p.home) {
			return step.Todo("Playwright's Chromium isn't downloaded"), nil
		}
	}
	tools := uvTools(ctx, env, p)
	for _, t := range env.Opts.List("uv_tools") {
		if !tools[normalize(t)] {
			return step.Todo("uv tool %s isn't installed", t), nil
		}
	}
	if ok, d := userenv.Check(env.Sys, u); !ok {
		return step.Todo("%s", d), nil
	}
	if b, err := env.Sys.ReadFile(p.dropIn); err != nil || string(b) != string(dropInContent(p)) {
		return step.Todo("%s is missing or different", p.dropIn), nil
	}
	return step.Done("Python venv at " + p.venv + " with uv"), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	u := env.User()
	p := pathsFor(env)
	var acts []action.Action

	// 1. Debian packages.
	if ok, missing := apt.AllInstalled(ctx, env.Sys, aptPackages); !ok {
		acts = append(acts, action.AptInstall{Pkgs: missing})
	}
	// 2. uv, as the user.
	if !exists(env.Sys, p.uv) {
		if env.Opts.String("uv_source") == uvSourceInstaller {
			acts = append(acts, installUV(u, p))
		} else {
			acts = append(acts, action.Run{Desc: "as " + u.Name + ": pipx install uv (into " + p.localBin + ")",
				Cmd: sys.Cmd{Name: "pipx", Args: []string{"install", "uv"}, As: u, Timeout: 15 * time.Minute}})
		}
	}
	// 3. The venv and its packages.
	if !exists(env.Sys, p.pyvenv) {
		acts = append(acts,
			action.Run{Desc: "create " + filepath.Dir(p.venv) + " as " + u.Name, K: action.Files,
				Cmd: sys.Cmd{Name: "mkdir", Args: []string{"-p", filepath.Dir(p.venv)}, As: u}},
			action.Run{Desc: "as " + u.Name + ": uv venv --python /usr/bin/python3 " + p.venv,
				Cmd: sys.Cmd{Name: p.uv, Args: []string{"venv", "--python", "/usr/bin/python3", p.venv}, As: u}})
	}
	if m := missingVenvPackages(ctx, env, p); len(m) > 0 {
		args := append([]string{"pip", "install", "--python", p.venv}, venvPackages...)
		acts = append(acts, action.Run{Desc: "as " + u.Name + ": uv pip install --python " + p.venv + " " + strings.Join(venvPackages, " "),
			Cmd: sys.Cmd{Name: p.uv, Args: args, As: u, Timeout: 30 * time.Minute}})
	}
	// 4. Playwright: system deps through kiln's apt helper (D25), then Chromium as the user.
	if env.Opts.Bool("playwright") {
		acts = append(acts, action.AptUpdate{}, action.Custom{
			Desc: "as " + u.Name + ": `" + p.playwright + " install-deps --dry-run chromium`; install any missing system libraries it lists with kiln's apt helper (never Playwright's own apt), then `playwright install chromium` as " + u.Name,
			K:    action.Packages,
			Fn: func(ctx context.Context, s sys.System) error {
				if s.DryRun() && !exists(s, p.playwright) {
					_, err := s.Run(ctx, sys.Cmd{Name: p.playwright, Args: []string{"install", "chromium"}, As: u})
					return err
				}
				pkgs, ok, err := missingDeps(ctx, s, u, p)
				if err != nil {
					return err
				}
				if !ok {
					if err := apt.Install(ctx, s, pkgs, apt.InstallOpts{}); err != nil {
						return fmt.Errorf("couldn't install Playwright's system libraries: %w", err)
					}
				}
				if _, err := s.Run(ctx, sys.Cmd{Name: p.playwright, Args: []string{"install", "chromium"}, As: u, Timeout: 30 * time.Minute}); err != nil {
					return fmt.Errorf("playwright couldn't download Chromium: %w. Check the network, then run kiln again", err)
				}
				return nil
			},
		})
	}
	// 5. uv tools, one per call.
	tools := uvTools(ctx, env, p)
	for _, t := range env.Opts.List("uv_tools") {
		if !tools[normalize(t)] {
			acts = append(acts, action.Run{Desc: "as " + u.Name + ": uv tool install " + t,
				Cmd: sys.Cmd{Name: p.uv, Args: []string{"tool", "install", t}, As: u, Timeout: 15 * time.Minute}})
		}
	}
	// 6. Shell: ~/.bashrc loads ~/.bashrc.d; the python drop-in.
	acts = append(acts, userenv.Actions(env.Sys, u)...)
	acts = append(acts, action.WriteFile{Path: p.dropIn, Content: dropInContent(p), Mode: 0o644, Owner: u})
	return acts, nil
}

// installUV runs uv's pinned official installer as the user (the "installer" option).
func installUV(u *sys.User, p paths) action.Action {
	dst := filepath.Join(p.cacheDir, "uv-install.sh")
	return action.Custom{
		Desc: fmt.Sprintf("download %s (pinned; sha256 logged), hand it to %s as %s (0600), and run it with bash as %s with UV_NO_MODIFY_PATH=1", uvInstallerURL, u.Name, dst, u.Name),
		Fn: func(ctx context.Context, s sys.System) error {
			src, err := s.Fetch(ctx, sys.FetchReq{URL: uvInstallerURL, Name: "uv-install.sh", MaxBytes: 4 << 20})
			if err != nil {
				return fmt.Errorf("couldn't download uv's installer: %w", err)
			}
			if _, merr := s.Run(ctx, sys.Cmd{Name: "mkdir", Args: []string{"-p", p.cacheDir}, As: u}); merr != nil {
				return merr
			}
			if _, ierr := s.Run(ctx, sys.Cmd{Name: "install", Args: []string{"-o", u.Name, "-m", "0600", src, dst}}); ierr != nil {
				return ierr
			}
			_, err = s.Run(ctx, sys.Cmd{Name: "bash", Args: []string{dst}, As: u, Timeout: 15 * time.Minute, Env: []string{"UV_NO_MODIFY_PATH=1"}})
			_, _ = s.Run(ctx, sys.Cmd{Name: "rm", Args: []string{"-f", dst}, As: u})
			if err != nil {
				return fmt.Errorf("uv's installer failed: %w. Check the network, or choose pipx as the uv source", err)
			}
			return nil
		},
	}
}
