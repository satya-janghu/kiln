// Package node implements catalog step 21 `node` (docs/steps-catalog.md):
// nvm and Node.js for the target user. Everything runs as the user.
package node

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
	"github.com/satya-janghu/kiln/internal/userenv"
)

// helperScript is the fixed (never interpolated) bash script kiln runs as the
// user, without errexit: nvm must never be sourced under `set -e`. The Node
// version arrives as a positional argument.
const helperScript = `# Managed by kiln — edits may be overwritten on the next run.
# Run as: bash <this file> install|check <node-version>. Never under set -e (nvm breaks).
export NVM_DIR="$HOME/.nvm"
. "$NVM_DIR/nvm.sh"
case "$1" in
install)
	nvm install "$2"
	nvm alias default "$2"
	node -v && npm -v
	;;
check)
	printf 'nvm=%s\n' "$(nvm --version)"
	printf 'default=%s\n' "$(nvm version default)"
	printf 'node=%s\n' "$(node -v)"
	;;
esac
`

// Step is the `node` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "node" }
func (Step) Title() string       { return "Node.js via nvm" }
func (Step) Category() string    { return "dev" }
func (Step) DependsOn() []string { return []string{"devtools"} }

var (
	nvmVerRE  = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	nodeVerRE = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)
)

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "nvm_version", Label: "nvm version", Kind: options.String, Default: "v0.41.0",
			Help: "The nvm release whose install.sh kiln downloads (pinned).",
			Validate: func(v any) error {
				if s, _ := v.(string); !nvmVerRE.MatchString(s) {
					return errors.New("use an nvm release tag such as v0.41.0")
				}
				return nil
			}},
		{Key: "node_version", Label: "Node.js version", Kind: options.String, Default: "26",
			Help: "Installed with `nvm install` and made the default.",
			Validate: func(v any) error {
				if s, _ := v.(string); !nodeVerRE.MatchString(s) {
					return errors.New("use a version number such as 26 or 26.13.0")
				}
				return nil
			}},
	}
}

type paths struct {
	nvmDir, nvmSh, cacheDir, helper, dropIn string
}

func pathsFor(u *sys.User) paths {
	cache := filepath.Join(u.Home, ".cache", "kiln")
	return paths{
		nvmDir:   filepath.Join(u.Home, ".nvm"),
		nvmSh:    filepath.Join(u.Home, ".nvm", "nvm.sh"),
		cacheDir: cache,
		helper:   filepath.Join(cache, "kiln-nvm.sh"),
		dropIn:   filepath.Join(userenv.BashrcD(u), "50-kiln-nvm.sh"),
	}
}

func dropInContent() []byte {
	return []byte(sys.ManagedHeader("#") +
		`export NVM_DIR="$HOME/.nvm"` + "\n" +
		`[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"` + "\n" +
		`[ -s "$NVM_DIR/bash_completion" ] && . "$NVM_DIR/bash_completion"` + "\n")
}

func fileIs(s sys.System, path string, want []byte) bool {
	b, err := s.ReadFile(path)
	return err == nil && string(b) == string(want)
}

