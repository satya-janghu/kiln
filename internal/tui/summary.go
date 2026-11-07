package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/satya-janghu/kiln/internal/logx"
	"github.com/satya-janghu/kiln/internal/runner"
)

// Screen 6: the summary.

func (m *model) keySummary(k string) tea.Cmd {
	switch k {
	case "q":
		return tea.Quit
	case "l":
		return m.openLog(scrSummary)
	case "r":
		lines := []string{"Reboot this machine now?", ""}
		if len(m.secretsToShow()) > 0 {
			lines = append(lines, "Write down the password shown on the summary first: it isn't shown again (root can read it later with: sudo kiln secrets show).", "")
		}
		lines = append(lines, "kiln has already saved its state, the summary and the log.")
		m.modal = &modal{
			title: "Reboot now?",
			lines: lines,
			hints: []hint{h("Y", "Reboot"), h("N", "Back")},
			keys: map[string]func() tea.Cmd{
				"y": func() tea.Cmd { m.c.requestReboot(); return tea.Quit },
				"n": nil, "esc": nil,
			},
		}
	default:
		m.sum.key(k)
	}
	return nil
}

type shownSecret struct{ label, value string }

// secretsToShow returns the secrets generated during this session, shown
// once (they never reach the log or kiln-summary.txt).
func (m *model) secretsToShow() []shownSecret {
	var out []shownSecret
	gen := m.a.Secrets.Generated()
	names := make([]string, 0, len(gen))
	for n := range gen {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := gen[n]
		if v == "" {
			continue
		}
		label := n
		if fd, ok := m.a.Opts.Fields[n]; ok {
			// An option the user later changed to a typed password, or one that
			// was reset, is not the generated value any more.
			if !m.generated[n] || m.a.Opts.Values[n] != v {
				continue
			}
			label = fd.Label
			if i := strings.IndexByte(n, '.'); i > 0 {
				label = m.stepTitle(n[:i]) + ", " + fd.Label
			}
		}
		out = append(out, shownSecret{label, v})
	}
	return out
}

func (m *model) summaryLines(w int) []string {
	rs := &m.run
	sum := rs.sum
	var out []string
	add := func(s string, ind int) { out = append(out, wrapIndent(s, w, ind)...) }
	white := func(s string, ind int) {
		for _, l := range wrapIndent(s, w, ind) {
			out = append(out, m.st.white15.Render(l))
		}
	}
	if sum == nil {
		white(m.g.err+" The run ended without a result.", 3)
		if rs.err != nil {
			add("   "+rs.err.Error(), 3)
		}
		add("   The details are in "+logx.Path+". Every step is safe to run again: start sudo kiln again.", 3)
		return out
	}
	counts := fmt.Sprintf("%d done%s%d unchanged%s%d failed%s%d skipped",
		sum.Count(runner.Done), m.g.dot, sum.Count(runner.Unchanged), m.g.dot, sum.Count(runner.Failed), m.g.dot, sum.Count(runner.Skipped))
	if sum.Stopped {
		white("Stopped early after "+dur(sum.Duration)+": "+counts, 2)
		add("Run sudo kiln again to finish; every step is safe to repeat.", 0)
	} else {
		white("Done in "+dur(sum.Duration)+": "+counts, 2)
	}
	if rs.err != nil {
		white(m.g.err+" "+rs.err.Error(), 3)
	}
	for _, r := range sum.Results {
		if r.Status != runner.Failed {
			continue
		}
		for _, l := range wrapIndent(m.g.err+" "+r.Title+":", w, 3) {
			out = append(out, m.st.red9.Render(l))
		}
		add("    "+m.a.Red.Apply(r.Reason), 4)
	}
	var skipped []string
	for _, r := range sum.Results {
		if r.Status == runner.Skipped && r.Reason != "by you" && !strings.HasPrefix(r.Reason, "not applicable") {
			skipped = append(skipped, r.Title+" ("+r.Reason+")")
		}
	}
	if len(skipped) > 0 {
		out = append(out, "")
		add("Skipped: "+strings.Join(skipped, ", "), 2)
	}
	if len(sum.Notes) > 0 {
		out = append(out, "", "Notes")
		for _, n := range sum.Notes {
			add(" - "+m.a.Red.Apply(n), 3)
		}
	}
	out = append(out, "")
	if len(sum.Reboot) > 0 {
		for _, l := range wrapIndent("Reboot needed: "+strings.Join(sum.Reboot, "; "), w, 2) {
			out = append(out, m.st.yellow11.Render(l))
		}
	} else {
		out = append(out, "No reboot needed.")
	}
	if sec := m.secretsToShow(); len(sec) > 0 {
		out = append(out, "")
		pw := min(w, 76)
		var inner []string
		for _, s := range sec {
			if one := s.label + ": " + s.value; width(one) <= pw-4 {
				inner = append(inner, m.st.white15.Render(one))
				continue
			}
			inner = append(inner, m.cut(s.label+":", pw-4), m.st.white15.Render(s.value))
		}
		inner = append(inner, "")
		inner = append(inner, wrap("kiln shows this only once. Root can read it again with: sudo kiln secrets show", pw-4)...)
		out = append(out, m.panel("Write this down now", inner, pw, len(inner)+2)...)
	}
	out = append(out, "")
	add("The summary is saved in "+runner.SummaryPath+" (without passwords). The full log is "+logx.Path+".", 0)
	out = append(out, "", "All steps")
	for _, r := range sum.Results {
		line := m.fit(m.marker(r.Status), 2) + " " + r.Title + ": " + string(r.Status)
		if r.Reason != "" && r.Status != runner.Done {
			line += " (" + m.a.Red.Apply(r.Reason) + ")"
		}
		add(line, 5)
	}
	return out
}

func (m *model) viewSummary() ([]string, []hint, string) {
	cw, bh := m.cw(), m.bodyH()
	lines := m.summaryLines(cw - 4)
	body := m.sum.window(lines, bh-2)
	title := "Summary"
	if m.run.sum != nil && m.run.sum.Count(runner.Failed) > 0 {
		title = "Summary: something failed"
	}
	hints := []hint{h("R", "Reboot now"), h("L", "Full log"), h("Q", "Quit"), h("PGUP/PGDN", "Scroll"), h("?", "Help")}
	right := "Setup finished"
	if m.run.sum != nil && m.run.sum.Stopped {
		right = "Setup stopped"
	}
	return m.panel(titled(title, m.sum.position(len(lines))), body, cw, bh), hints, right
}
