// Package firewall implements catalog step 14 `firewall` (docs/steps-catalog.md).
// It is risky: /etc/firewalld is snapshotted first, the permanent config is
// checked and reloaded, the result is polled, and anything that fails is put
// back (architecture: "Risky changes").
package firewall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"regexp"
	"slices"
	"sort"
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

// Paths (firewalld 2.3.1).
const (
	ConfigDir = "/etc/firewalld"
	ZonesDir  = "/etc/firewalld/zones"
	LANZone   = "/etc/firewalld/zones/kiln-lan.xml"
	Conf      = "/etc/firewalld/firewalld.conf"
	zoneName  = "kiln-lan"
)

// stateDeadline is how long firewalld may take to report "running" (D41).
const stateDeadline = 30 * time.Second

// Step is the `firewall` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "firewall" }
func (Step) Title() string       { return "Firewall with LAN-only services" }
func (Step) Category() string    { return "security" }
func (Step) DependsOn() []string { return []string{"preflight"} }

func (Step) Packages() []step.PackageRef {
	return []step.PackageRef{{Name: "firewalld", Suite: "trixie", Component: "main", Repo: "debian"}}
}

var portRe = regexp.MustCompile(`^([0-9]{1,5})(-([0-9]{1,5}))?/(tcp|udp)$`)

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "backend", Label: "Firewall", Kind: options.Choice, Default: "firewalld",
			Choices: []options.ChoiceItem{
				{Value: "firewalld", Label: "firewalld (works with Docker and Cockpit)"},
				{Value: "nftables", Label: "nftables (kiln's own table inet kiln)",
					DisabledReason: "not available yet: the steps catalog doesn't define the nftables rules"},
			}},
		{Key: "lan_subnets", Label: "LAN subnets", Kind: options.List, Default: []string{"auto"},
			Help: "auto = private IPv4 (RFC 1918) and IPv6 ULA subnets on physical network cards, or a list of CIDRs.",
			Validate: func(v any) error {
				l, _ := v.([]string)
				for _, x := range l {
					if x == "auto" {
						continue
					}
					p, err := netip.ParsePrefix(x)
					if err != nil {
						return fmt.Errorf("%q isn't a subnet like 192.168.1.0/24", x)
					}
					if p.Addr().Is6() && !netip.MustParsePrefix("fc00::/7").Contains(p.Addr()) {
						return fmt.Errorf("%q is a global IPv6 prefix; only ULA (fc00::/7) IPv6 subnets count as LAN", x)
					}
				}
				return nil
			}},
		{Key: "extra_ports", Label: "Extra open ports", Kind: options.List, Default: []string{},
			Help: "Open to the internet, e.g. 8080/tcp.",
			Validate: func(v any) error {
				l, _ := v.([]string)
				for _, x := range l {
					m := portRe.FindStringSubmatch(x)
					if m == nil {
						return fmt.Errorf("%q isn't a port like 8080/tcp or 6000-6010/udp", x)
					}
					for _, n := range []string{m[1], m[3]} {
						if n == "" {
							continue
						}
						if p, _ := strconv.Atoi(n); p < 1 || p > 65535 {
							return fmt.Errorf("%q: ports go from 1 to 65535", x)
						}
					}
				}
				return nil
			}},
		{Key: "strict_docker_ports", Label: "Block Docker published ports", Kind: options.Bool, Default: false,
			Help: "firewalld StrictForwardPorts=yes: ports published with docker -p then follow the zone rules."},
	}
}

