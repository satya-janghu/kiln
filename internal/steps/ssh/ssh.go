// Package ssh implements catalog step 15 `ssh` (docs/steps-catalog.md). It is
// risky: the drop-in is validated, applied with a reload, checked by polling,
// and put back if the check fails (architecture: "Risky changes").
package ssh

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
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
	"github.com/satya-janghu/kiln/internal/sys"
)

// Paths used by this step and read by firewall and fail2ban.
const (
	MainConfig   = "/etc/ssh/sshd_config"
	DropInDir    = "/etc/ssh/sshd_config.d"
	DropIn       = "/etc/ssh/sshd_config.d/10-kiln.conf"
	SocketDropIn = "/etc/systemd/system/ssh.socket.d/60-kiln.conf"
)

// listenDeadline is how long sshd may take to listen again after a reload (D41).
const listenDeadline = 15 * time.Second

// Step is the `ssh` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "ssh" }
func (Step) Title() string       { return "SSH server and logins" }
func (Step) Category() string    { return "security" }
func (Step) DependsOn() []string { return []string{"firewall"} }

// Applies always: kiln installs and enables the SSH server when it's missing (D45).
func (Step) Applies(*step.Env) step.Applicability { return step.Yes() }

func (Step) Packages() []step.PackageRef {
	return []step.PackageRef{{Name: "openssh-server", Suite: "trixie", Component: "main", Repo: "debian"}}
}

var githubUser = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "github_user", Label: "GitHub user for keys", Kind: options.String, Default: "",
			Help: "kiln adds the public keys from https://github.com/<user>.keys.",
			Validate: func(v any) error {
				if s, _ := v.(string); s != "" && !githubUser.MatchString(s) {
					return errors.New("that isn't a valid GitHub user name")
				}
				return nil
			}},
		{Key: "extra_keys", Label: "Extra public keys", Kind: options.List, Default: []string{},
			Help: "One per line, as in authorized_keys (e.g. ssh-ed25519 AAAA… me@laptop).",
			Validate: func(v any) error {
				l, _ := v.([]string)
				for _, k := range l {
					if _, ok := parseKey(k); !ok {
						return fmt.Errorf("%q isn't a public key line", short(k))
					}
				}
				return nil
			}},
		{Key: "port", Label: "Port", Kind: options.Int, Default: 22,
			Validate: func(v any) error {
				if n, _ := v.(int); n < 1 || n > 65535 {
					return errors.New("use a port from 1 to 65535")
				}
				return nil
			}},
		{Key: "password_logins", Label: "Password logins", Kind: options.Choice, Default: "on",
			Help: "On by default (D45): your user and root can log in with their passwords from anywhere SSH is reachable.",
			Choices: []options.ChoiceItem{
				{Value: "on", Label: "On"},
				{Value: "auto", Label: "Off once at least one key is installed"},
				{Value: "off", Label: "Off"},
			}},
		{Key: "root_login", Label: "Allow root to log in over SSH", Kind: options.Bool, Default: true,
			Help: "PermitRootLogin yes (D45). With password logins on, root can log in with its password."},
		{Key: "root_password", Label: "If root has no password, give it your user's password", Kind: options.Bool, Default: true,
			Help: "Copies your user's password hash from /etc/shadow to root (D45); kiln never sees the password itself."},
		{Key: "max_auth_tries", Label: "MaxAuthTries", Kind: options.Int, Default: 4,
			Help: "Agents that offer many keys need IdentitiesOnly on the client.",
			Validate: func(v any) error {
				if n, _ := v.(int); n < 1 || n > 20 {
					return errors.New("use 1 to 20")
				}
				return nil
			}},
	}
}

func short(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}

// ---- keys ----

type pubkey struct {
	typ, blob, line string
}

func (k pubkey) id() string { return k.typ + " " + k.blob }

// parseKey finds "type base64 [comment]" in an authorized_keys line (options
// may precede it) and checks that the blob's embedded type matches.
func parseKey(line string) (pubkey, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return pubkey{}, false
	}
	f := strings.Fields(line)
	for i := 0; i+1 < len(f); i++ {
		t := f[i]
		if !strings.HasPrefix(t, "ssh-") && !strings.HasPrefix(t, "ecdsa-") && !strings.HasPrefix(t, "sk-") {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(f[i+1])
		if err != nil || len(raw) < 4 {
			continue
		}
		n := int64(binary.BigEndian.Uint32(raw[:4]))
		if n > int64(len(raw)-4) || string(raw[4:4+n]) != t {
			continue
		}
		return pubkey{typ: t, blob: f[i+1], line: strings.Join(f[i:], " ")}, true
	}
	return pubkey{}, false
}

