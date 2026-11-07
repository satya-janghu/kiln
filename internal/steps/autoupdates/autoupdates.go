// Package autoupdates implements catalog step 20 `autoupdates` (docs/steps-catalog.md, D40).
package autoupdates

import (
	"context"
	"strings"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Paths written by this step.
const (
	ConfPath       = "/etc/apt/apt.conf.d/52kiln-unattended-upgrades"
	DailyDropIn    = "/etc/systemd/system/apt-daily.timer.d/60-kiln.conf"
	UpgradeDropIn  = "/etc/systemd/system/apt-daily-upgrade.timer.d/60-kiln.conf"
	NeedrestartCfg = "/etc/needrestart/conf.d/50-kiln.conf"
)

var packages = []string{"unattended-upgrades", "apt-listchanges", "needrestart"}

const preseed = "unattended-upgrades unattended-upgrades/enable_auto_updates boolean true"

// Origins-Pattern values (D40).
const (
	patternAll      = "origin=*"
	patternSecurity = "origin=Debian,codename=${distro_codename}-security,label=Debian-Security"
)

// Step is the `autoupdates` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string       { return "autoupdates" }
func (Step) Title() string    { return "Automatic updates every 12 hours" }
func (Step) Category() string { return "security" }

// DependsOn is only apt-sources: the repository steps the catalog lists
// (docker, devtools, repos, multimedia) come earlier in catalog order, and
// `origin=*` covers whatever repositories exist, so a skipped one mustn't
// switch automatic updates off.
func (Step) DependsOn() []string { return []string{"apt-sources"} }

func (Step) Packages() []step.PackageRef {
	out := make([]step.PackageRef, 0, len(packages))
	for _, p := range packages {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "main", Repo: "debian"})
	}
	return out
}

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "scope", Label: "What updates automatically", Kind: options.Choice, Default: "all",
			Choices: []options.ChoiceItem{
				{Value: "all", Label: "Every package from every configured repository"},
				{Value: "security", Label: "Debian security updates only"},
			}},
		{Key: "schedule", Label: "Schedule", Kind: options.Choice, Default: "12h",
			Help:    "Refresh at about 06:00 and 18:00, upgrade 30 minutes later; skipped while on battery.",
			Choices: []options.ChoiceItem{{Value: "12h", Label: "Every 12 hours"}}},
		{Key: "automatic_reboot", Label: "Reboot automatically when an update needs it", Kind: options.Bool, Default: false},
	}
}

func pattern(env *step.Env) string {
	if env.Opts.String("scope") == "security" {
		return patternSecurity
	}
	return patternAll
}

func confContent(env *step.Env) []byte {
	reboot := "false"
	if env.Opts.Bool("automatic_reboot") {
		reboot = "true"
	}
	return []byte(sys.ManagedHeader("//") +
		"APT::Periodic::Update-Package-Lists \"always\";\n" +
		"APT::Periodic::Unattended-Upgrade \"always\";\n" +
		"#clear Unattended-Upgrade::Origins-Pattern;\n" +
		"Unattended-Upgrade::Origins-Pattern {\n" +
		"    \"" + pattern(env) + "\";\n" +
		"};\n" +
		"Unattended-Upgrade::Automatic-Reboot \"" + reboot + "\";\n")
}

func timerDropIn(onCalendar string) []byte {
	return []byte(sys.ManagedHeader("#") + "[Timer]\nOnCalendar=\nOnCalendar=" + onCalendar + "\nRandomizedDelaySec=15m\n")
}

var (
	dailyContent       = timerDropIn("*-*-* 06,18:00")
	upgradeContent     = timerDropIn("*-*-* 06,18:30")
	needrestartContent = []byte(sys.ManagedHeader("#") + "$nrconf{restart} = 'a';\n")
)

type file struct {
	path    string
	content []byte
}

func files(env *step.Env) []file {
	return []file{
		{ConfPath, confContent(env)},
		{DailyDropIn, dailyContent},
		{UpgradeDropIn, upgradeContent},
		{NeedrestartCfg, needrestartContent},
	}
}

