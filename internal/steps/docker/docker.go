// Package docker implements catalog step 17 `docker` (docs/steps-catalog.md).
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// Values from Docker's Debian install docs (docs.docker.com/engine/install/debian/).
const (
	keyURL      = "https://download.docker.com/linux/debian/gpg"
	keyPath     = "/etc/apt/keyrings/docker.asc"
	keyringsDir = "/etc/apt/keyrings"
	sourcesPath = "/etc/apt/sources.list.d/docker.sources"
	daemonJSON  = "/etc/docker/daemon.json"
)

var (
	conflicting = []string{"docker.io", "docker-compose", "docker-doc", "docker-buildx", "podman-docker", "containerd", "runc"}
	packages    = []string{"docker-ce", "docker-ce-cli", "containerd.io", "docker-buildx-plugin", "docker-compose-plugin"}
)

// Step is the `docker` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "docker" }
func (Step) Title() string       { return "Docker Engine" }
func (Step) Category() string    { return "apps" }
func (Step) DependsOn() []string { return []string{"apt-sources", "firewall"} }

func (Step) Packages() []step.PackageRef {
	out := make([]step.PackageRef, 0, len(packages))
	for _, p := range packages {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "stable", Repo: "docker"})
	}
	return out
}

var sizeRE = regexp.MustCompile(`^[1-9][0-9]*[kmg]$`)

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "log_max_size", Label: "Log rotation: size per file", Kind: options.String, Default: "10m",
			Help: "json-file driver max-size, e.g. 10m. Applies to new containers only.",
			Validate: func(v any) error {
				if s, _ := v.(string); !sizeRE.MatchString(s) {
					return errors.New("use a number followed by k, m or g, e.g. 10m")
				}
				return nil
			}},
		{Key: "log_max_file", Label: "Log rotation: files kept", Kind: options.Int, Default: 3,
			Validate: func(v any) error {
				if n, _ := v.(int); n < 1 || n > 100 {
					return errors.New("use 1 to 100 files")
				}
				return nil
			}},
		{Key: "hello_world", Label: "Run hello-world", Kind: options.Bool, Default: true,
			Help: "Runs `docker run --rm hello-world` once Docker is up, to prove it works."},
	}
}

func sourcesContent() []byte {
	st := apt.Stanza{Fields: []apt.Field{
		{Key: "Types", Value: "deb"},
		{Key: "URIs", Value: "https://download.docker.com/linux/debian"},
		{Key: "Suites", Value: "trixie"},
		{Key: "Components", Value: "stable"},
		{Key: "Architectures", Value: "amd64"},
		{Key: "Signed-By", Value: keyPath},
	}}
	return []byte(apt.FormatDeb822(sys.ManagedHeader("#")+"# Docker CE (https://docs.docker.com/engine/install/debian/)\n", []apt.Stanza{st}))
}

func wantLogOpts(env *step.Env) map[string]string {
	return map[string]string{
		"max-size": env.Opts.String("log_max_size"),
		"max-file": fmt.Sprint(env.Opts.Int("log_max_file")), // log-opts values must be strings
	}
}

// readDaemon parses daemon.json; a missing file is an empty object.
func readDaemon(s sys.System) (map[string]any, []byte, bool, error) {
	b, err := s.ReadFile(daemonJSON)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	m := map[string]any{}
	if len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, b, true, fmt.Errorf("%s isn't valid JSON (%w); fix or remove it, then run kiln again", daemonJSON, err)
		}
	}
	return m, b, true, nil
}

func daemonOK(m map[string]any, want map[string]string) bool {
	if m["log-driver"] != "json-file" {
		return false
	}
	lo, ok := m["log-opts"].(map[string]any)
	if !ok {
		return false
	}
	for k, v := range want {
		if lo[k] != v {
			return false
		}
	}
	return true
}