func authorizedKeys(env *step.Env) string {
	return filepath.Join(env.User().Home, ".ssh", "authorized_keys")
}

// installed returns the keys already in authorized_keys.
func installed(s sys.System, path string) (raw string, keys map[string]bool) {
	keys = map[string]bool{}
	b, err := s.ReadFile(path)
	if err != nil {
		return "", keys
	}
	for _, l := range strings.Split(string(b), "\n") {
		if k, ok := parseKey(l); ok && k.typ != "ssh-dss" {
			keys[k.id()] = true
		}
	}
	return string(b), keys
}

type wanted struct {
	key pubkey
	tag string // kiln:github or kiln:manual
}

// fetchGitHub reads https://github.com/<user>.keys (plain text, one key per line).
func fetchGitHub(ctx context.Context, s sys.System, user string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, err := s.HTTPGet(ctx, "https://github.com/"+user+".keys", 256<<10)
	var he *sys.HTTPError
	if errors.As(err, &he) && he.Status == 404 {
		return nil, fmt.Errorf("GitHub user %q doesn't exist; fix \"GitHub user for keys\" in the SSH options", user)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't fetch the keys of GitHub user %q (%w); check the network, or clear the GitHub user in the SSH options", user, err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// desired returns the keys the options ask for, plus what was skipped.
func desired(ctx context.Context, env *step.Env) (keys []wanted, skipped []string, err error) {
	var gh []string
	if u := env.Opts.String("github_user"); u != "" {
		if gh, err = fetchGitHub(ctx, env.Sys, u); err != nil {
			return nil, nil, err
		}
		if len(gh) == 0 {
			skipped = append(skipped, fmt.Sprintf("GitHub user %s has no public keys", u))
		}
	}
	seen := map[string]bool{}
	add := func(lines []string, tag string) {
		for _, l := range lines {
			k, ok := parseKey(l)
			switch {
			case !ok:
				skipped = append(skipped, fmt.Sprintf("%q isn't a valid public key", short(l)))
			case k.typ == "ssh-dss":
				skipped = append(skipped, "a DSA (ssh-dss) key, which OpenSSH 10 no longer supports")
			case !seen[k.id()]:
				seen[k.id()] = true
				keys = append(keys, wanted{key: k, tag: tag})
			}
		}
	}
	add(gh, "kiln:github")
	add(env.Opts.List("extra_keys"), "kiln:manual")
	return keys, skipped, nil
}

// validWithKeygen checks one key with `ssh-keygen -l -f <tmp>` (catalog step 15.1).
func validWithKeygen(ctx context.Context, s sys.System, k pubkey) bool {
	if err := s.MkdirAll("/run/kiln", 0o755, nil); err != nil {
		return false
	}
	tmp := "/run/kiln/ssh-key-check.pub"
	if _, err := s.WriteFile(tmp, []byte(k.line+"\n"), sys.WriteOpts{Mode: 0o600, NoBackup: true}); err != nil {
		return false
	}
	_, err := s.Query(ctx, sys.Cmd{Name: "ssh-keygen", Args: []string{"-l", "-f", tmp}})
	_ = s.RemoveAll(tmp)
	return err == nil
}

// ---- sshd configuration ----

// Port returns the configured SSH port (the ssh step's option).
func Port(env *step.Env) int {
	if p, ok := env.All.Values["ssh.port"].(int); ok && p > 0 {
		return p
	}
	return 22
}

// portLines returns uncommented Port values per file, outside kiln's drop-in.
func portLines(s sys.System) map[string][]int {
	files := []string{MainConfig}
	more, _ := s.Glob(filepath.Join(DropInDir, "*.conf"))
	files = append(files, more...)
	out := map[string][]int{}
	for _, f := range files {
		if f == DropIn {
			continue
		}
		b, err := s.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			fl := strings.Fields(l)
			if len(fl) >= 2 && strings.EqualFold(fl[0], "Port") {
				if n, err := strconv.Atoi(fl[1]); err == nil {
					out[f] = append(out[f], n)
				}
			}
		}
	}
	return out
}

// KilnPort returns the Port in kiln's current drop-in (0 if none), i.e. the
// custom port a previous run applied.
func KilnPort(s sys.System) int {
	b, err := s.ReadFile(DropIn)
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		fl := strings.Fields(l)
		if len(fl) >= 2 && fl[0] == "Port" {
			n, _ := strconv.Atoi(fl[1])
			return n
		}
	}
	return 0
}

