// Package power implements catalog step 11 `power` (docs/steps-catalog.md):
// always-on (no sleep, lid ignored), GNOME no-suspend keys, and one power
// manager (power-profiles-daemon with GNOME, tuned without; D5).
package power

import (
	"context"
	"fmt"
	"io/fs"
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
	logindDropIn = "/etc/systemd/logind.conf.d/60-kiln-always-on.conf"
	dconfProfile = "/etc/dconf/profile/user"
	dconfKeys    = "/etc/dconf/db/local.d/60-kiln-power"
	dconfLocks   = "/etc/dconf/db/local.d/locks/60-kiln-power"
	dconfDB      = "/etc/dconf/db/local"
	greeterPath  = "/etc/gdm3/greeter.dconf-defaults"
	powerSection = "org/gnome/settings-daemon/plugins/power"
	// tunedProfileName is trixie's stock tuned profile (D52): schedutil,
	// EPP balance_performance, platform_profile balanced (tuned 2.25.1
	// /usr/lib/tuned/profiles/balanced/tuned.conf).
	tunedProfileName = "balanced"
	// oldTunedProfile is the custom profile earlier kiln versions wrote; it's
	// kiln's own file, so it's removed.
	oldTunedProfile = "/etc/tuned/profiles/kiln-performance"

	ppd      = "power-profiles-daemon"
	tuned    = "tuned"
	tunedPPD = "tuned-ppd"
)

// sleepTargets are masked so logind reports CanSuspend and friends as "na".
var sleepTargets = []string{"sleep.target", "suspend.target", "hibernate.target", "hybrid-sleep.target", "suspend-then-hibernate.target"}

// Step is the `power` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "power" }
func (Step) Title() string       { return "Always-on power settings" }
func (Step) Category() string    { return "power" }
func (Step) DependsOn() []string { return []string{"preflight"} }

func (Step) Packages() []step.PackageRef {
	return []step.PackageRef{
		{Name: ppd, Suite: "trixie", Component: "main", Repo: "debian"},
		{Name: tuned, Suite: "trixie", Component: "main", Repo: "debian"},
	}
}

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "always_on", Label: "Always on (no sleep, lid ignored)", Kind: options.Bool, Default: true,
			Help: "Masks the sleep targets and tells logind to ignore the lid and the suspend/hibernate keys."},
		{Key: "lock_gnome_settings", Label: "Lock GNOME no-suspend settings", Kind: options.Bool, Default: true,
			Help: "GNOME only: GNOME Settings can't turn automatic suspend back on."},
		{Key: "power_manager", Label: "Power manager", Kind: options.Choice, Default: "auto",
			Help: "auto = power-profiles-daemon with GNOME, tuned without it, both on their balanced profile (D52). Never both.",
			Choices: []options.ChoiceItem{
				{Value: "auto", Label: "Automatic"},
				{Value: ppd, Label: "power-profiles-daemon"},
				{Value: tuned, Label: "tuned (balanced)"},
			}},
	}
}

// Manager resolves the power-manager option for this machine.
func Manager(f *facts.Facts, choice string) string {
	switch choice {
	case ppd, tuned:
		return choice
	}
	if f.GNOME {
		return ppd
	}
	return tuned
}

// --- file contents ---

func logindContent() []byte {
	return []byte(sys.ManagedHeader("#") + `[Login]
HandleLidSwitch=ignore
HandleLidSwitchExternalPower=ignore
HandleLidSwitchDocked=ignore
HandleSuspendKey=ignore
HandleSuspendKeyLongPress=ignore
HandleHibernateKey=ignore
HandleHibernateKeyLongPress=ignore
IdleAction=ignore
`)
}

var dconfPowerKeys = []struct{ key, value string }{
	{"sleep-inactive-ac-type", "'nothing'"},
	{"sleep-inactive-battery-type", "'nothing'"},
	{"power-button-action", "'interactive'"},
}

func dconfKeysContent() []byte {
	var b strings.Builder
	b.WriteString(sys.ManagedHeader("#"))
	b.WriteString("[" + powerSection + "]\n")
	for _, k := range dconfPowerKeys {
		b.WriteString(k.key + "=" + k.value + "\n")
	}
	return []byte(b.String())
}

// dconfLocksContent lists the locked key paths. No header: kiln doesn't rely
// on comment support in dconf lock files.
func dconfLocksContent() []byte {
	var b strings.Builder
	for _, k := range dconfPowerKeys {
		b.WriteString("/" + powerSection + "/" + k.key + "\n")
	}
	return []byte(b.String())
}

