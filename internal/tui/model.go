package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/satya-janghu/kiln/internal/app"
	"github.com/satya-janghu/kiln/internal/step"
)

type screen int

const (
	scrPre       screen = iota // 0: before anything else
	scrLocked                  // 0: another kiln is running
	scrFacts                   // 1
	scrAllFacts                // 1: "f all facts"
	scrChecklist               // 2
	scrForm                    // 3
	scrReview                  // 4
	scrRun                     // 5
	scrSummary                 // 6
	scrLog                     // full log viewer
)

// Layout: title (2 lines) + blank, body, message line, status bar.
const (
	headerH = 3
	footerH = 2
	minW    = 80
	minH    = 24
	wideW   = 120
)

type model struct {
	c     *controller
	a     *app.App
	force bool

	w, h int
	g    glyphs
	st   *styles
	tc   bool // truecolor palette

	scr     screen
	logBack screen // where the log viewer returns to
	modal   *modal
	help    bool
	frame   int
	ticking bool

	flash     string
	flashWarn bool
	flashAt   time.Time

	gateOK  bool
	gateWhy string
	lockPID int
	lockErr error

	signaled bool

	categories []string

	pre  preState
	fact scroll
	ck   checklistState
	form *formState
	rv   reviewState
	run  runState
	sum  scroll
	logv logView

	// option keys whose current value kiln generated (shown once in the summary)
	generated map[string]bool
}

func newModel(c *controller, force bool, lockPID int, lockErr error) *model {
	a := c.a
	m := &model{c: c, a: a, force: force, g: pickGlyphs(), lockPID: lockPID, lockErr: lockErr,
		generated: map[string]bool{}}
	m.st = newStyles(false)
	m.gateOK, m.gateWhy = a.Gate(force)
	if m.gateOK && lockErr != nil && lockPID <= 0 {
		m.gateOK = false
		m.gateWhy = "kiln couldn't take its single-instance lock (" + lockErr.Error() + ")"
	}
	seen := map[string]bool{}
	for _, cat := range step.Categories {
		seen[cat] = true
		m.categories = append(m.categories, cat)
	}
	for _, s := range a.Steps {
		if !seen[s.Category()] {
			seen[s.Category()] = true
			m.categories = append(m.categories, s.Category())
		}
	}
	m.ck.folded = map[string]bool{}
	m.ck.appl = map[string]step.Applicability{}
	if lockPID > 0 {
		m.scr = scrLocked
	} else {
		m.scr = scrPre
	}
	return m
}

func (m *model) Init() tea.Cmd {
	if m.scr == scrLocked {
		return nil
	}
	return tea.Batch(m.checkPre(), m.ensureTick())
}

// --- ticking (spinner and clocks) ------------------------------------------

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) busy() bool {
	return !m.pre.checked && m.scr == scrPre || m.pre.busy || m.ck.detecting || m.rv.inflight ||
		m.scr == scrRun && !m.run.finished || m.flash != ""
}

func (m *model) ensureTick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return tickCmd()
}

func (m *model) spin() string { return m.g.spinner[m.frame%len(m.g.spinner)] }

func (m *model) setFlash(warn bool, format string, a ...any) tea.Cmd {
	m.flash, m.flashWarn, m.flashAt = fmt.Sprintf(format, a...), warn, time.Now()
	return m.ensureTick()
}

// --- update -----------------------------------------------------------------

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, m.resizeForm()
	case tea.ColorProfileMsg:
		tc := msg.Profile == colorprofile.TrueColor
		if tc != m.tc {
			m.tc = tc
			m.st = newStyles(tc)
			if m.form != nil {
				m.form.f.WithTheme(m.huhTheme())
			}
		}
		return m, nil
	case tickMsg:
		m.frame++
		if m.flash != "" && time.Since(m.flashAt) > 6*time.Second {
			m.flash = ""
		}
		if m.busy() {
			return m, tickCmd()
		}
		m.ticking = false
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case batchMsg:
		var cmds []tea.Cmd
		for _, x := range msg {
			cmds = append(cmds, m.handle(x))
		}
		return m, tea.Batch(cmds...)
	}
	if cmd, ok := m.handleOwn(msg); ok {
		return m, cmd
	}
	// Anything else belongs to the open Huh form (its internal messages).
	if m.form != nil {
		return m, m.updateForm(msg)
	}
	return m, nil
}

func (m *model) handle(msg tea.Msg) tea.Cmd {
	cmd, _ := m.handleOwn(msg)
	return cmd
}

func (m *model) handleOwn(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case preChecksMsg:
		return m.onPreChecks(msg), true
	case taskDoneMsg:
		return m.onTaskDone(msg), true
	case taskOutputMsg:
		m.pre.log.add(m.a.Red.Apply(msg.line))
		return nil, true
	case applMsg:
		return m.onAppl(msg), true
	case previewMsg:
		return m.onPreview(msg), true
	case previewProgressMsg:
		if msg.gen == m.rv.gen {
			m.rv.progress = msg.text
		}
		return nil, true
	case eventMsg:
		m.onEvent(msg.e)
		return nil, true
	case runDoneMsg:
		return m.onRunDone(msg), true
	case logLoadedMsg:
		return m.onLogLoaded(msg), true
	case logRefreshMsg:
		return m.onLogRefresh(msg), true
	case signalMsg:
		m.signaled = true
		if m.scr == scrRun && !m.run.finished {
			m.run.stopping = true
			return m.setFlash(true, "kiln got a stop signal: it stops after the current step and then exits."), true
		}
		return m.setFlash(true, "kiln got a stop signal: it exits once the repair has finished."), true
	}
	return nil, false
}

