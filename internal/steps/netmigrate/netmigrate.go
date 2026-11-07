// Package netmigrate implements catalog step 6 `network-migration`
// (docs/steps-catalog.md, D4, D19). It is risky: each interface is moved on
// its own, checked by polling, and put back on ifupdown if the check fails.
package netmigrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/revert"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// netDeadline is how long the network check after a migration may take
// (architecture.md "One command at a time": up to 60 s).
const netDeadline = 60 * time.Second

const stepID = "network-migration"

// Step is the `network-migration` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return stepID }
func (Step) Title() string       { return "Move networking to NetworkManager" }
func (Step) Category() string    { return "network" }
func (Step) DependsOn() []string { return []string{"preflight"} }

func (Step) Packages() []step.PackageRef {
	return []step.PackageRef{{Name: "network-manager", Suite: "trixie", Component: "main", Repo: "debian"}}
}

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "migrate", Label: "Move interfaces to NetworkManager", Kind: options.Bool, Default: true,
			Help: "Only if /etc/network/interfaces configures a physical network card."},
		{Key: "wifi", Label: "Include Wi-Fi", Kind: options.Bool, Default: true},
	}
}

func nicByName(f *facts.Facts, name string) *facts.NIC {
	for i := range f.NICs {
		if f.NICs[i].Name == name {
			return &f.NICs[i]
		}
	}
	return nil
}

// target is one interface and what happens to it.
type target struct {
	nic        *facts.NIC
	name       string
	skip       string // why it stays on ifupdown; "" = migrate
	note       bool   // tell the user about the skip
	ssid, psk  string
	files      []string
	oldAddress []string
}

// classify decides, for every non-lo interface, whether to migrate it.
func classify(env *step.Env, c *config) []target {
	var out []target
	for _, name := range c.ifaces() {
		t := target{name: name, nic: nicByName(env.Facts, name), files: c.filesFor(name)}
		st := c.stanzasFor(name)
		in4, in6 := c.stanza(name, "inet"), c.stanza(name, "inet6")
		switch {
		case t.nic == nil || !t.nic.Physical:
			t.skip = "isn't a physical network card on this machine"
		case t.nic.Wireless && !env.Opts.Bool("wifi"):
			t.skip = "is Wi-Fi, and \"include Wi-Fi\" is off"
		case (in4 == nil || in4.method == "manual") && (in6 == nil || in6.method == "manual"):
			t.skip, t.note = "is configured as manual", true
		case t.nic.Wireless:
			if t.ssid, t.psk, t.skip = wifiCredentials(env.Sys, st); t.skip != "" {
				t.skip, t.note = "stays on ifupdown: "+t.skip, true
			}
		}
		if t.nic != nil {
			for _, a := range t.nic.Addrs {
				t.oldAddress = append(t.oldAddress, a.Addr().String())
			}
		}
		out = append(out, t)
	}
	// Wired first, Wi-Fi last (catalog step 6).
	slices.SortStableFunc(out, func(a, b target) int {
		wa, wb := a.nic != nil && a.nic.Wireless, b.nic != nil && b.nic.Wireless
		switch {
		case wa == wb:
			return 0
		case wb:
			return -1
		}
		return 1
	})
	return out
}

func (Step) Applies(env *step.Env) step.Applicability {
	c := parse(env.Sys)
	physical := false
	for _, name := range c.ifaces() {
		if n := nicByName(env.Facts, name); n != nil && n.Physical {
			physical = true
		}
	}
	switch {
	case c.manual != "":
		return step.No(c.manual + "; move that interface to NetworkManager by hand")
	case !physical:
		return step.No("NetworkManager already manages networking")
	case !env.Opts.Bool("migrate"):
		return step.No("moving interfaces to NetworkManager is turned off in this step's options")
	}
	return step.Yes()
}

// deviceState reads NM's view of a device, e.g. "100 (connected)" or
// "10 (unmanaged)"; "" when NetworkManager doesn't know the device.
func deviceState(ctx context.Context, s sys.System, iface string) string {
	r, err := s.Query(ctx, sys.Cmd{Name: "nmcli", Args: []string{"-t", "-g", "GENERAL.STATE", "device", "show", iface}})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(r.Stdout)
}

