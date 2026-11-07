// Package fail2ban implements catalog step 16 `fail2ban` (docs/steps-catalog.md, D9).
package fail2ban

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/revert"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/steps/ssh"
	"github.com/satya-janghu/kiln/internal/sys"
)

// JailPath is kiln's jail file; jail.d/*.local is read last (fail2ban 1.1.0).
const JailPath = "/etc/fail2ban/jail.d/50-kiln.local"

// pingDeadline is how long fail2ban may take to answer after a restart (D41).
const pingDeadline = 30 * time.Second

var packages = []string{"fail2ban", "python3-systemd"}

// Step is the `fail2ban` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "fail2ban" }
func (Step) Title() string       { return "Block SSH password guessing (fail2ban)" }
func (Step) Category() string    { return "security" }
func (Step) DependsOn() []string { return []string{"ssh"} }

func (Step) Packages() []step.PackageRef {
	out := make([]step.PackageRef, 0, len(packages))
	for _, p := range packages {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "main", Repo: "debian"})
	}
	return out
}

func (Step) Applies(env *step.Env) step.Applicability {
	if !ssh.WillHaveServer(env) {
		return step.No("no SSH server installed, and the SSH step is off")
	}
	return step.Yes()
}

var banTime = regexp.MustCompile(`^[0-9]+[smhdw]?$`)

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "bantime", Label: "Ban time", Kind: options.String, Default: "1h",
			Help: "e.g. 10m, 1h, 1d.",
			Validate: func(v any) error {
				if s, _ := v.(string); !banTime.MatchString(s) {
					return errors.New("use a number with s, m, h, d or w, such as 1h")
				}
				return nil
			}},
		{Key: "bantime_increment", Label: "Longer bans for repeat offenders", Kind: options.Bool, Default: true},
		{Key: "maxretry", Label: "Failed logins before a ban", Kind: options.Int, Default: 5,
			Validate: func(v any) error {
				if n, _ := v.(int); n < 1 || n > 100 {
					return errors.New("use 1 to 100")
				}
				return nil
			}},
	}
}

func jailContent(ctx context.Context, env *step.Env) []byte {
	ignore := append([]string{"127.0.0.1/8", "::1"}, env.Facts.LANStrings()...)
	var ports []string
	for _, p := range ssh.ListenPorts(ctx, env) {
		ports = append(ports, strconv.Itoa(p))
	}
	var b strings.Builder
	b.WriteString(sys.ManagedHeader("#"))
	b.WriteString("[DEFAULT]\n")
	b.WriteString("ignoreip = " + strings.Join(ignore, " ") + "\n")
	b.WriteString("bantime = " + env.Opts.String("bantime") + "\n")
	b.WriteString("bantime.increment = " + strconv.FormatBool(env.Opts.Bool("bantime_increment")) + "\n")
	b.WriteString("maxretry = " + strconv.Itoa(env.Opts.Int("maxretry")) + "\n")
	b.WriteString("\n[sshd]\n")
	b.WriteString("enabled = true\n")
	b.WriteString("backend = systemd\n")
	b.WriteString("port = " + strings.Join(ports, ",") + "\n")
	// trixie's jail.d/defaults-debian.conf sets banaction_allports =
	// nftables[type=allports]; nftables is priority "important", so present.
	b.WriteString("banaction = %(banaction_allports)s\n")
	b.WriteString("journalmatch = _SYSTEMD_UNIT=ssh.service + _COMM=sshd + _COMM=sshd-session\n")
	return []byte(b.String())
}

// running checks the server and the sshd jail; read-only.
func running(ctx context.Context, s sys.System) error {
	r, err := s.Query(ctx, sys.Cmd{Name: "fail2ban-client", Args: []string{"ping"}, Timeout: 20 * time.Second})
	if err != nil || !strings.Contains(r.Stdout, "pong") {
		return errors.New("fail2ban doesn't answer (fail2ban-client ping)")
	}
	if _, err := s.Query(ctx, sys.Cmd{Name: "fail2ban-client", Args: []string{"status", "sshd"}, Timeout: 20 * time.Second}); err != nil {
		return fmt.Errorf("the sshd jail isn't running (%w); see journalctl -u fail2ban", err)
	}
	return nil
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	sy := env.Sys
	if ok, missing := apt.AllInstalled(ctx, sy, packages); !ok {
		return step.Todo("not installed: %s", strings.Join(missing, ", ")), nil
	}
	if b, err := sy.ReadFile(JailPath); err != nil || string(b) != string(jailContent(ctx, env)) {
		return step.Todo("%s is missing or different", JailPath), nil
	}
	if r, _ := sy.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-active", "fail2ban"}}); strings.TrimSpace(r.Stdout) != "active" {
		return step.Todo("fail2ban isn't active"), nil
	}
	if err := running(ctx, sy); err != nil {
		return step.Todo("%s", err.Error()), nil
	}
	return step.Done("the sshd jail bans on all ports after " + strconv.Itoa(env.Opts.Int("maxretry")) + " failures"), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	var acts []action.Action
	if ok, _ := apt.AllInstalled(ctx, env.Sys, packages); !ok {
		acts = append(acts, action.AptInstall{Pkgs: packages})
	}
	acts = append(acts,
		action.WriteFile{Path: JailPath, Mode: 0o644, Content: jailContent(ctx, env)},
		action.Systemctl{Verb: "restart", Units: []string{"fail2ban"}},
		action.Custom{
			Desc: fmt.Sprintf("wait up to %s for fail2ban to answer (fail2ban-client ping), then check the sshd jail (fail2ban-client status sshd)", pingDeadline),
			K:    action.Services,
			Fn: func(ctx context.Context, s sys.System) error {
				if s.DryRun() {
					return nil
				}
				if err := revert.Poll(ctx, pingDeadline, 2*time.Second, func() error { return running(ctx, s) }); err != nil {
					return fmt.Errorf("fail2ban didn't start with kiln's jail: %w. Check `journalctl -u fail2ban`", err)
				}
				return nil
			},
		})
	return acts, nil
}