// state runs the helper's check mode as the user (read-only).
func state(ctx context.Context, env *step.Env, p paths) map[string]string {
	out := map[string]string{}
	if !fileIs(env.Sys, p.helper, []byte(helperScript)) {
		return out
	}
	r, _ := env.Sys.Query(ctx, sys.Cmd{Name: "bash", Args: []string{p.helper, "check", env.Opts.String("node_version")}, As: env.User(), Timeout: 2 * time.Minute})
	for _, l := range strings.Split(r.Stdout, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

// matches reports whether a "vX.Y.Z" version belongs to the requested one ("26" → v26.x).
func matches(got, want string) bool {
	if got == "" {
		return false
	}
	return got == "v"+want || strings.HasPrefix(got, "v"+want+".")
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	p := pathsFor(env.User())
	want := env.Opts.String("node_version")
	if _, err := env.Sys.Stat(p.nvmSh); err != nil {
		return step.Todo("nvm isn't installed (%s is missing)", p.nvmSh), nil
	}
	st := state(ctx, env, p)
	if nvmVer := env.Opts.String("nvm_version"); "v"+st["nvm"] != nvmVer {
		return step.Todo("nvm isn't %s (it's %q)", nvmVer, st["nvm"]), nil
	}
	if !matches(st["default"], want) {
		return step.Todo("nvm's default Node isn't v%s (it's %q)", want, st["default"]), nil
	}
	if !matches(st["node"], want) {
		return step.Todo("node -v doesn't report v%s (it's %q)", want, st["node"]), nil
	}
	if !fileIs(env.Sys, p.dropIn, dropInContent()) {
		return step.Todo("%s is missing or different", p.dropIn), nil
	}
	if ok, d := userenv.Check(env.Sys, env.User()); !ok {
		return step.Todo("%s", d), nil
	}
	return step.Done("Node " + st["node"] + " via nvm " + st["nvm"]), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	u := env.User()
	p := pathsFor(u)
	nvmVer := env.Opts.String("nvm_version")
	nodeVer := env.Opts.String("node_version")
	var acts []action.Action

	acts = append(acts,
		action.Run{Desc: "create " + p.nvmDir + " and " + p.cacheDir + " as " + u.Name, K: action.Files,
			Cmd: sys.Cmd{Name: "mkdir", Args: []string{"-p", p.nvmDir, p.cacheDir}, As: u}},
		action.WriteFile{Path: p.helper, Content: []byte(helperScript), Mode: 0o644, Owner: u},
	)

	st := state(ctx, env, p)
	_, nvmErr := env.Sys.Stat(p.nvmSh)
	if nvmErr != nil || "v"+st["nvm"] != nvmVer {
		acts = append(acts, installNvm(u, p, nvmVer))
	}
	acts = append(acts, action.Run{
		Desc: fmt.Sprintf("as %s: nvm install %s; nvm alias default %s; node -v && npm -v (bash, no errexit)", u.Name, nodeVer, nodeVer),
		Cmd:  sys.Cmd{Name: "bash", Args: []string{p.helper, "install", nodeVer}, As: u, Timeout: 30 * time.Minute},
	})
	acts = append(acts, userenv.Actions(env.Sys, u)...)
	acts = append(acts, action.WriteFile{Path: p.dropIn, Content: dropInContent(), Mode: 0o644, Owner: u})
	return acts, nil
}

// installNvm downloads nvm's pinned install.sh and runs it with bash as the user.
func installNvm(u *sys.User, p paths, ver string) action.Action {
	url := "https://raw.githubusercontent.com/nvm-sh/nvm/" + ver + "/install.sh"
	dst := filepath.Join(p.cacheDir, "nvm-install.sh")
	return action.Custom{
		Desc: fmt.Sprintf("download %s (pinned; sha256 logged), hand it to %s as %s (0600), and run it with bash as %s with PROFILE=/dev/null NVM_DIR=%s METHOD=script", url, u.Name, dst, u.Name, p.nvmDir),
		Fn: func(ctx context.Context, s sys.System) error {
			src, err := s.Fetch(ctx, sys.FetchReq{URL: url, Name: "nvm-install.sh", MaxBytes: 1 << 20})
			if err != nil {
				return fmt.Errorf("couldn't download nvm's installer: %w", err)
			}
			if _, ierr := s.Run(ctx, sys.Cmd{Name: "install", Args: []string{"-o", u.Name, "-m", "0600", src, dst}}); ierr != nil {
				return ierr
			}
			_, err = s.Run(ctx, sys.Cmd{Name: "bash", Args: []string{dst}, As: u, Timeout: 15 * time.Minute,
				Env: []string{"PROFILE=/dev/null", "NVM_DIR=" + p.nvmDir, "METHOD=script"}})
			_, _ = s.Run(ctx, sys.Cmd{Name: "rm", Args: []string{"-f", dst}, As: u})
			if err != nil {
				return fmt.Errorf("nvm's installer failed: %w. Check the network, then run kiln again", err)
			}
			return nil
		},
	}
}
