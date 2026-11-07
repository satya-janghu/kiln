// Package golang implements catalog step 23 `go` (docs/steps-catalog.md):
// the Go toolchain from go.dev in /usr/local/go, plus the user's Go tools.
package golang

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
	"github.com/satya-janghu/kiln/internal/userenv"
)

const (
	goRoot     = "/usr/local/go"
	goNew      = "/usr/local/go.kiln-new"
	goOld      = "/usr/local/go.kiln-old"
	goBin      = goRoot + "/bin/go"
	debianGo   = "/usr/bin/go"
	profileD   = "/etc/profile.d/kiln-go.sh"
	latest     = "latest"
	indexURL   = "https://go.dev/dl/?mode=json"
	allURL     = "https://go.dev/dl/?mode=json&include=all"
	dlBase     = "https://go.dev/dl/"
	backports  = "trixie-backports"
	pathExport = "export PATH=/usr/local/go/bin:$PATH:$HOME/go/bin\n"
)

// toolPaths maps the option's tool names to their packages (one `go install` each:
// a single invocation can't take packages from several modules).
var toolPaths = map[string]string{
	"gopls":       "golang.org/x/tools/gopls",
	"dlv":         "github.com/go-delve/delve/cmd/dlv",
	"staticcheck": "honnef.co/go/tools/cmd/staticcheck",
	"govulncheck": "golang.org/x/vuln/cmd/govulncheck",
}

// Step is the `go` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "go" }
func (Step) Title() string       { return "Go toolchain and tools" }
func (Step) Category() string    { return "dev" }
func (Step) DependsOn() []string { return []string{"devtools"} }

func (Step) Packages() []step.PackageRef {
	return []step.PackageRef{
		{Name: "golang-go", Suite: backports, Component: "main", Repo: "debian"},
		{Name: "golang-go", Suite: "trixie", Component: "main", Repo: "debian"},
	}
}

var versionRE = regexp.MustCompile(`^1\.[0-9]+(\.[0-9]+)?$`)

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "version", Label: "Go version", Kind: options.String, Default: latest,
			Help: "latest = the newest stable release on go.dev, or a version such as 1.27.3.",
			Validate: func(v any) error {
				if s, _ := v.(string); s != latest && !versionRE.MatchString(s) {
					return errors.New("use latest or a version such as 1.27.3")
				}
				return nil
			}},
		{Key: "debian_fallback", Label: "Debian fallback", Kind: options.Bool, Default: true,
			Help: "If go.dev can't be reached, install golang-go from trixie-backports (or trixie) instead."},
		{Key: "tools", Label: "Go tools", Kind: options.List, Default: []string{"gopls", "dlv", "staticcheck", "govulncheck"},
			Help: "Installed with `go install <path>@latest` into ~/go/bin.",
			Validate: func(v any) error {
				l, _ := v.([]string)
				for _, t := range l {
					if _, ok := toolPaths[t]; !ok {
						return fmt.Errorf("unknown tool %q; choose from gopls, dlv, staticcheck, govulncheck", t)
					}
				}
				return nil
			}},
		{Key: "update_tools", Label: "Update tools on this run", Kind: options.Bool, Default: false, PerRun: true,
			Help: "Off by design: on re-installs every tool @latest, so a second run would change something (P4)."},
	}
}

// release is one go.dev download.
type release struct {
	Version  string // "go1.27.3"
	Filename string
	SHA256   string
}

type goFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
	Kind     string `json:"kind"`
}

type goRelease struct {
	Version string   `json:"version"`
	Stable  bool     `json:"stable"`
	Files   []goFile `json:"files"`
}

// run holds what this process did, so the re-check after Apply can tell a tool
// skipped on purpose (fallback Go too old) and an update that already ran.
var run struct {
	sync.Mutex
	skipped map[string]bool
	updated bool
}

func skipped(name string) bool {
	run.Lock()
	defer run.Unlock()
	return run.skipped[name]
}

// resolution is cached for the process: Check, Plan and the re-check share it.
var (
	resMu    sync.Mutex
	resCache = map[string]resolved{}
)