// profileContent returns the dconf user profile with system-db:local present.
// Debian ships none; an existing one only gains the missing line. No header,
// for the same reason as the locks file.
func profileContent(cur []byte, exists bool) []byte {
	if !exists {
		return []byte("user-db:user\nsystem-db:local\n")
	}
	for _, l := range strings.Split(string(cur), "\n") {
		if strings.TrimSpace(l) == "system-db:local" {
			return cur
		}
	}
	s := string(cur)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return []byte(s + "system-db:local\n")
}

var greeterKeys = []struct{ key, value string }{
	{"sleep-inactive-ac-type", "'nothing'"},
	{"sleep-inactive-battery-type", "'nothing'"},
}

// editGreeter uncomments or inserts the greeter's no-suspend keys under the
// power section header of the ucf-managed greeter.dconf-defaults (key-level edit).
func editGreeter(content string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	header := "[" + powerSection + "]"
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == header {
			start = i
			break
		}
	}
	if start < 0 {
		lines = append(lines, "", header)
		start = len(lines) - 1
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	insertAt := start + 1
	var missing []string
	for _, k := range greeterKeys {
		want := k.key + "=" + k.value
		found := false
		for i := start + 1; i < end; i++ {
			t := strings.TrimSpace(lines[i])
			t = strings.TrimSpace(strings.TrimLeft(t, "#"))
			if name, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(name) == k.key {
				lines[i] = want
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		lines = append(lines[:insertAt], append(missing, lines[insertAt:]...)...)
	}
	return strings.Join(lines, "\n") + "\n"
}

// --- reads ---

func fileIs(s sys.System, path string, want []byte) bool {
	b, err := s.ReadFile(path)
	return err == nil && string(b) == string(want)
}

func query(ctx context.Context, s sys.System, name string, args ...string) string {
	r, _ := s.Query(ctx, sys.Cmd{Name: name, Args: args, Timeout: 30 * time.Second})
	return strings.TrimSpace(r.Stdout)
}

func masked(ctx context.Context, s sys.System, unit string) bool {
	return query(ctx, s, "systemctl", "show", "-p", "LoadState", "--value", unit) == "masked"
}

// newerOrSame reports that generated is at least as new as every source file.
func newerOrSame(s sys.System, generated string, sources ...string) bool {
	g, err := s.Stat(generated)
	if err != nil {
		return false
	}
	for _, src := range sources {
		if fi, err := s.Stat(src); err == nil && fi.ModTime().After(g.ModTime()) {
			return false
		}
	}
	return true
}

// ppdProfiles parses `powerprofilesctl list`: profile names are indented two
// columns ("* balanced:" marks the active one); their details are indented further.
func ppdProfiles(out string) []string {
	var p []string
	for _, l := range strings.Split(out, "\n") {
		if len(l) < 3 || (!strings.HasPrefix(l, "* ") && !strings.HasPrefix(l, "  ")) || l[2] == ' ' || l[2] == '\t' {
			continue
		}
		t := strings.TrimSpace(l[2:])
		if strings.HasSuffix(t, ":") && !strings.ContainsAny(t, " \t") {
			p = append(p, strings.TrimSuffix(t, ":"))
		}
	}
	return p
}

func tunedActive(ctx context.Context, s sys.System) string {
	out := query(ctx, s, "tuned-adm", "active")
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "Current active profile:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// --- Check ---

type part struct {
	ok     bool
	reason string
}

func todo(format string, a ...any) part { return part{reason: fmt.Sprintf(format, a...)} }

var good = part{ok: true}

func alwaysOnPart(ctx context.Context, s sys.System) part {
	for _, t := range sleepTargets {
		if !masked(ctx, s, t) {
			return todo("%s isn't masked", t)
		}
	}
	if !fileIs(s, logindDropIn, logindContent()) {
		return todo("%s is missing or different", logindDropIn)
	}
	return good
}

func gnomePart(env *step.Env) part {
	s := env.Sys
	cur, err := s.ReadFile(dconfProfile)
	if string(profileContent(cur, err == nil)) != string(cur) {
		return todo("%s doesn't include system-db:local", dconfProfile)
	}
	if !fileIs(s, dconfKeys, dconfKeysContent()) {
		return todo("%s is missing or different", dconfKeys)
	}
	if env.Opts.Bool("lock_gnome_settings") && !fileIs(s, dconfLocks, dconfLocksContent()) {
		return todo("%s is missing or different", dconfLocks)
	}
	if !env.Opts.Bool("lock_gnome_settings") {
		if _, err := s.Stat(dconfLocks); err == nil {
			return todo("%s is still present", dconfLocks)
		}
	}
	if !newerOrSame(s, dconfDB, dconfKeys, dconfLocks) {
		return todo("the dconf database wasn't rebuilt (dconf update)")
	}
	if g, err := s.ReadFile(greeterPath); err == nil {
		if editGreeter(string(g)) != strings.TrimRight(string(g), "\n")+"\n" {
			return todo("the login screen's no-suspend keys aren't set in %s", greeterPath)
		}
	}
	return good
}

func managerPart(ctx context.Context, env *step.Env) part {
	s := env.Sys
	switch Manager(env.Facts, env.Opts.String("power_manager")) {
	case ppd:
		if !apt.Installed(ctx, s, ppd) {
			return todo("power-profiles-daemon isn't installed")
		}
		if apt.Installed(ctx, s, tuned) {
			return todo("tuned is installed alongside power-profiles-daemon")
		}
		if _, err := s.Lstat(oldTunedProfile); err == nil {
			return todo("kiln's old kiln-performance profile is still there")
		}
		if cur := query(ctx, s, "powerprofilesctl", "get"); cur != "balanced" {
			return todo("the power profile is %q, not balanced", cur)
		}
		return good
	default:
		if !apt.Installed(ctx, s, tuned) {
			return todo("tuned isn't installed")
		}
		if apt.Installed(ctx, s, ppd) {
			return todo("power-profiles-daemon is installed alongside tuned")
		}
		if _, err := s.Lstat(oldTunedProfile); err == nil {
			return todo("kiln's old kiln-performance profile is still there")
		}
		if a := tunedActive(ctx, s); a != tunedProfileName {
			return todo("the tuned profile is %q, not %s", a, tunedProfileName)
		}
		return good
	}
}

func (Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	if env.Opts.Bool("always_on") {
		if p := alwaysOnPart(ctx, env.Sys); !p.ok {
			return step.Todo("%s", p.reason), nil
		}
		if env.Facts.GNOME {
			if p := gnomePart(env); !p.ok {
				return step.Todo("%s", p.reason), nil
			}
		}
	}
	if p := managerPart(ctx, env); !p.ok {
		return step.Todo("%s", p.reason), nil
	}
	return step.Done("always on; power manager " + Manager(env.Facts, env.Opts.String("power_manager"))), nil
}

// --- Plan ---

func (Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	var acts []action.Action
	if env.Opts.Bool("always_on") {
		if p := alwaysOnPart(ctx, env.Sys); !p.ok {
			acts = append(acts,
				action.Systemctl{Verb: "mask", Units: sleepTargets},
				action.WriteFile{Path: logindDropIn, Content: logindContent(), Mode: 0o644},
				// logind is Type=notify-reload: SIGHUP re-reads the config; the lid handler reads it at event time.
				action.Systemctl{Verb: "reload", Units: []string{"systemd-logind.service"}},
			)
		}
		if env.Facts.GNOME {
			if p := gnomePart(env); !p.ok {
				acts = append(acts, gnomeActions(env)...)
			}
		}
	}
	if p := managerPart(ctx, env); !p.ok {
		acts = append(acts, managerActions(ctx, env)...)
	}
	return acts, nil
}

func gnomeActions(env *step.Env) []action.Action {
	s := env.Sys
	cur, err := s.ReadFile(dconfProfile)
	acts := []action.Action{
		action.WriteFile{Path: dconfProfile, Content: profileContent(cur, err == nil), Mode: 0o644},
		action.WriteFile{Path: dconfKeys, Content: dconfKeysContent(), Mode: 0o644},
	}
	if env.Opts.Bool("lock_gnome_settings") {
		acts = append(acts, action.WriteFile{Path: dconfLocks, Content: dconfLocksContent(), Mode: 0o644})
	} else if _, err := s.Stat(dconfLocks); err == nil {
		acts = append(acts, action.RemoveFile{Path: dconfLocks})
	}
	acts = append(acts, action.Run{Cmd: sys.Cmd{Name: "dconf", Args: []string{"update"}}})
	if g, err := s.ReadFile(greeterPath); err == nil {
		mode := fs.FileMode(0o644)
		if fi, serr := s.Stat(greeterPath); serr == nil {
			mode = fi.Mode().Perm()
		}
		acts = append(acts,
			action.WriteFile{Path: greeterPath, Content: []byte(editGreeter(string(g))), Mode: mode},
			// ExecReload runs /usr/share/gdm/generate-config, then SIGHUP; sessions survive.
			action.Systemctl{Verb: "reload", Units: []string{"gdm3.service"}},
		)
	}
	return acts
}

func managerActions(ctx context.Context, env *step.Env) []action.Action {
	s := env.Sys
	var acts []action.Action
	mgr := Manager(env.Facts, env.Opts.String("power_manager"))
	switch mgr {
	case ppd:
		if apt.Installed(ctx, s, tuned) || apt.Installed(ctx, s, tunedPPD) {
			acts = append(acts,
				action.Systemctl{Verb: "disable", Now: true, Units: []string{"tuned.service"}},
				action.AptRemove{Pkgs: []string{tunedPPD, tuned}},
			)
		}
		if !apt.Installed(ctx, s, ppd) {
			acts = append(acts, action.AptInstall{Pkgs: []string{ppd}})
		}
		// Installed but stopped or disabled is repaired too (setPPD then waits).
		acts = append(acts, action.Systemctl{Verb: "enable", Now: true, Units: []string{"power-profiles-daemon.service"}})
		acts = append(acts, removeOldProfile())
		acts = append(acts, action.Custom{
			Desc: "set the power profile with `powerprofilesctl set balanced` (the choice persists in /var/lib/power-profiles-daemon/state.ini)",
			K:    action.Services,
			Fn:   func(ctx context.Context, s sys.System) error { return setPPD(ctx, env, s) },
		})
	default:
		if apt.Installed(ctx, s, ppd) {
			if env.Facts.GNOME {
				env.Notes.Warn("Switching this GNOME machine from power-profiles-daemon to tuned removes power-profiles-daemon; GNOME's power mode menu goes away.")
			}
			acts = append(acts,
				action.Systemctl{Verb: "disable", Now: true, Units: []string{"power-profiles-daemon.service"}},
				action.AptRemove{Pkgs: []string{ppd}},
			)
		}
		if !apt.Installed(ctx, s, tuned) {
			acts = append(acts, action.AptInstall{Pkgs: []string{tuned}})
		}
		acts = append(acts,
			// Installed but stopped or disabled is repaired too.
			action.Systemctl{Verb: "enable", Now: true, Units: []string{"tuned.service"}},
			removeOldProfile(),
			action.Custom{
				Desc: "wait for tuned to be running, then select trixie's balanced profile (tuned-adm profile " + tunedProfileName + ")",
				K:    action.Services,
				Fn: func(ctx context.Context, s sys.System) error {
					if !s.DryRun() {
						if err := waitActive(ctx, s, "tuned.service", 30*time.Second); err != nil {
							return err
						}
					}
					_, err := s.Run(ctx, sys.Cmd{Name: "tuned-adm", Args: []string{"profile", tunedProfileName}})
					return err
				},
			})
	}
	acts = append(acts, action.Custom{
		Desc: "record the power manager (" + mgr + ") in /etc/kiln/state.json",
		Fn: func(_ context.Context, s sys.System) error {
			if !s.DryRun() {
				env.State.PowerManager = mgr
			}
			return nil
		},
	})
	return acts
}

// removeOldProfile removes the custom tuned profile earlier kiln versions
// wrote (kiln's own file), whichever power manager is now in use.
func removeOldProfile() action.Action {
	return action.Custom{
		Desc: "remove kiln's old " + oldTunedProfile + " profile if it's there",
		K:    action.Files,
		Fn: func(_ context.Context, s sys.System) error {
			if _, err := s.Lstat(oldTunedProfile); err != nil {
				return nil
			}
			return s.RemoveAll(oldTunedProfile)
		},
	}
}

func waitActive(ctx context.Context, s sys.System, unit string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for query(ctx, s, "systemctl", "is-active", unit) != "active" {
		if time.Now().After(deadline) {
			return fmt.Errorf("%s didn't start within %s; see `journalctl -u %s`", unit, d, unit)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil
}

func setPPD(ctx context.Context, env *step.Env, s sys.System) error {
	if s.DryRun() {
		_, err := s.Run(ctx, sys.Cmd{Name: "powerprofilesctl", Args: []string{"set", "balanced"}})
		return err
	}
	// "started" isn't "ready": poll until the daemon answers.
	deadline := time.Now().Add(30 * time.Second)
	var list []string
	for {
		list = ppdProfiles(query(ctx, s, "powerprofilesctl", "list"))
		if len(list) > 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("power-profiles-daemon didn't answer within 30s; see `journalctl -u power-profiles-daemon`")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	want := "balanced" // always offered (ppd 0.30)
	if query(ctx, s, "powerprofilesctl", "get") == want {
		return nil
	}
	_, err := s.Run(ctx, sys.Cmd{Name: "powerprofilesctl", Args: []string{"set", want}})
	return err
}
