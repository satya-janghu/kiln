package runner

import (
	"context"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/state"
	"github.com/satya-janghu/kiln/internal/sys"
)

// SummaryPath is the human summary (0644, no secrets).
const SummaryPath = "/var/log/kiln-summary.txt"

// quietUnits are stopped, awaited and runtime-masked during a run (D41).
var quietTimers = []string{"apt-daily.timer", "apt-daily-upgrade.timer"}
var quietServices = []string{"apt-daily.service", "apt-daily-upgrade.service", "packagekit.service"}

type lifecycle struct {
	r         *Runner
	inhibitor func()
	quiet     *QuietState
}

func newLifecycle(r *Runner) *lifecycle { return &lifecycle{r: r} }

func (l *lifecycle) phase(t string) { l.r.emit(Event{Kind: EvPhase, Text: t}) }

func (l *lifecycle) begin(ctx context.Context) {
	r := l.r
	if r.Sys.DryRun() {
		return
	}
	l.inhibitor = startInhibitor(r.Log)
	l.phase("Pausing automatic updates and GNOME Software for this run")
	q, err := Quiesce(ctx, r.Sys, func(t string) { l.phase(t) })
	l.quiet = q
	if err != nil {
		r.Notes.Add("Automatic updates couldn't be fully paused (%v); kiln waits for package locks instead.", err)
	}
	r.State.Run = &state.Run{Started: time.Now()}
	_ = r.State.Save(r.Sys)
}

func (l *lifecycle) record(stepID, title string) {
	r := l.r
	if r.Sys.DryRun() || r.State.Run == nil {
		return
	}
	r.State.Run.Step, r.State.Run.Action = stepID, title
	_ = r.State.Save(r.Sys)
}

// end runs the end-of-run tasks: one needrestart pass, then restore (D41).
func (l *lifecycle) end(ctx context.Context) {
	r := l.r
	if r.Sys.DryRun() {
		return
	}
	l.phase("Restarting services that use updated libraries")
	needrestartPass(ctx, r)
	if l.quiet != nil {
		l.phase("Turning automatic updates back on")
		if err := l.quiet.Restore(ctx, r.Sys); err != nil {
			r.Notes.Add("Automatic updates couldn't be re-enabled (%v). Reboot, or run: systemctl unmask --runtime apt-daily.timer apt-daily-upgrade.timer packagekit.service", err)
		}
	}
}

func (l *lifecycle) finish(ctx context.Context, sum *Summary) {
	r := l.r
	if l.inhibitor != nil {
		l.inhibitor()
	}
	if r.Sys.DryRun() {
		return
	}
	_, _ = r.Sys.WriteFile(SummaryPath, []byte(sum.Text(r.Facts.Hostname, r.Secrets.Values())), sys.WriteOpts{Mode: 0o644, NoBackup: true})
	r.State.LastRun = time.Now()
	r.State.KilnVersion = r.KilnVersion
	if r.State.Run != nil {
		r.State.Run.Finished = true
		r.State.Run.Result = "stopped"
		if !sum.Stopped {
			r.State.Run.Result = "completed"
		}
	}
	if err := r.State.Save(r.Sys); err != nil {
		r.Log.Error("state", "err", err.Error())
	}
	_, _ = r.Sys.Run(ctx, sys.Cmd{Name: "sync"})
}

// QuietState records exactly what Quiesce changed, so Restore undoes only that.
type QuietState struct {
	activeTimers []string // timers that were active and were stopped
	masked       bool
}