type resolved struct {
	rel release
	err error
}

// resolve finds the release and its sha256 in go.dev's JSON index.
func resolve(ctx context.Context, s sys.System, want string) (release, error) {
	resMu.Lock()
	defer resMu.Unlock()
	if r, ok := resCache[want]; ok {
		return r.rel, r.err
	}
	rel, err := fetchRelease(ctx, s, want)
	if err == nil { // failures are retried: a network blip in the preview mustn't force the fallback
		resCache[want] = resolved{rel, nil}
	}
	return rel, err
}

// errUnreachable marks "go.dev couldn't be reached" (the fallback's trigger).
var errUnreachable = errors.New("go.dev can't be reached")

func fetchRelease(ctx context.Context, s sys.System, want string) (release, error) {
	url := indexURL
	if want != latest {
		url = allURL
	}
	b, err := s.HTTPGet(ctx, url, 16<<20)
	if err != nil {
		return release{}, fmt.Errorf("%w (%w)", errUnreachable, err)
	}
	var rels []goRelease
	if err := json.Unmarshal(b, &rels); err != nil {
		return release{}, fmt.Errorf("go.dev's download index couldn't be read (%w); try again later", err)
	}
	for _, r := range rels {
		if want == latest && !r.Stable {
			continue
		}
		if want != latest && r.Version != "go"+want {
			continue
		}
		for _, f := range r.Files {
			if f.Kind == "archive" && f.OS == "linux" && f.Arch == "amd64" && f.SHA256 != "" {
				return release{Version: r.Version, Filename: f.Filename, SHA256: f.SHA256}, nil
			}
		}
	}
	return release{}, fmt.Errorf("go.dev lists no linux-amd64 archive for Go %s; check the version in the go options", want)
}

// installedVersion returns the first line of /usr/local/go/VERSION ("go1.27.3").
func installedVersion(s sys.System) string {
	b, err := s.ReadFile(filepath.Join(goRoot, "VERSION"))
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(first)
}

// goVersion asks the binary itself ("go version go1.27.3 linux/amd64").
func goVersion(ctx context.Context, s sys.System, bin string) string {
	r, err := s.Query(ctx, sys.Cmd{Name: bin, Args: []string{"version"}, Env: []string{"GOTOOLCHAIN=local"}, Dir: "/"})
	if err != nil {
		return ""
	}
	f := strings.Fields(r.Stdout)
	if len(f) >= 3 {
		return f[2]
	}
	return ""
}

func backportsOn(env *step.Env) bool {
	v, ok := env.All.Values["apt-sources.backports"].(bool)
	return ok && v
}

func userGoBin(u *sys.User) string { return filepath.Join(u.Home, "go", "bin") }

func missingTools(env *step.Env) []string {
	var out []string
	for _, t := range env.Opts.List("tools") {
		if skipped(t) {
			continue
		}
		if _, err := env.Sys.Stat(filepath.Join(userGoBin(env.User()), t)); err != nil {
			out = append(out, t)
		}
	}
	return out
}

func dropInPath(u *sys.User) string { return filepath.Join(userenv.BashrcD(u), "50-kiln-go.sh") }

func profileContent() []byte { return []byte(sys.ManagedHeader("#") + pathExport) }

