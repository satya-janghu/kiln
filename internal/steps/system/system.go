// Package system implements catalog step 10 `system` (docs/steps-catalog.md):
// hostname, timezone, time sync, journald cap, inotify limits and zram (systemd 257).
package system

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
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

const (
	hostnamePath  = "/etc/hostname"
	hostsPath     = "/etc/hosts"
	journalDropIn = "/etc/systemd/journald.conf.d/60-kiln.conf"
	inotifyPath   = "/etc/sysctl.d/60-kiln-inotify.conf"
	zramDropIn    = "/etc/systemd/zram-generator.conf.d/60-kiln.conf"

	keyWatches   = "fs.inotify.max_user_watches"
	keyInstances = "fs.inotify.max_user_instances"

	timesyncd     = "systemd-timesyncd"
	chrony        = "chrony"
	zramGenerator = "systemd-zram-generator"
	zramTools     = "zram-tools"
)

// Step is the `system` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "system" }
func (Step) Title() string       { return "Hostname, time, journal, inotify and zram" }
func (Step) Category() string    { return "base" }
func (Step) DependsOn() []string { return []string{"preflight"} }

func (Step) Packages() []step.PackageRef {
	out := []step.PackageRef{}
	for _, p := range []string{timesyncd, chrony, zramGenerator} {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "main", Repo: "debian"})
	}
	return out
}

