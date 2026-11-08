package steps

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/run"
)

const fail2banJail = "/etc/fail2ban/jail.d/50-kiln.local"

func doFirewall(c *Ctx) error {
	ports := []string{"22/tcp"}
	if c.Picked("cockpit") {
		ports = append(ports, "9090/tcp")
	}

	// UFW. Rules may be added before enabling; `--force` skips the prompt,
	// and enabling sets ENABLED=yes so ufw.service loads the rules at boot.
	if err := c.Install("ufw"); err != nil {
		return err
	}
	cmds := [][]string{{"default", "deny", "incoming"}, {"default", "allow", "outgoing"}}
	for _, p := range ports {
		cmds = append(cmds, []string{"allow", p})
	}
	cmds = append(cmds, []string{"--force", "enable"})
	for _, a := range cmds {
		if err := c.Run("ufw", a...); err != nil {
			return fmt.Errorf("`ufw %s` failed: %w", strings.Join(a, " "), err)
		}
	}
	if err := ufwCheck(c, ports); err != nil {
		return err
	}
	if c.Picked("docker") {
		c.Note("Ports Docker publishes bypass UFW (Docker's docs), so Portainer's 9443 and any `-p` port are open to every network. Publish as `127.0.0.1:<port>:<port>` to keep one private.")
	}

	// fail2ban only with the SSH checkbox (owner's answer, D54). UFW is
	// enabled above, which the ufw ban action needs.
	if !c.Picked("ssh") {
		return nil
	}
	if err := c.Install("fail2ban", "python3-systemd"); err != nil {
		return err
	}
	ignore := append([]string{"127.0.0.1/8", "::1"}, c.F.LANSubnets...)
	jail := "# Managed by kiln\n" +
		"[DEFAULT]\n" +
		"ignoreip = " + strings.Join(ignore, " ") + "\n" +
		"bantime = 1h\n" +
		"bantime.increment = true\n" +
		"maxretry = 5\n" +
		"banaction = ufw\n" +
		"banaction_allports = ufw\n" +
		"\n" +
		"[sshd]\n" +
		"enabled = true\n" +
		"backend = systemd\n" +
		"port = 22\n" +
		"banaction = ufw\n" +
		"journalmatch = _SYSTEMD_UNIT=ssh.service + _COMM=sshd + _COMM=sshd-session\n"
	if err := c.Write(fail2banJail, 0o644, jail); err != nil {
		return err
	}
	if err := c.Run("systemctl", "restart", "fail2ban"); err != nil {
		return fmt.Errorf("restarting fail2ban failed: %w. See `journalctl -u fail2ban`", err)
	}
	err := pollFor(c, 30*time.Second, func() error {
		if out, err := c.Out("fail2ban-client", "ping"); err != nil || !strings.Contains(out, "pong") {
			return errors.New("fail2ban doesn't answer `fail2ban-client ping`")
		}
		if !c.OK("fail2ban-client", "status", "sshd") {
			return errors.New("fail2ban's sshd jail isn't running")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%w; see `journalctl -u fail2ban` and check %s", err, fail2banJail)
	}
	return nil
}

// ufwCheck reads `ufw status verbose` (ufw 0.36.2 backend_iptables.py
// get_status): "Status: active", "Default: deny (incoming), allow
// (outgoing), …", and one "22/tcp  ALLOW IN  Anywhere" line per rule.
func ufwCheck(c *Ctx, ports []string) error {
	out, err := c.Out("ufw", "status", "verbose")
	if err != nil {
		return fmt.Errorf("`ufw status verbose` failed: %w", err)
	}
	if !strings.Contains(out, "Status: active") {
		return errors.New("UFW isn't active after `ufw --force enable`; see `journalctl -u ufw`")
	}
	if !strings.Contains(out, "deny (incoming)") || !strings.Contains(out, "allow (outgoing)") {
		return errors.New("UFW's defaults aren't deny incoming and allow outgoing; check /etc/default/ufw")
	}
	for _, p := range ports {
		found := false
		for _, l := range run.Scan(out) {
			if f := strings.Fields(l); len(f) >= 3 && f[0] == p && f[1] == "ALLOW" && f[2] == "IN" {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("UFW has no rule allowing %s; run `ufw allow %s`", p, p)
		}
	}
	return nil
}
