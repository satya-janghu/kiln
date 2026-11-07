package tui

import (
	"fmt"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/satya-janghu/kiln/internal/runner"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Screen 4: review. Drawn from runner.Preview, the same code path as the
// real run (rule 7).

var tabs = []string{"Summary", "Packages", "Files", "Services", "Firewall", "Commands"}

type reviewState struct {
	gen      int // bumped every time the options may have changed
	pvGen    int
	inflight bool
	queued   bool
	pv       *runner.Preview
	err      string
	progress string
	tab      int
	sc       [6]scroll
	cache    map[string][]string
}

type previewMsg struct {
	gen int
	pv  *runner.Preview
	err error
}

type previewProgressMsg struct {
	gen  int
	text string
}

func (m *model) startPreview() tea.Cmd {
	m.rv.gen++
	m.rv.progress = ""
	m.rv.sc = [6]scroll{}
	if m.rv.inflight {
		// Only one preview at a time: Preview isn't safe to run twice at once.
		m.rv.queued = true
		return m.ensureTick()
	}
	return m.launchPreview()
}

func (m *model) launchPreview() tea.Cmd {
	m.rv.inflight = true
	gen := m.rv.gen
	a, q, ctx := m.a, m.c.q, m.c.ctx
	r := a.Runner(func(e runner.Event) {
		if e.Kind == runner.EvProgress || e.Kind == runner.EvOutput {
			q.push(previewProgressMsg{gen: gen, text: joinNonEmpty(": ", e.Title, clean(a.Red.Apply(e.Text)))})
		}
	})
	r.Opts = a.Opts.Clone() // the UI may change a.Opts while this runs
	return tea.Batch(func() (msg tea.Msg) {
		defer func() {
			if p := recover(); p != nil {
				a.Log.Error("internal error while working out the plan", "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
				msg = previewMsg{gen: gen, err: fmt.Errorf("kiln hit an internal error while working out the plan (%v)", p)}
			}
		}()
		return previewMsg{gen: gen, pv: r.Preview(ctx)}
	}, m.ensureTick())
}

func (m *model) onPreview(msg previewMsg) tea.Cmd {
	rv := &m.rv
	rv.inflight = false
	if msg.gen == rv.gen {
		rv.pv, rv.pvGen, rv.cache = msg.pv, msg.gen, nil
		rv.err = ""
		if msg.err != nil {
			rv.err = msg.err.Error()
		}
	}
	if rv.queued {
		rv.queued = false
		return m.launchPreview()
	}
	return nil
}

func (m *model) previewReady() bool {
	return !m.rv.inflight && m.rv.pv != nil && m.rv.pvGen == m.rv.gen && m.rv.err == ""
}

// startBlocked says why Start is off, or "".
func (m *model) startBlocked() string {
	switch {
	case !m.gateOK:
		return m.gateWhy
	case m.rv.err != "" && m.rv.pvGen == m.rv.gen:
		return "the plan couldn't be worked out: " + m.rv.err
	case !m.previewReady():
		return "kiln is still working out the plan"
	case m.ck.detecting:
		return "kiln is still detecting what applies"
	}
	return ""
}

func (m *model) keyReview(k string) tea.Cmd {
	rv := &m.rv
	switch k {
	case "esc":
		m.scr = scrChecklist
		return m.detect()
	case "enter":
		if why := m.startBlocked(); why != "" {
			return m.setFlash(true, "Start is off: %s.", why)
		}
		return m.startRun()
	case "tab", "right", "l":
		rv.tab = (rv.tab + 1) % len(tabs)
	case "shift+tab", "left", "h":
		rv.tab = (rv.tab + len(tabs) - 1) % len(tabs)
	case "1", "2", "3", "4", "5", "6":
		rv.tab = int(k[0] - '1')
	case "r":
		if !rv.inflight {
			return m.startPreview()
		}
	default:
		rv.sc[rv.tab].key(k)
	}
	return nil
}

// --- content ------------------------------------------------------------------

func (m *model) opText(op sys.Op) string {
	s := op.Summary
	switch {
	case s != "":
	case op.Cmd != nil:
		s = op.Cmd.String()
	default:
		s = joinNonEmpty(" ", string(op.Kind), op.Path)
	}
	return clean(m.a.Red.Apply(s))
}

func suiteOf(op sys.Op) string {
	if op.Cmd == nil {
		return ""
	}
	for i, a := range op.Cmd.Args {
		if (a == "-t" || a == "--target-release") && i+1 < len(op.Cmd.Args) {
			return op.Cmd.Args[i+1]
		}
		if strings.HasPrefix(a, "--target-release=") {
			return strings.TrimPrefix(a, "--target-release=")
		}
	}
	return ""
}

func serviceUnits(op sys.Op) []string {
	if op.Cmd == nil || op.Cmd.Name != "systemctl" || len(op.Cmd.Args) < 2 {
		return nil
	}
	var out []string
	for _, a := range op.Cmd.Args[1:] {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

func (m *model) reviewStats(pv *runner.Preview) string {
	pkgs, files, units := map[string]bool{}, map[string]bool{}, map[string]bool{}
	fw := 0
	for _, s := range pv.Steps {
		for _, op := range s.Ops {
			switch runner.Group(op) {
			case "Packages":
				for _, p := range runner.Packages(op) {
					pkgs[p] = true
				}
			case "Files":
				files[op.Path] = true
			case "Services":
				for _, u := range serviceUnits(op) {
					units[u] = true
				}
			case "Firewall":
				fw++
			}
		}
	}
	parts := []string{
		plural(len(pkgs), "package", "packages"),
		plural(len(files), "file", "files"),
		plural(len(units), "service", "services"),
		plural(fw, "firewall rule", "firewall rules"),
	}
	if len(pv.Reboot) > 0 {
		parts = append(parts, "reboot likely ("+strings.Join(pv.Reboot, "; ")+")")
	} else {
		parts = append(parts, "no reboot expected")
	}
	return strings.Join(parts, m.g.dot)
}

func (m *model) marker(st runner.Status) string {
	g := m.g
	switch st {
	case runner.Done:
		return g.ok
	case runner.Unchanged:
		return g.same
	case runner.Failed:
		return g.fail
	case runner.Skipped:
		return g.skip
	case runner.Running:
		return m.spin()
	}
	return g.pending
}

func (m *model) tabLines(tab, w int) []string {
	key := fmt.Sprintf("%d|%d", tab, w)
	if l, ok := m.rv.cache[key]; ok {
		return l
	}
	if m.rv.cache == nil {
		m.rv.cache = map[string][]string{}
	}
	pv := m.rv.pv
	var out []string
	add := func(s string, ind int) { out = append(out, wrapIndent(s, w, ind)...) }
	white := func(s string) { out = append(out, m.st.white15.Render(m.cut(s, w))) }
	switch tabs[tab] {
	case "Summary":
		for _, wn := range pv.Warnings {
			for _, l := range wrapIndent(m.g.warn+" "+m.a.Red.Apply(wn), w, 2) {
				out = append(out, m.st.yellow11.Render(l))
			}
		}
		if len(pv.Warnings) > 0 {
			out = append(out, "")
		}
		for _, s := range pv.Steps {
			mk := m.fit(m.marker(s.Status), 2)
			switch s.Status {
			case runner.Done:
				out = append(out, mk+" "+m.st.white15.Render(m.cut(s.Title, w-3))+" will change")
				for _, a := range s.Actions {
					add("     - "+m.a.Red.Apply(a), 7)
				}
			case runner.Unchanged:
				add(mk+" "+s.Title+": already set up"+suffix(s.Reason), 5)
			case runner.Failed:
				for _, l := range wrapIndent(mk+" "+s.Title+": can't be planned: "+m.a.Red.Apply(s.Reason), w, 5) {
					out = append(out, m.st.red9.Render(l))
				}
			default:
				add(mk+" "+s.Title+": skipped"+suffix(s.Reason), 5)
			}
		}
		if len(pv.Notes) > 0 {
			out = append(out, "")
			white("Notes")
			for _, n := range pv.Notes {
				add(" - "+m.a.Red.Apply(n), 3)
			}
		}
	case "Packages":
		total := map[string]bool{}
		var other []string
		var blocks []string
		for _, s := range pv.Steps {
			var names []string
			for _, op := range s.Ops {
				if runner.Group(op) != "Packages" {
					continue
				}
				pk := runner.Packages(op)
				if len(pk) == 0 {
					other = append(other, s.Title+": "+m.opText(op))
					continue
				}
				suite := suiteOf(op)
				for _, p := range pk {
					total[p] = true
					if suite != "" {
						p += " (" + suite + ")"
					}
					names = append(names, p)
				}
			}
			if len(names) > 0 {
				blocks = append(blocks, s.Title+"\x00"+strings.Join(names, " "))
			}
		}
		if len(total) > 0 {
			white(plural(len(total), "package", "packages") + " to install")
		}
		for _, b := range blocks {
			title, names, _ := strings.Cut(b, "\x00")
			out = append(out, "")
			out = append(out, m.cut(title, w))
			add("  "+names, 2)
		}
		if len(other) > 0 {
			out = append(out, "")
			white("Other package actions")
			for _, o := range other {
				add(" - "+o, 3)
			}
		}
	default:
		for _, s := range pv.Steps {
			var items []string
			for _, op := range s.Ops {
				if runner.Group(op) == tabs[tab] {
					items = append(items, m.opText(op))
				}
			}
			if len(items) == 0 {
				continue
			}
			if len(out) > 0 {
				out = append(out, "")
			}
			white(s.Title)
			for _, it := range items {
				add(" - "+it, 3)
			}
		}
	}
	if len(out) == 0 {
		out = []string{"Nothing here would change."}
	}
	m.rv.cache[key] = out
	return out
}

func suffix(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + reason + ")"
}

func (m *model) viewReview() ([]string, []hint, string) {
	rv := &m.rv
	cw, bh := m.cw(), m.bodyH()
	head := m.st.white15.Render("Review: nothing has changed yet")
	out := []string{head}
	if !m.gateOK {
		for _, l := range wrapIndent(m.g.err+" Start is off: "+m.gateWhy+". You can still look at the plan.", cw, 3) {
			out = append(out, m.st.white15.Render(l))
		}
	}
	if rv.inflight || rv.pv == nil {
		if rv.err != "" && rv.pvGen == rv.gen {
			out = append(out, "", m.st.white15.Render(m.g.err+" "+rv.err))
		} else {
			out = append(out, "", m.spin()+" Working out what would change on this machine (nothing is changed)...")
			if rv.progress != "" {
				out = append(out, "  "+m.cut(rv.progress, cw-2))
			}
		}
		hints := []hint{h("ESC", "Back"), h("F3", "Quit")}
		return out, hints, "Review"
	}
	if rv.err != "" {
		out = append(out, m.st.white15.Render(m.cut(m.g.err+" "+rv.err, cw)))
	}
	pv := rv.pv
	maxWarn := max(1, bh/4)
	var warn []string
	for _, wn := range pv.Warnings {
		warn = append(warn, wrapIndent(m.g.warn+" "+m.a.Red.Apply(wn), cw, 2)...)
	}
	if len(warn) > maxWarn {
		more := len(pv.Warnings)
		warn = append(warn[:maxWarn-1], fmt.Sprintf("  ... all %d warnings are at the top of the Summary tab", more))
	}
	for _, l := range warn {
		out = append(out, m.st.yellow11.Render(l))
	}
	out = append(out, m.cut(m.reviewStats(pv), cw))
	var tl []string
	for i, t := range tabs {
		if i == rv.tab {
			tl = append(tl, m.st.tabOn.Render("["+t+"]"))
		} else {
			tl = append(tl, " "+t+" ")
		}
	}
	out = append(out, strings.Join(tl, " "))
	ph := bh - len(out)
	if ph < 3 {
		return out[:min(len(out), bh)], nil, "Review"
	}
	lines := m.tabLines(rv.tab, cw-4)
	sc := &rv.sc[rv.tab]
	body := sc.window(lines, ph-2)
	out = append(out, m.panel(titled(tabs[rv.tab], sc.position(len(lines))), body, cw, ph)...)
	start := "Start"
	if m.startBlocked() != "" {
		start = "Start (off)"
	}
	hints := []hint{h("ENTER", start), h("ESC", "Back"), h("TAB", "Next tab"), h("PGUP/PGDN", "Scroll"), h("F3", "Quit"), h("?", "Help")}
	return out, hints, "Review"
}
