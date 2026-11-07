package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/satya-janghu/kiln/internal/app"
)

// Screen 0 (docs/tui-design.md "Before anything else"; architecture.md
// "Interrupted runs and the next start"). Order: single instance (handled
// before the program starts), the pending revert, the interrupted run and
// dpkg repair, stale runtime masks, low battery.

type preStage int

const (
	stChecking preStage = iota
	stPending
	stInterrupted
	stBusy
	stResult
	stBattery
)

type preState struct {
	checked bool
	p       app.Prechecks
	stage   preStage

	busy     bool
	busyText string
	result   []string
	failed   bool
	log      logBuf

	pendingDone, interruptedDone, masksDone, batteryDone bool
	// notes for the facts screen's Warnings line
	notes []string
}

type preChecksMsg struct{ p app.Prechecks }

func (m *model) checkPre() tea.Cmd {
	ctx, a := m.c.ctx, m.a
	return func() tea.Msg { return preChecksMsg{a.Check(ctx)} }
}

func (m *model) onPreChecks(msg preChecksMsg) tea.Cmd {
	m.pre.checked, m.pre.p = true, msg.p
	return m.preAdvance()
}

// preAdvance moves to the next precheck that applies, or to the facts screen.
func (m *model) preAdvance() tea.Cmd {
	ps := &m.pre
	ps.result, ps.failed = nil, false
	p := ps.p
	switch {
	case p.Pending != nil && !ps.pendingDone:
		ps.stage = stPending
		return nil
	case (p.Interrupted != nil || p.DpkgInterrupted) && !ps.interruptedDone:
		ps.stage = stInterrupted
		return nil
	case p.StaleMasks && !ps.masksDone:
		ps.masksDone = true
		if !m.gateOK {
			ps.notes = append(ps.notes, "Automatic updates are still paused by a kiln run that crashed. A reboot turns them back on.")
			return m.preAdvance()
		}
		return m.startTask(taskMasks, "Turning automatic updates back on (a crashed run left them paused)", m.a.ClearStaleMasks)
	case p.LowBattery && !ps.batteryDone:
		ps.stage = stBattery
		return nil
	}
	m.scr = scrFacts
	return nil
}

func (m *model) startTask(kind taskKind, text string, fn func(ctx context.Context) error) tea.Cmd {
	m.pre.busy, m.pre.busyText, m.pre.stage = true, text, stBusy
	m.pre.log = logBuf{follow: true}
	m.c.task(kind, fn)
	return m.ensureTick()
}

func (m *model) onTaskDone(msg taskDoneMsg) tea.Cmd {
	ps := &m.pre
	ps.busy = false
	ps.stage = stResult
	ps.failed = msg.err != nil
	errText := ""
	if msg.err != nil {
		errText = m.a.Red.Apply(msg.err.Error())
		m.a.Log.Error("precheck repair failed", "task", int(msg.kind), "err", errText)
	}
	switch msg.kind {
	case taskRevert:
		ps.pendingDone = true
		if msg.err == nil {
			ps.result = []string{"The unfinished change was reverted: everything it touched is back to how it was before."}
		} else {
			ps.result = []string{
				"!! The revert didn't finish: " + errText,
				"",
				"The backups are under /var/lib/kiln/backup and the details are in /var/log/kiln.log. You can continue, but check the network or SSH settings first.",
			}
			ps.notes = append(ps.notes, "Reverting the unfinished change from last time failed; see /var/log/kiln.log.")
		}
	case taskRepair:
		ps.interruptedDone = true
		if msg.err == nil {
			if ps.p.Interrupted != nil {
				m.a.AcknowledgeInterrupted()
			}
			ps.result = []string{"The package system is repaired. kiln will now run again; every step is safe to repeat."}
		} else {
			ps.result = []string{
				"!! The package system couldn't be repaired: " + errText,
				"",
				"Try it by hand: sudo dpkg --configure -a, then sudo apt-get -f install. Then start kiln again. If you continue, steps that install packages will probably fail.",
			}
			ps.notes = append(ps.notes, "The package system needs repair (dpkg was interrupted); package steps will probably fail.")
		}
	case taskMasks:
		if msg.err == nil {
			ps.result = nil
			if m.signaled {
				return tea.Quit
			}
			return m.preAdvance()
		}
		ps.result = []string{
			"!! Automatic updates couldn't be turned back on: " + errText,
			"",
			"A reboot turns them back on. kiln pauses them again during its run anyway.",
		}
	}
	if m.signaled {
		return tea.Quit
	}
	return nil
}