// CurrentPorts returns the ports sshd is configured to listen on right now:
// the effective configuration from `sshd -T` (Port lines add up across every
// file), or ssh.socket's ListenStream when socket activation is used. If
// sshd -T fails, the Port lines in the files are read instead. Read-only.
func CurrentPorts(ctx context.Context, s sys.System) []int {
	set := map[int]bool{}
	if SocketMode(ctx, s) {
		set[22] = true
		if b, err := s.ReadFile(SocketDropIn); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				v, ok := strings.CutPrefix(strings.TrimSpace(l), "ListenStream=")
				if !ok {
					continue
				}
				if v = strings.TrimSpace(v); v == "" {
					clear(set) // an empty ListenStream= resets the list
					continue
				}
				if i := strings.LastIndex(v, ":"); i >= 0 {
					v = v[i+1:]
				}
				if n, err := strconv.Atoi(v); err == nil {
					set[n] = true
				}
			}
		}
	} else {
		if r, err := s.Query(ctx, sys.Cmd{Name: "sshd", Args: []string{"-T"}}); err == nil {
			for _, l := range strings.Split(r.Stdout, "\n") {
				if k, v, ok := strings.Cut(strings.TrimSpace(l), " "); ok && k == "port" {
					if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
						set[n] = true
					}
				}
			}
		}
		if len(set) == 0 {
			for _, ps := range portLines(s) {
				for _, p := range ps {
					set[p] = true
				}
			}
			if p := KilnPort(s); p != 0 {
				set[p] = true
			}
		}
	}
	if len(set) == 0 {
		set[22] = true
	}
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// FirewallSync is set by the firewall step's package (which imports this one,
// so this package can't call it directly). After a port change is committed,
// the ssh step calls it with the ports sshd listened on before the change, so
// the firewall closes the ones sshd no longer uses only once the new port works.
var FirewallSync func(ctx context.Context, s sys.System, env *step.Env, before []int) error

// portTaken reports whether something other than sshd already listens on TCP
// port (`ss -H -l -t -n -p sport = :<port>`), and who. Read-only.
func portTaken(ctx context.Context, s sys.System, port int) (who string, taken bool) {
	r, err := s.Query(ctx, sys.Cmd{Name: "ss", Args: []string{"-H", "-l", "-t", "-n", "-p", "sport", "=", ":" + strconv.Itoa(port)}})
	if err != nil {
		return "", false // can't tell; sshd -t and the listen check still catch it
	}
	for _, l := range strings.Split(r.Stdout, "\n") {
		f := strings.Fields(l)
		if len(f) == 0 || strings.Contains(l, `(("sshd",`) {
			continue
		}
		who = "another program"
		if u := f[len(f)-1]; strings.HasPrefix(u, "users:") {
			who = u
		}
		return who, true
	}
	return "", false
}

// SocketMode reports ssh.socket enabled by hand instead of ssh.service.
func SocketMode(ctx context.Context, s sys.System) bool {
	sock, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-enabled", "ssh.socket"}})
	svc, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-enabled", "ssh.service"}})
	return strings.TrimSpace(sock.Stdout) == "enabled" && strings.TrimSpace(svc.Stdout) != "enabled"
}