// key handles a key press: modal and help first, then the global keys, then
// the current screen.
func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if m.help {
		m.help = false
		return nil
	}
	if m.modal != nil {
		md := m.modal
		mk := k
		if len(mk) == 1 {
			mk = strings.ToLower(mk) // Y/N as well as y/n
		}
		if f, ok := md.keys[mk]; ok {
			m.modal = nil
			if f == nil {
				return nil
			}
			return f()
		}
		if k == "ctrl+c" || k == "f3" {
			if f, ok := md.keys["esc"]; ok {
				m.modal = nil
				if f != nil {
					return f()
				}
			}
		}
		return nil
	}
	if k == "ctrl+c" || k == "f3" {
		return m.quitRequest()
	}
	if m.w < minW || m.h < minH {
		return nil
	}
	if k == "?" && m.scr != scrForm && (m.scr != scrChecklist || !m.ck.filtering) {
		m.help = true
		return nil
	}
	switch m.scr {
	case scrPre:
		return m.keyPre(k)
	case scrLocked:
		return m.keyLocked(k)
	case scrFacts, scrAllFacts:
		return m.keyFacts(k)
	case scrChecklist:
		return m.keyChecklist(msg)
	case scrForm:
		return m.keyForm(msg)
	case scrReview:
		return m.keyReview(k)
	case scrRun:
		return m.keyRun(k)
	case scrSummary:
		return m.keySummary(k)
	case scrLog:
		return m.keyLog(k)
	}
	return nil
}

// quitRequest is ctrl+c / F3: during a run it only offers to stop after the
// current step; elsewhere it asks before quitting.
func (m *model) quitRequest() tea.Cmd {
	switch {
	case m.scr == scrRun && !m.run.finished:
		if m.run.stopping {
			return m.setFlash(true, "kiln is already stopping after the current step.")
		}
		m.modal = &modal{
			title: "Stop setup?",
			lines: []string{
				"Stop after the current step?",
				"",
				"kiln finishes the step it is on (it never stops apt halfway through an install), skips the rest, turns automatic updates back on and shows the summary. Every step is safe to run again later.",
			},
			hints: []hint{h("Y", "Stop after this step"), h("N", "Keep going")},
			keys: map[string]func() tea.Cmd{
				"y": func() tea.Cmd {
					m.run.stopping = true
					m.c.stopRun()
					return m.setFlash(true, "Stopping after the current step...")
				},
				"n": nil, "esc": nil,
			},
		}
		return nil
	case m.pre.busy:
		return m.setFlash(true, "kiln is repairing this machine and can't stop halfway. Please wait; it only takes a moment.")
	case m.scr == scrLocked || m.scr == scrSummary:
		return tea.Quit
	case m.scr == scrLog && (m.logBack == scrLocked || m.logBack == scrSummary):
		return tea.Quit
	}
	m.modal = &modal{
		title: "Quit kiln?",
		lines: []string{
			"Setup hasn't started, so nothing else on this machine will change.",
			"",
			"Run sudo kiln again whenever you're ready.",
		},
		hints: []hint{h("Y", "Quit"), h("N", "Back")},
		keys:  map[string]func() tea.Cmd{"y": func() tea.Cmd { return tea.Quit }, "n": nil, "esc": nil},
	}
	return nil
}

// --- view -------------------------------------------------------------------

