// Package rdp implements catalog step 19 `remote-desktop` (docs/steps-catalog.md):
// GNOME Remote Desktop in system mode (RDP at the login screen), LAN-only.
package rdp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/secrets"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

const (
	daemonUser = "gnome-remote-desktop"
	daemonHome = "/var/lib/gnome-remote-desktop"
	tlsDir     = daemonHome + "/.local/share/gnome-remote-desktop"
	tlsKey     = tlsDir + "/tls.key"
	tlsCert    = tlsDir + "/tls.crt"
	unit       = "gnome-remote-desktop.service"
	secretName = "rdp"
	// renewBefore: a certificate that expires within 30 days is replaced.
	renewBefore = "2592000"
)

var packages = []string{"gnome-remote-desktop", "polkitd"}

// Step is the `remote-desktop` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "remote-desktop" }
func (Step) Title() string       { return "Remote desktop (RDP at the login screen)" }
func (Step) Category() string    { return "apps" }
func (Step) DependsOn() []string { return []string{"firewall"} }

func (Step) Packages() []step.PackageRef {
	out := make([]step.PackageRef, 0, len(packages))
	for _, p := range packages {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "main", Repo: "debian"})
	}
	return out
}

var rdpUserRE = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._-]*$`)

func (Step) Options(f *facts.Facts) []options.Field {
	def := ""
	if f != nil && f.User != nil {
		def = f.User.Name
	}
	return []options.Field{
		{Key: "username", Label: "RDP user name", Kind: options.String, Default: def,
			Help: "The name you type in the RDP client. It doesn't have to be a Linux account.",
			Validate: func(v any) error {
				if s, _ := v.(string); !rdpUserRE.MatchString(s) {
					return errors.New("use letters, digits, '.', '_' or '-'")
				}
				return nil
			}},
		{Key: "password", Label: "RDP password", Kind: options.Secret, Default: "",
			Help: "Leave empty to generate one (shown once; `kiln secrets show rdp` shows it again). Stored in /etc/kiln/secrets/rdp.",
			Validate: func(v any) error {
				if s, _ := v.(string); strings.ContainsAny(s, "\n\r") {
					return errors.New("the password can't contain line breaks")
				}
				return nil
			}},
	}
}

func (Step) Applies(env *step.Env) step.Applicability {
	if !env.Facts.GNOME {
		return step.No("no GNOME desktop installed")
	}
	return step.Yes()
}

// grdctl runs grdctl as the daemon user without --system, so exit codes are real
// (grdctl enters system mode by itself when euid is gnome-remote-desktop).
func grdctl(args ...string) sys.Cmd {
	return asDaemon("grdctl", append([]string{"rdp"}, args...)...)
}

func asDaemon(name string, args ...string) sys.Cmd {
	a := []string{"-u", daemonUser, "--", "env", "-i", "HOME=" + daemonHome, "PATH=/usr/bin:/bin", name}
	return sys.Cmd{Name: "runuser", Args: append(a, args...), Dir: daemonHome}
}

// status is the parsed RDP part of `grdctl status`.
type status struct {
	enabled            bool
	cert, key          string
	username, password string
}

func readStatus(ctx context.Context, s sys.System) (status, error) {
	c := asDaemon("grdctl", "status")
	r, err := s.Query(ctx, c)
	if err != nil {
		return status{}, err
	}
	var st status
	inRDP := false
	for _, l := range strings.Split(r.Stdout, "\n") {
		if !strings.HasPrefix(l, "\t") {
			inRDP = strings.TrimSpace(l) == "RDP:"
			continue
		}
		if !inRDP {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimSpace(l), ": ")
		if !ok {
			continue
		}
		switch k {
		case "Status":
			st.enabled = v == "enabled"
		case "TLS certificate":
			st.cert = v
		case "TLS key":
			st.key = v
		case "Username":
			st.username = v
		case "Password":
			st.password = v
		}
	}
	return st, nil
}

// certFresh reports whether key and cert exist and the cert is valid for more than 30 days.
func certFresh(ctx context.Context, s sys.System) bool {
	if _, err := s.Stat(tlsKey); err != nil {
		return false
	}
	if _, err := s.Stat(tlsCert); err != nil {
		return false
	}
	_, err := s.Query(ctx, sys.Cmd{Name: "openssl", Args: []string{"x509", "-checkend", renewBefore, "-noout", "-in", tlsCert}})
	return err == nil
}

// storedPassword returns the saved secret, or "" if there is none.
func storedPassword(s sys.System) string {
	v, err := secrets.Load(s, secretName)
	if err != nil {
		return ""
	}
	return v
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	if ok, missing := apt.AllInstalled(ctx, env.Sys, packages); !ok {
		return step.Todo("not installed yet: %s", strings.Join(missing, ", ")), nil
	}
	if !certFresh(ctx, env.Sys) {
		return step.Todo("the RDP TLS certificate is missing or expires within 30 days"), nil
	}
	st, err := readStatus(ctx, env.Sys)
	if err != nil {
		return step.Todo("couldn't read `grdctl status`: %v", err), nil
	}
	switch {
	case !st.enabled:
		return step.Todo("RDP isn't enabled"), nil
	case st.cert != tlsCert || st.key != tlsKey:
		return step.Todo("RDP doesn't use kiln's TLS certificate and key"), nil
	case st.username != "(hidden)" || st.password != "(hidden)":
		return step.Todo("RDP credentials aren't set"), nil
	}
	if _, err := env.Sys.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-enabled", unit}}); err != nil {
		return step.Todo("%s isn't enabled", unit), nil
	}
	// The password can't be read back from GNOME; compare kiln's hash marker instead.
	stored := storedPassword(env.Sys)
	marker := env.State.SecretHashes[secretName]
	if stored == "" || marker == "" || secrets.Hash(stored) != marker {
		return step.Todo("the RDP password kiln stored doesn't match what was set"), nil
	}
	if want := env.Opts.String("password"); want != "" && secrets.Hash(want) != marker {
		return step.Todo("the RDP password changed"), nil
	}
	// grdctl shows the user name only as "(hidden)", so kiln keeps a marker for it too.
	if env.State.SecretHashes[secretName+"-user"] != secrets.Hash(env.Opts.String("username")) {
		return step.Todo("the RDP user name changed"), nil
	}
	return step.Done("RDP enabled for " + env.Opts.String("username") + " on port 3389"), nil
}

// password resolves the password: the option, else one already known this run,
// else the stored one (so a re-run doesn't change it), else a generated one.
func password(env *step.Env) (string, bool, error) {
	if v := env.Opts.String("password"); v != "" {
		env.Secrets.Add(secretName, v)
		return v, false, nil
	}
	if v := env.Secrets.Get(secretName); v != "" {
		_, gen := env.Secrets.Generated()[secretName]
		return v, gen, nil
	}
	if v := storedPassword(env.Sys); v != "" {
		env.Secrets.Add(secretName, v)
		return v, false, nil
	}
	v, err := env.Secrets.GenerateAndRegister(secretName, 20)
	return v, true, err
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	user := env.Opts.String("username")
	if user == "" {
		return nil, errors.New("no RDP user name is set; enter one in the remote-desktop options")
	}
	pw, generated, err := password(env)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate an RDP password: %w", err)
	}

	var acts []action.Action
	if ok, missing := apt.AllInstalled(ctx, env.Sys, packages); !ok {
		acts = append(acts, action.AptInstall{Pkgs: missing})
	}
	if !certFresh(ctx, env.Sys) {
		host := env.Facts.Hostname
		if host == "" {
			host = "localhost"
		}
		acts = append(acts,
			action.Run{Desc: "create " + tlsDir + " as " + daemonUser, K: action.Files,
				Cmd: asDaemon("mkdir", "-p", tlsDir)},
			action.Run{Desc: "create a self-signed RSA-4096 TLS certificate for /CN=" + host + ", valid 10 years, as " + daemonUser,
				Cmd: asDaemon("openssl", "req", "-x509", "-newkey", "rsa:4096", "-noenc", "-days", "3650",
					"-subj", "/CN="+host, "-keyout", tlsKey, "-out", tlsCert)},
			action.Run{Desc: "set " + tlsKey + " to mode 0600", K: action.Files,
				Cmd: asDaemon("chmod", "0600", tlsKey)},
		)
	}
	acts = append(acts,
		action.Run{Desc: "grdctl rdp set-tls-key " + tlsKey, Cmd: grdctl("set-tls-key", tlsKey)},
		action.Run{Desc: "grdctl rdp set-tls-cert " + tlsCert, Cmd: grdctl("set-tls-cert", tlsCert)},
		action.Custom{
			Desc: "store the RDP password in " + secrets.Path(secretName) + " (0600), then set the RDP credentials for " + user + " (grdctl rdp set-credentials, password on stdin only)",
			Fn: func(ctx context.Context, s sys.System) error {
				if err := secrets.Save(s, secretName, pw); err != nil {
					return fmt.Errorf("couldn't store the RDP password: %w", err)
				}
				c := grdctl("set-credentials", user)
				c.Stdin = []byte(pw + "\n")
				c.Redact = []string{pw}
				if _, err := s.Run(ctx, c); err != nil {
					return fmt.Errorf("couldn't set the RDP credentials: %w", err)
				}
				if !s.DryRun() {
					if env.State.SecretHashes == nil {
						env.State.SecretHashes = map[string]string{}
					}
					env.State.SecretHashes[secretName] = secrets.Hash(pw)
					env.State.SecretHashes[secretName+"-user"] = secrets.Hash(user)
				}
				return nil
			},
		},
		action.Run{Desc: "grdctl rdp enable (enables and starts " + unit + ")", K: action.Services, Cmd: grdctl("enable")},
		action.Systemctl{Verb: "enable", Now: true, Units: []string{unit}},
	)

	if generated {
		env.Notes.Add("A new RDP password was generated. It's shown once at the end of the run; `sudo kiln secrets show rdp` shows it again.")
	}
	ip := env.Facts.PrimaryIPv4()
	if ip == "" {
		ip = "<lan-ip>"
	}
	env.Notes.Add("Remote desktop: connect an RDP client to %s:3389 as %s (LAN only, firewall zone kiln-lan). The login screen appears first.", ip, user)
	return acts, nil
}