// ListenPorts returns the ports sshd listens on once this step has run.
func ListenPorts(ctx context.Context, env *step.Env) []int {
	port := Port(env)
	if SocketMode(ctx, env.Sys) {
		return []int{port}
	}
	set := map[int]bool{}
	for _, ps := range portLines(env.Sys) {
		for _, p := range ps {
			set[p] = true
		}
	}
	if port != 22 {
		set[port] = true
	}
	if len(set) == 0 {
		set[22] = true
	}
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

func rootLogin(env *step.Env) string {
	if env.Opts.Bool("root_login") {
		return "yes"
	}
	return "no"
}

// shadowHash returns a user's password field from /etc/shadow.
func shadowHash(s sys.System, user string) (string, bool) {
	b, err := s.ReadFile("/etc/shadow")
	if err != nil {
		return "", false
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Split(l, ":")
		if len(f) > 1 && f[0] == user {
			return f[1], true
		}
	}
	return "", false
}

// noPassword reports a locked or empty password field ("!", "*", "!*", "").
func noPassword(h string) bool {
	return h == "" || strings.HasPrefix(h, "!") || strings.HasPrefix(h, "*")
}

// rootNeedsPassword reports that root has no usable password but the target
// user has one kiln can copy (D45).
func rootNeedsPassword(env *step.Env) bool {
	if !env.Opts.Bool("root_password") {
		return false
	}
	rh, ok := shadowHash(env.Sys, "root")
	if !ok || !noPassword(rh) {
		return false
	}
	uh, ok := shadowHash(env.Sys, env.User().Name)
	return ok && !noPassword(uh)
}

// WillHaveServer reports whether this run ends with an SSH server: already
// installed, or the ssh step (which installs it, D45) is ticked. The firewall
// and fail2ban steps use it.
func WillHaveServer(env *step.Env) bool {
	return env.Facts.SSHServer || env.All.IsEnabled("ssh")
}

func serverReady(ctx context.Context, env *step.Env) string {
	if !apt.Installed(ctx, env.Sys, "openssh-server") {
		return "the SSH server (openssh-server) isn't installed"
	}
	if SocketMode(ctx, env.Sys) {
		return ""
	}
	r, _ := env.Sys.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-enabled", "ssh.service"}})
	if strings.TrimSpace(r.Stdout) != "enabled" {
		return "the SSH service isn't enabled"
	}
	return ""
}

func passwordAuth(env *step.Env, keys int) string {
	switch env.Opts.String("password_logins") {
	case "on":
		return "yes"
	case "off":
		return "no"
	}
	if keys > 0 {
		return "no"
	}
	return "yes"
}

func dropInContent(env *step.Env, pa string, socket bool) []byte {
	var b strings.Builder
	b.WriteString(sys.ManagedHeader("#"))
	fmt.Fprintf(&b, "PermitRootLogin %s\n", rootLogin(env))
	fmt.Fprintf(&b, "MaxAuthTries %d\n", env.Opts.Int("max_auth_tries"))
	b.WriteString("X11Forwarding no\n")
	fmt.Fprintf(&b, "PasswordAuthentication %s\n", pa)
	if p := Port(env); p != 22 && !socket {
		fmt.Fprintf(&b, "Port %d\n", p)
	}
	return []byte(b.String())
}

func socketContent(port int) []byte {
	return []byte(sys.ManagedHeader("#") + fmt.Sprintf("[Socket]\nListenStream=\nListenStream=%d\n", port))
}

// expected is what `sshd -T -C …` must report.
func expected(env *step.Env, pa string) map[string]string {
	return map[string]string{
		"permitrootlogin":        rootLogin(env),
		"maxauthtries":           strconv.Itoa(env.Opts.Int("max_auth_tries")),
		"x11forwarding":          "no",
		"passwordauthentication": pa,
	}
}

// verify is the local check (catalog step 15.4.3); read-only.
func verify(ctx context.Context, s sys.System, env *step.Env, pa string, socket bool) error {
	unit := "ssh"
	if socket {
		unit = "ssh.socket"
	}
	if r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-active", unit}}); strings.TrimSpace(r.Stdout) != "active" {
		return fmt.Errorf("%s isn't active", unit)
	}
	port := Port(env)
	r, err := s.Query(ctx, sys.Cmd{Name: "ss", Args: []string{"-H", "-l", "-t", "-n"}})
	if err != nil {
		return fmt.Errorf("ss failed: %w", err)
	}
	listening := false
	for _, l := range strings.Split(r.Stdout, "\n") {
		if f := strings.Fields(l); len(f) >= 4 && strings.HasSuffix(f[3], ":"+strconv.Itoa(port)) {
			listening = true
		}
	}
	if !listening {
		return fmt.Errorf("nothing is listening on port %d", port)
	}
	addr := env.Facts.PrimaryIPv4()
	if addr == "" {
		addr = "127.0.0.1"
	}
	host := env.Facts.Hostname
	if host == "" {
		host = "localhost"
	}
	conn := "user=" + env.User().Name + ",host=" + host + ",addr=" + addr
	t, err := s.Query(ctx, sys.Cmd{Name: "sshd", Args: []string{"-T", "-C", conn}})
	if err != nil {
		return fmt.Errorf("sshd -T failed: %w", err)
	}
	got := map[string]string{}
	var ports []string
	for _, l := range strings.Split(t.Stdout, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), " ")
		if !ok {
			continue
		}
		if k == "port" {
			ports = append(ports, v)
		}
		if _, seen := got[k]; !seen {
			got[k] = v
		}
	}
	for k, v := range expected(env, pa) {
		if got[k] != v {
			return fmt.Errorf("sshd -T -C %s shows %s %q, expected %q (another sshd_config file or Match block overrides kiln's drop-in)", conn, k, got[k], v)
		}
	}
	if !socket && !slices.Contains(ports, strconv.Itoa(port)) {
		return fmt.Errorf("sshd -T doesn't list port %d", port)
	}
	return nil
}