func fileIs(s sys.System, path string, want []byte) bool {
	b, err := s.ReadFile(path)
	return err == nil && string(b) == string(want)
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	// An interrupted swap leaves go.kiln-new or go.kiln-old behind (D41).
	for _, d := range []string{goNew, goOld} {
		if _, err := env.Sys.Lstat(d); err == nil {
			return step.Todo("%s is left over from an interrupted run", d), nil
		}
	}
	want := env.Opts.String("version")
	rel, err := resolve(ctx, env.Sys, want)
	detail := ""
	switch {
	case err == nil:
		if got := goVersion(ctx, env.Sys, goBin); got != rel.Version {
			return step.Todo("%s is %q, not %s", goBin, got, rel.Version), nil
		}
		detail = rel.Version
	case errors.Is(err, errUnreachable):
		// Offline: accept what's installed (go.dev's Go, or the Debian fallback).
		if got := goVersion(ctx, env.Sys, goBin); got != "" {
			detail = got + " (go.dev unreachable, newer releases not checked)"
		} else if env.Opts.Bool("debian_fallback") && apt.Installed(ctx, env.Sys, "golang-go") {
			detail = "golang-go " + apt.InstalledVersion(ctx, env.Sys, "golang-go") + " (Debian fallback)"
		} else {
			return step.Todo("Go isn't installed and %v", err), nil
		}
	default:
		return step.Todo("%v", err), nil
	}
	if m := missingTools(env); len(m) > 0 {
		return step.Todo("Go tools missing from %s: %s", userGoBin(env.User()), strings.Join(m, ", ")), nil
	}
	run.Lock()
	updated := run.updated
	run.Unlock()
	if env.Opts.Bool("update_tools") && !updated {
		return step.Todo("updating the Go tools was requested"), nil
	}
	if !fileIs(env.Sys, profileD, profileContent()) || !fileIs(env.Sys, dropInPath(env.User()), profileContent()) {
		return step.Todo("the Go PATH settings (%s, %s) are missing or different", profileD, dropInPath(env.User())), nil
	}
	if ok, d := userenv.Check(env.Sys, env.User()); !ok {
		return step.Todo("%s", d), nil
	}
	return step.Done("Go " + detail), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	u := env.User()
	want := env.Opts.String("version")
	var acts []action.Action

	// Leftovers from an interrupted swap (D41). If kiln died between the two
	// renames there is no /usr/local/go, and go.kiln-old is the only working
	// Go: put it back instead of deleting it.
	_, rootErr := env.Sys.Lstat(goRoot)
	_, oldErr := env.Sys.Stat(filepath.Join(goOld, "VERSION"))
	if rootErr != nil && oldErr == nil {
		acts = append(acts, action.Custom{Desc: "put " + goOld + " back as " + goRoot + " (an interrupted run left no " + goRoot + ")", K: action.Files,
			Fn: func(_ context.Context, s sys.System) error { return s.Rename(goOld, goRoot) }})
	}
	for _, d := range []string{goNew, goOld} {
		if _, err := env.Sys.Lstat(d); err == nil && (d != goOld || rootErr == nil || oldErr != nil) {
			acts = append(acts, action.Custom{Desc: "remove the leftover " + d + " from an interrupted run", K: action.Files,
				Fn: func(_ context.Context, s sys.System) error { return s.RemoveAll(d) }})
		}
	}

	goCmd, fallback := goBin, false
	rel, err := resolve(ctx, env.Sys, want)
	switch {
	case err == nil:
		if installedVersion(env.Sys) != rel.Version {
			acts = append(acts, installGo(rel))
		}
	case errors.Is(err, errUnreachable) && env.Opts.Bool("debian_fallback"):
		if goVersion(ctx, env.Sys, goBin) == "" {
			goCmd, fallback = debianGo, true
			suite := ""
			if backportsOn(env) {
				suite = backports
			}
			acts = append(acts, action.AptInstall{Pkgs: []string{"golang-go"}, Suite: suite})
			from := "trixie (1.24)"
			if suite != "" {
				from = "trixie-backports (1.27)"
			}
			env.Notes.Add("go.dev couldn't be reached, so kiln installed Debian's golang-go from %s instead. Run kiln again later to get Go from go.dev.", from)
		}
	default:
		return nil, fmt.Errorf("couldn't find the Go release to install: %w", err)
	}

	// PATH: /usr/local/go/bin first, so it wins over /usr/bin/go.
	acts = append(acts, action.WriteFile{Path: profileD, Content: profileContent(), Mode: 0o644})
	acts = append(acts, userenv.Actions(env.Sys, u)...)
	acts = append(acts, action.WriteFile{Path: dropInPath(u), Content: profileContent(), Mode: 0o644, Owner: u})

	// Tools, as the user, one `go install` per module.
	missing := map[string]bool{}
	for _, t := range missingTools(env) {
		missing[t] = true
	}
	for _, t := range env.Opts.List("tools") {
		if !missing[t] && !env.Opts.Bool("update_tools") {
			continue
		}
		acts = append(acts, goInstall(env, goCmd, t, toolPaths[t], fallback))
	}
	return acts, nil
}