// merged returns daemon.json with kiln's log settings merged into the existing keys.
func merged(m map[string]any, want map[string]string) ([]byte, error) {
	m["log-driver"] = "json-file"
	lo, _ := m["log-opts"].(map[string]any)
	if lo == nil {
		lo = map[string]any{}
	}
	for k, v := range want {
		lo[k] = v
	}
	m["log-opts"] = lo
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	var back map[string]any // parse the result before writing it
	if err := json.Unmarshal(b, &back); err != nil || !daemonOK(back, want) {
		return nil, fmt.Errorf("kiln built an invalid %s; please report this", daemonJSON)
	}
	return b, nil
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

func active(ctx context.Context, s sys.System, unit string) bool {
	r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-active", unit}})
	return strings.TrimSpace(r.Stdout) == "active"
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	if ok, missing := apt.AllInstalled(ctx, env.Sys, packages); !ok {
		return step.Todo("not installed yet: %s", strings.Join(missing, ", ")), nil
	}
	m, _, _, err := readDaemon(env.Sys)
	if err != nil {
		return step.Todo("%s", err.Error()), nil
	}
	if !daemonOK(m, wantLogOpts(env)) {
		return step.Todo("%s doesn't have kiln's log rotation settings", daemonJSON), nil
	}
	if u := env.User().Name; !inGroup(ctx, env.Sys, u, "docker") {
		return step.Todo("%s isn't in the docker group", u), nil
	}
	if !active(ctx, env.Sys, "docker") {
		return step.Todo("docker isn't running"), nil
	}
	return step.Done("Docker is installed and running"), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	u := env.User()
	var acts []action.Action
	var present []string
	for _, p := range conflicting {
		if apt.Installed(ctx, env.Sys, p) {
			present = append(present, p)
		}
	}
	if len(present) > 0 {
		acts = append(acts, action.AptRemove{Pkgs: present})
	}

	refresh := false
	key, err := env.Sys.ReadFile(keyPath)
	if err != nil || !isArmoredKey(key) {
		acts = append(acts, action.Mkdir{Path: keyringsDir, Mode: 0o755}, fetchKey())
		refresh = true
	}
	if b, err := env.Sys.ReadFile(sourcesPath); err != nil || string(b) != string(sourcesContent()) {
		acts = append(acts, action.WriteFile{Path: sourcesPath, Content: sourcesContent(), Mode: 0o644})
		refresh = true
	}
	if refresh || !apt.Installed(ctx, env.Sys, "docker-ce") {
		acts = append(acts, action.AptUpdate{})
	}
	if ok, missing := apt.AllInstalled(ctx, env.Sys, packages); !ok {
		acts = append(acts, action.AptInstall{Pkgs: missing})
	}

	want := wantLogOpts(env)
	acts = append(acts, action.Custom{
		Desc: fmt.Sprintf("merge {\"log-driver\":\"json-file\",\"log-opts\":{\"max-size\":%q,\"max-file\":%q}} into %s; if it changed, validate it (dockerd --validate, when available), restart docker and check it comes back, restoring the previous file if it doesn't",
			want["max-size"], want["max-file"], daemonJSON),
		K:  action.Files,
		Fn: func(ctx context.Context, s sys.System) error { return applyDaemon(ctx, s, want) },
	})
	if !inGroup(ctx, env.Sys, u.Name, "docker") {
		acts = append(acts, action.UserAddGroup{User: u.Name, Group: "docker"})
	}
	hello := env.Opts.Bool("hello_world")
	desc := "wait until `docker info` succeeds (up to 60 s)"
	if hello {
		desc += ", then run `docker run --rm hello-world` as root"
	}
	acts = append(acts, action.Custom{Desc: desc, Fn: func(ctx context.Context, s sys.System) error {
		if s.DryRun() {
			if hello {
				_, err := s.Run(ctx, sys.Cmd{Name: "docker", Args: []string{"run", "--rm", "hello-world"}})
				return err
			}
			return nil
		}
		if err := waitDocker(ctx, s, 60*time.Second); err != nil {
			return err
		}
		if hello {
			if _, err := s.Run(ctx, sys.Cmd{Name: "docker", Args: []string{"run", "--rm", "hello-world"}, Timeout: 10 * time.Minute}); err != nil {
				return fmt.Errorf("docker is running but hello-world failed (%w); check the network connection to Docker Hub", err)
			}
		}
		return nil
	}})

	env.Notes.Add("Log out and back in for %s's docker group membership to take effect.", u.Name)
	env.Notes.Add("Docker `-p` published ports bypass the host firewall zones; bind private services to 127.0.0.1 (e.g. -p 127.0.0.1:8080:80).")
	env.Notes.Add("The docker group is root-equivalent: anyone in it can control the whole machine.")
	env.Notes.Add("Existing containers keep their old log settings; the %s × %s rotation applies to new containers.", want["max-size"], want["max-file"])
	return acts, nil
}

func isArmoredKey(b []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(b), []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----"))
}

// fetchKey downloads Docker's signing key (HTTPS, as Docker's docs do; no published checksum).
func fetchKey() action.Action {
	return action.Custom{
		Desc: "download " + keyURL + " to " + keyPath + " (0644, world-readable as Docker's docs require)",
		K:    action.Files,
		Fn: func(ctx context.Context, s sys.System) error {
			p, err := s.Fetch(ctx, sys.FetchReq{URL: keyURL, Name: "docker.asc", MaxBytes: 1 << 20})
			if err != nil {
				return fmt.Errorf("couldn't download Docker's signing key: %w", err)
			}
			if s.DryRun() {
				// Nothing was downloaded; record the write with a stand-in so the preview shows it.
				_, err = s.WriteFile(keyPath, []byte("(downloaded from "+keyURL+" when applied)\n"), sys.WriteOpts{Mode: 0o644})
				return err
			}
			b, err := s.ReadFile(p)
			if err != nil {
				return err
			}
			if !isArmoredKey(b) {
				return fmt.Errorf("%s didn't return an OpenPGP public key; try again later", keyURL)
			}
			_, err = s.WriteFile(keyPath, b, sys.WriteOpts{Mode: 0o644})
			return err
		},
	}
}

func applyDaemon(ctx context.Context, s sys.System, want map[string]string) error {
	m, old, existed, err := readDaemon(s)
	if err != nil {
		return err
	}
	if daemonOK(m, want) {
		return nil // unchanged: no restart, running containers keep running
	}
	data, err := merged(m, want)
	if err != nil {
		return err
	}
	if err := s.MkdirAll("/etc/docker", 0o755, nil); err != nil {
		return err
	}
	if _, err := s.WriteFile(daemonJSON, data, sys.WriteOpts{Mode: 0o644}); err != nil {
		return err
	}
	restore := func(cause error) error {
		if s.DryRun() {
			return cause
		}
		if existed {
			_, _ = s.WriteFile(daemonJSON, old, sys.WriteOpts{Mode: 0o644})
		} else {
			_ = s.RemoveAll(daemonJSON)
		}
		_, _ = s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"restart", "docker"}})
		return fmt.Errorf("%w; kiln restored the previous %s. Check `journalctl -u docker`", cause, daemonJSON)
	}
	if hasValidate(ctx, s) {
		if _, err := s.Run(ctx, sys.Cmd{Name: "dockerd", Args: []string{"--validate", "--config-file", daemonJSON}}); err != nil {
			return restore(fmt.Errorf("dockerd rejected the new %s: %w", daemonJSON, err))
		}
	}
	if _, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"restart", "docker"}}); err != nil {
		return restore(fmt.Errorf("docker didn't restart with the new log settings: %w", err))
	}
	if s.DryRun() {
		return nil
	}
	if !pollActive(ctx, s, "docker", 60*time.Second) {
		return restore(errors.New("docker didn't come back up with the new log settings"))
	}
	return nil
}

// hasValidate detects `dockerd --validate` at run time (fact R).
func hasValidate(ctx context.Context, s sys.System) bool {
	r, err := s.Query(ctx, sys.Cmd{Name: "dockerd", Args: []string{"--help"}})
	return err == nil && strings.Contains(r.Stdout+r.Stderr, "--validate")
}

func pollActive(ctx context.Context, s sys.System, unit string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if active(ctx, s, unit) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
}

// waitDocker polls `docker info`: docker starts asynchronously after install or restart.
func waitDocker(ctx context.Context, s sys.System, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		if _, err := s.Query(ctx, sys.Cmd{Name: "docker", Args: []string{"info"}, Timeout: 20 * time.Second}); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("docker didn't answer `docker info` within %s; check `systemctl status docker` and `journalctl -u docker`", d)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