func (m *model) View() tea.View {
	var content string
	switch {
	case m.w == 0 || m.h == 0:
		content = ""
	case m.w < minW || m.h < minH:
		msg := fmt.Sprintf("Make the window at least 80x24 (now %d%s%d)", m.w, m.g.times, m.h)
		content = m.st.paint([]string{m.cut(msg, m.w)}, m.w, m.h)
	default:
		content = m.st.paint(m.render(), m.w, m.h)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "kiln Setup"
	return v
}

// cw is the content width (one column margin on each side); bodyH is the
// height between the title and the status bar.
func (m *model) cw() int    { return m.w - 2 }
func (m *model) bodyH() int { return m.h - headerH - footerH }

func (m *model) render() []string {
	var body []string
	var hints []hint
	var right string
	switch {
	case m.help:
		body, hints = m.helpView()
	case m.modal != nil:
		body, hints = m.modalView()
		_, _, right = m.screenView()
	default:
		body, hints, right = m.screenView()
	}
	title := "kiln Setup"
	out := []string{" " + title, " " + repeat(m.g.rule, width(title)), ""}
	bh := m.bodyH()
	for i := 0; i < bh; i++ {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		out = append(out, " "+l)
	}
	msg := ""
	if m.flash != "" {
		f := m.cut(m.flash, m.cw())
		if m.flashWarn {
			f = m.st.yellow11.Render(f)
		} else {
			f = m.st.white15.Render(f)
		}
		msg = " " + f
	}
	return append(out, msg, m.statusBar(hints, right))
}

func (m *model) screenView() ([]string, []hint, string) {
	switch m.scr {
	case scrPre:
		return m.viewPre()
	case scrLocked:
		return m.viewLocked()
	case scrFacts:
		return m.viewFacts()
	case scrAllFacts:
		return m.viewAllFacts()
	case scrChecklist:
		return m.viewChecklist()
	case scrForm:
		return m.viewForm()
	case scrReview:
		return m.viewReview()
	case scrRun:
		return m.viewRun()
	case scrSummary:
		return m.viewSummary()
	case scrLog:
		return m.viewLog()
	}
	return nil, nil, ""
}

// --- modal and help ---------------------------------------------------------

// modal replaces the body with a centred panel, the way XP Setup asks.
type modal struct {
	title string
	lines []string
	hints []hint
	// keys maps a key to its action; a nil action just closes the modal.
	keys map[string]func() tea.Cmd
}

func (m *model) modalView() ([]string, []hint) {
	md := m.modal
	return m.centred(md.title, md.lines), md.hints
}

func (m *model) centred(title string, lines []string) []string {
	w := min(m.cw(), 76)
	var inner []string
	inner = append(inner, "")
	for _, l := range lines {
		if l == "" {
			inner = append(inner, "")
			continue
		}
		inner = append(inner, wrap(l, w-4)...)
	}
	inner = append(inner, "")
	maxH := m.bodyH()
	hgt := min(len(inner)+2, maxH)
	box := m.panel(title, inner, w, hgt)
	top := (maxH - len(box)) / 2
	pad := spaces((m.cw() - w) / 2)
	out := make([]string, 0, maxH)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, l := range box {
		out = append(out, pad+l)
	}
	return out
}

func (m *model) helpView() ([]string, []hint) {
	common := []string{
		"",
		"Up/Down or k/j: move   Enter: next   Esc: back (your edits are kept)",
		"F3 or Ctrl+C: quit (during a run: stop after the current step)",
	}
	var own []string
	switch m.scr {
	case scrPre, scrLocked:
		own = []string{"Y/N               answer the question", "C                 continue anyway (low battery)", "L                 view the log"}
	case scrFacts, scrAllFacts:
		own = []string{"F                 all facts", "PgUp/PgDn         scroll"}
	case scrChecklist:
		own = []string{
			"Space             turn the step on or off",
			"C                 turn the whole category on or off",
			"/                 filter by title, ID or category (Enter keeps it, Esc clears it)",
			"A                 turn everything on",
			"N                 turn everything off (asks first)",
			"O                 options for this step",
			"D                 reset to kiln's defaults",
			"Left/Right        fold or unfold a category",
			"Enter             review the plan",
		}
	case scrReview:
		own = []string{"Tab/Shift+Tab     next/previous tab (or Left/Right, 1-6)", "PgUp/PgDn         scroll", "Enter             start setting up this machine"}
	case scrRun:
		own = []string{"PgUp/PgDn         scroll the log (End follows it again)", "L                 log full screen"}
	case scrSummary:
		own = []string{"R                 reboot now", "L                 full log", "Q                 quit"}
	case scrLog:
		own = []string{"PgUp/PgDn/Home/End scroll", "Esc               back"}
	}
	return m.centred("Help", append(own, common...)), []hint{h("ANY KEY", "Close help")}
}

// --- shared scrolling helper ------------------------------------------------

type scroll struct {
	top   int
	lastH int
}

// window returns the visible part of lines and clamps the position.
func (s *scroll) window(lines []string, h int) []string {
	s.lastH = h
	maxTop := max(0, len(lines)-h)
	s.top = max(0, min(s.top, maxTop))
	return lines[s.top:min(len(lines), s.top+h)]
}

func (s *scroll) key(k string) bool {
	page := max(1, s.lastH-1)
	switch k {
	case "up", "k":
		s.top--
	case "down", "j":
		s.top++
	case "pgup", "b":
		s.top -= page
	case "pgdown", "space":
		s.top += page
	case "home", "g":
		s.top = 0
	case "end", "G":
		s.top = 1 << 30
	default:
		return false
	}
	return true
}

// position is "lines a-b of n" for panel titles.
func (s *scroll) position(n int) string {
	if n <= s.lastH {
		return ""
	}
	return fmt.Sprintf("%d-%d of %d", s.top+1, min(n, s.top+s.lastH), n)
}

func titled(t, pos string) string {
	if pos == "" {
		return t
	}
	return t + " (" + pos + ")"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

func dur(d time.Duration) string {
	d = d.Round(time.Second)
	hh, mm, ss := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case hh > 0:
		return fmt.Sprintf("%dh %dm %ds", hh, mm, ss)
	case mm > 0:
		return fmt.Sprintf("%dm %ds", mm, ss)
	}
	return fmt.Sprintf("%ds", ss)
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