var (
	hostnameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	zoneRe     = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+)*$`)
	sizeRe     = regexp.MustCompile(`^[0-9]+[KMGTPE]?$`)
)

func positive(v any) error {
	if n, _ := v.(int); n < 1 {
		return errors.New("use a positive number")
	}
	return nil
}

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "hostname", Label: "Hostname", Kind: options.String, Default: "",
			Help: "Empty keeps the current hostname.",
			Validate: func(v any) error {
				if s, _ := v.(string); s != "" && !hostnameRe.MatchString(s) {
					return errors.New("use letters, digits and hyphens only (no dots), up to 63 characters, not starting or ending with a hyphen")
				}
				return nil
			}},
		{Key: "timezone", Label: "Timezone", Kind: options.String, Default: "",
			Help: "Empty keeps the current timezone; for example Asia/Kolkata.",
			Validate: func(v any) error {
				if s, _ := v.(string); s != "" && (!zoneRe.MatchString(s) || strings.Contains(s, "..")) {
					return errors.New("use a zone name such as Asia/Kolkata (see `timedatectl list-timezones`)")
				}
				return nil
			}},
		{Key: "time_sync", Label: "Time sync", Kind: options.Choice, Default: "timesyncd",
			Choices: []options.ChoiceItem{
				{Value: "timesyncd", Label: "systemd-timesyncd (Debian's default)"},
				{Value: "chrony", Label: "chrony (replaces timesyncd)"},
			}},
		{Key: "journal_size", Label: "Journal size cap", Kind: options.String, Default: "500M",
			Help: "journald SystemMaxUse, e.g. 500M or 2G.",
			Validate: func(v any) error {
				if s, _ := v.(string); !sizeRe.MatchString(s) {
					return errors.New("use a size such as 500M or 2G")
				}
				return nil
			}},
		{Key: "inotify_watches", Label: "inotify max_user_watches", Kind: options.Int, Default: 524288,
			Help: "Applied as max(current, this), so a value is never lowered.", Validate: positive},
		{Key: "inotify_instances", Label: "inotify max_user_instances", Kind: options.Int, Default: 1024,
			Help: "Applied as max(current, this).", Validate: positive},
		{Key: "zram", Label: "zram swap", Kind: options.Bool, Default: true,
			Help: "systemd-zram-generator: min(ram / 2, 4096) MiB, zstd compression."},
	}
}

// --- reads (all through env.Sys) ---

func readTrim(s sys.System, p string) string {
	b, err := s.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func currentZone(s sys.System) string {
	l, err := s.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	if i := strings.Index(l, "zoneinfo/"); i >= 0 {
		return l[i+len("zoneinfo/"):]
	}
	return ""
}

func query(ctx context.Context, s sys.System, name string, args ...string) string {
	r, _ := s.Query(ctx, sys.Cmd{Name: name, Args: args, Timeout: 30 * time.Second})
	return strings.TrimSpace(r.Stdout)
}

func ntpOn(ctx context.Context, s sys.System) bool {
	return query(ctx, s, "timedatectl", "show", "-p", "NTP", "--value") == "yes"
}

func sysctlInt(ctx context.Context, s sys.System, key string) int {
	n, _ := strconv.Atoi(query(ctx, s, "sysctl", "-n", key))
	return n
}

func fileIs(s sys.System, path string, want []byte) bool {
	b, err := s.ReadFile(path)
	return err == nil && string(b) == string(want)
}

// --- desired content ---

// editHosts sets the 127.0.1.1 line to the new name (Debian Reference §5.1),
// keeping a domain the old first name carried ("old.example.org old" →
// "new.example.org new"). Without such a line, one is added after 127.0.0.1.
func editHosts(content, name string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if content == "" {
		lines = nil
	}
	found := false
	for i, l := range lines {
		body, _, _ := strings.Cut(l, "#")
		f := strings.Fields(body)
		if len(f) == 0 || f[0] != "127.0.1.1" {
			continue
		}
		domain := ""
		if len(f) > 1 {
			if j := strings.IndexByte(f[1], '.'); j > 0 {
				domain = f[1][j:]
			}
		}
		nl := "127.0.1.1\t" + name
		if domain != "" {
			nl = "127.0.1.1\t" + name + domain + "\t" + name
		}
		lines[i] = nl
		found = true
		break
	}
	if !found {
		at := 0
		for i, l := range lines {
			if f := strings.Fields(l); len(f) > 0 && f[0] == "127.0.0.1" {
				at = i + 1
				break
			}
		}
		lines = append(lines[:at], append([]string{"127.0.1.1\t" + name}, lines[at:]...)...)
	}
	return strings.Join(lines, "\n") + "\n"
}

// hostsOK reports the 127.0.1.1 line already names the host (trailing blank lines ignored).
func hostsOK(content, name string) bool {
	return editHosts(content, name) == strings.TrimRight(content, "\n")+"\n"
}

func journalContent(size string) []byte {
	return []byte(sys.ManagedHeader("#") + "[Journal]\nSystemMaxUse=" + size + "\n")
}

func inotifyContent(watches, instances int) []byte {
	return []byte(fmt.Sprintf("%s%s = %d\n%s = %d\n", sys.ManagedHeader("#"), keyWatches, watches, keyInstances, instances))
}

func zramContent() []byte {
	return []byte(sys.ManagedHeader("#") + "[zram0]\ncompression-algorithm = zstd\n")
}

type inotify struct {
	liveW, liveI int
	wantW, wantI int // max(live, configured)
	content      []byte
}

func inotifyState(ctx context.Context, env *step.Env) inotify {
	in := inotify{liveW: sysctlInt(ctx, env.Sys, keyWatches), liveI: sysctlInt(ctx, env.Sys, keyInstances)}
	in.wantW = max(in.liveW, env.Opts.Int("inotify_watches"))
	in.wantI = max(in.liveI, env.Opts.Int("inotify_instances"))
	in.content = inotifyContent(in.wantW, in.wantI)
	return in
}

func zramActive(s sys.System) bool {
	b, err := s.ReadFile("/proc/swaps")
	return err == nil && strings.Contains(string(b), "/dev/zram0")
}

// meminfo returns used swap and MemAvailable in kB.
func meminfo(s sys.System) (usedSwap, available int, ok bool) {
	b, err := s.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	v := map[string]int{}
	for _, l := range strings.Split(string(b), "\n") {
		k, rest, found := strings.Cut(l, ":")
		if !found {
			continue
		}
		if f := strings.Fields(rest); len(f) > 0 {
			v[k], _ = strconv.Atoi(f[0])
		}
	}
	return v["SwapTotal"] - v["SwapFree"], v["MemAvailable"], v["MemAvailable"] > 0
}

// --- Check ---

func (Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	s, o := env.Sys, env.Opts
	if name := o.String("hostname"); name != "" {
		if cur := readTrim(s, hostnamePath); cur != name {
			return step.Todo("hostname is %q, not %q", cur, name), nil
		}
		if hosts, _ := s.ReadFile(hostsPath); !hostsOK(string(hosts), name) {
			return step.Todo("the 127.0.1.1 line in %s doesn't name %s", hostsPath, name), nil
		}
	}
	if tz := o.String("timezone"); tz != "" && currentZone(s) != tz {
		return step.Todo("timezone is %q, not %q", currentZone(s), tz), nil
	}
	switch o.String("time_sync") {
	case "chrony":
		if !apt.Installed(ctx, s, chrony) {
			return step.Todo("chrony isn't installed"), nil
		}
	default:
		if !apt.Installed(ctx, s, timesyncd) {
			return step.Todo("systemd-timesyncd isn't installed"), nil
		}
		if !ntpOn(ctx, s) {
			return step.Todo("network time sync is off"), nil
		}
	}
	if !fileIs(s, journalDropIn, journalContent(o.String("journal_size"))) {
		return step.Todo("%s is missing or different", journalDropIn), nil
	}
	in := inotifyState(ctx, env)
	if !fileIs(s, inotifyPath, in.content) {
		return step.Todo("%s is missing or different", inotifyPath), nil
	}
	if in.liveW < o.Int("inotify_watches") || in.liveI < o.Int("inotify_instances") {
		return step.Todo("live inotify limits (%d / %d) are below the configured ones", in.liveW, in.liveI), nil
	}
	if o.Bool("zram") {
		switch {
		case apt.Installed(ctx, s, zramTools):
			return step.Todo("zram-tools is installed alongside systemd-zram-generator"), nil
		case !apt.Installed(ctx, s, zramGenerator):
			return step.Todo("systemd-zram-generator isn't installed"), nil
		case !fileIs(s, zramDropIn, zramContent()):
			return step.Todo("%s is missing or different", zramDropIn), nil
		case !zramActive(s):
			// When settings wait for the next boot, the old zram0 is still listed, so that counts as done.
			return step.Todo("/dev/zram0 isn't in use as swap"), nil
		}
	}
	return step.Done("hostname, time, journal, inotify and zram are as chosen"), nil
}

// --- Plan ---

func (Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	s, o := env.Sys, env.Opts
	var acts []action.Action

	if name := o.String("hostname"); name != "" {
		cur := readTrim(s, hostnamePath)
		if cur != name {
			acts = append(acts, action.Run{Cmd: sys.Cmd{Name: "hostnamectl", Args: []string{"set-hostname", name}}})
			env.Notes.Add("The hostname changed to %s: log out and back in, or new windows may not open in a running X11 session.", name)
		}
		if hosts, _ := s.ReadFile(hostsPath); !hostsOK(string(hosts), name) {
			acts = append(acts, action.WriteFile{Path: hostsPath, Content: []byte(editHosts(string(hosts), name)), Mode: 0o644})
		}
	}

	if tz := o.String("timezone"); tz != "" && currentZone(s) != tz {
		if _, err := s.Stat("/usr/share/zoneinfo/" + tz); err != nil {
			return nil, fmt.Errorf("%q isn't a known timezone; pick one from `timedatectl list-timezones`", tz)
		}
		acts = append(acts, action.Run{Cmd: sys.Cmd{Name: "timedatectl", Args: []string{"set-timezone", tz}}})
	}

	switch o.String("time_sync") {
	case "chrony":
		if !apt.Installed(ctx, s, chrony) {
			acts = append(acts, action.AptInstall{Pkgs: []string{chrony}}) // Conflicts: time-daemon removes timesyncd
		}
	default:
		if !apt.Installed(ctx, s, timesyncd) {
			acts = append(acts, action.AptInstall{Pkgs: []string{timesyncd}})
		}
		if !apt.Installed(ctx, s, timesyncd) || !ntpOn(ctx, s) {
			acts = append(acts, action.Run{Cmd: sys.Cmd{Name: "timedatectl", Args: []string{"set-ntp", "true"}}})
		}
	}

	if jc := journalContent(o.String("journal_size")); !fileIs(s, journalDropIn, jc) {
		acts = append(acts,
			action.WriteFile{Path: journalDropIn, Content: jc, Mode: 0o644},
			action.Systemctl{Verb: "restart", Units: []string{"systemd-journald.service"}},
		)
	}

	in := inotifyState(ctx, env)
	if !fileIs(s, inotifyPath, in.content) || in.liveW < in.wantW || in.liveI < in.wantI {
		acts = append(acts,
			action.WriteFile{Path: inotifyPath, Content: in.content, Mode: 0o644},
			action.Run{Cmd: sys.Cmd{Name: "sysctl", Args: []string{"-q", "-p", inotifyPath}}},
		)
	}

	if o.Bool("zram") {
		acts = append(acts, zramActions(ctx, env)...)
	}
	return acts, nil
}

func zramActions(ctx context.Context, env *step.Env) []action.Action {
	s := env.Sys
	var acts []action.Action
	if apt.Installed(ctx, s, zramTools) {
		acts = append(acts, action.AptRemove{Pkgs: []string{zramTools}})
	}
	installed := apt.Installed(ctx, s, zramGenerator)
	if !installed {
		acts = append(acts, action.AptInstall{Pkgs: []string{zramGenerator}})
	}
	changed := !fileIs(s, zramDropIn, zramContent())
	if changed {
		acts = append(acts, action.WriteFile{Path: zramDropIn, Content: zramContent(), Mode: 0o644})
	}
	if changed || !installed {
		acts = append(acts, action.Systemctl{Verb: "daemon-reload"})
	}
	acts = append(acts, action.Custom{
		Desc: "start dev-zram0.swap if it isn't active; if it is active and its settings changed, restart systemd-zram-setup@zram0.service " +
			"only when used swap is under 50% of available memory (otherwise the new settings apply at the next boot)",
		K: action.Services,
		Fn: func(ctx context.Context, s sys.System) error {
			active := query(ctx, s, "systemctl", "is-active", "dev-zram0.swap") == "active"
			if !active {
				_, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"start", "dev-zram0.swap"}})
				if err != nil {
					return fmt.Errorf("zram swap didn't start: %w. See `journalctl -u systemd-zram-setup@zram0`", err)
				}
				return nil
			}
			if !changed {
				return nil
			}
			used, avail, ok := meminfo(s)
			if ok && used*2 < avail {
				// Restarting the setup unit also restarts the swap unit (swapoff).
				_, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"restart", "systemd-zram-setup@zram0.service"}})
				return err
			}
			env.Notes.Add("zram settings apply at the next boot (too much swap is in use to restart zram safely now).")
			return nil
		},
	})
	return acts
}