// aptConfig reads the effective values from `apt-config dump`.
func aptConfig(ctx context.Context, s sys.System) (periodic map[string]string, origins []string, err error) {
	r, err := s.Query(ctx, sys.Cmd{Name: "apt-config", Args: []string{"dump"}, Env: []string{"LC_ALL=C"}})
	if err != nil {
		return nil, nil, err
	}
	periodic = map[string]string{}
	for _, l := range strings.Split(r.Stdout, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), " ")
		if !ok {
			continue
		}
		v = strings.TrimSuffix(strings.TrimSpace(v), ";")
		v = strings.Trim(v, "\"")
		switch k {
		case "APT::Periodic::Update-Package-Lists", "APT::Periodic::Unattended-Upgrade":
			periodic[k] = v
		case "Unattended-Upgrade::Origins-Pattern::":
			origins = append(origins, v)
		}
	}
	return periodic, origins, nil
}

// pausedByKiln reports a runtime mask from kiln's own run (D41): the timers
// start again in the end-of-run restore.
func pausedByKiln(s sys.System, unit string) bool {
	t, err := s.Readlink("/run/systemd/system/" + unit)
	return err == nil && t == "/dev/null"
}

func timerState(ctx context.Context, s sys.System, unit, slot string) string {
	if pausedByKiln(s, unit) {
		return ""
	}
	r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"show", unit, "-p", "ActiveState", "-p", "TimersCalendar"}})
	if !strings.Contains(r.Stdout, "ActiveState=active") {
		return unit + " isn't active"
	}
	if !strings.Contains(r.Stdout, slot) {
		return unit + " isn't scheduled at " + slot
	}
	return ""
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	sy := env.Sys
	if ok, missing := apt.AllInstalled(ctx, sy, packages); !ok {
		return step.Todo("not installed: %s", strings.Join(missing, ", ")), nil
	}
	for _, f := range files(env) {
		if b, err := sy.ReadFile(f.path); err != nil || string(b) != string(f.content) {
			return step.Todo("%s is missing or different", f.path), nil
		}
	}
	periodic, origins, err := aptConfig(ctx, sy)
	if err != nil {
		return step.Todo("apt-config dump failed: %v", err), nil
	}
	for _, k := range []string{"APT::Periodic::Update-Package-Lists", "APT::Periodic::Unattended-Upgrade"} {
		if periodic[k] != "always" {
			return step.Todo("%s is %q, not \"always\" (another apt.conf.d file overrides it)", k, periodic[k]), nil
		}
	}
	if len(origins) != 1 || origins[0] != pattern(env) {
		return step.Todo("Unattended-Upgrade::Origins-Pattern is %q, not just %q", strings.Join(origins, ", "), pattern(env)), nil
	}
	if why := timerState(ctx, sy, "apt-daily.timer", "06,18:00"); why != "" {
		return step.Todo("%s", why), nil
	}
	if why := timerState(ctx, sy, "apt-daily-upgrade.timer", "06,18:30"); why != "" {
		return step.Todo("%s", why), nil
	}
	what := "every package"
	if env.Opts.String("scope") == "security" {
		what = "Debian security updates"
	}
	return step.Done(what + " update automatically at about 06:00 and 18:00"), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	var acts []action.Action
	if ok, _ := apt.AllInstalled(ctx, env.Sys, packages); !ok {
		acts = append(acts, action.AptInstall{Pkgs: packages, Debconf: []string{preseed}})
	}
	for _, f := range files(env) {
		acts = append(acts, action.WriteFile{Path: f.path, Mode: 0o644, Content: f.content})
	}
	// daemon-reload only: the timers stay paused until the end-of-run restore (D41).
	acts = append(acts, action.Systemctl{Verb: "daemon-reload"})

	env.Notes.Add("Updates run at about 06:00 and 18:00, refresh then upgrade 30 minutes later, and skip while on battery. Logs are in /var/log/unattended-upgrades/.")
	if env.Opts.Bool("automatic_reboot") {
		env.Notes.Add("The machine reboots by itself when an update needs it (unattended-upgrades' Automatic-Reboot).")
	} else {
		env.Notes.Add("Kernel and NVIDIA driver updates need a reboot to take effect. kiln never reboots automatically; /run/reboot-required shows when one is pending.")
	}
	if env.Opts.String("scope") != "security" {
		env.Notes.Add("A Docker update restarts the Docker daemon. Containers come back only if they have a restart policy.")
		env.Notes.Add("A new NVIDIA driver branch can arrive unattended, because the branch isn't pinned (D39).")
	}
	return acts, nil
}