// managed reports whether NetworkManager manages the device: its state is
// known and is neither 0 (unknown) nor 10 (unmanaged). A down link (20
// unavailable, 30 disconnected) still counts: the cable may just be unplugged.
func managed(state string) bool {
	code, _, _ := strings.Cut(state, " ")
	return code != "" && code != "0" && code != "10"
}

func autoconnect(ctx context.Context, s sys.System, profile string) string {
	r, _ := s.Query(ctx, sys.Cmd{Name: "nmcli", Args: []string{"-t", "-g", "connection.autoconnect", "connection", "show", profile}})
	return strings.TrimSpace(r.Stdout)
}

func networkingEnabled(ctx context.Context, s sys.System) bool {
	r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-enabled", "networking.service"}})
	return strings.TrimSpace(r.Stdout) == "enabled"
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	sy := env.Sys
	c := parse(sy)
	var left []string
	for _, t := range classify(env, c) {
		if t.skip == "" {
			return step.Todo("%s is still configured by ifupdown (%s)", t.name, strings.Join(t.files, ", ")), nil
		}
		left = append(left, t.name+" "+t.skip)
	}
	keyfiles, _ := sy.Glob(filepath.Join(ConnDir, "kiln-*.nmconnection"))
	var moved []string
	for _, kf := range keyfiles {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(kf), "kiln-"), ".nmconnection")
		if nicByName(env.Facts, name) == nil {
			continue // the card isn't present any more
		}
		if st := deviceState(ctx, sy, name); !managed(st) {
			return step.Todo("NetworkManager doesn't manage %s (state %q)", name, st), nil
		}
		if autoconnect(ctx, sy, profileName(name)) != "yes" {
			return step.Todo("%s doesn't connect automatically at boot yet", profileName(name)), nil
		}
		moved = append(moved, name)
	}
	if len(c.ifaces()) == 0 && networkingEnabled(ctx, sy) {
		return step.Todo("networking.service is still enabled, but no interface is left in %s", Interfaces), nil
	}
	detail := "NetworkManager manages " + strings.Join(moved, ", ")
	if len(moved) == 0 {
		detail = "nothing to move"
	}
	if len(left) > 0 {
		detail += "; left on ifupdown on purpose: " + strings.Join(left, "; ")
	}
	return step.Done(detail), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	sy := env.Sys
	c := parse(sy)
	targets := classify(env, c)
	var acts []action.Action
	if !apt.Installed(ctx, sy, "network-manager") {
		acts = append(acts, action.AptInstall{Pkgs: []string{"network-manager"}})
	}
	// The migration and its revert both talk to NetworkManager through nmcli.
	nmUp := false
	if r, err := sy.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-active", "NetworkManager.service"}}); err == nil && strings.TrimSpace(r.Stdout) == "active" {
		r2, _ := sy.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-enabled", "NetworkManager.service"}})
		nmUp = strings.TrimSpace(r2.Stdout) == "enabled"
	}
	nmAct := action.Systemctl{Verb: "enable", Units: []string{"NetworkManager.service"}, Now: true}
	migrating := map[string]bool{}
	for _, t := range targets {
		if t.skip != "" {
			if t.note {
				env.Notes.Add("%s %s, so it stays in %s.", t.name, t.skip, Interfaces)
			}
			continue
		}
		p, err := buildProfile(sy, env.Facts, c, t.nic, t.ssid, t.psk)
		if err != nil {
			return nil, fmt.Errorf("can't translate the %s stanza: %w; fix %s or untick this step", t.name, err, Interfaces)
		}
		if p.note != "" {
			env.Notes.Add("%s: %s.", t.name, p.note)
		}
		if len(migrating) == 0 && !nmUp {
			acts = append(acts, nmAct)
		}
		migrating[t.name] = true
		acts = append(acts, action.Custom{
			Desc: fmt.Sprintf("move %s to NetworkManager: write %s (0600, %s, autoconnect off for now), load it, ifdown %s, comment out its stanzas in %s, restart NetworkManager, stop any leftover DHCP client, let NetworkManager manage %s and bring %s up (nmcli --wait 90); then check link, address, default route, DNS and HTTPS to deb.debian.org for up to %s, turn autoconnect on if that passes, and put %s back on ifupdown if it fails",
				t.name, keyfilePath(t.name), p.summary, t.name, strings.Join(t.files, ", "), t.name, profileName(t.name), netDeadline, t.name),
			K:  action.Commands,
			Fn: func(ctx context.Context, s sys.System) error { return migrate(ctx, s, env, t, p) },
		})
	}
	if len(migrating) == 0 {
		return acts, nil
	}
	remaining := 0
	for _, n := range c.ifaces() {
		if !migrating[n] {
			remaining++
		}
	}
	acts = append(acts, action.Custom{
		Desc: "disable networking.service if no interface other than lo is left in " + Interfaces,
		K:    action.Services,
		Fn: func(ctx context.Context, s sys.System) error {
			left := remaining
			if !s.DryRun() {
				left = len(parse(s).ifaces())
			}
			if left > 0 || !networkingEnabled(ctx, s) {
				return nil
			}
			_, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"disable", "networking.service"}})
			return err
		},
	})
	return acts, nil
}

