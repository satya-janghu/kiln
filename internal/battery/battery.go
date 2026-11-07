// Package battery implements the charge-limit method ladder (catalog step 13),
// shared by the battery step and `kiln battery status|apply|watch`.
// All sysfs reads and writes go through sys.System (decisions: "Domain
// packages read sysfs through sys.System").
package battery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/runner"
	"github.com/satya-janghu/kiln/internal/state"
	"github.com/satya-janghu/kiln/internal/sys"
)

// LockName is /run/kiln/battery.lock, shared by the step and the helpers (D41).
const LockName = "battery"

// Lock takes the battery lock, waiting up to wait for a helper to finish.
func Lock(ctx context.Context, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	for {
		release, pid, err := runner.Lock(LockName)
		if err == nil {
			return release, nil
		}
		busy := strings.Contains(err.Error(), "busy") // runner.Lock: another process holds it
		if !busy || time.Now().After(deadline) {
			if busy {
				who := "another kiln battery helper"
				if pid > 0 {
					who += fmt.Sprintf(" (pid %d)", pid)
				}
				return nil, fmt.Errorf("%s is still changing the charge limit; wait a minute and run kiln again", who)
			}
			return nil, fmt.Errorf("could not take %s/%s.lock: %w", runner.LockDir, LockName, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// OptionsFromState returns the wizard's battery options as saved for this
// machine (only non-default values are stored; the rest are defaults).
func OptionsFromState(st *state.State) Options {
	o := DefaultOptions()
	if st == nil {
		return o
	}
	num := func(v any) (int, bool) {
		switch t := v.(type) {
		case int:
			return t, true
		case float64:
			return int(t), true
		}
		return 0, false
	}
	if n, ok := num(st.Choices.Options["battery.target_stop"]); ok {
		o.Stop = n
	}
	if n, ok := num(st.Choices.Options["battery.start_gap"]); ok {
		o.Gap = n
	}
	if b, ok := st.Choices.Options["battery.software_fallback"].(bool); ok {
		o.Software = b
	}
	return o
}

// loggerFor logs through the Real system's logger when there is one.
func loggerFor(s sys.System) Logf {
	l := slog.Default()
	if r, ok := s.(*sys.Real); ok && r.Log != nil {
		l = r.Log
	}
	return func(format string, a ...any) { l.Info("battery: " + fmt.Sprintf(format, a...)) }
}

// Status describes batteries, interfaces, values and the owner (kiln battery status).
func Status(ctx context.Context, s sys.System, f *facts.Facts, st *state.State) (string, error) {
	var b strings.Builder
	if len(f.Batteries) == 0 {
		b.WriteString("No system battery found (peripheral batteries are ignored).\n")
		return b.String(), nil
	}
	b.WriteString("Batteries:\n")
	for _, bat := range f.Batteries {
		capacity, _ := readStr(s, bat.Path+"/capacity")
		status, _ := readStr(s, bat.Path+"/status")
		model, _ := readStr(s, bat.Path+"/model_name")
		fmt.Fprintf(&b, "  %s (%s): %s%%, %s, model %q\n", bat.Name, bat.Path, orDash(capacity), orDash(status), model)
		for _, attr := range []string{"charge_control_start_threshold", "charge_control_end_threshold", "charge_behaviour", "charge_types"} {
			if v, ok := readStr(s, bat.Path+"/"+attr); ok {
				fmt.Fprintf(&b, "    %s: %s\n", attr, v)
			}
		}
	}
	p := probe(s, f)
	if len(p.Fixed) > 0 {
		b.WriteString("Vendor fixed modes:\n")
		for _, h := range p.Fixed {
			v, _ := readStr(s, h.Path)
			fmt.Fprintf(&b, "  %s: %s = %s\n", h.Mode.Name, h.Path, v)
		}
	}
	b.WriteString("Probed:\n")
	for _, l := range p.Probed {
		b.WriteString("  " + l + "\n")
	}
	o := OptionsFromState(st)
	rec := (*state.Battery)(nil)
	if st != nil {
		rec = st.Battery
	}
	if rec == nil || rec.Method == "" {
		fmt.Fprintf(&b, "Accepted values: none yet (kiln's battery step hasn't run). It would start with: %s\n", p.predict(o))
		b.WriteString("Owner: not managed by kiln\n")
	} else {
		b.WriteString("Accepted values (last apply): " + describe(rec) + "\n")
		ok, detail := Verify(s, f, rec, o)
		effect := "in effect"
		if !ok {
			effect = "NOT in effect"
		}
		fmt.Fprintf(&b, "Active method: %s, %s (%s)\n", rec.Method, effect, detail)
		b.WriteString("Owner: kiln (re-applied at boot, on charger changes and every 15 minutes)\n")
	}
	for _, bat := range f.Batteries {
		r, err := s.Query(ctx, sys.Cmd{Name: "busctl", Args: []string{"get-property", "org.freedesktop.UPower",
			"/org/freedesktop/UPower/devices/battery_" + bat.Name, "org.freedesktop.UPower.Device", "ChargeThresholdEnabled"}, Timeout: 10 * time.Second})
		v := strings.TrimPrefix(strings.TrimSpace(r.Stdout), "b ")
		if err != nil || v == "" {
			v = "unknown (UPower not running or no such device)"
		}
		fmt.Fprintf(&b, "UPower ChargeThresholdEnabled (%s): %s\n", bat.Name, v)
	}
	if exists(s, HwdbPath) {
		b.WriteString("UPower's limit is aligned with kiln's through " + HwdbPath + ". Leave GNOME's \"Preserve Battery Health\" switch alone.\n")
	}
	return b.String(), nil
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func describe(rec *state.Battery) string {
	switch {
	case rec.Method == MethodThresholds:
		s := fmt.Sprintf("kernel thresholds on %s: start %d%%, stop %d%%", rec.Battery, rec.Start, rec.Stop)
		if st, gap, ok := requested(rec); ok {
			s += fmt.Sprintf(" (requested stop %d%%, gap %d)", st, gap)
		}
		if unverified(rec) {
			s += ", set, not verifiable (the driver caches the value)"
		}
		return s
	case strings.HasPrefix(rec.Method, vendorPrefix):
		return fmt.Sprintf("%s: %s = %s", fixedLabel(rec.Method), rec.Path, rec.Value)
	case rec.Method == MethodWatch:
		return fmt.Sprintf("software fallback on %s: %s at %d%%, auto at %d%%", rec.Battery, rec.Value, rec.Stop, rec.Start)
	case rec.Method == MethodNone:
		return "none: no charge-limit method works on this machine"
	}
	return rec.Method
}

// Describe is the plain-language form of a recorded result (summary, status).
func Describe(rec *state.Battery) string {
	if rec == nil {
		return "none"
	}
	return describe(rec)
}

// ApplyRecorded re-applies the recorded method (kiln battery apply, used at
// boot, on udev power events and by the timer). The caller holds the battery
// lock. It starts from the recorded method and falls back to the full ladder
// if that method stops working, saving the new result.
func ApplyRecorded(ctx context.Context, s sys.System, f *facts.Facts, st *state.State) error {
	logf := loggerFor(s)
	if len(f.Batteries) == 0 {
		logf("no system battery; nothing to do")
		return nil
	}
	if st == nil {
		return errors.New("kiln's state file couldn't be read; run `sudo kiln` again")
	}
	rec := st.Battery
	if rec != nil && rec.Method != "" && rec.Method != MethodNone && reapply(ctx, s, f, rec, logf) {
		logf("re-applied: %s", describe(rec))
		return nil
	}
	if rec != nil && rec.Method != "" && rec.Method != MethodNone {
		logf("the recorded method (%s) no longer works; running the full ladder", rec.Method)
	}
	out, err := Run(ctx, s, f, OptionsFromState(st), logf)
	if err != nil {
		return err
	}
	valuesChanged := rec == nil || *rec != out.Rec
	if valuesChanged {
		r := out.Rec
		st.Battery = &r
		if err := st.Save(s); err != nil {
			return fmt.Errorf("the charge limit is set, but %s couldn't be saved: %w", state.Path, err)
		}
	}
	// After saving: the watcher reads the recorded method when it starts.
	if out.Rec.Method == MethodWatch {
		if err := EnsureWatch(ctx, s, f, valuesChanged); err != nil {
			return fmt.Errorf("could not start the software charge limit: %w", err)
		}
	} else if err := StopWatch(ctx, s, f); err != nil {
		return err
	}
	for _, n := range out.Notes {
		logf("%s", n)
	}
	logf("applied: %s", describe(&out.Rec))
	return nil
}

// Watch runs the software charge limit (kiln battery watch, a systemd service):
// every minute it reads the capacity, inhibits charging at or above the stop
// value and returns to auto at or below the start value (hysteresis). It logs
// only on transitions and takes the battery lock around each write.
func Watch(ctx context.Context, s sys.System, f *facts.Facts, st *state.State) error {
	if s.DryRun() {
		return nil
	}
	if st == nil || st.Battery == nil || st.Battery.Method != MethodWatch {
		return errors.New("the software charge limit isn't the method kiln chose for this machine; run `sudo kiln` again (battery step)")
	}
	rec := *st.Battery
	logf := loggerFor(s)
	batPath := filepath.Dir(rec.Path)
	for _, b := range f.Batteries {
		if b.Name == rec.Battery {
			batPath = b.Path
		}
	}
	set := func(ctx context.Context, value string) {
		release, _, err := runner.Lock(LockName)
		if err != nil {
			return // an apply is running; try again next minute
		}
		defer release()
		if err := writeAttr(ctx, s, rec.Path, value); err != nil {
			logf("%s: setting charge_behaviour to %s failed: %v", rec.Battery, value, err)
			return
		}
		logf("%s: charge_behaviour set to %s", rec.Battery, value)
	}
	logf("software charge limit for %s: %s at %d%%, auto at %d%%", rec.Battery, rec.Value, rec.Stop, rec.Start)
	for {
		capacity, okCap := readInt(s, batPath+"/capacity")
		cur, okCur := readStr(s, rec.Path)
		if okCap && okCur {
			sel := selectedChoice(cur)
			switch {
			case capacity >= rec.Stop && sel != rec.Value:
				set(ctx, rec.Value)
			case capacity <= rec.Start && sel != "auto":
				set(ctx, "auto")
			}
		}
		select {
		case <-ctx.Done():
			// Don't leave charging inhibited without the watcher running.
			if cur, ok := readStr(s, rec.Path); ok && selectedChoice(cur) != "auto" {
				c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				set(c, "auto")
				cancel()
			}
			return nil
		case <-time.After(time.Minute):
		}
	}
}
