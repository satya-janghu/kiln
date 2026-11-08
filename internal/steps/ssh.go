package steps

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/run"
)

const sshDropIn = "/etc/ssh/sshd_config.d/10-kiln.conf"

// sshWant is the drop-in's settings as `sshd -T` prints them (lowercase keys).
var sshWant = [][2]string{
	{"permitrootlogin", "yes"},
	{"passwordauthentication", "yes"},
	{"maxauthtries", "4"},
	{"x11forwarding", "no"},
}

const sshDropInContent = `# Managed by kiln
PermitRootLogin yes
PasswordAuthentication yes
MaxAuthTries 4
X11Forwarding no
`

// shadowField returns a user's password field from /etc/shadow.
func shadowField(user string) (string, bool) {
	for _, l := range strings.Split(run.Read("/etc/shadow"), "\n") {
		f := strings.Split(l, ":")
		if len(f) > 1 && f[0] == user {
			return f[1], true
		}
	}
	return "", false
}

// noPassword reports a locked or empty password field.
func noPassword(h string) bool {
	return h == "" || strings.HasPrefix(h, "!") || strings.HasPrefix(h, "*")
}

func doSSH(c *Ctx) error {
	u := c.U.Name
	if err := c.Install("openssh-server"); err != nil {
		return err
	}
	if err := c.Run("systemctl", "enable", "--now", "ssh.service"); err != nil {
		return fmt.Errorf("starting the SSH server failed: %w. See `journalctl -u ssh`", err)
	}

	// Root gets the user's password if it has none (D45). The hash goes on
	// stdin only and is kept out of the log.
	if rh, ok := shadowField("root"); ok && noPassword(rh) {
		if uh, ok := shadowField(u); ok && !noPassword(uh) {
			if _, err := c.Exec(run.Cmd{Name: "chpasswd", Args: []string{"-e"}, Stdin: "root:" + uh + "\n", Quiet: true}); err != nil {
				return fmt.Errorf("giving root a password failed: %w. Set one with `passwd root`", err)
			}
			c.Note("root had no password, so it now has the same password as %s.", u)
		} else {
			c.Note("root has no password and %s has none either, so root can't log in with a password. Set one with `passwd root`.", u)
		}
	}

	if err := c.Write(sshDropIn, 0o644, sshDropInContent); err != nil {
		return err
	}
	if err := c.Run("sshd", "-t"); err != nil {
		_ = c.Remove(sshDropIn)
		return fmt.Errorf("the SSH settings didn't pass `sshd -t`, so kiln removed its file and left SSH as it was: %w", err)
	}
	if err := c.Run("systemctl", "reload-or-restart", "ssh"); err != nil {
		return fmt.Errorf("reloading the SSH server failed: %w. See `journalctl -u ssh`", err)
	}

	// Check, polled: sshd re-execs on reload, so it may briefly not listen.
	if err := pollFor(c, 15*time.Second, func() error { return sshCheck(c) }); err != nil {
		return fmt.Errorf("SSH didn't come back as expected: %w", err)
	}
	c.Note("SSH: password logins for %s and root are open wherever SSH is reachable. Use strong passwords.", u)
	return nil
}

func sshCheck(c *Ctx) error {
	if out, _ := c.Out("systemctl", "is-active", "ssh.service"); strings.TrimSpace(out) != "active" {
		return errors.New("ssh.service isn't active; see `journalctl -u ssh`")
	}
	out, err := c.Out("ss", "-H", "-ltn")
	if err != nil {
		return fmt.Errorf("`ss -ltn` failed: %w", err)
	}
	listening := false
	for _, l := range run.Scan(out) {
		if f := strings.Fields(l); len(f) >= 4 && strings.HasSuffix(f[3], ":22") {
			listening = true
		}
	}
	if !listening {
		return errors.New("nothing is listening on port 22")
	}
	addr, host := c.F.IP, c.F.Hostname
	if addr == "" {
		addr = "127.0.0.1"
	}
	if host == "" {
		host = "localhost"
	}
	conn := "user=" + c.U.Name + ",host=" + host + ",addr=" + addr
	out, err = c.Out("sshd", "-T", "-C", conn)
	if err != nil {
		return fmt.Errorf("`sshd -T` failed: %w", err)
	}
	got := map[string]string{}
	for _, l := range run.Scan(out) {
		if k, v, ok := strings.Cut(l, " "); ok {
			if _, seen := got[k]; !seen {
				got[k] = strings.TrimSpace(v)
			}
		}
	}
	for _, kv := range sshWant {
		if got[kv[0]] != kv[1] {
			return fmt.Errorf("`sshd -T -C %s` shows %s %q, not %q; another file in /etc/ssh/sshd_config.d or a Match block overrides %s", conn, kv[0], got[kv[0]], kv[1], sshDropIn)
		}
	}
	return nil
}