// Quiesce stops, waits for and runtime-masks other package managers (D41).
// It returns what it changed even when it fails partway, so it can be undone.
func Quiesce(ctx context.Context, s sys.System, say func(string)) (*QuietState, error) {
	q := &QuietState{}
	for _, t := range quietTimers {
		if r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-active", t}}); strings.TrimSpace(r.Stdout) == "active" {
			q.activeTimers = append(q.activeTimers, t)
		}
	}
	if len(q.activeTimers) > 0 {
		if _, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: append([]string{"stop"}, q.activeTimers...)}); err != nil {
			return q, err
		}
	}
	// Mask first so nothing new can start, then wait for what's running.
	args := append([]string{"mask", "--runtime"}, quietTimers...)
	if _, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: append(args, quietServices...)}); err != nil {
		return q, err
	}
	q.masked = true
	say("Waiting for any automatic update already in progress to finish")
	if err := apt.WaitForAutomatic(ctx, s); err != nil {
		return q, err
	}
	// PackageKit idles between transactions; stop it only when no transaction
	// holds the dpkg lock (it can't be re-activated while masked).
	if err := apt.WaitDpkgUnlocked(ctx, s); err != nil {
		return q, err
	}
	_, _ = s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"stop", "packagekit.service"}})
	return q, nil
}

// Restore removes the runtime masks and restarts only the timers that were active.
func (q *QuietState) Restore(ctx context.Context, s sys.System) error {
	if q.masked {
		args := append([]string{"unmask", "--runtime"}, quietTimers...)
		if _, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: append(args, quietServices...)}); err != nil {
			return err
		}
	}
	if len(q.activeTimers) > 0 {
		_, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: append([]string{"start"}, q.activeTimers...)})
		return err
	}
	return nil
}

// ClearStale removes runtime masks left by a crashed run and starts the timers
// that are enabled (a crash may have left them stopped).
func ClearStale(ctx context.Context, s sys.System) error {
	args := append([]string{"unmask", "--runtime"}, quietTimers...)
	if _, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: append(args, quietServices...)}); err != nil {
		return err
	}
	for _, t := range quietTimers {
		if r, _ := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"is-enabled", t}}); strings.TrimSpace(r.Stdout) == "enabled" {
			if _, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"start", t}}); err != nil {
				return err
			}
		}
	}
	return nil
}

// StaleMasks reports runtime masks left by a crashed run.
func StaleMasks(s sys.System) bool {
	for _, u := range append(append([]string{}, quietTimers...), quietServices...) {
		if t, err := s.Readlink("/run/systemd/system/" + u); err == nil && t == "/dev/null" {
			return true
		}
	}
	return false
}

func needrestartPass(ctx context.Context, r *Runner) {
	if _, err := r.Sys.Query(ctx, sys.Cmd{Name: "sh", Args: []string{"-c", "command -v needrestart"}}); err != nil {
		return
	}
	_, _ = r.Sys.Run(ctx, sys.Cmd{Name: "needrestart", Args: []string{"-r", "a", "-q"}, Env: []string{"NEEDRESTART_MODE=a", "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive"}, Timeout: 10 * time.Minute})
	res, err := r.Sys.Query(ctx, sys.Cmd{Name: "needrestart", Args: []string{"-b"}, Env: []string{"LC_ALL=C"}})
	if err != nil && res.Stdout == "" {
		return
	}
	var svcs []string
	for _, l := range strings.Split(res.Stdout, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "NEEDRESTART-KSTA":
			if v == "2" || v == "3" {
				r.Notes.Reboot("a newer kernel is installed")
			}
		case "NEEDRESTART-UCSTA":
			if v == "2" {
				r.Notes.Reboot("new CPU microcode")
			}
		case "NEEDRESTART-SVC":
			svcs = append(svcs, v)
		}
	}
	if len(svcs) > 0 {
		r.Notes.Add("These services use updated libraries and restart at the next reboot (Debian never restarts them automatically): %s", strings.Join(svcs, ", "))
	}
}

// Reboot flushes state and reboots. The inhibitor is already gone (finish ran).
func Reboot(ctx context.Context, s sys.System) error {
	_, _ = s.Run(ctx, sys.Cmd{Name: "sync"})
	_, err := s.Run(ctx, sys.Cmd{Name: "systemctl", Args: []string{"reboot"}})
	return err
}