// ---- step ----

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	if why := serverReady(ctx, env); why != "" {
		return step.Todo("%s", why), nil
	}
	if rootNeedsPassword(env) {
		return step.Todo("root has no password yet"), nil
	}
	want, _, err := desired(ctx, env)
	if err != nil {
		return step.Todo("%s", err.Error()), nil
	}
	_, have := installed(env.Sys, authorizedKeys(env))
	for _, w := range want {
		if !have[w.key.id()] {
			return step.Todo("a %s key isn't in %s yet", w.key.typ, authorizedKeys(env)), nil
		}
	}
	socket := SocketMode(ctx, env.Sys)
	pa := passwordAuth(env, len(have))
	if b, err := env.Sys.ReadFile(DropIn); err != nil || string(b) != string(dropInContent(env, pa, socket)) {
		return step.Todo("%s is missing or different", DropIn), nil
	}
	if socket && Port(env) != 22 {
		if b, err := env.Sys.ReadFile(SocketDropIn); err != nil || string(b) != string(socketContent(Port(env))) {
			return step.Todo("%s is missing or different", SocketDropIn), nil
		}
	}
	if err := verify(ctx, env.Sys, env, pa, socket); err != nil {
		return step.Todo("%s", err.Error()), nil
	}
	return step.Done(fmt.Sprintf("root login %s, password logins %s, port %d, %d keys", map[string]string{"yes": "on", "no": "off"}[rootLogin(env)], map[string]string{"yes": "on", "no": "off"}[pa], Port(env), len(have))), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	want, skipped, err := desired(ctx, env)
	if err != nil {
		return nil, err
	}
	for _, sk := range skipped {
		env.Notes.Add("SSH keys: skipped %s.", sk)
	}
	akPath := authorizedKeys(env)
	_, have := installed(env.Sys, akPath)
	var missing []wanted
	for _, w := range want {
		if !have[w.key.id()] {
			missing = append(missing, w)
		}
	}
	predicted := len(have) + len(missing)
	socket := SocketMode(ctx, env.Sys)
	port := Port(env)

	if passwordAuth(env, predicted) == "yes" {
		who := env.User().Name
		if rootLogin(env) == "yes" {
			who += " and root"
		}
		msg := "SSH password logins are on for " + who
		if env.Opts.String("password_logins") == "auto" {
			msg += " because no SSH key is installed for " + env.User().Name
		}
		msg += ", from anywhere SSH is reachable (D45). The firewall keeps SSH open to the internet and fail2ban slows down guessing, but a guessed password gives root access. Use strong passwords, or turn password logins off in the SSH options."
		env.Notes.Warn("%s", msg)
		env.Notes.Add("%s", msg)
	} else if predicted == 0 {
		env.Notes.Warn("SSH password logins will be off, but no SSH key is installed for %s, so nobody can log in to this machine over SSH. Add a GitHub user or a public key in the SSH options, or set password logins to \"Off once at least one key is installed\".", env.User().Name)
	}
	if !slices.Contains(CurrentPorts(ctx, env.Sys), port) {
		if who, taken := portTaken(ctx, env.Sys, port); taken {
			env.Notes.Warn("Port %d is already in use (%s), so sshd can't listen there and the SSH step will fail. Choose another port in the SSH options.", port, who)
		}
	}
	for f, ps := range portLines(env.Sys) {
		if port != 22 && !socket {
			env.Notes.Warn("%s has Port %v; Port lines add up, so sshd will also listen there. kiln leaves that file alone.", f, ps)
		}
	}
	env.Notes.Add("SSH allows %d authentication attempts per connection (MaxAuthTries). If an agent offers more keys than that, set `IdentitiesOnly yes` and `IdentityFile` for this host in the client's ~/.ssh/config.", env.Opts.Int("max_auth_tries"))
	env.Notes.Add("To reach SSH from the internet, forward port %d on your router to this machine yourself; kiln doesn't change the router.", port)

	var acts []action.Action
	if !apt.Installed(ctx, env.Sys, "openssh-server") {
		acts = append(acts, action.AptInstall{Pkgs: []string{"openssh-server"}})
	}
	if !socket {
		if r, _ := env.Sys.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-enabled", "ssh.service"}}); strings.TrimSpace(r.Stdout) != "enabled" {
			acts = append(acts, action.Systemctl{Verb: "enable", Units: []string{"ssh.service"}, Now: true})
		}
	}
	if rootNeedsPassword(env) {
		u := env.User().Name
		acts = append(acts, action.Custom{
			Desc: fmt.Sprintf("if root has no password, give it the same password as %s by copying %s's password hash from /etc/shadow (chpasswd -e, hash on stdin, never logged)", u, u),
			K:    action.Commands,
			Fn: func(ctx context.Context, s sys.System) error {
				rh, ok := shadowHash(s, "root")
				if s.DryRun() || !ok || !noPassword(rh) {
					return nil
				}
				uh, ok := shadowHash(s, u)
				if !ok || noPassword(uh) {
					env.Notes.Add("root has no password and %s has none either, so root can't log in with a password. Set one with `sudo passwd root`.", u)
					return nil
				}
				if _, err := s.Run(ctx, sys.Cmd{Name: "chpasswd", Args: []string{"-e"}, Stdin: []byte("root:" + uh + "\n"), Redact: []string{uh}, Quiet: true}); err != nil {
					return fmt.Errorf("couldn't set root's password: %w", err)
				}
				env.Notes.Add("root had no password, so it now has the same password as %s.", u)
				return nil
			},
		})
	}
	if len(missing) > 0 {
		acts = append(acts, action.Custom{
			Desc: fmt.Sprintf("add %d public key(s) to %s (each checked with ssh-keygen -l first; existing lines are kept, 0600, directory 0700, owned by %s)", len(missing), akPath, env.User().Name),
			K:    action.Files,
			Fn:   func(ctx context.Context, s sys.System) error { return mergeKeys(ctx, s, env, akPath, missing) },
		})
	}
	desc := fmt.Sprintf("write %s (PermitRootLogin %s, MaxAuthTries %d, X11Forwarding no, PasswordAuthentication %s", DropIn, rootLogin(env), env.Opts.Int("max_auth_tries"), passwordAuth(env, predicted))
	if port != 22 {
		if socket {
			desc += fmt.Sprintf("; port %d in %s", port, SocketDropIn)
		} else {
			desc += fmt.Sprintf(", Port %d", port)
		}
	}
	desc += fmt.Sprintf("), check that port %d is free and the config passes sshd -t, apply it, then wait up to %s for sshd to listen on port %d with the expected sshd -T -C values; put the previous files back if anything fails, and once it works close any SSH port sshd no longer uses in the firewall", port, listenDeadline, port)
	acts = append(acts, action.Custom{
		Desc: desc,
		K:    action.Services,
		Fn: func(ctx context.Context, s sys.System) error {
			pa := passwordAuth(env, predicted)
			if !s.DryRun() {
				_, now := installed(s, akPath)
				pa = passwordAuth(env, len(now))
			}
			return applyConfig(ctx, s, env, pa, socket)
		},
	})
	return acts, nil
}

