package battery

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/state"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Recorded method names (state.Battery.Method).
const (
	MethodThresholds = "thresholds"
	MethodWatch      = "watch"
	MethodNone       = "none"
	vendorPrefix     = "vendor:"
)

// Options are the battery step's wizard options.
type Options struct {
	Stop     int  // target stop %
	Gap      int  // start = stop − gap
	Software bool // allow the inhibit-charge watcher (method 3)
}

// DefaultOptions are the options-reference defaults.
func DefaultOptions() Options { return Options{Stop: 80, Gap: 5, Software: true} }

// Logf receives one line per tried value and its result.
type Logf func(format string, a ...any)

// Outcome is what the ladder accepted.
type Outcome struct {
	Rec   state.Battery
	Notes []string // summary notes
}

// candidates returns the stop values to try: below MaxStop, nearest to target
// first, the higher value first on a tie (catalog: 80, 85, 75, 70, 60, 50).
func candidates(target int) []int {
	set := map[int]bool{}
	if target > 0 && target < MaxStop {
		set[target] = true
	}
	for _, c := range stopCandidates {
		if c < MaxStop {
			set[c] = true
		}
	}
	out := make([]int, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := abs(out[i]-target), abs(out[j]-target)
		if di != dj {
			return di < dj
		}
		return out[i] > out[j]
	})
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func startFor(stop, gap int) int {
	if s := stop - gap; s > 0 {
		return s
	}
	return 0
}

// --- sysfs helpers (all reads and writes go through sys.System) ---

