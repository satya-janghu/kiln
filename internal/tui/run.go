package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/satya-janghu/kiln/internal/runner"
)

// Screen 5: the run.

type runState struct {
	started   time.Time
	total     int
	results   map[string]*runner.StepResult
	current   string // step ID running now
	curTitle  string
	stepStart time.Time
	status    string // short status (risky-step checks, phases)
	log       logBuf
	logFull   bool
	stopping  bool
	finished  bool
	sum       *runner.Summary
	err       error
	listTop   int
	listCur   string // step the list was last centred on
}

func (m *model) startRun() tea.Cmd {
	// Choices are stored before anything changes (secrets are never stored:
	// SaveChoices skips Secret fields). The runner saves state.json when it begins.
	m.a.SaveChoices()
	m.run = runState{started: time.Now(), total: len(m.a.Steps), results: map[string]*runner.StepResult{},
		log: logBuf{follow: true}}
	m.scr = scrRun
	m.a.Log.Info("wizard: starting the run")
	m.c.startRun()
	return m.ensureTick()
}

func (m *model) onEvent(e runner.Event) {
	rs := &m.run
	if rs.results == nil {
		return
	}
	g := m.g
	switch e.Kind {
	case runner.EvPhase:
		rs.status = e.Text
		rs.log.add(g.same + " " + m.a.Red.Apply(e.Text))
	case runner.EvStepStart:
		if e.Total > 0 {
			rs.total = e.Total
		}
		rs.current, rs.curTitle, rs.stepStart, rs.status = e.Step, e.Title, time.Now(), ""
		rs.log.add(g.running + " " + e.Title)
	case runner.EvStepEnd:
		if e.Result != nil {
			r := *e.Result
			rs.results[e.Step] = &r
			line := m.fit(m.marker(r.Status), 2) + " " + r.Title + ": " + string(r.Status)
			if r.Reason != "" && r.Status != runner.Done {
				line += " (" + m.a.Red.Apply(r.Reason) + ")"
			}
			rs.log.add(line)
		}
		if rs.current == e.Step {
			rs.current, rs.curTitle, rs.status = "", "", ""
		}
	case runner.EvOutput:
		rs.log.add(m.a.Red.Apply(e.Text))
	case runner.EvProgress:
		rs.status = m.a.Red.Apply(e.Text)
		rs.log.add("   " + rs.status)
	}
}

func (m *model) onRunDone(msg runDoneMsg) tea.Cmd {
	rs := &m.run
	rs.finished, rs.sum, rs.err = true, msg.sum, msg.err
	rs.current = ""
	m.modal = nil
	m.scr = scrSummary
	m.sum = scroll{}
	m.a.Log.Info("wizard: the run finished")
	if m.signaled {
		return tea.Quit
	}
	return nil
}

func (m *model) keyRun(k string) tea.Cmd {
	rs := &m.run
	switch k {
	case "l":
		rs.logFull = !rs.logFull
	case "up", "k":
		rs.listTop--
	case "down", "j":
		rs.listTop++
	default:
		rs.log.key(k)
	}
	return nil
}

func (m *model) runStatus(id string) runner.Status {
	if r, ok := m.run.results[id]; ok {
		return r.Status
	}
	if id == m.run.current {
		return runner.Running
	}
	return runner.Pending
}

func (m *model) viewRun() ([]string, []hint, string) {
	rs := &m.run
	cw, bh := m.cw(), m.bodyH()
	done := len(rs.results)
	total := max(1, rs.total)
	var info string
	switch {
	case rs.current != "":
		info = fmt.Sprintf("Step %d of %d: %s (%s)", min(done+1, total), total, rs.curTitle, dur(time.Since(rs.stepStart)))
	case done >= total:
		info = "Finishing up"
	default:
		info = "Getting ready"
	}
	elapsed := dur(time.Since(rs.started)) + " in total"
	if width(info)+3+width(elapsed) <= cw {
		info = m.st.white15.Render(info) + spaces(cw-width(info)-width(elapsed)) + elapsed
	} else {
		info = m.st.white15.Render(m.cut(info, cw))
	}
	out := []string{info}
	out = append(out, m.progressBar(float64(done)/float64(total), cw)...)
	status := rs.status
	if rs.stopping {
		status = joinNonEmpty(m.g.dot, "Stopping after the current step", status)
	}
	if status != "" {
		status = m.spin() + " " + status
	}
	out = append(out, m.st.yellow11.Render(m.cut(status, cw)), "")
	rest := bh - len(out)

	logTitle := "Log"
	if !rs.log.follow {
		logTitle = "Log (scrolled; End follows it again)"
	}
	switch {
	case rs.logFull:
		out = append(out, m.panel(logTitle, rs.log.view(cw-4, rest-2), cw, rest)...)
	case cw >= wideW-2:
		lw := max(40, cw*30/100)
		list := m.panel("Steps", m.runList(lw-4, rest-2), lw, rest)
		logW := cw - lw - 1
		logP := m.panel(logTitle, rs.log.view(logW-4, rest-2), logW, rest)
		out = append(out, sideBySide(list, logP, lw, 1)...)
	default:
		lh := max(4, min(rest/3+1, len(m.a.Steps)+2))
		out = append(out, m.panel("Steps", m.runList(cw-4, lh-2), cw, lh)...)
		out = append(out, m.panel(logTitle, rs.log.view(cw-4, rest-lh-2), cw, rest-lh)...)
	}
	hints := []hint{h("F3", "Stop after this step"), h("L", "Full log"), h("PGUP/PGDN", "Scroll log"), h("?", "Help")}
	right := rs.status
	if right == "" && rs.curTitle != "" {
		right = rs.curTitle
	}
	if right == "" {
		right = "Working"
	}
	return out, hints, right
}

// runList is the step list with status markers; it keeps the running step
// in view unless the user scrolled.
func (m *model) runList(w, hgt int) []string {
	rs := &m.run
	steps := m.a.Steps
	if hgt <= 0 {
		return nil
	}
	cur := -1
	for i, s := range steps {
		if s.ID() == rs.current {
			cur = i
		}
	}
	if cur >= 0 && rs.listCur != rs.current {
		rs.listCur = rs.current
		if cur < rs.listTop || cur >= rs.listTop+hgt {
			rs.listTop = cur - hgt/2
		}
	}
	rs.listTop = max(0, min(rs.listTop, max(0, len(steps)-hgt)))
	var out []string
	for i := rs.listTop; i < len(steps) && i < rs.listTop+hgt; i++ {
		s := steps[i]
		st := m.runStatus(s.ID())
		mk := m.fit(m.marker(st), 2)
		line := mk + " " + s.Title()
		if r, ok := rs.results[s.ID()]; ok && r.Status != runner.Skipped {
			d := dur(r.Duration)
			line = m.fit(line, w-width(d)-1) + " " + d
		}
		switch st {
		case runner.Failed:
			line = m.st.red9.Render(m.cut(line, w))
		case runner.Running:
			line = m.st.sel.Render(m.fit(line, w))
		case runner.Done:
			line = m.st.white15.Render(m.cut(line, w))
		}
		out = append(out, line)
	}
	return out
}
