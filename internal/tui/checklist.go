package tui

import (
	"fmt"
	"runtime/debug"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/satya-janghu/kiln/internal/app"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Screen 2: the step checklist.

type checklistState struct {
	cursor, top int
	filter      string
	filtering   bool
	folded      map[string]bool

	appl       map[string]step.Applicability
	applKnown  bool
	detecting  bool
	applQueued bool
}

type ckRow struct {
	cat string
	idx int // index into a.Steps; -1 for the category header
}

// --- applicability ("not applicable: <reason>") -----------------------------

type applMsg struct{ res map[string]step.Applicability }

// detect re-evaluates every step's Applies in the background against a
// Recorder (nothing can change) and a copy of the options.
func (m *model) detect() tea.Cmd {
	if m.ck.detecting {
		m.ck.applQueued = true
		return nil
	}
	m.ck.detecting = true
	a, opts := m.a, m.a.Opts.Clone()
	return tea.Batch(func() tea.Msg { return applMsg{applicability(a, opts)} }, m.ensureTick())
}

func applicability(a *app.App, opts *options.Resolved) map[string]step.Applicability {
	rec := sys.NewRecorder(a.Real)
	out := map[string]step.Applicability{}
	for _, s := range a.Steps {
		env := &step.Env{Facts: a.Facts, Opts: opts.For(s.ID()), All: opts, Sys: rec, Log: a.Log.With("step", s.ID()),
			Notes: &step.Notes{}, Secrets: a.Secrets, State: a.State,
			Progress: func(string) {}, Output: func(string) {}}
		out[s.ID()] = safeApplies(a, s, env)
	}
	return out
}

func safeApplies(a *app.App, s step.Step, env *step.Env) (res step.Applicability) {
	defer func() {
		if p := recover(); p != nil {
			a.Log.Error("internal error while checking whether a step applies", "step", s.ID(), "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			res = step.Yes()
		}
	}()
	return s.Applies(env)
}

func (m *model) onAppl(msg applMsg) tea.Cmd {
	m.ck.detecting = false
	m.ck.appl, m.ck.applKnown = msg.res, true
	if m.ck.applQueued {
		m.ck.applQueued = false
		return m.detect()
	}
	return nil
}

// --- rows -------------------------------------------------------------------

func (m *model) matches(s step.Step) bool {
	f := strings.ToLower(strings.TrimSpace(m.ck.filter))
	if f == "" {
		return true
	}
	for _, x := range []string{s.Title(), s.ID(), s.Category(), step.CategoryTitle(s.Category())} {
		if strings.Contains(strings.ToLower(x), f) {
			return true
		}
	}
	return false
}

func (m *model) ckRows() []ckRow {
	var rows []ckRow
	filtered := strings.TrimSpace(m.ck.filter) != ""
	for _, cat := range m.categories {
		var idxs []int
		for i, s := range m.a.Steps {
			if s.Category() == cat && m.matches(s) {
				idxs = append(idxs, i)
			}
		}
		if len(idxs) == 0 {
			continue
		}
		rows = append(rows, ckRow{cat: cat, idx: -1})
		if m.ck.folded[cat] && !filtered {
			continue
		}
		for _, i := range idxs {
			rows = append(rows, ckRow{cat: cat, idx: i})
		}
	}
	return rows
}

func (m *model) catSteps(cat string) []step.Step {
	var out []step.Step
	for _, s := range m.a.Steps {
		if s.Category() == cat {
			out = append(out, s)
		}
	}
	return out
}

func (m *model) enabledCount() (on, total int) {
	for _, s := range m.a.Steps {
		total++
		if m.a.Opts.IsEnabled(s.ID()) {
			on++
		}
	}
	return on, total
}

// dependents lists enabled steps that would be skipped because id is off.
func (m *model) dependents(id string) []string {
	off := map[string]bool{id: true}
	var titles []string
	for changed := true; changed; {
		changed = false
		for _, s := range m.a.Steps {
			if off[s.ID()] || !m.a.Opts.IsEnabled(s.ID()) {
				continue
			}
			for _, d := range s.DependsOn() {
				if off[d] {
					off[s.ID()] = true
					titles = append(titles, s.Title())
					changed = true
					break
				}
			}
		}
	}
	return titles
}

func (m *model) setStep(id string, on bool) {
	m.a.Opts.Enabled[id] = on
}

// --- keys -------------------------------------------------------------------

func (m *model) toChecklist() tea.Cmd {
	m.scr = scrChecklist
	return m.detect()
}

func (m *model) keyChecklist(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	ck := &m.ck
	rows := m.ckRows()
	if ck.filtering {
		switch k {
		case "enter":
			ck.filtering = false
			return nil
		case "esc":
			ck.filtering, ck.filter = false, ""
			return nil
		case "backspace":
			if r := []rune(ck.filter); len(r) > 0 {
				ck.filter = string(r[:len(r)-1])
			}
			ck.cursor = 0
			return nil
		case "up", "down", "pgup", "pgdown":
			// fall through to movement below
		default:
			if msg.Text != "" && !msg.Mod.Contains(tea.ModCtrl) && !msg.Mod.Contains(tea.ModAlt) {
				ck.filter += msg.Text
				ck.cursor = 0
			}
			return nil
		}
	}
	page := max(1, m.ckListH()-1)
	switch k {
	case "up", "k":
		ck.cursor--
	case "down", "j":
		ck.cursor++
	case "pgup":
		ck.cursor -= page
	case "pgdown":
		ck.cursor += page
	case "home", "g":
		ck.cursor = 0
	case "end", "G":
		ck.cursor = len(rows) - 1
	}
	ck.cursor = max(0, min(ck.cursor, len(rows)-1))
	var cur ckRow
	if len(rows) > 0 {
		cur = rows[ck.cursor]
	} else {
		cur = ckRow{idx: -1}
	}
	switch k {
	case "/":
		ck.filtering = true
	case "esc":
		if ck.filter != "" {
			ck.filter = ""
			return nil
		}
		m.scr = scrFacts
	case "left", "h":
		if cur.cat != "" && strings.TrimSpace(ck.filter) == "" {
			ck.folded[cur.cat] = true
			for i, r := range m.ckRows() {
				if r.cat == cur.cat && r.idx < 0 {
					ck.cursor = i
				}
			}
		}
	case "right", "l":
		if cur.cat != "" {
			ck.folded[cur.cat] = false
		}
	case "space":
		if cur.idx < 0 {
			return m.toggleCategory(cur.cat)
		}
		s := m.a.Steps[cur.idx]
		on := !m.a.Opts.IsEnabled(s.ID())
		var cmd tea.Cmd
		if !on {
			if deps := m.dependents(s.ID()); len(deps) > 0 {
				cmd = m.setFlash(true, "Also skipped: %s", strings.Join(deps, ", "))
			}
		} else if off := m.offDeps(s); len(off) > 0 {
			cmd = m.setFlash(true, "%s needs %s, which is off. Turn it on too, or this step is skipped.", s.Title(), strings.Join(off, ", "))
		}
		m.setStep(s.ID(), on)
		return tea.Batch(cmd, m.detect())
	case "c":
		if cur.cat != "" {
			return m.toggleCategory(cur.cat)
		}
	case "a":
		for _, s := range m.a.Steps {
			m.setStep(s.ID(), true)
		}
		return tea.Batch(m.setFlash(false, "Every step is on."), m.detect())
	case "n":
		m.modal = &modal{
			title: "Turn everything off?",
			lines: []string{"Turn every step off? You can then turn on just the ones you want."},
			hints: []hint{h("Y", "Turn all off"), h("N", "Back")},
			keys: map[string]func() tea.Cmd{
				"y": func() tea.Cmd {
					for _, s := range m.a.Steps {
						m.setStep(s.ID(), false)
					}
					return m.detect()
				},
				"n": nil, "esc": nil,
			},
		}
	case "d":
		m.modal = &modal{
			title: "Reset to defaults?",
			lines: []string{"Reset every step and option to kiln's defaults? This drops this machine's choices from its last run and the changes you made here."},
			hints: []hint{h("Y", "Reset"), h("N", "Back")},
			keys: map[string]func() tea.Cmd{
				"y": func() tea.Cmd {
					m.a.ResetToDefaults()
					m.generated = map[string]bool{}
					return tea.Batch(m.setFlash(false, "Everything is back to kiln's defaults."), m.detect())
				},
				"n": nil, "esc": nil,
			},
		}
	case "o":
		if cur.idx < 0 {
			return m.setFlash(true, "Move to a step to change its options.")
		}
		return m.openForm(cur.idx)
	case "enter":
		return m.toReview()
	}
	return nil
}

func (m *model) offDeps(s step.Step) []string {
	var out []string
	for _, d := range s.DependsOn() {
		if on, ok := m.a.Opts.Enabled[d]; ok && !on {
			out = append(out, m.stepTitle(d))
		}
	}
	return out
}

func (m *model) toggleCategory(cat string) tea.Cmd {
	steps := m.catSteps(cat)
	allOn := true
	for _, s := range steps {
		if !m.a.Opts.IsEnabled(s.ID()) {
			allOn = false
		}
	}
	var deps []string
	for _, s := range steps {
		m.setStep(s.ID(), !allOn)
	}
	if allOn {
		for _, s := range steps {
			deps = append(deps, m.dependents(s.ID())...)
		}
	}
	var cmd tea.Cmd
	if len(deps) > 0 {
		sort.Strings(deps)
		deps = dedupe(deps)
		cmd = m.setFlash(true, "Also skipped: %s", strings.Join(deps, ", "))
	}
	return tea.Batch(cmd, m.detect())
}

func dedupe(in []string) []string {
	var out []string
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// --- validation before the review screen -----------------------------------

type problem struct {
	idx  int
	text string
}

// validateOpts checks options once, before the review screen
// (architecture.md "Options and state").
func (m *model) validateOpts() []problem {
	var out []problem
	for i, s := range m.a.Steps {
		id := s.ID()
		if !m.a.Opts.IsEnabled(id) {
			continue
		}
		if ap, ok := m.ck.appl[id]; ok && !ap.OK {
			continue
		}
		for _, f := range m.stepFields(s) {
			k := id + "." + f.Key
			fd, ok := m.a.Opts.Fields[k]
			if !ok {
				continue
			}
			v := m.a.Opts.Values[k]
			if fd.Kind == options.Secret {
				// Empty is allowed: the step keeps the stored secret, or generates
				// one and shows it once on the summary screen (catalog step 19).
				if sv, _ := v.(string); sv == "" {
					continue
				}
			}
			if fd.Validate != nil {
				if err := fd.Validate(v); err != nil {
					out = append(out, problem{i, s.Title() + ": " + fd.Label + ": " + err.Error()})
				}
			}
		}
	}
	return out
}

func (m *model) toReview() tea.Cmd {
	if m.ck.detecting {
		return m.setFlash(true, "Still detecting what applies to this machine; try again in a moment.")
	}
	if probs := m.validateOpts(); len(probs) > 0 {
		lines := []string{"Fix these options before reviewing the plan:", ""}
		for _, p := range probs {
			lines = append(lines, m.g.warn+" "+p.text)
		}
		first := probs[0].idx
		m.modal = &modal{
			title: "Some options need attention",
			lines: lines,
			hints: []hint{h("O", "Open "+m.a.Steps[first].Title()), h("ESC", "Back")},
			keys: map[string]func() tea.Cmd{
				"o":     func() tea.Cmd { return m.openForm(first) },
				"esc":   func() tea.Cmd { m.cursorTo(first); return nil },
				"enter": func() tea.Cmd { m.cursorTo(first); return nil },
			},
		}
		return nil
	}
	m.scr = scrReview
	return m.startPreview()
}

func (m *model) cursorTo(idx int) {
	s := m.a.Steps[idx]
	m.ck.folded[s.Category()] = false
	for i, r := range m.ckRows() {
		if r.idx == idx {
			m.ck.cursor = i
		}
	}
}

// --- view -------------------------------------------------------------------

func (m *model) ckListH() int { return m.bodyH() - 1 - 2 }

func (m *model) box(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

func (m *model) catBox(cat string) string {
	on, n := 0, 0
	for _, s := range m.catSteps(cat) {
		n++
		if m.a.Opts.IsEnabled(s.ID()) {
			on++
		}
	}
	switch on {
	case n:
		return "[x]"
	case 0:
		return "[ ]"
	}
	return "[-]"
}

func (m *model) viewChecklist() ([]string, []hint, string) {
	ck := &m.ck
	cw := m.cw()
	rows := m.ckRows()
	ck.cursor = max(0, min(ck.cursor, len(rows)-1))

	on, total := m.enabledCount()
	right := fmt.Sprintf("%d of %d on%sfilter: %s", on, total, m.g.dot, ck.filter)
	if ck.filtering {
		right += "_"
	} else if ck.filter == "" {
		right += "_"
	}
	left := m.st.white15.Render("Choose what to set up")
	if ck.detecting {
		left += "  " + m.spin() + " Detecting what applies..."
	}
	head := left + spaces(cw-width(left)-width(right)) + right

	listW := cw
	wide := cw >= wideW-2
	if wide {
		listW = cw * 62 / 100
	}
	inner := listW - 4
	lh := max(1, m.ckListH()) // panel inner height
	if ck.cursor < ck.top {
		ck.top = ck.cursor
	}
	if ck.cursor >= ck.top+lh {
		ck.top = ck.cursor - lh + 1
	}
	ck.top = max(0, min(ck.top, max(0, len(rows)-lh)))

	titleW := 0
	for _, r := range rows {
		if r.idx >= 0 {
			titleW = max(titleW, width(m.a.Steps[r.idx].Title()))
		}
	}
	titleW = min(titleW, inner*55/100)

	var lines []string
	if len(rows) == 0 {
		lines = append(lines, "Nothing matches "+fmt.Sprintf("%q", ck.filter)+". Esc clears the filter.")
	}
	for i := ck.top; i < len(rows) && i < ck.top+lh; i++ {
		r := rows[i]
		var plain, styled string
		if r.idx < 0 {
			fold := "v"
			if ck.folded[r.cat] && strings.TrimSpace(ck.filter) == "" {
				fold = ">"
			}
			plain = fold + " " + m.catBox(r.cat) + " " + step.CategoryTitle(r.cat)
			if fold == ">" {
				plain += fmt.Sprintf(" (%d)", len(m.catSteps(r.cat)))
			}
			styled = plain
		} else {
			s := m.a.Steps[r.idx]
			enabled := m.a.Opts.IsEnabled(s.ID())
			reason := ""
			if ap, ok := ck.appl[s.ID()]; ok && !ap.OK {
				reason = "not applicable: " + ap.Reason
			}
			pre := "    " + m.box(enabled) + " "
			// Titles are cut only to make room for a reason.
			title := m.cut(s.Title(), inner-width(pre))
			if reason != "" {
				title = m.fit(s.Title(), titleW)
			}
			plain = pre + title
			if reason != "" {
				plain += "  " + reason
			}
			t := title
			if enabled && reason == "" {
				t = m.st.white15.Render(title)
			}
			styled = pre + t
			if reason != "" {
				styled += "  " + m.cut(reason, inner-width(pre)-titleW-2)
			}
		}
		if i == ck.cursor {
			lines = append(lines, m.st.sel.Render(m.fit(plain, inner)))
		} else {
			lines = append(lines, styled)
		}
	}
	pos := ""
	if len(rows) > lh {
		pos = fmt.Sprintf("%d-%d of %d", ck.top+1, min(len(rows), ck.top+lh), len(rows))
	}
	list := m.panel(titled("Steps", pos), lines, listW, lh+2)
	body := []string{head}
	if wide {
		var cur ckRow
		if len(rows) > 0 {
			cur = rows[ck.cursor]
		} else {
			cur.idx = -1
		}
		dw := cw - listW - 1
		details := m.panel("Details", m.details(cur, dw-4), dw, lh+2)
		body = append(body, sideBySide(list, details, listW, 1)...)
	} else {
		body = append(body, list...)
	}

	hints := []hint{h("SPACE", "Toggle"), h("ENTER", "Review"), h("O", "Options"), h("C", "Category"), h("/", "Filter"),
		h("A", "All on"), h("N", "All off"), h("D", "Defaults"), h("ESC", "Back"), h("F3", "Quit")}
	if ck.filtering {
		hints = []hint{h("TYPE", "Filter"), h("ENTER", "Keep filter"), h("ESC", "Clear filter"), h("F3", "Quit")}
	}
	return body, hints, "Choose steps"
}

func (m *model) details(r ckRow, w int) []string {
	var out []string
	add := func(s string) { out = append(out, wrapIndent(s, w, 2)...) }
	if r.idx < 0 {
		if r.cat == "" {
			return nil
		}
		steps := m.catSteps(r.cat)
		on := 0
		for _, s := range steps {
			if m.a.Opts.IsEnabled(s.ID()) {
				on++
			}
		}
		out = append(out, m.st.white15.Render(m.cut(step.CategoryTitle(r.cat), w)))
		add(fmt.Sprintf("%d of %d steps on.", on, len(steps)))
		out = append(out, "")
		add("C or Space turns the whole category on or off. Left/Right folds it.")
		return out
	}
	s := m.a.Steps[r.idx]
	id := s.ID()
	out = append(out, m.st.white15.Render(m.cut(s.Title(), w)))
	add("ID: " + id + "   Category: " + step.CategoryTitle(s.Category()))
	if m.a.Opts.IsEnabled(id) {
		add("On.")
	} else {
		add("Off: skipped by you.")
	}
	switch ap, ok := m.ck.appl[id]; {
	case !ok:
		add("Checking whether it applies...")
	case ap.OK:
		add("Applies to this machine.")
	default:
		add("Not applicable: " + ap.Reason)
	}
	if deps := s.DependsOn(); len(deps) > 0 {
		var t []string
		for _, d := range deps {
			t = append(t, m.stepTitle(d))
		}
		add("Needs: " + strings.Join(t, ", "))
	}
	fields := m.stepFields(s)
	if len(fields) == 0 {
		out = append(out, "")
		add("No options.")
		return out
	}
	out = append(out, "", "Options (O to change):")
	for _, f := range fields {
		k := id + "." + f.Key
		add("  " + f.Label + ": " + m.optValue(k))
	}
	return out
}

// optValue renders an option's current value for display (never a secret).
func (m *model) optValue(k string) string {
	fd := m.a.Opts.Fields[k]
	v := m.a.Opts.Values[k]
	switch fd.Kind {
	case options.Bool:
		b, _ := v.(bool)
		return yesNo(b)
	case options.Secret:
		sv, _ := v.(string)
		switch {
		case sv == "":
			return "not set"
		case m.generated[k]:
			return "generated (shown once in the summary)"
		}
		return "set"
	case options.List:
		l, _ := v.([]string)
		if len(l) == 0 {
			return "none"
		}
		return strings.Join(l, ", ")
	case options.Choice:
		sv, _ := v.(string)
		for _, c := range fd.Choices {
			if c.Value == sv {
				return c.Label
			}
		}
		return sv
	}
	s := fmt.Sprint(v)
	if s == "" {
		return "(empty)"
	}
	return s
}