func mergeKeys(ctx context.Context, s sys.System, env *step.Env, path string, missing []wanted) error {
	raw, have := installed(s, path)
	var add []string
	for _, w := range missing {
		if have[w.key.id()] {
			continue
		}
		if !s.DryRun() && !validWithKeygen(ctx, s, w.key) {
			env.Notes.Add("SSH keys: skipped a %s key that ssh-keygen rejected.", w.key.typ)
			continue
		}
		add = append(add, w.key.line+" "+w.tag)
		have[w.key.id()] = true
	}
	if len(add) == 0 {
		return nil
	}
	u := env.User()
	if err := s.MkdirAll(filepath.Dir(path), 0o700, u); err != nil {
		return err
	}
	if raw != "" && !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	_, err := s.WriteFile(path, []byte(raw+strings.Join(add, "\n")+"\n"), sys.WriteOpts{Mode: 0o600, Owner: u})
	return err
}

// applyConfig is the verify-then-revert sequence (catalog step 15.4).
func applyConfig(ctx context.Context, s sys.System, env *step.Env, pa string, socket bool) error {
	port := Port(env)
	before := CurrentPorts(ctx, s)
	// A port another program holds would make sshd fail when it re-executes.
	if !s.DryRun() && !slices.Contains(before, port) {
		if who, taken := portTaken(ctx, s, port); taken {
			return fmt.Errorf("port %d is already in use (%s), so sshd couldn't listen there; nothing was changed. Choose another port in the SSH options", port, who)
		}
	}
	paths := []string{DropIn}
	// reload-or-restart also starts sshd again if the change made it exit.
	restore := [][]string{{"systemctl", "reload-or-restart", "ssh"}}
	if socket {
		paths = append(paths, SocketDropIn)
		restore = [][]string{{"systemctl", "daemon-reload"}, {"systemctl", "restart", "ssh.socket"}}
	}
	if err := revert.Begin(ctx, s, env.State, "ssh", "SSH server settings", paths, restore); err != nil {
		return fmt.Errorf("couldn't record the SSH change before making it: %w", err)
	}
	undo := func(cause error) error {
		if uerr := revert.Undo(ctx, s, env.State); uerr != nil {
			return fmt.Errorf("%w; putting the previous SSH settings back also failed: %w. Restore %s from %s and run `systemctl reload-or-restart ssh`", cause, uerr, DropIn, sys.BackupPath(DropIn))
		}
		return fmt.Errorf("%w. The previous SSH settings were put back", cause)
	}
	if _, err := s.WriteFile(DropIn, dropInContent(env, pa, socket), sys.WriteOpts{Mode: 0o644}); err != nil {
		return undo(err)
	}
	if socket {
		if port != 22 {
			if _, err := s.WriteFile(SocketDropIn, socketContent(port), sys.WriteOpts{Mode: 0o644}); err != nil {
				return undo(err)
			}
		} else if _, err := s.Lstat(SocketDropIn); err == nil {
			if err := s.Remove(SocketDropIn); err != nil {
				return undo(err)
			}
		}
	}
	if !s.DryRun() {
		if _, err := s.Query(ctx, sys.Cmd{Name: "sshd", Args: []string{"-t"}}); err != nil {
			return undo(fmt.Errorf("the new SSH settings didn't pass sshd -t: %w", err))
		}
	}
	var applyCmds [][]string
	if socket {
		applyCmds = [][]string{{"systemctl", "daemon-reload"}, {"systemctl", "restart", "ssh.socket"}}
	} else {
		applyCmds = [][]string{{"systemctl", "reload", "ssh"}}
	}
	for _, c := range applyCmds {
		if _, err := s.Run(ctx, sys.Cmd{Name: c[0], Args: c[1:]}); err != nil {
			return undo(fmt.Errorf("%s failed: %w", strings.Join(c, " "), err))
		}
	}
	if s.DryRun() {
		return nil
	}
	if err := revert.Poll(ctx, listenDeadline, time.Second, func() error { return verify(ctx, s, env, pa, socket) }); err != nil {
		return undo(fmt.Errorf("SSH didn't come back as expected: %w", err))
	}
	if err := revert.Commit(s, env.State); err != nil {
		return err
	}
	// The firewall kept the old port open until now; close what sshd no longer uses.
	if FirewallSync != nil {
		if err := FirewallSync(ctx, s, env, before); err != nil {
			env.Notes.Add("SSH now works on port %d, but closing the SSH port(s) it no longer uses in the firewall failed: %v. Run kiln again to retry.", port, err)
		}
	}
	return nil
}