// migrate moves one interface (catalog step 6, actions 3-7).
func migrate(ctx context.Context, s sys.System, env *step.Env, t target, p profile) error {
	name, prof, kf := t.name, profileName(t.name), keyfilePath(t.name)
	// What the revert runs after restoring the files depends on how far the
	// migration got, so it never removes a live address ifupdown still owns:
	//   - before ifdown: ifupdown still runs the interface; only make NM forget the profile;
	//   - once ifdown has started: bring it back up with ifup (a no-op while it is still up);
	//   - once ifdown succeeded: NM may have configured it, so take it from NM,
	//     flush whatever NM set, then ifup.
	reload := []string{"nmcli", "connection", "reload"}
	beforeDown := [][]string{reload}
	downStarted := [][]string{reload, {"ifup", name}}
	afterDown := [][]string{reload, {"nmcli", "device", "set", name, "managed", "no"}, {"ip", "addr", "flush", "dev", name}, {"ifup", name}}
	paths := append(slices.Clone(t.files), kf)
	if err := revert.Begin(ctx, s, env.State, stepID, "moving "+name+" to NetworkManager", paths, beforeDown); err != nil {
		return fmt.Errorf("couldn't record the network change before making it: %w", err)
	}
	// progress updates the recorded restore commands, so a revert after a
	// crash (the next start) does the same as one done now.
	progress := func(restore [][]string) error {
		if s.DryRun() || env.State.Pending == nil {
			return nil
		}
		env.State.Pending.Restore = restore
		if err := env.State.Save(s); err != nil {
			return fmt.Errorf("couldn't record the network change's progress: %w", err)
		}
		return nil
	}
	undo := func(cause error) error {
		_, _ = s.Run(ctx, sys.Cmd{Name: "nmcli", Args: []string{"connection", "down", prof}, Timeout: 2 * time.Minute})
		if uerr := revert.Undo(ctx, s, env.State); uerr != nil {
			env.Notes.Add("The network change for %s couldn't be undone. To recover by hand: copy %s to %s and run `systemctl restart networking`.", name, sys.BackupPath(Interfaces), Interfaces)
			return fmt.Errorf("%w; putting %s back on ifupdown also failed: %w", cause, name, uerr)
		}
		// ifup returns before DHCP (and, on Wi-Fi, association) has finished. Wait
		// for the network to work again, so the next step doesn't fail on a
		// network that's still coming back (D41: "started" isn't "ready").
		if !s.DryRun() {
			env.Say("waiting for %s to come back on ifupdown", name)
			if werr := revert.Poll(ctx, netDeadline, 3*time.Second, func() error { return netCheck(ctx, s, name, env.Facts.DefaultIface == name) }); werr != nil {
				env.Notes.Add("%s was put back on ifupdown but the network hadn't come back after %s; later steps may fail. Check `ip addr show %s`.", name, netDeadline, name)
			}
		}
		return fmt.Errorf("%w. %s was put back on ifupdown", cause, name)
	}
	run := func(timeout time.Duration, args ...string) error {
		if _, err := s.Run(ctx, sys.Cmd{Name: args[0], Args: args[1:], Timeout: timeout}); err != nil {
			return fmt.Errorf("%s failed: %w", strings.Join(args, " "), err)
		}
		return nil
	}
	// 3-4. The profile exists, loaded but not autoconnecting, before NM manages the device.
	if _, err := s.WriteFile(kf, p.content, sys.WriteOpts{Mode: 0o600}); err != nil {
		return undo(err)
	}
	if err := run(time.Minute, "nmcli", "connection", "load", kf); err != nil {
		return undo(err)
	}
	// 6.1 ifdown while the stanza still exists.
	if err := progress(downStarted); err != nil {
		return undo(err)
	}
	if err := run(2*time.Minute, "ifdown", name); err != nil {
		return undo(err)
	}
	if err := progress(afterDown); err != nil {
		return undo(err)
	}
	// 6.2 Comment out its stanzas, keeping lo and the file mode.
	c := parse(s)
	for _, f := range t.files {
		mode := fs.FileMode(0o644)
		if fi, err := s.Stat(f); err == nil {
			mode = fi.Mode().Perm()
		}
		if _, err := s.WriteFile(f, c.commentOut(f, name), sys.WriteOpts{Mode: mode}); err != nil {
			return undo(err)
		}
	}
	// 6.3 Make NetworkManager drop the interface from its unmanaged list. A
	// `nmcli connection reload` re-reads /etc/network/interfaces but doesn't
	// re-evaluate a device NM marked unmanaged at startup (seen on the HP test
	// machine: still "10 (unmanaged)" after reload + `managed yes`). The Debian
	// wiki's documented procedure is: comment out the stanza, create the NM
	// connection, restart NetworkManager (wiki.debian.org/NetworkManager).
	if err := run(2*time.Minute, "systemctl", "restart", "NetworkManager.service"); err != nil {
		return undo(err)
	}
	// 6.4 No DHCP client may keep running for the interface.
	stopDHCP(ctx, s, name)
	// 6.5-6.6
	if err := run(time.Minute, "nmcli", "device", "set", name, "managed", "yes"); err != nil {
		return undo(err)
	}
	// NetworkManager makes a newly managed device available asynchronously, and a
	// Wi-Fi device must also have scanned and seen its network; activating
	// before that fails with "No suitable device found" (seen on the HP test
	// machine, 2026-11-07). Wait for the device, then activate on it by name.
	if !s.DryRun() {
		if err := waitDeviceReady(ctx, s, name, t.ssid != ""); err != nil {
			return undo(err)
		}
	}
	if err := run(2*time.Minute, "nmcli", "--wait", "90", "connection", "up", prof, "ifname", name); err != nil {
		return undo(err)
	}
	if s.DryRun() {
		return nil
	}
	// 7. Check locally, polling (D41).
	env.Say("checking the network on %s", name)
	if err := revert.Poll(ctx, netDeadline, 3*time.Second, func() error { return netCheck(ctx, s, name, env.Facts.DefaultIface == name) }); err != nil {
		return undo(fmt.Errorf("the network didn't work on %s through NetworkManager: %w", name, err))
	}
	if err := run(time.Minute, "nmcli", "connection", "modify", prof, "connection.autoconnect", "yes"); err != nil {
		return undo(err)
	}
	if err := revert.Commit(s, env.State); err != nil {
		return err
	}
	if now := addresses(ctx, s, name); len(t.oldAddress) > 0 && len(now) > 0 && !overlap(t.oldAddress, now) {
		env.Say("%s now has the address %s (it was %s)", name, strings.Join(now, ", "), strings.Join(t.oldAddress, ", "))
		env.Notes.Add("%s now has the address %s (it was %s). Use the new address to reach this machine.", name, strings.Join(now, ", "), strings.Join(t.oldAddress, ", "))
	}
	return nil
}