// Apply runs the plan, then remembers that this run already updated the tools.
func (s Step) Apply(ctx context.Context, env *step.Env, plan []action.Action) error {
	err := action.RunAll(ctx, env.Sys, plan)
	if err == nil && !env.Sys.DryRun() {
		run.Lock()
		run.updated = true
		run.Unlock()
	}
	return err
}

// installGo downloads, verifies, extracts to go.kiln-new and swaps it in by rename.
func installGo(rel release) action.Action {
	url := dlBase + rel.Filename
	return action.Custom{
		Desc: fmt.Sprintf("download %s (sha256 %s), extract it into %s, then swap: %s → %s, %s → %s, remove %s",
			url, rel.SHA256, goNew, goRoot, goOld, goNew, goRoot, goOld),
		K: action.Files,
		Fn: func(ctx context.Context, s sys.System) error {
			tgz, err := s.Fetch(ctx, sys.FetchReq{URL: url, SHA256: rel.SHA256, Name: rel.Filename, MaxBytes: 512 << 20})
			if err != nil {
				return fmt.Errorf("couldn't download Go %s: %w", rel.Version, err)
			}
			if err := s.MkdirAll(goNew, 0o755, nil); err != nil {
				return err
			}
			if _, err := s.Run(ctx, sys.Cmd{Name: "tar", Args: []string{"-C", goNew, "--strip-components=1", "--no-same-owner", "-xzf", tgz}, Timeout: 20 * time.Minute}); err != nil {
				_ = s.RemoveAll(goNew)
				return fmt.Errorf("couldn't extract Go %s: %w", rel.Version, err)
			}
			if !s.DryRun() {
				if b, err := s.ReadFile(filepath.Join(goNew, "VERSION")); err != nil || !strings.HasPrefix(string(b), rel.Version) {
					_ = s.RemoveAll(goNew)
					return fmt.Errorf("the extracted Go archive isn't %s; nothing was changed", rel.Version)
				}
			}
			hadOld := false
			if _, err := s.Lstat(goRoot); err == nil {
				hadOld = true
				if err := s.Rename(goRoot, goOld); err != nil {
					return err
				}
			}
			if err := s.Rename(goNew, goRoot); err != nil {
				if hadOld {
					_ = s.Rename(goOld, goRoot)
				}
				return err
			}
			if hadOld {
				return s.RemoveAll(goOld)
			}
			return nil
		},
	}
}

// goInstall runs one `go install <path>@latest` as the user. With Debian's Go it
// uses GOTOOLCHAIN=local, and skips (with a note) a tool that needs a newer Go.
func goInstall(env *step.Env, goCmd, name, path string, fallback bool) action.Action {
	u := env.User()
	c := sys.Cmd{Name: goCmd, Args: []string{"install", path + "@latest"}, As: u, Timeout: 20 * time.Minute}
	if !fallback {
		return action.Run{Desc: "as " + u.Name + ": " + goCmd + " install " + path + "@latest", Cmd: c}
	}
	c.Env = []string{"GOTOOLCHAIN=local"}
	return action.Custom{
		Desc: "as " + u.Name + ": GOTOOLCHAIN=local " + goCmd + " install " + path + "@latest (skipped with a note if it needs a newer Go)",
		Fn: func(ctx context.Context, s sys.System) error {
			_, err := s.Run(ctx, c)
			var ee *sys.ExitError
			if errors.As(err, &ee) && strings.Contains(ee.Stderr, "requires go >=") {
				run.Lock()
				if run.skipped == nil {
					run.skipped = map[string]bool{}
				}
				run.skipped[name] = true
				run.Unlock()
				env.Notes.Add("Skipped the Go tool %s: it needs a newer Go than Debian's golang-go. Run kiln again when go.dev is reachable.", name)
				return nil
			}
			return err
		},
	}
}
