// Package devtools implements catalog step 9 `devtools` (docs/steps-catalog.md).
package devtools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// GitHub CLI repository (D18; values from GitHub's install_linux.md, 2026-11-07).
const (
	ghKeyURL     = "https://cli.github.com/packages/githubcli-archive-keyring.gpg"
	ghKeySHA256  = "6084d5d7bd8e288441e0e94fc6275570895da18e6751f70f057485dc2d1a811b"
	ghKeyring    = "/etc/apt/keyrings/githubcli-archive-keyring.gpg"
	ghSources    = "/etc/apt/sources.list.d/github-cli.sources"
	keyringsDir  = "/etc/apt/keyrings"
	sourceGitHub = "github"
	sourceDebian = "debian"
)

// basePackages is the catalog's list (all in trixie main; gh may come from GitHub's repo).
var basePackages = []string{
	"build-essential", "git", "git-lfs", "gh", "curl", "wget", "jq", "vim", "neovim", "tmux",
	"htop", "btop", "ripgrep", "fd-find", "fzf", "bat", "tree", "unzip", "zip", "rsync", "lsof",
	"strace", "bind9-dnsutils", "iproute2", "traceroute", "mtr-tiny", "netcat-openbsd", "cmake",
	"ninja-build", "pkgconf", "libssl-dev", "shellcheck", "bash-completion", "man-db",
}

// links are the user's convenience symlinks (Debian renames these binaries).
var links = []struct{ Name, Target string }{
	{"fd", "/usr/bin/fdfind"},
	{"bat", "/usr/bin/batcat"},
}

// Step is the `devtools` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "devtools" }
func (Step) Title() string       { return "Developer tools and GitHub CLI" }
func (Step) Category() string    { return "dev" }
func (Step) DependsOn() []string { return []string{"apt-sources"} }

func (Step) Packages() []step.PackageRef {
	out := make([]step.PackageRef, 0, len(basePackages)+1)
	for _, p := range basePackages {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "main", Repo: "debian"})
	}
	return append(out, step.PackageRef{Name: "gh", Suite: "stable", Component: "main", Repo: "github-cli"})
}

var pkgName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "gh_source", Label: "GitHub CLI source", Kind: options.Choice, Default: sourceGitHub,
			Help: "GitHub's own apt repository (current gh), or Debian's gh 2.46 (old).",
			Choices: []options.ChoiceItem{
				{Value: sourceGitHub, Label: "GitHub's apt repository (cli.github.com)"},
				{Value: sourceDebian, Label: "Debian trixie (gh 2.46)"},
			}},
		{Key: "extra_packages", Label: "Extra packages", Kind: options.List, Default: []string{},
			Help: "More Debian packages to install. Names apt doesn't know are noted and skipped.",
			Validate: func(v any) error {
				l, _ := v.([]string)
				for _, p := range l {
					if !pkgName.MatchString(p) {
						return fmt.Errorf("%q isn't a valid Debian package name", p)
					}
				}
				return nil
			}},
	}
}

func useGitHub(env *step.Env) bool { return env.Opts.String("gh_source") == sourceGitHub }

func ghSourcesContent() []byte {
	st := apt.Stanza{Fields: []apt.Field{
		{Key: "Types", Value: "deb"},
		{Key: "URIs", Value: "https://cli.github.com/packages"},
		{Key: "Suites", Value: "stable"},
		{Key: "Components", Value: "main"},
		{Key: "Architectures", Value: "amd64"},
		{Key: "Signed-By", Value: ghKeyring},
	}}
	return []byte(apt.FormatDeb822(sys.ManagedHeader("#")+"# GitHub CLI (https://github.com/cli/cli/blob/trunk/docs/install_linux.md)\n", []apt.Stanza{st}))
}