// stopDHCP releases a DHCP client ifdown left behind (catalog step 6.6.4).
func stopDHCP(ctx context.Context, s sys.System, iface string) {
	for _, c := range []struct{ proc, flag string }{{"dhcpcd", "-k"}, {"dhclient", "-r"}} {
		r, err := s.Query(ctx, sys.Cmd{Name: "pgrep", Args: []string{"-a", c.proc}})
		if err != nil {
			continue // no such process
		}
		for _, l := range strings.Split(r.Stdout, "\n") {
			if slices.Contains(strings.Fields(l), iface) || (c.proc == "dhcpcd" && strings.TrimSpace(l) != "") {
				_, _ = s.Run(ctx, sys.Cmd{Name: c.proc, Args: []string{c.flag, iface}, Timeout: time.Minute})
				break
			}
		}
	}
}

type ipAddr struct {
	Flags    []string `json:"flags"`
	AddrInfo []struct {
		Local string `json:"local"`
		Scope string `json:"scope"`
	} `json:"addr_info"`
}

func addrInfo(ctx context.Context, s sys.System, iface string) (up bool, addrs []string) {
	r, err := s.Query(ctx, sys.Cmd{Name: "ip", Args: []string{"-j", "addr", "show", "dev", iface}})
	if err != nil {
		return false, nil
	}
	var v []ipAddr
	if json.Unmarshal([]byte(r.Stdout), &v) != nil || len(v) == 0 {
		return false, nil
	}
	up = slices.Contains(v[0].Flags, "LOWER_UP")
	for _, a := range v[0].AddrInfo {
		if a.Scope == "global" {
			addrs = append(addrs, a.Local)
		}
	}
	return up, addrs
}

