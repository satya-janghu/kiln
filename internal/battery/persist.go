package battery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Files kiln installs for persistence (catalog step 13, "Persistence").
const (
	ServicePath      = "/etc/systemd/system/kiln-battery.service"
	TimerPath        = "/etc/systemd/system/kiln-battery.timer"
	WatchServicePath = "/etc/systemd/system/kiln-battery-watch.service"
	UdevRulePath     = "/etc/udev/rules.d/90-kiln-battery.rules"
	HwdbPath         = "/etc/udev/hwdb.d/61-kiln-battery.hwdb"

	ServiceUnit      = "kiln-battery.service"
	TimerUnit        = "kiln-battery.timer"
	WatchServiceUnit = "kiln-battery-watch.service"
)

// ServiceContent is kiln-battery.service: a oneshot without RemainAfterExit, so
// every later start (boot, udev, timer) runs the apply again.
func ServiceContent() []byte {
	return []byte(sys.ManagedHeader("#") + `[Unit]
Description=kiln: re-apply the battery charge limit

[Service]
Type=oneshot
ExecStart=/usr/local/bin/kiln battery apply

[Install]
WantedBy=multi-user.target
`)
}

// TimerContent is the 15-minute safety net: threshold writes by anything else emit no uevent.
func TimerContent() []byte {
	return []byte(sys.ManagedHeader("#") + `[Unit]
Description=kiln: re-apply the battery charge limit every 15 minutes

[Timer]
OnCalendar=*:0/15
Unit=kiln-battery.service

[Install]
WantedBy=timers.target
`)
}

// WatchServiceContent is the software fallback (method 3).
func WatchServiceContent() []byte {
	return []byte(sys.ManagedHeader("#") + `[Unit]
Description=kiln: software battery charge limit (inhibits charging at the limit)

[Service]
ExecStart=/usr/local/bin/kiln battery watch
Restart=on-failure
RestartSec=30

[Install]
WantedBy=multi-user.target
`)
}

// UdevRuleContent starts the apply on charger changes and battery hot-plug.
// ENV{SYSTEMD_WANTS} can't be used: device Wants act only on first activation.
func UdevRuleContent() []byte {
	return []byte(sys.ManagedHeader("#") +
		`SUBSYSTEM=="power_supply", ACTION=="change", ATTR{type}=="Mains", RUN+="/usr/bin/systemctl --no-block start kiln-battery.service"` + "\n" +
		`SUBSYSTEM=="power_supply", ACTION=="add", ATTR{type}=="Battery", RUN+="/usr/bin/systemctl --no-block start kiln-battery.service"` + "\n")
}

// hwdbGlobSafe replaces hwdb glob metacharacters with '?', which still matches them.
func hwdbGlobSafe(v string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '*', '?', '[', ']', '\n', '\r':
			return '?'
		}
		return r
	}, v)
}

// HwdbContent aligns UPower's CHARGE_LIMIT (GNOME "Preserve Battery Health")
// with kiln's values. Key format (UPower 1.90.9): battery:<kernel name>:<model_name>:<dmi modalias>.
func hwdbContent(s sys.System, bats []thrBat, start, stop int) []byte {
	modalias, _ := readStr(s, "/sys/class/dmi/id/modalias")
	var b strings.Builder
	b.WriteString(sys.ManagedHeader("#"))
	b.WriteString("# Aligns UPower's charge limit (GNOME \"Preserve Battery Health\") with kiln's.\n")
	for _, bat := range bats {
		model, _ := readStr(s, bat.Path+"/model_name")
		fmt.Fprintf(&b, "battery:%s:%s:%s\n CHARGE_LIMIT=%d,%d\n\n", hwdbGlobSafe(bat.Name), hwdbGlobSafe(model), hwdbGlobSafe(modalias), start, stop)
	}
	return []byte(b.String())
}

// syncHwdb writes the hwdb file and, when it changed, rebuilds the hwdb,
// re-triggers power_supply devices and restarts UPower if it's running.
func syncHwdb(ctx context.Context, s sys.System, _ *facts.Facts, bats []thrBat, start, stop int) error {
	changed, err := s.WriteFile(HwdbPath, hwdbContent(s, bats, start, stop), sys.WriteOpts{Mode: 0o644})
	if err != nil {
		return fmt.Errorf("could not write %s: %w", HwdbPath, err)
	}
	if !changed {
		return nil
	}
	for _, c := range []sys.Cmd{
		{Name: "systemd-hwdb", Args: []string{"update"}, Timeout: 2 * time.Minute},
		{Name: "udevadm", Args: []string{"trigger", "-s", "power_supply"}, Timeout: time.Minute},
		{Name: "systemctl", Args: []string{"try-restart", "upower.service"}, Timeout: time.Minute},
	} {
		if _, err := s.Run(ctx, c); err != nil {
			return fmt.Errorf("could not make UPower use kiln's charge limit: %w", err)
		}
	}
	return nil
}

// EnsureWatch installs, enables and starts the software fallback service.
// The watcher reads the recorded values once, when it starts, so when they
// changed (valuesChanged) or the unit file changed it is stopped, charging is
// returned to auto and the watcher started again with the new values. The
// caller holds the battery lock.
func EnsureWatch(ctx context.Context, s sys.System, f *facts.Facts, valuesChanged bool) error {
	changed, err := s.WriteFile(WatchServicePath, WatchServiceContent(), sys.WriteOpts{Mode: 0o644})
	if err != nil {
		return err
	}
	if changed {
		if _, rerr := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"daemon-reload"}}); rerr != nil {
			return rerr
		}
	}
	if _, eerr := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"enable", WatchServiceUnit}}); eerr != nil {
		return eerr
	}
	if changed || valuesChanged {
		if _, serr := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"stop", WatchServiceUnit}}); serr != nil {
			return serr
		}
		// The stopping watcher can't reset charging itself: we hold the lock.
		if rerr := resetChargeBehaviour(ctx, s, f); rerr != nil {
			return rerr
		}
	}
	_, err = s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"start", WatchServiceUnit}})
	return err
}

// StopWatch disables the software fallback when another method took over.
// The caller holds the battery lock, so the stopping watcher can't take it to
// return charging to auto; StopWatch does that itself.
func StopWatch(ctx context.Context, s sys.System, f *facts.Facts) error {
	if !exists(s, WatchServicePath) {
		return nil
	}
	if _, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"disable", "--now", WatchServiceUnit}}); err != nil {
		return err
	}
	return resetChargeBehaviour(ctx, s, f)
}

// resetChargeBehaviour sets charge_behaviour back to auto on every system
// battery where something else is selected (e.g. inhibit-charge left by the
// software fallback).
func resetChargeBehaviour(ctx context.Context, s sys.System, f *facts.Facts) error {
	if f == nil {
		return nil
	}
	for _, b := range f.Batteries {
		p := b.Path + "/charge_behaviour"
		v, ok := readStr(s, p)
		if !ok || selectedChoice(v) == "auto" || !listedChoice(v, "auto") {
			continue
		}
		if err := writeAttr(ctx, s, p, "auto"); err != nil {
			return fmt.Errorf("the software charge limit was stopped, but charging on %s may still be paused: setting %s to auto failed (%w); run `echo auto | sudo tee %s`", b.Name, p, err, p)
		}
	}
	return nil
}