func fileSHA256(s sys.System, path string) string {
	b, err := s.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func keyOK(s sys.System) bool { return fileSHA256(s, ghKeyring) == ghKeySHA256 }

func sourcesOK(s sys.System) bool {
	b, err := s.ReadFile(ghSources)
	return err == nil && string(b) == string(ghSourcesContent())
}

// extras splits the extra packages into ones apt knows and ones it doesn't.
func extras(ctx context.Context, env *step.Env) (known, unknown []string) {
	for _, p := range env.Opts.List("extra_packages") {
		if apt.Available(ctx, env.Sys, p) {
			known = append(known, p)
		} else {
			unknown = append(unknown, p)
		}
	}
	return known, unknown
}

func localBin(u *sys.User) string { return filepath.Join(u.Home, ".local", "bin") }

// linkState reports whether a link is correct, or a non-symlink file that's left alone.
func linkState(s sys.System, path, target string) (ok, foreign bool) {
	fi, err := s.Lstat(path)
	if err != nil {
		return false, false
	}
	if fi.Mode()&fs.ModeSymlink == 0 {
		return true, true // a real file the user put there; "replaced only if they point elsewhere"
	}
	cur, err := s.Readlink(path)
	return err == nil && cur == target, false
}

func lfsConfigured(ctx context.Context, env *step.Env) bool {
	r, err := env.Sys.Query(ctx, sys.Cmd{Name: "git", Args: []string{"config", "--global", "--get", "filter.lfs.process"}, As: env.User()})
	return err == nil && strings.TrimSpace(r.Stdout) != ""
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	u := env.User()
	if useGitHub(env) {
		if !keyOK(env.Sys) {
			return step.Todo("the GitHub CLI signing key %s is missing or different", ghKeyring), nil
		}
		if !sourcesOK(env.Sys) {
			return step.Todo("%s is missing or different", ghSources), nil
		}
	}
	known, _ := extras(ctx, env)
	if ok, missing := apt.AllInstalled(ctx, env.Sys, append(append([]string{}, basePackages...), known...)); !ok {
		return step.Todo("not installed yet: %s", strings.Join(missing, ", ")), nil
	}
	for _, l := range links {
		p := filepath.Join(localBin(u), l.Name)
		if ok, _ := linkState(env.Sys, p, l.Target); !ok {
			return step.Todo("%s doesn't point to %s", p, l.Target), nil
		}
	}
	if !lfsConfigured(ctx, env) {
		return step.Todo("git-lfs isn't set up for %s (git lfs install --skip-repo)", u.Name), nil
	}
	return step.Done("developer tools installed"), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	u := env.User()
	var acts []action.Action
	refresh := false
	if useGitHub(env) {
		if !keyOK(env.Sys) {
			acts = append(acts, action.Mkdir{Path: keyringsDir, Mode: 0o755}, installGHKey())
			refresh = true
		}
		if !sourcesOK(env.Sys) {
			acts = append(acts, action.WriteFile{Path: ghSources, Content: ghSourcesContent(), Mode: 0o644})
			refresh = true
		}
	}
	if refresh {
		acts = append(acts, action.AptUpdate{})
	}

	known, unknown := extras(ctx, env)
	if len(unknown) > 0 {
		env.Notes.Warn("These extra packages aren't known to apt and will be skipped: %s", strings.Join(unknown, ", "))
		env.Notes.Add("Skipped extra packages apt doesn't know: %s. Check the names with `apt search`.", strings.Join(unknown, ", "))
	}
	if ok, missing := apt.AllInstalled(ctx, env.Sys, append(append([]string{}, basePackages...), known...)); !ok {
		acts = append(acts, action.AptInstall{Pkgs: missing})
	}

	// As the user (user-run rule): ~/.local/bin, the two symlinks, git-lfs.
	acts = append(acts, action.Run{Desc: "create " + localBin(u) + " as " + u.Name,
		Cmd: sys.Cmd{Name: "mkdir", Args: []string{"-p", localBin(u)}, As: u}})
	for _, l := range links {
		p := filepath.Join(localBin(u), l.Name)
		ok, foreign := linkState(env.Sys, p, l.Target)
		if foreign {
			env.Notes.Add("%s is a regular file, not a symlink, so kiln left it alone (Debian's binary is %s).", p, l.Target)
		}
		if !ok {
			acts = append(acts, action.Run{Desc: "link " + p + " → " + l.Target + " (as " + u.Name + ")", K: action.Files,
				Cmd: sys.Cmd{Name: "ln", Args: []string{"-sfn", l.Target, p}, As: u}})
		}
	}
	if !lfsConfigured(ctx, env) {
		acts = append(acts, action.Run{Desc: "set up git-lfs for " + u.Name + " (git lfs install --skip-repo)",
			Cmd: sys.Cmd{Name: "git", Args: []string{"lfs", "install", "--skip-repo"}, As: u}})
	}
	return acts, nil
}

// installGHKey downloads GitHub's keyring, checks its sha256 and installs it.
func installGHKey() action.Action {
	return action.Custom{
		Desc: fmt.Sprintf("download %s, check its sha256 is %s, and install it as %s (0644)", ghKeyURL, ghKeySHA256, ghKeyring),
		K:    action.Files,
		Fn: func(ctx context.Context, s sys.System) error {
			p, err := s.Fetch(ctx, sys.FetchReq{URL: ghKeyURL, SHA256: ghKeySHA256, Name: "githubcli-archive-keyring.gpg", MaxBytes: 1 << 20})
			if err != nil {
				if strings.Contains(err.Error(), "checksum mismatch") {
					return fmt.Errorf("GitHub's CLI signing key changed (%w). kiln needs an update; until then choose Debian's gh in the devtools options", err)
				}
				return fmt.Errorf("couldn't download the GitHub CLI signing key: %w", err)
			}
			if s.DryRun() {
				// Nothing was downloaded; record the write with a stand-in so the preview shows it.
				_, err = s.WriteFile(ghKeyring, []byte("(downloaded from "+ghKeyURL+" when applied)\n"), sys.WriteOpts{Mode: 0o644})
				return err
			}
			b, err := s.ReadFile(p)
			if err != nil {
				return err
			}
			_, err = s.WriteFile(ghKeyring, b, sys.WriteOpts{Mode: 0o644})
			return err
		},
	}
}