func addresses(ctx context.Context, s sys.System, iface string) []string {
	_, a := addrInfo(ctx, s, iface)
	return a
}

func overlap(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// netCheck: link up with an address, a default route, DNS, and HTTPS to deb.debian.org.
// When iface carried the default route before the migration (hadDefault), the
// default route must now go through iface too, not only through another card.
func netCheck(ctx context.Context, s sys.System, iface string, hadDefault bool) error {
	up, addrs := addrInfo(ctx, s, iface)
	if !up {
		return fmt.Errorf("the link on %s is down", iface)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%s has no address", iface)
	}
	route := false
	for _, fam := range []string{"-4", "-6"} {
		args := []string{fam, "-j", "route", "show", "default"}
		if hadDefault {
			args = append(args, "dev", iface)
		}
		r, err := s.Query(ctx, sys.Cmd{Name: "ip", Args: args})
		if err == nil && strings.Contains(r.Stdout, "\"dst\"") {
			route = true
		}
	}
	switch {
	case !route && hadDefault:
		return fmt.Errorf("there is no default route through %s", iface)
	case !route:
		return errors.New("there is no default route")
	}
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := net.DefaultResolver.LookupHost(lctx, "deb.debian.org"); err != nil {
		return fmt.Errorf("DNS doesn't resolve deb.debian.org: %w", err)
	}
	if _, err := s.HTTPGet(lctx, "https://deb.debian.org/debian/", 4<<20); err != nil {
		return fmt.Errorf("HTTPS to deb.debian.org fails: %w", err)
	}
	return nil
}

// waitDeviceReady waits until NetworkManager reports the device as available
// (not unmanaged/unavailable) and, for Wi-Fi, until a scan shows an SSID, so
// `nmcli connection up` can succeed.
func waitDeviceReady(ctx context.Context, s sys.System, name string, wifi bool) error {
	state := func() string {
		r, _ := s.Query(ctx, sys.Cmd{Name: "nmcli", Args: []string{"-t", "-g", "GENERAL.STATE", "device", "show", name}})
		return strings.TrimSpace(r.Stdout)
	}
	err := revert.Poll(ctx, 45*time.Second, time.Second, func() error {
		st := state()
		// NM device states: 10 unmanaged, 20 unavailable, 30 disconnected and
		// higher can activate a profile (nm-dbus-interface NMDeviceState).
		code, _, _ := strings.Cut(st, " ")
		if n, err := strconv.Atoi(code); err == nil && n >= 30 {
			return nil
		}
		return fmt.Errorf("NetworkManager reports %s as %q", name, st)
	})
	if err != nil {
		return fmt.Errorf("NetworkManager didn't make %s available: %w", name, err)
	}
	if !wifi {
		return nil
	}
	_, _ = s.Run(ctx, sys.Cmd{Name: "nmcli", Args: []string{"device", "wifi", "rescan", "ifname", name}, Timeout: time.Minute})
	return revert.Poll(ctx, 45*time.Second, 2*time.Second, func() error {
		r, _ := s.Query(ctx, sys.Cmd{Name: "nmcli", Args: []string{"-t", "-f", "SSID", "device", "wifi", "list", "ifname", name, "--rescan", "no"}})
		for _, l := range strings.Split(r.Stdout, "\n") {
			if strings.TrimSpace(l) != "" {
				return nil
			}
		}
		return fmt.Errorf("no Wi-Fi networks visible on %s yet", name)
	})
}
