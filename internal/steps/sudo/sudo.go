// Package sudo implements catalog step 1 `sudo` (docs/steps-catalog.md).
package sudo

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Step is the `sudo` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "sudo" }
func (Step) Title() string       { return "sudo access for your user" }
func (Step) Category() string    { return "base" }
func (Step) DependsOn() []string { return []string{"preflight"} }

func (Step) Packages() []step.PackageRef {
	return []step.PackageRef{{Name: "sudo", Suite: "trixie", Component: "main", Repo: "debian"}}
}

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{{Key: "nopasswd", Label: "Passwordless sudo", Kind: options.Bool, Default: true,
		Help: "Writes /etc/sudoers.d/90-<user>-nopasswd so your user can run sudo without a password."}}
}

var unsafe = regexp.MustCompile(`[^a-z0-9_-]`)

// fileFor returns the sudoers.d path; sudoers(5) skips names with '.' or ending in '~'.
func fileFor(user string) string {
	return "/etc/sudoers.d/90-" + unsafe.ReplaceAllString(strings.ToLower(user), "_") + "-nopasswd"
}

func content(user string) []byte {
	return []byte(sys.ManagedHeader("#") + user + " ALL=(ALL:ALL) NOPASSWD: ALL\n")
}

func inGroup(ctx context.Context, s sys.System, user, group string) bool {
	r, err := s.Query(ctx, sys.Cmd{Name: "id", Args: []string{"-nG", user}})
	if err != nil {
		return false
	}
	for _, g := range strings.Fields(r.Stdout) {
		if g == group {
			return true
		}
	}
	return false
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	u := env.User().Name
	if !apt.Installed(ctx, env.Sys, "sudo") {
		return step.Todo("sudo isn't installed"), nil
	}
	if !inGroup(ctx, env.Sys, u, "sudo") {
		return step.Todo("%s isn't in the sudo group", u), nil
	}
	if env.Opts.Bool("nopasswd") {
		path := fileFor(u)
		b, err := env.Sys.ReadFile(path)
		if err != nil || string(b) != string(content(u)) {
			return step.Todo("%s is missing or different", path), nil
		}
		fi, err := env.Sys.Stat(path)
		if err != nil || fi.Mode().Perm() != 0o440 {
			return step.Todo("%s doesn't have mode 0440", path), nil
		}
		if _, err := env.Sys.Query(ctx, sys.Cmd{Name: "visudo", Args: []string{"-c", "-q"}}); err != nil {
			return step.Todo("the sudo policy doesn't validate: %v", err), nil
		}
	}
	return step.Done(u + " can use sudo"), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	u := env.User().Name
	var acts []action.Action
	if !apt.Installed(ctx, env.Sys, "sudo") {
		acts = append(acts, action.AptInstall{Pkgs: []string{"sudo"}})
	}
	acts = append(acts, action.UserAddGroup{User: u, Group: "sudo"})
	if env.Opts.Bool("nopasswd") {
		path := fileFor(u)
		data := content(u)
		acts = append(acts, action.Custom{
			Desc: fmt.Sprintf("write %s (0440) after validating it with visudo -cf, then check the whole policy with visudo -c", path),
			K:    action.Files,
			Fn: func(ctx context.Context, s sys.System) error {
				// The check copy has a '.' in its name, so sudo ignores it (sudoers(5)).
				tmp := "/etc/sudoers.d/.kiln-check." + strings.TrimPrefix(path, "/etc/sudoers.d/")
				if _, err := s.WriteFile(tmp, data, sys.WriteOpts{Mode: 0o440, NoBackup: true}); err != nil {
					return err
				}
				_, verr := s.Run(ctx, sys.Cmd{Name: "visudo", Args: []string{"-c", "-q", "-f", tmp}})
				_ = s.RemoveAll(tmp) // kiln's own temporary file: no backup
				if verr != nil {
					return fmt.Errorf("the sudoers rule didn't validate, nothing was installed: %w", verr)
				}
				prev, perr := s.ReadFile(path)
				prevMode := fs.FileMode(0o440)
				if fi, err := s.Stat(path); err == nil {
					prevMode = fi.Mode().Perm()
				}
				if _, err := s.WriteFile(path, data, sys.WriteOpts{Mode: 0o440}); err != nil {
					return err
				}
				if _, err := s.Run(ctx, sys.Cmd{Name: "visudo", Args: []string{"-c", "-q"}}); err != nil {
					// Never leave an invalid file in place: put the previous
					// file back if there was one, otherwise remove the new one.
					if perr == nil {
						if _, rerr := s.WriteFile(path, prev, sys.WriteOpts{Mode: prevMode}); rerr != nil {
							return fmt.Errorf("the sudo policy didn't validate with the new rule (%w), and the previous %s couldn't be put back (%w); restore it from %s", err, path, rerr, sys.BackupPath(path))
						}
						return fmt.Errorf("the sudo policy didn't validate with the new rule, so the previous %s was put back: %w", path, err)
					}
					_ = s.Remove(path)
					return fmt.Errorf("the sudo policy didn't validate with the new rule, so it was removed again: %w", err)
				}
				return nil
			},
		})
	}
	env.Notes.Add("Log out and back in for %s's sudo group membership to take effect.", u)
	return acts, nil
}