func (m *model) keyPre(k string) tea.Cmd {
	ps := &m.pre
	switch ps.stage {
	case stPending:
		if !m.gateOK {
			if k == "enter" {
				ps.pendingDone = true
				ps.notes = append(ps.notes, "A change from an earlier run is unfinished: "+ps.p.Pending.Desc)
				return m.preAdvance()
			}
			return nil
		}
		switch k {
		case "y":
			return m.startTask(taskRevert, "Reverting the unfinished change", m.a.RevertPending)
		case "n":
			ps.pendingDone = true
			ps.notes = append(ps.notes, "A change from an earlier run is unfinished and was left as it is: "+ps.p.Pending.Desc)
			return m.preAdvance()
		}
	case stInterrupted:
		if k != "enter" {
			return nil
		}
		if !m.gateOK {
			ps.interruptedDone = true
			if ps.p.DpkgInterrupted {
				ps.notes = append(ps.notes, "The package system needs repair (dpkg was interrupted).")
			}
			return m.preAdvance()
		}
		if ps.p.DpkgInterrupted {
			return m.startTask(taskRepair, "Repairing the package system (dpkg --configure -a, then apt-get -f install)", m.a.RepairDpkg)
		}
		ps.interruptedDone = true
		m.a.AcknowledgeInterrupted()
		return m.preAdvance()
	case stResult:
		if k == "enter" {
			return m.preAdvance()
		}
	case stBattery:
		if k == "c" {
			ps.batteryDone = true
			return m.preAdvance()
		}
	}
	return nil
}

func (m *model) stepTitle(id string) string {
	for _, s := range m.a.Steps {
		if s.ID() == id {
			return s.Title()
		}
	}
	return id
}

func (m *model) viewPre() ([]string, []hint, string) {
	ps := &m.pre
	cw := m.cw()
	quit := h("F3", "Quit")
	var lines []string
	var hints []hint
	right := "Checking this machine"
	noGate := ""
	if !m.gateOK {
		noGate = "kiln won't change this machine (" + m.gateWhy + "), so it can't do this now."
	}
	switch {
	case !ps.checked:
		return []string{"", m.spin() + " Checking this machine before anything else..."}, []hint{quit}, right
	case ps.stage == stPending:
		p := ps.p.Pending
		lines = []string{
			"A change didn't finish last time.",
			"",
			fmt.Sprintf("%s (%s, started %s). kiln died or lost power in the middle of it.",
				p.Desc, m.stepTitle(p.Step), p.Started.Local().Format("Jan 2 15:04")),
			"",
		}
		if noGate != "" {
			lines = append(lines, noGate)
			hints = []hint{h("ENTER", "Continue"), quit}
		} else {
			lines = append(lines, "Revert it now? kiln restores the files it backed up and restarts what it changed.")
			hints = []hint{h("Y", "Revert now"), h("N", "Leave it"), quit}
		}
		right = "Unfinished change"
	case ps.stage == stInterrupted:
		r := ps.p.Interrupted
		if r != nil {
			where := "before its first step"
			if r.Step != "" {
				where = "during " + m.stepTitle(r.Step)
			}
			lines = []string{fmt.Sprintf("The last run stopped %s (it started at %s; power lost?).", where, r.Started.Local().Format("Jan 2 15:04")), ""}
		} else {
			lines = []string{"The package system was left half-configured (dpkg was interrupted).", ""}
		}
		switch {
		case noGate != "":
			lines = append(lines, noGate)
		case ps.p.DpkgInterrupted:
			lines = append(lines, "kiln will repair the package system and then run again; every step is safe to repeat.")
		default:
			lines = append(lines, "The package system is fine. kiln will run again; every step is safe to repeat.")
		}
		hints = []hint{h("ENTER", "Continue"), quit}
		right = "Last run interrupted"
	case ps.stage == stBusy:
		lines = []string{m.spin() + " " + ps.busyText + "...", "", "Please wait. This can't be stopped halfway."}
		hints = []hint{}
		right = ps.busyText
		if ps.log.len() > 0 {
			ph := m.bodyH() - len(lines) - 1
			pw := cw
			lines = append(lines, "")
			lines = append(lines, m.panel("Output", ps.log.view(pw-4, ph-2), pw, ph)...)
		}
		return lines, hints, right
	case ps.stage == stResult:
		lines = ps.result
		hints = []hint{h("ENTER", "Continue"), quit}
		right = "Repair finished"
		if ps.failed {
			right = "Repair failed"
		}
	case ps.stage == stBattery:
		lines = []string{
			fmt.Sprintf("Plug in the charger. The battery is at %d%% and the machine is not on AC power.", m.a.Facts.BatteryPercent),
			"",
			"A run that loses power while installing packages can break the system.",
		}
		hints = []hint{h("C", "Continue anyway"), quit}
		right = "Low battery"
	}
	var out []string
	out = append(out, "")
	for _, l := range lines {
		if l == "" {
			out = append(out, "")
			continue
		}
		styled := strings.HasPrefix(l, "!! ")
		for _, w := range wrap(l, cw-2) {
			if styled {
				w = m.st.white15.Render(w)
			}
			out = append(out, "  "+w)
		}
	}
	return out, hints, right
}

// --- another kiln is running -----------------------------------------------

func (m *model) keyLocked(k string) tea.Cmd {
	switch k {
	case "q":
		return tea.Quit
	case "l":
		return m.openLog(scrLocked)
	}
	return nil
}

func (m *model) viewLocked() ([]string, []hint, string) {
	lines := []string{
		"",
		"  " + m.st.white15.Render(fmt.Sprintf("kiln is already running (pid %d).", m.lockPID)),
		"",
	}
	for _, l := range wrap("Only one kiln can run at a time. The other one keeps going even if its window was closed, and logs everything to /var/log/kiln.log. Its result is shown the next time you start kiln.", m.cw()-2) {
		lines = append(lines, "  "+l)
	}
	return lines, []hint{h("L", "View its log"), h("Q", "Quit")}, "Already running"
}