// LANSubnets returns the LAN subnets (option list, or auto-detected; D20). Used by fail2ban too.
func LANSubnets(env *step.Env) []string {
	var out []string
	l, _ := env.All.Values["firewall.lan_subnets"].([]string)
	for _, x := range l {
		if x == "auto" {
			continue
		}
		if p, err := netip.ParsePrefix(x); err == nil {
			out = append(out, p.Masked().String())
		}
	}
	if len(out) == 0 {
		out = env.Facts.LANStrings()
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// want is the desired firewalld state.
type want struct {
	lan         []string
	lanServices []string
	lanPorts    []string // "<n>/tcp": the SSH ports other than 22
	ssh         bool     // SSH server detected
	sshPorts    []int    // SSH ports other than 22 (22 is the ssh service)
	extra       []string
	strict      bool
}

// sshPorts are the ports sshd uses now (its effective configuration, so a
// custom port from an earlier run stays open even when the ssh step is
// unticked) plus the port the ssh step is about to configure. An old port is
// kept open here; the ssh step closes it through closeOldSSHPorts only after
// the new port has been verified and committed.
func sshPorts(ctx context.Context, env *step.Env) []int {
	if !env.Facts.SSHServer {
		return nil
	}
	ports := ssh.CurrentPorts(ctx, env.Sys)
	if env.All.IsEnabled("ssh") {
		ports = append(ports, ssh.Port(env))
	}
	var out []int
	for _, p := range ports {
		if p != 22 {
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return slices.Compact(out)
}

func desired(ctx context.Context, env *step.Env) want {
	f := env.Facts
	w := want{lan: LANSubnets(env), ssh: f.SSHServer, sshPorts: sshPorts(ctx, env), extra: env.Opts.List("extra_ports"), strict: env.Opts.Bool("strict_docker_ports")}
	w.lanServices = []string{"cockpit"}
	if f.GNOME {
		w.lanServices = append(w.lanServices, "rdp")
	}
	if f.SSHServer {
		w.lanServices = append(w.lanServices, "ssh")
	}
	for _, p := range w.sshPorts {
		w.lanPorts = append(w.lanPorts, strconv.Itoa(p)+"/tcp")
	}
	return w
}

func (w want) publicPorts() []string {
	out := slices.Clone(w.lanPorts)
	for _, p := range w.extra {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func zoneXML(w want) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString("<!-- " + strings.TrimSpace(sys.ManagedHeader("")) + " -->\n")
	b.WriteString("<zone>\n  <short>kiln LAN</short>\n")
	b.WriteString("  <description>Private subnets on this machine's network cards. Cockpit, remote desktop and SSH are reachable only from here.</description>\n")
	for _, s := range w.lan {
		b.WriteString(`  <source address="` + s + `"/>` + "\n")
	}
	for _, s := range w.lanServices {
		b.WriteString(`  <service name="` + s + `"/>` + "\n")
	}
	for _, p := range w.lanPorts {
		n, proto, _ := strings.Cut(p, "/")
		b.WriteString(`  <port port="` + n + `" protocol="` + proto + `"/>` + "\n")
	}
	b.WriteString("</zone>\n")
	return []byte(b.String())
}

// strictValue returns the effective StrictForwardPorts value ("no" when unset).
func strictValue(content string) string {
	v := "no"
	for _, l := range strings.Split(content, "\n") {
		if k, val, ok := strings.Cut(strings.TrimSpace(l), "="); ok && strings.TrimSpace(k) == "StrictForwardPorts" {
			v = strings.ToLower(strings.TrimSpace(val))
		}
	}
	return v
}

// setStrict is a key-level edit of firewalld.conf.
func setStrict(content, value string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	done := false
	for i, l := range lines {
		if k, _, ok := strings.Cut(strings.TrimSpace(l), "="); ok && strings.TrimSpace(k) == "StrictForwardPorts" {
			lines[i] = "StrictForwardPorts=" + value
			done = true
		}
	}
	if !done {
		lines = append(lines, "StrictForwardPorts="+value)
	}
	return strings.Join(lines, "\n") + "\n"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func fwq(ctx context.Context, s sys.System, args ...string) (string, error) {
	r, err := s.Query(ctx, sys.Cmd{Name: "firewall-cmd", Args: args, Timeout: time.Minute})
	return strings.TrimSpace(r.Stdout), err
}

// publicChanges returns the firewall-cmd commands the permanent public zone
// still needs. A query that fails (firewalld not running yet) counts as missing.
func publicChanges(ctx context.Context, s sys.System, w want) [][]string {
	var out [][]string
	if d, err := fwq(ctx, s, "--get-default-zone"); err != nil || d != "public" {
		out = append(out, []string{"--set-default-zone=public"})
	}
	if w.ssh {
		if _, err := fwq(ctx, s, "--permanent", "--zone=public", "--query-service=ssh"); err != nil {
			out = append(out, []string{"--permanent", "--zone=public", "--add-service=ssh"})
		}
	}
	for _, p := range w.publicPorts() {
		if _, err := fwq(ctx, s, "--permanent", "--zone=public", "--query-port="+p); err != nil {
			out = append(out, []string{"--permanent", "--zone=public", "--add-port=" + p})
		}
	}
	return out
}

func activeZoneSources(ctx context.Context, s sys.System) map[string][]string {
	out := map[string][]string{}
	r, err := fwq(ctx, s, "--get-active-zones")
	if err != nil {
		return out
	}
	zone := ""
	for _, l := range strings.Split(r, "\n") {
		if l == "" {
			continue
		}
		if l[0] != ' ' && l[0] != '\t' {
			zone = strings.Fields(l)[0]
			out[zone] = nil
			continue
		}
		if k, v, ok := strings.Cut(strings.TrimSpace(l), ":"); ok && k == "sources" && zone != "" {
			out[zone] = append(out[zone], strings.Fields(v)...)
		}
	}
	return out
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

func fields(ctx context.Context, s sys.System, args ...string) ([]string, error) {
	r, err := fwq(ctx, s, args...)
	return strings.Fields(r), err
}

// verify checks the running firewall against w (catalog step 14.5); read-only.
func verify(ctx context.Context, s sys.System, w want) error {
	if st, _ := fwq(ctx, s, "--state"); st != "running" {
		return errors.New("firewalld isn't running")
	}
	if d, _ := fwq(ctx, s, "--get-default-zone"); d != "public" {
		return fmt.Errorf("the default zone is %q, not public", d)
	}
	svcs, err := fields(ctx, s, "--zone=public", "--list-services")
	if err != nil {
		return fmt.Errorf("couldn't list the public zone: %w", err)
	}
	if w.ssh && !slices.Contains(svcs, "ssh") {
		return errors.New("the public zone doesn't allow ssh")
	}
	ports, _ := fields(ctx, s, "--zone=public", "--list-ports")
	for _, p := range w.publicPorts() {
		if !slices.Contains(ports, p) {
			return fmt.Errorf("port %s isn't open in the public zone", p)
		}
	}
	active := activeZoneSources(ctx, s)
	if len(w.lan) == 0 {
		if _, ok := active[zoneName]; ok {
			return errors.New("the kiln-lan zone is still active, but no LAN was found")
		}
		return nil
	}
	if !sameSet(active[zoneName], w.lan) {
		return fmt.Errorf("the kiln-lan zone is bound to %v, expected %v", active[zoneName], w.lan)
	}
	lsvc, _ := fields(ctx, s, "--zone="+zoneName, "--list-services")
	if !sameSet(lsvc, w.lanServices) {
		return fmt.Errorf("the kiln-lan zone allows %v, expected %v", lsvc, w.lanServices)
	}
	lports, _ := fields(ctx, s, "--zone="+zoneName, "--list-ports")
	if !sameSet(lports, w.lanPorts) {
		return fmt.Errorf("the kiln-lan zone opens ports %v, expected %v", lports, w.lanPorts)
	}
	return nil
}

func filesDiffer(s sys.System, w want) []string {
	var diff []string
	if len(w.lan) > 0 {
		if b, err := s.ReadFile(LANZone); err != nil || string(b) != string(zoneXML(w)) {
			diff = append(diff, LANZone)
		}
	} else if _, err := s.Lstat(LANZone); err == nil {
		diff = append(diff, LANZone+" (no LAN found)")
	}
	if b, err := s.ReadFile(Conf); err == nil && strictValue(string(b)) != yesNo(w.strict) {
		diff = append(diff, "StrictForwardPorts in "+Conf)
	}
	return diff
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	sy := env.Sys
	if !apt.Installed(ctx, sy, "firewalld") {
		return step.Todo("firewalld isn't installed"), nil
	}
	if r, _ := sy.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-active", "firewalld"}}); strings.TrimSpace(r.Stdout) != "active" {
		return step.Todo("firewalld isn't active"), nil
	}
	w := desired(ctx, env)
	if d := filesDiffer(sy, w); len(d) > 0 {
		return step.Todo("%s differs", strings.Join(d, ", ")), nil
	}
	if ch := publicChanges(ctx, sy, w); len(ch) > 0 {
		return step.Todo("the public zone needs: firewall-cmd %s", strings.Join(ch[0], " ")), nil
	}
	if err := verify(ctx, sy, w); err != nil {
		return step.Todo("%s", err.Error()), nil
	}
	if len(w.lan) == 0 {
		return step.Done("firewalld: public zone only (no LAN found)"), nil
	}
	return step.Done("firewalld: " + strings.Join(w.lanServices, ", ") + " from " + strings.Join(w.lan, ", ")), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	if env.Opts.String("backend") != "firewalld" {
		return nil, errors.New("only the firewalld backend is available; choose firewalld in the firewall options")
	}
	sy := env.Sys
	w := desired(ctx, env)
	had := apt.Installed(ctx, sy, "firewalld")
	active := had && isActive(ctx, sy)
	changed := !active || len(filesDiffer(sy, w)) > 0 || len(publicChanges(ctx, sy, w)) > 0

	if len(w.extra) > 0 {
		env.Notes.Warn("These ports will be open to the internet: %s.", strings.Join(w.extra, ", "))
	}
	if len(w.lan) == 0 {
		env.Notes.Add("No LAN subnet was found, so Cockpit and remote desktop stay closed. Reach Cockpit locally at https://localhost:9090, or through an SSH tunnel: ssh -L 9090:localhost:9090 %s@<this machine>.", env.User().Name)
	}
	env.Notes.Add("Ports published by Docker containers (-p) bypass the firewall zones unless \"block Docker published ports\" is on: bind private services to 127.0.0.1 or a LAN IP.")

	var acts []action.Action
	// 1. Snapshot and record pending. If kiln installs firewalld, the undo is
	// to switch it off again rather than restore an /etc/firewalld that didn't
	// exist; if it was installed but not running, the undo puts the files back
	// and switches it off again. Otherwise reload-or-restart applies the restored
	// files whether or not firewalld is still running, so the undo (and with it
	// the in-progress record) can always finish.
	paths, restore := []string{ConfigDir}, [][]string{undoRestart}
	desc := "back up " + ConfigDir + " and record the firewall change as in progress"
	switch {
	case !had:
		paths, restore = nil, [][]string{{"systemctl", "disable", "--now", "firewalld"}}
		desc = "record the firewall change as in progress (undo: systemctl disable --now firewalld)"
	case !active:
		restore = [][]string{{"systemctl", "disable", "--now", "firewalld"}}
		desc += " (undo: put the files back and systemctl disable --now firewalld, as it was)"
	}
	acts = append(acts, action.Custom{Desc: desc, K: action.Firewall,
		Fn: func(ctx context.Context, s sys.System) error {
			return revert.Begin(ctx, s, env.State, "firewall", "firewall settings", paths, restore)
		}})
	// 2. Install (starts at once with zone public, which allows ssh), or start
	// an installed firewalld that isn't running (e.g. switched off by an
	// earlier revert), so the zones below can be applied and checked.
	if !had {
		acts = append(acts, action.AptInstall{Pkgs: []string{"firewalld"}})
	} else if !active {
		acts = append(acts, action.Systemctl{Verb: "enable", Units: []string{"firewalld"}, Now: true})
	}
	// 4. Permanent config as managed files.
	if len(w.lan) > 0 {
		acts = append(acts,
			action.Mkdir{Path: ZonesDir, Mode: 0o750},
			action.WriteFile{Path: LANZone, Mode: 0o644, Content: zoneXML(w)})
	} else if _, err := sy.Lstat(LANZone); err == nil {
		acts = append(acts, action.RemoveFile{Path: LANZone})
	}
	acts = append(acts, action.Custom{
		Desc: fmt.Sprintf("set StrictForwardPorts=%s in %s (key-level edit), if it differs", yesNo(w.strict), Conf),
		K:    action.Firewall,
		Fn: func(_ context.Context, s sys.System) error {
			b, err := s.ReadFile(Conf)
			if err != nil && !s.DryRun() {
				return fmt.Errorf("%s is missing (%w); reinstall firewalld (apt-get install --reinstall firewalld)", Conf, err)
			}
			if err != nil && !w.strict {
				return nil // preview before firewalld is installed: its default is already "no"
			}
			if err == nil && strictValue(string(b)) == yesNo(w.strict) {
				return nil
			}
			mode := fs.FileMode(0o644)
			if fi, serr := s.Stat(Conf); serr == nil {
				mode = fi.Mode().Perm()
			}
			_, err = s.WriteFile(Conf, []byte(setStrict(string(b), yesNo(w.strict))), sys.WriteOpts{Mode: mode})
			return err
		},
	})
	pubDesc := "make sure the permanent public zone is the default"
	if w.ssh {
		pubDesc += " and allows ssh"
	}
	if pp := w.publicPorts(); len(pp) > 0 {
		pubDesc += ", and open " + strings.Join(pp, " ")
	}
	acts = append(acts, action.Custom{Desc: pubDesc + " (firewall-cmd --permanent --zone=public …)", K: action.Firewall,
		Fn: func(ctx context.Context, s sys.System) error {
			for _, args := range publicChanges(ctx, s, w) {
				if _, err := s.Run(ctx, sys.Cmd{Name: "firewall-cmd", Args: args, Timeout: time.Minute}); err != nil {
					return fmt.Errorf("firewall-cmd %s failed: %w", strings.Join(args, " "), err)
				}
			}
			return nil
		}})
	// 5. Check the permanent config, reload, then poll the result.
	acts = append(acts, action.Custom{
		Desc: fmt.Sprintf("check the permanent config (firewall-cmd --check-config), reload it, then wait up to %s for firewalld to run with the expected zones", stateDeadline),
		K:    action.Firewall,
		Fn: func(ctx context.Context, s sys.System) error {
			if !s.DryRun() {
				if _, err := s.Query(ctx, sys.Cmd{Name: "firewall-cmd", Args: []string{"--check-config"}, Timeout: time.Minute}); err != nil {
					return fmt.Errorf("the firewall config didn't pass firewall-cmd --check-config: %w", err)
				}
			}
			if _, err := s.Run(ctx, sys.Cmd{Name: "firewall-cmd", Args: []string{"--reload"}, Timeout: 2 * time.Minute}); err != nil {
				return fmt.Errorf("firewall-cmd --reload failed: %w", err)
			}
			if s.DryRun() {
				return nil
			}
			if err := revert.Poll(ctx, stateDeadline, 2*time.Second, func() error { return verify(ctx, s, w) }); err != nil {
				return fmt.Errorf("the firewall didn't come up as expected: %w", err)
			}
			return nil
		},
	})
	// 6. Success: clear pending, and let Docker recreate its zone if this run changed the config.
	commitDesc := "clear the in-progress record"
	if changed {
		commitDesc += ", then restart Docker if it's running so it recreates its firewalld zone (this restarts containers)"
	}
	acts = append(acts, action.Custom{Desc: commitDesc, K: action.Firewall,
		Fn: func(ctx context.Context, s sys.System) error {
			if err := revert.Commit(s, env.State); err != nil {
				return err
			}
			if !changed {
				return nil
			}
			return restartDocker(ctx, s)
		}})
	return acts, nil
}

// undoRestart applies restored firewalld files: a reload while firewalld runs,
// a start when it doesn't (systemctl(1) reload-or-restart), so an undo can
// finish (and clear the in-progress record) even if firewalld stopped.
var undoRestart = []string{"systemctl", "reload-or-restart", "firewalld"}

func isActive(ctx context.Context, s sys.System) bool {
	r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-active", "firewalld"}})
	return strings.TrimSpace(r.Stdout) == "active"
}

// restartDocker lets a running Docker recreate its firewalld zone after this
// run changed the firewall config (catalog step 14.6).
func restartDocker(ctx context.Context, s sys.System) error {
	if r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-active", "docker"}}); strings.TrimSpace(r.Stdout) == "active" {
		if _, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"restart", "docker"}}); err != nil {
			return fmt.Errorf("the firewall is set up, but restarting Docker failed (%w); run `systemctl restart docker`", err)
		}
	}
	return nil
}

func init() { ssh.FirewallSync = closeOldSSHPorts }

// closeOldSSHPorts runs from the ssh step once a port change is verified and
// committed: it closes, in kiln-lan and public, the SSH ports sshd used before
// the change (before) but no longer uses, unless they are extra open ports.
// It is snapshotted, checked and reverted like the firewall step itself.
func closeOldSSHPorts(ctx context.Context, s sys.System, env *step.Env, before []int) error {
	if s.DryRun() || !env.All.IsEnabled("firewall") || !isActive(ctx, s) {
		return nil
	}
	w := desired(ctx, env) // sshd now reports only the ports it uses after the change
	open := w.publicPorts()
	var stale []string
	for _, p := range before {
		if ps := strconv.Itoa(p) + "/tcp"; p != 22 && !slices.Contains(open, ps) {
			if _, err := fwq(ctx, s, "--permanent", "--zone=public", "--query-port="+ps); err == nil {
				stale = append(stale, ps)
			}
		}
	}
	zoneChanged := false
	if len(w.lan) > 0 {
		b, err := s.ReadFile(LANZone)
		zoneChanged = err == nil && string(b) != string(zoneXML(w))
	}
	if len(stale) == 0 && !zoneChanged {
		return nil
	}
	if err := revert.Begin(ctx, s, env.State, "firewall", "closing the old SSH port in the firewall", []string{ConfigDir}, [][]string{undoRestart}); err != nil {
		return err
	}
	undo := func(cause error) error {
		if uerr := revert.Undo(ctx, s, env.State); uerr != nil {
			return fmt.Errorf("%w; putting the firewall back also failed: %w", cause, uerr)
		}
		return fmt.Errorf("%w (the firewall was put back, so the old port is still open)", cause)
	}
	if zoneChanged {
		if _, err := s.WriteFile(LANZone, zoneXML(w), sys.WriteOpts{Mode: 0o644}); err != nil {
			return undo(err)
		}
	}
	for _, p := range stale {
		if _, err := s.Run(ctx, sys.Cmd{Name: "firewall-cmd", Args: []string{"--permanent", "--zone=public", "--remove-port=" + p}, Timeout: time.Minute}); err != nil {
			return undo(fmt.Errorf("firewall-cmd --permanent --zone=public --remove-port=%s failed: %w", p, err))
		}
	}
	if _, err := s.Query(ctx, sys.Cmd{Name: "firewall-cmd", Args: []string{"--check-config"}, Timeout: time.Minute}); err != nil {
		return undo(fmt.Errorf("the firewall config didn't pass firewall-cmd --check-config: %w", err))
	}
	if _, err := s.Run(ctx, sys.Cmd{Name: "firewall-cmd", Args: []string{"--reload"}, Timeout: 2 * time.Minute}); err != nil {
		return undo(fmt.Errorf("firewall-cmd --reload failed: %w", err))
	}
	if err := revert.Poll(ctx, stateDeadline, 2*time.Second, func() error { return verify(ctx, s, w) }); err != nil {
		return undo(fmt.Errorf("the firewall didn't come up as expected: %w", err))
	}
	if err := revert.Commit(s, env.State); err != nil {
		return err
	}
	return restartDocker(ctx, s)
}

// Apply runs the plan and, if anything fails after the change was recorded,
// puts the previous firewall back (catalog step 14.6).
func (s Step) Apply(ctx context.Context, env *step.Env, plan []action.Action) error {
	err := action.RunAll(ctx, env.Sys, plan)
	if err == nil || env.Sys.DryRun() || env.State.Pending == nil || env.State.Pending.Step != "firewall" {
		return err
	}
	if uerr := revert.Undo(ctx, env.Sys, env.State); uerr != nil {
		return fmt.Errorf("%w; putting the previous firewall back also failed: %w", err, uerr)
	}
	return fmt.Errorf("%w. The previous firewall settings were put back", err)
}