func readStr(s sys.System, p string) (string, bool) {
	b, err := s.ReadFile(p)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func readInt(s sys.System, p string) (int, bool) {
	v, ok := readStr(s, p)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

func exists(s sys.System, p string) bool { _, err := s.Stat(p); return err == nil }

// writeAttr writes one sysfs/procfs attribute. sys.System has no raw attribute
// write (WriteFile is an atomic rename, which sysfs doesn't support), so the
// value goes through `tee <path>` on stdin: recorded by the Recorder, executed
// by Real, and the kernel's error (EINVAL…) comes back as tee's exit status.
func writeAttr(ctx context.Context, s sys.System, path, value string) error {
	_, err := s.Run(ctx, sys.Cmd{Name: "tee", Args: []string{path}, Stdin: []byte(value + "\n"), Timeout: 15 * time.Second})
	return err
}

// selectedChoice returns the [bracketed] entry of a choice attribute
// ("[auto] inhibit-charge force-discharge" → "auto"); a single plain value is returned as-is.
func selectedChoice(v string) string {
	for _, f := range strings.Fields(v) {
		if strings.HasPrefix(f, "[") && strings.HasSuffix(f, "]") {
			return strings.Trim(f, "[]")
		}
	}
	if f := strings.Fields(v); len(f) == 1 {
		return f[0]
	}
	return ""
}

// listedChoice reports whether a choice attribute offers v.
func listedChoice(v, want string) bool {
	for _, f := range strings.Fields(v) {
		if strings.Trim(f, "[]") == want {
			return true
		}
	}
	return false
}

func moduleLoaded(s sys.System, m string) bool { return m == "" || exists(s, "/sys/module/"+m) }

func vendorMatches(f *facts.Facts, v string) bool {
	return v == "" || strings.Contains(strings.ToLower(f.DMI.Vendor), strings.ToLower(v))
}

// --- probing (read-only) ---

type thrBat struct {
	Name, Path string
	HasStart   bool
}

func (b thrBat) end() string   { return b.Path + "/charge_control_end_threshold" }
func (b thrBat) start() string { return b.Path + "/charge_control_start_threshold" }
func (b thrBat) types() string { return b.Path + "/charge_types" }

type fixedHit struct {
	Mode    fixedMode
	Path    string
	Battery string // set for {bat} rows
}

type watchHit struct {
	Battery facts.Battery
	Path    string
	Value   string // inhibit-charge, or inhibit-charge-awake where only that is listed
}

type probeResult struct {
	Thresholds []thrBat
	Fixed      []fixedHit
	Watch      []watchHit
	Probed     []string // what was looked at, for the summary and `kiln battery status`
}

func probe(s sys.System, f *facts.Facts) probeResult {
	var p probeResult
	for _, b := range f.Batteries {
		tb := thrBat{Name: b.Name, Path: b.Path}
		if exists(s, tb.end()) {
			tb.HasStart = exists(s, tb.start())
			p.Thresholds = append(p.Thresholds, tb)
			p.Probed = append(p.Probed, b.Name+": charge_control_end_threshold"+map[bool]string{true: " and charge_control_start_threshold", false: ""}[tb.HasStart])
		} else {
			p.Probed = append(p.Probed, b.Name+": no charge_control thresholds")
		}
		if v, ok := readStr(s, b.Path+"/charge_behaviour"); ok {
			val := ""
			switch {
			case listedChoice(v, "inhibit-charge"):
				val = "inhibit-charge"
			case listedChoice(v, "inhibit-charge-awake"):
				val = "inhibit-charge-awake"
			}
			p.Probed = append(p.Probed, b.Name+": charge_behaviour ("+v+")")
			if val != "" {
				p.Watch = append(p.Watch, watchHit{Battery: b, Path: b.Path + "/charge_behaviour", Value: val})
			}
		} else {
			p.Probed = append(p.Probed, b.Name+": no charge_behaviour")
		}
	}
	for _, m := range fixedModes {
		if !moduleLoaded(s, m.Module) || !vendorMatches(f, m.Vendor) {
			continue
		}
		var globs []struct{ pat, bat string }
		if strings.Contains(m.Glob, "{bat}") {
			for _, b := range f.Batteries {
				globs = append(globs, struct{ pat, bat string }{strings.ReplaceAll(m.Glob, "{bat}", b.Path), b.Name})
			}
		} else {
			globs = append(globs, struct{ pat, bat string }{m.Glob, ""})
		}
		for _, g := range globs {
			paths, _ := s.Glob(g.pat)
			if len(paths) == 0 {
				continue
			}
			sort.Strings(paths)
			path := paths[0]
			if m.Selection {
				v, _ := readStr(s, path)
				any := false
				for _, want := range m.Values {
					any = any || listedChoice(v, want)
				}
				if !any {
					continue
				}
			}
			p.Fixed = append(p.Fixed, fixedHit{Mode: m, Path: path, Battery: g.bat})
			p.Probed = append(p.Probed, "vendor mode "+m.Name+" at "+path)
		}
	}
	return p
}

// predict names the method the ladder would start with (used for previews).
func (p probeResult) predict(o Options) string {
	switch {
	case len(p.Thresholds) > 0:
		return MethodThresholds
	case len(p.Fixed) > 0:
		return vendorPrefix + p.Fixed[0].Mode.Name
	case len(p.Watch) > 0 && o.Software:
		return MethodWatch
	}
	return MethodNone
}

func activeQuirk(s sys.System, f *facts.Facts) *thresholdQuirk {
	for i := range thresholdQuirks {
		q := &thresholdQuirks[i]
		if moduleLoaded(s, q.Module) && vendorMatches(f, q.Vendor) {
			return q
		}
	}
	return nil
}

// --- method 1: kernel thresholds ---

// ensureCustomType selects charge_types=Custom where it's offered (Dell needs it
// before thresholds take effect) and verifies it stuck.
func ensureCustomType(ctx context.Context, s sys.System, b thrBat, logf Logf) bool {
	v, ok := readStr(s, b.types())
	if !ok || !listedChoice(v, "Custom") || selectedChoice(v) == "Custom" {
		return true
	}
	if err := writeAttr(ctx, s, b.types(), "Custom"); err != nil {
		logf("%s: writing charge_types=Custom failed: %v", b.Name, err)
		return false
	}
	nv, _ := readStr(s, b.types())
	if selectedChoice(nv) != "Custom" {
		logf("%s: charge_types=Custom didn't stick (reads %q)", b.Name, nv)
		return false
	}
	logf("%s: charge_types set to Custom", b.Name)
	return true
}

// setPair writes start/stop with the catalog's write order and read-back rule.
// It returns the read-back pair and whether it's acceptable (< MaxStop).
func setPair(ctx context.Context, s sys.System, b thrBat, start, stop int, logf Logf) (int, int, bool) {
	curStart, _ := readInt(s, b.start())
	// thinkpad_acpi rejects stop < current start and start > current stop with
	// EINVAL: lowering writes start first, raising writes stop first.
	startFirst := b.HasStart && stop < curStart
	attempt := func(startFirst bool) (rs, re int, wrote bool) {
		type w struct{ path, val, what string }
		writes := []w{{b.end(), strconv.Itoa(stop), "stop"}}
		if b.HasStart {
			sw := w{b.start(), strconv.Itoa(start), "start"}
			if startFirst {
				writes = append([]w{sw}, writes...)
			} else {
				writes = append(writes, sw)
			}
		}
		for _, x := range writes {
			if err := writeAttr(ctx, s, x.path, x.val); err != nil {
				logf("%s: writing %s %s failed: %v", b.Name, x.what, x.val, err)
				return 0, 0, false
			}
		}
		re, _ = readInt(s, b.end())
		rs, _ = readInt(s, b.start())
		return rs, re, true
	}
	anyWrote := false
	for i, order := range []bool{startFirst, !startFirst} {
		rs, re, wrote := attempt(order)
		anyWrote = anyWrote || wrote
		if !wrote {
			continue
		}
		if re == stop && (!b.HasStart || rs == start) {
			logf("%s: start %d / stop %d accepted (read back %d/%d)", b.Name, start, stop, rs, re)
			return rs, re, true
		}
		logf("%s: wrote start %d / stop %d, read back %d/%d (attempt %d)", b.Name, start, stop, rs, re, i+1)
	}
	if !anyWrote {
		return 0, 0, false
	}
	// Drivers may round or clamp (kernel ABI, dell-laptop): accept what reads
	// back only if it's still a real limit below MaxStop.
	re, _ := readInt(s, b.end())
	rs, _ := readInt(s, b.start())
	if re > 0 && re < MaxStop {
		logf("%s: accepting rounded/clamped values start %d / stop %d", b.Name, rs, re)
		return rs, re, true
	}
	logf("%s: stop %d not accepted (reads back %d)", b.Name, stop, re)
	return rs, re, false
}

func thresholdRecord(bats []thrBat, rs, re int, o Options, cached bool) state.Battery {
	names := make([]string, 0, len(bats))
	for _, b := range bats {
		names = append(names, b.Name)
	}
	if !bats[0].HasStart {
		rs = 0
	}
	v := fmt.Sprintf("req=%d/%d", o.Stop, o.Gap)
	if cached {
		v += ";unverified"
	}
	return state.Battery{Method: MethodThresholds, Battery: strings.Join(names, ","), Start: rs, Stop: re, Value: v}
}

// requested parses the request stored in a thresholds record ("req=80/5").
func requested(rec *state.Battery) (stop, gap int, ok bool) {
	for _, part := range strings.Split(rec.Value, ";") {
		if r, found := strings.CutPrefix(part, "req="); found {
			a, b, _ := strings.Cut(r, "/")
			s1, e1 := strconv.Atoi(a)
			g1, e2 := strconv.Atoi(b)
			return s1, g1, e1 == nil && e2 == nil
		}
	}
	return 0, 0, false
}

func unverified(rec *state.Battery) bool { return strings.Contains(rec.Value, "unverified") }

func applyThresholds(ctx context.Context, s sys.System, f *facts.Facts, bats []thrBat, o Options, logf Logf) (state.Battery, bool) {
	q := activeQuirk(s, f)
	if q != nil {
		logf("threshold driver quirk: %s", q.Name)
	}
	primary := bats[0]
	if !ensureCustomType(ctx, s, primary, logf) {
		return state.Battery{}, false
	}
	for _, c := range candidates(o.Stop) {
		start := startFor(c, o.Gap)
		rs, re, ok := setPair(ctx, s, primary, start, c, logf)
		if !ok {
			continue
		}
		// Record only the batteries that took the limit: a battery recorded
		// but never set would fail Verify on every run and send every
		// helper run back through the whole ladder.
		took := []thrBat{primary}
		for _, other := range bats[1:] {
			if !ensureCustomType(ctx, s, other, logf) {
				continue
			}
			if _, ore, ook := setPair(ctx, s, other, rs, re, logf); !ook || ore != re {
				logf("%s: couldn't apply the same limit (reads back %d)", other.Name, ore)
				continue
			}
			took = append(took, other)
		}
		return thresholdRecord(took, rs, re, o, q != nil && q.Cached), true
	}
	return state.Battery{}, false
}

// --- method 2: vendor fixed modes ---

func readMatches(s sys.System, h fixedHit, want string) bool {
	v, ok := readStr(s, h.Path)
	if !ok {
		return false
	}
	if h.Mode.Selection {
		return selectedChoice(v) == want
	}
	return v == want
}

func applyFixed(ctx context.Context, s sys.System, hits []fixedHit, logf Logf) (state.Battery, bool) {
	for _, h := range hits {
		cur, _ := readStr(s, h.Path)
		for _, v := range h.Mode.Values {
			if h.Mode.Selection && !listedChoice(cur, v) {
				continue
			}
			if err := writeAttr(ctx, s, h.Path, v); err != nil {
				logf("%s: writing %q failed: %v", h.Path, v, err)
				continue
			}
			if readMatches(s, h, v) {
				logf("%s: %q accepted", h.Path, v)
				return state.Battery{Method: vendorPrefix + h.Mode.Name, Battery: h.Battery, Path: h.Path, Value: v}, true
			}
			got, _ := readStr(s, h.Path)
			logf("%s: wrote %q, reads back %q", h.Path, v, got)
		}
	}
	return state.Battery{}, false
}

func fixedLabel(name string) string {
	for _, m := range fixedModes {
		if vendorPrefix+m.Name == name {
			return m.Label
		}
	}
	return name
}

// --- the ladder ---

// Run runs the full method ladder (methods 1–4) and returns what was accepted.
// Under a Recorder it changes nothing: it records the writes the first
// candidate of the predicted method would make and returns that prediction.
func Run(ctx context.Context, s sys.System, f *facts.Facts, o Options, logf Logf) (Outcome, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	p := probe(s, f)
	if s.DryRun() {
		return preview(ctx, s, f, p, o)
	}
	var out Outcome
	if len(p.Thresholds) > 0 {
		first := candidates(o.Stop)[0]
		// UPower first, so its restart can't overwrite kiln's values afterwards.
		if err := syncHwdb(ctx, s, f, p.Thresholds, startFor(first, o.Gap), first); err != nil {
			return out, err
		}
		if rec, ok := applyThresholds(ctx, s, f, p.Thresholds, o, logf); ok {
			if rec.Stop != first || (p.Thresholds[0].HasStart && rec.Start != startFor(first, o.Gap)) {
				hs := rec.Start
				if !p.Thresholds[0].HasStart {
					hs = startFor(rec.Stop, o.Gap)
				}
				if err := syncHwdb(ctx, s, f, p.Thresholds, hs, rec.Stop); err != nil {
					return out, err
				}
				// The UPower restart may have re-applied its limits; make sure ours stand.
				if ok, _ := verifyThresholds(s, &rec); !ok {
					if !reapplyThresholds(ctx, s, f, &rec, logf) {
						logf("the accepted limit didn't survive the UPower restart")
					}
				}
			}
			out.Rec = rec
			if unverified(&rec) {
				out.Notes = append(out.Notes, fmt.Sprintf("Battery charge limit set to stop at %d%%, but this driver (asus-wmi) can't read the value back, so it's set, not verifiable.", rec.Stop))
			}
			return out, nil
		}
		if q := activeQuirk(s, f); q != nil && q.FailHint != "" {
			out.Notes = append(out.Notes, q.FailHint)
		}
	}
	if rec, ok := applyFixed(ctx, s, p.Fixed, logf); ok {
		out.Rec = rec
		return out, nil
	}
	if o.Software && len(p.Watch) > 0 {
		w := p.Watch[0]
		logf("%s: using the software fallback (%s at %d%%, auto at %d%%)", w.Battery.Name, w.Value, o.Stop, startFor(o.Stop, o.Gap))
		out.Rec = state.Battery{Method: MethodWatch, Battery: w.Battery.Name, Path: w.Path, Value: w.Value,
			Start: startFor(o.Stop, o.Gap), Stop: o.Stop}
		return out, nil
	}
	out.Rec = state.Battery{Method: MethodNone}
	out.Notes = append(out.Notes, noneNote(f, p))
	return out, nil
}

func noneNote(f *facts.Facts, p probeResult) string {
	machine := f.MachineName()
	if machine == "" {
		machine = "this machine"
	}
	return fmt.Sprintf("No battery charge limit could be set on %s. kiln probed: %s. Look in the BIOS/UEFI setup for an option such as %s. (A smart plug switched by battery percentage would also work; kiln doesn't set that up.)",
		machine, strings.Join(p.Probed, "; "), `"`+strings.Join(biosOptions, `", "`)+`"`)
}

func preview(ctx context.Context, s sys.System, f *facts.Facts, p probeResult, o Options) (Outcome, error) {
	var out Outcome
	switch m := p.predict(o); {
	case m == MethodThresholds:
		first := candidates(o.Stop)[0]
		start := startFor(first, o.Gap)
		if err := syncHwdb(ctx, s, f, p.Thresholds, start, first); err != nil {
			return out, err
		}
		b := p.Thresholds[0]
		_ = writeAttr(ctx, s, b.end(), strconv.Itoa(first))
		if b.HasStart {
			_ = writeAttr(ctx, s, b.start(), strconv.Itoa(start))
		}
		out.Rec = thresholdRecord(p.Thresholds, start, first, o, false)
	case strings.HasPrefix(m, vendorPrefix):
		h := p.Fixed[0]
		_ = writeAttr(ctx, s, h.Path, h.Mode.Values[0])
		out.Rec = state.Battery{Method: m, Battery: h.Battery, Path: h.Path, Value: h.Mode.Values[0]}
	case m == MethodWatch:
		w := p.Watch[0]
		out.Rec = state.Battery{Method: MethodWatch, Battery: w.Battery.Name, Path: w.Path, Value: w.Value,
			Start: startFor(o.Stop, o.Gap), Stop: o.Stop}
	default:
		out.Rec = state.Battery{Method: MethodNone}
		out.Notes = append(out.Notes, noneNote(f, p))
	}
	return out, nil
}

// --- re-applying and verifying a recorded result ---

func recordedBatteries(s sys.System, f *facts.Facts, rec *state.Battery) []thrBat {
	var out []thrBat
	for _, n := range strings.Split(rec.Battery, ",") {
		for _, b := range f.Batteries {
			if b.Name == n {
				tb := thrBat{Name: b.Name, Path: b.Path}
				if exists(s, tb.end()) {
					tb.HasStart = exists(s, tb.start())
					out = append(out, tb)
				}
			}
		}
	}
	return out
}

func reapplyThresholds(ctx context.Context, s sys.System, f *facts.Facts, rec *state.Battery, logf Logf) bool {
	bats := recordedBatteries(s, f, rec)
	if len(bats) == 0 {
		return false
	}
	// The helpers run at every boot, power event and every 15 minutes: write
	// only when the firmware has actually reset something.
	if ok, _ := verifyThresholds(s, rec); ok {
		return true
	}
	ok := true
	for _, b := range bats {
		if !ensureCustomType(ctx, s, b, logf) {
			ok = false
			continue
		}
		_, re, good := setPair(ctx, s, b, rec.Start, rec.Stop, logf)
		if !good || re != rec.Stop {
			ok = false
		}
	}
	return ok
}

// verifyThresholds is read-only: every recorded battery reads back the recorded pair.
func verifyThresholds(s sys.System, rec *state.Battery) (bool, string) {
	for _, n := range strings.Split(rec.Battery, ",") {
		path := filepath.Join("/sys/class/power_supply", n)
		end, ok := readInt(s, path+"/charge_control_end_threshold")
		if !ok {
			return false, n + " no longer exposes charge_control_end_threshold"
		}
		if end != rec.Stop {
			return false, fmt.Sprintf("%s stops charging at %d%%, expected %d%%", n, end, rec.Stop)
		}
		if start, ok := readInt(s, path+"/charge_control_start_threshold"); ok && rec.Start > 0 && start != rec.Start {
			return false, fmt.Sprintf("%s starts charging at %d%%, expected %d%%", n, start, rec.Start)
		}
		if v, ok := readStr(s, path+"/charge_types"); ok && listedChoice(v, "Custom") && selectedChoice(v) != "Custom" {
			return false, n + " charge_types isn't Custom"
		}
	}
	return true, fmt.Sprintf("charging stops at %d%%", rec.Stop)
}

// Verify reports, read-only, whether the recorded method still holds.
func Verify(s sys.System, f *facts.Facts, rec *state.Battery, o Options) (bool, string) {
	if rec == nil || rec.Method == "" {
		return false, "no battery charge limit has been applied yet"
	}
	p := probe(s, f)
	switch {
	case rec.Method == MethodThresholds:
		if st, gap, ok := requested(rec); !ok || st != o.Stop || gap != o.Gap {
			return false, fmt.Sprintf("the requested limit changed to %d%% (gap %d)", o.Stop, o.Gap)
		}
		return verifyThresholds(s, rec)
	case strings.HasPrefix(rec.Method, vendorPrefix):
		if len(p.Thresholds) > 0 {
			return false, "kernel charge thresholds are now available"
		}
		if readMatches(s, fixedHit{Mode: fixedModeByName(rec.Method), Path: rec.Path}, rec.Value) {
			return true, fixedLabel(rec.Method)
		}
		return false, rec.Path + " doesn't read back " + rec.Value
	case rec.Method == MethodWatch:
		if !o.Software {
			return false, "the software fallback was turned off"
		}
		if len(p.Thresholds) > 0 || len(p.Fixed) > 0 {
			return false, "a hardware charge limit is now available"
		}
		if rec.Stop != o.Stop || rec.Start != startFor(o.Stop, o.Gap) {
			return false, fmt.Sprintf("the requested limit changed to %d%% (gap %d)", o.Stop, o.Gap)
		}
		v, ok := readStr(s, rec.Path)
		if !ok || !listedChoice(v, rec.Value) {
			return false, rec.Path + " no longer offers " + rec.Value
		}
		return true, fmt.Sprintf("software limit: %s at %d%%, auto at %d%%", rec.Value, rec.Stop, rec.Start)
	case rec.Method == MethodNone:
		if m := p.predict(o); m != MethodNone {
			return false, "a charge-limit interface is now available"
		}
		return true, "no charge-limit method works on this machine (see the summary)"
	}
	return false, "unknown recorded method " + rec.Method
}

func fixedModeByName(method string) fixedMode {
	for _, m := range fixedModes {
		if vendorPrefix+m.Name == method {
			return m
		}
	}
	return fixedMode{}
}

// reapply writes a recorded result again; false means it no longer works.
func reapply(ctx context.Context, s sys.System, f *facts.Facts, rec *state.Battery, logf Logf) bool {
	switch {
	case rec.Method == MethodThresholds:
		return reapplyThresholds(ctx, s, f, rec, logf)
	case strings.HasPrefix(rec.Method, vendorPrefix):
		h := fixedHit{Mode: fixedModeByName(rec.Method), Path: rec.Path}
		if readMatches(s, h, rec.Value) {
			return true
		}
		if err := writeAttr(ctx, s, rec.Path, rec.Value); err != nil {
			logf("%s: writing %q failed: %v", rec.Path, rec.Value, err)
			return false
		}
		return readMatches(s, h, rec.Value)
	case rec.Method == MethodWatch:
		v, ok := readStr(s, rec.Path)
		return ok && listedChoice(v, rec.Value)
	}
	return false
}

// Predict names the method the ladder would start with, read-only (for previews).
func Predict(s sys.System, f *facts.Facts, o Options) string { return probe(s, f).predict(o) }

// CandidatesText lists the stop values the ladder tries, in order ("80, 85, 75, 70, 60, 50").
func CandidatesText(target int) string {
	c := candidates(target)
	parts := make([]string, len(c))
	for i, v := range c {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}
