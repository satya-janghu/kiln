// Package tui is kiln's three screens: checklist, progress and summary
// (docs/tui-design.md), in the Okabe Night palette (theme.go, D57).
package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/satya-janghu/kiln/internal/detect"
	"github.com/satya-janghu/kiln/internal/run"
	"github.com/satya-janghu/kiln/internal/steps"
)

const (
	minW, minH = 80, 24
	maxOutput  = 400
)

type screen int

const (
	scrChecklist screen = iota
	scrRun
	scrSummary
)

type item struct {
	step steps.Step
	on   bool
}

type model struct {
	f       *detect.Facts
	version string
	w, h    int
	scr     screen

	items  []item
	cursor int
	top    int

	all     []steps.Step
	results []steps.Result
	output  []string
	asking  bool // "stop after the current step?"
	stopped *atomic.Bool
	picked  map[string]bool
	start   func(picked map[string]bool)

	summary []string
	sTop    int
	reboot  bool // a reboot is needed
	doBoot  bool // the user chose to reboot
	quit    bool
}

type lineMsg string
type resultMsg struct {
	i int
	r steps.Result
}
type doneMsg struct {
	notes, reboots []string
	secret         string
}

// Result is what the user chose at the end.
type Result struct {
	Reboot  bool
	Summary []string
}

// Run shows the screens and runs the chosen steps. It returns after the run
// has finished, even if the terminal went away during it.
func Run(f *detect.Facts, logw io.Writer, version string) (Result, error) {
	ctx := context.Background()
	m := &model{f: f, version: version, stopped: &atomic.Bool{}, all: steps.List()}
	for _, st := range m.all {
		if !st.Always && st.Shown(f) {
			m.items = append(m.items, item{step: st, on: true})
		}
	}
	m.results = make([]steps.Result, len(m.all))
	for i, st := range m.all {
		m.results[i] = steps.Result{ID: st.ID, Title: st.Title}
	}

	var p *tea.Program
	var wg sync.WaitGroup
	m.start = func(picked map[string]bool) {
		s := run.New(ctx, logw, func(l string) { p.Send(lineMsg(l)) })
		s.Log("kiln %s starting on %s (%s), user %s", version, f.Machine(), f.Desktop(), f.User.Name)
		c := steps.NewCtx(s, f, picked)
		wg.Add(1)
		go func() {
			defer wg.Done()
			steps.Execute(c, m.all, m.stopped.Load, func(i int, r steps.Result) { p.Send(resultMsg{i, r}) })
			notes, reboots, secret := s.Results()
			if run.Exists("/run/reboot-required") {
				reboots = append(reboots, "Debian says a reboot is required (/run/reboot-required)")
			}
			p.Send(doneMsg{notes, reboots, secret})
		}()
	}

	p = tea.NewProgram(m, tea.WithoutSignalHandler())
	_, err := p.Run()
	// The screen may be gone while a run is still going: let it finish.
	wg.Wait()
	return Result{Reboot: m.doBoot, Summary: m.plainSummary()}, err
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case lineMsg:
		m.output = append(m.output, string(msg))
		if len(m.output) > maxOutput {
			m.output = m.output[len(m.output)-maxOutput:]
		}
	case resultMsg:
		m.results[msg.i] = msg.r
	case doneMsg:
		m.buildSummary(msg)
		m.scr = scrSummary
	case tea.KeyPressMsg:
		return m, m.key(msg.String())
	}
	return m, nil
}

func (m *model) key(k string) tea.Cmd {
	switch m.scr {
	case scrChecklist:
		switch k {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "space", " ":
			m.items[m.cursor].on = !m.items[m.cursor].on
		case "a":
			all := true
			for _, it := range m.items {
				all = all && it.on
			}
			for i := range m.items {
				m.items[i].on = !all
			}
		case "enter":
			picked := map[string]bool{}
			for _, it := range m.items {
				picked[it.step.ID] = it.on
			}
			m.scr = scrRun
			m.picked = picked
			m.start(picked)
		case "q", "esc", "ctrl+c", "f3":
			m.quit = true
			return tea.Quit
		}
	case scrRun:
		if m.asking {
			switch k {
			case "y", "Y":
				m.stopped.Store(true)
				m.asking = false
			case "n", "N", "esc":
				m.asking = false
			}
			return nil
		}
		if k == "ctrl+c" || k == "esc" || k == "f3" {
			m.asking = !m.stopped.Load()
		}
	case scrSummary:
		switch k {
		case "up", "k":
			if m.sTop > 0 {
				m.sTop--
			}
		case "down", "j":
			m.sTop++
		case "pgup":
			m.sTop -= m.h / 2
			if m.sTop < 0 {
				m.sTop = 0
			}
		case "pgdown", "space", " ":
			m.sTop += m.h / 2
		case "enter":
			if m.reboot {
				m.doBoot = true
				return tea.Quit
			}
		case "q", "esc", "f3", "ctrl+c":
			return tea.Quit
		}
	}
	return nil
}

func (m *model) View() tea.View {
	var content string
	switch {
	case m.w == 0 || m.h == 0:
	case m.w < minW || m.h < minH:
		content = paint([]string{fmt.Sprintf("Make the window at least %dx%d (now %dx%d).", minW, minH, m.w, m.h)}, m.w, m.h)
	default:
		var body []string
		var status string
		switch m.scr {
		case scrChecklist:
			body, status = m.viewChecklist()
		case scrRun:
			body, status = m.viewRun()
		case scrSummary:
			body, status = m.viewSummary()
		}
		lines := []string{" " + cTitle + "kiln Setup" + reset + cMuted + "  one-time setup for Debian 13" + reset, " " + cRule + strings.Repeat("─", m.w-2) + reset}
		lines = append(lines, body...)
		for len(lines) < m.h-1 {
			lines = append(lines, "")
		}
		lines = lines[:m.h-1]
		lines = append(lines, statusBar(status, m.w))
		content = paint(lines, m.w, m.h)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "kiln Setup"
	return v
}

func (m *model) bodyH() int { return m.h - 3 }

func (m *model) viewChecklist() ([]string, string) {
	f := m.f
	head := []string{
		"",
		fmt.Sprintf("  This sets up %s (%s) for %s.", cut(f.Machine(), 40), f.Desktop(), f.User.Name),
		"  Always done: package sources, full upgrade, firmware and microcode.",
		"  Untick anything you don't want, then press ENTER to start.",
		"",
	}
	rows := m.bodyH() - len(head)
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+rows {
		m.top = m.cursor - rows + 1
	}
	body := head
	for i := m.top; i < len(m.items) && i < m.top+rows; i++ {
		it := m.items[i]
		box := cMuted + "[ ]" + reset
		if it.on {
			box = cPrimary + "[x]" + reset
		}
		title := pad(it.step.Title, 33)
		desc := cut(it.step.Desc, m.w-46)
		if i == m.cursor {
			// The ">" marks the cursor; the color is only a second cue.
			row := cAccent + ">" + reset + cSelected + " " + box + cSelected + " " + title + " " + cSecondary + desc
			body = append(body, "  "+cSelected+pad(row, m.w-4)+reset)
		} else {
			body = append(body, "    "+box+" "+title+" "+cSecondary+desc+reset)
		}
	}
	return body, "ENTER=Start  SPACE=Tick/untick  A=All/none  Up/Down=Move  F3=Quit"
}

func (m *model) viewRun() ([]string, string) {
	var list []string
	done, total := 0, 0
	for _, r := range m.results {
		st := m.stepByID(r.ID)
		if !st.Shown(m.f) || (!st.Always && !m.picked[st.ID]) {
			continue
		}
		total++
		mark := cMuted + "  waiting " + reset
		switch r.Status {
		case steps.Running:
			mark = cInfo + "> running " + reset
		case steps.Done:
			mark = cSuccess + "+ done    " + reset
			done++
		case steps.Failed:
			mark = cDanger + "x FAILED  " + reset
			done++
		case steps.Skipped:
			mark = cMuted + "- skipped " + reset
			done++
		}
		list = append(list, "  "+mark+r.Title)
	}
	body := []string{""}
	listH := len(list)
	outH := m.bodyH() - 3 - listH
	if outH < 6 {
		// Too many steps for the screen: show the ones around the running one.
		outH = 6
		maxList := m.bodyH() - 3 - outH
		cur := 0
		for i, l := range list {
			if strings.Contains(l, "> running") {
				cur = i
			}
		}
		startAt := cur - maxList/2
		if startAt < 0 {
			startAt = 0
		}
		if startAt+maxList > len(list) {
			startAt = len(list) - maxList
		}
		list = list[startAt : startAt+maxList]
	}
	body = append(body, list...)
	body = append(body, "", "  "+cRule+strings.Repeat("─", m.w-4)+reset)
	out := m.output
	if len(out) > outH {
		out = out[len(out)-outH:]
	}
	for _, l := range out {
		body = append(body, "  "+cMuted+cut(l, m.w-4)+reset)
	}
	status := fmt.Sprintf("Setting up: %d of %d done. The full log is in %s.", done, total, run.LogPath)
	if m.asking {
		status = "Stop after the current step? Y=Yes  N=No"
	} else if m.stopped.Load() {
		status = "Stopping after the current step..."
	}
	return body, status
}

func (m *model) stepByID(id string) steps.Step {
	for _, st := range m.all {
		if st.ID == id {
			return st
		}
	}
	return steps.Step{}
}

func (m *model) buildSummary(d doneMsg) {
	var s []string
	failed := 0
	for _, r := range m.results {
		if r.Status == steps.Failed {
			failed++
		}
	}
	if failed == 0 {
		s = append(s, cSuccess+"+ Setup finished."+reset)
	} else {
		s = append(s, cDanger+fmt.Sprintf("x Setup finished with %d problem(s).", failed)+reset+" Fix them and run kiln again; it's safe to repeat.")
	}
	s = append(s, "")
	for _, r := range m.results {
		st := m.stepByID(r.ID)
		if !st.Shown(m.f) {
			continue
		}
		switch r.Status {
		case steps.Done:
			s = append(s, cSuccess+"+ done    "+reset+r.Title)
		case steps.Failed:
			s = append(s, cDanger+"x FAILED  "+reset+r.Title)
			for _, l := range wrap("   "+r.Detail, m.w-16) {
				s = append(s, "          "+cDanger+l+reset)
			}
		case steps.Skipped:
			s = append(s, cMuted+"- skipped "+r.Title+" ("+r.Detail+")"+reset)
		}
	}
	if len(d.notes) > 0 {
		s = append(s, "", cTitle+"Notes"+reset)
		for _, n := range d.notes {
			s = append(s, wrap("- "+n, m.w-6)...)
		}
	}
	s = append(s, "")
	if len(d.reboots) > 0 {
		m.reboot = true
		s = append(s, cWarning+"! A reboot is needed:"+reset)
		for _, r := range d.reboots {
			s = append(s, wrap("- "+r, m.w-6)...)
		}
	} else {
		s = append(s, cInfo+"i No reboot is needed."+reset)
	}
	if d.secret != "" {
		s = append(s, "", cWarning+"! WRITE THIS DOWN NOW: Secure Boot one-time password: "+cAccent+d.secret+reset,
			"At the next boot choose \"Enroll MOK\", then \"Continue\", and type this password.",
			"It is shown only here and is not saved anywhere.")
	}
	m.summary = s
}

func (m *model) viewSummary() ([]string, string) {
	rows := m.bodyH() - 1
	maxTop := len(m.summary) - rows
	if maxTop < 0 {
		maxTop = 0
	}
	if m.sTop > maxTop {
		m.sTop = maxTop
	}
	body := []string{""}
	for i := m.sTop; i < len(m.summary) && i < m.sTop+rows; i++ {
		body = append(body, "  "+m.summary[i])
	}
	status := "Up/Down/PgUp/PgDn=Scroll  F3=Quit"
	if m.reboot {
		status = "ENTER=Reboot now  Up/Down/PgUp/PgDn=Scroll  F3=Quit without rebooting"
	}
	return body, status
}

// plainSummary is the summary without colors, for the log and the terminal.
func (m *model) plainSummary() []string {
	out := make([]string, 0, len(m.summary))
	for _, l := range m.summary {
		if strings.Contains(l, "WRITE THIS DOWN NOW") {
			l = "Secure Boot: a one-time password was shown on screen (it is not saved)."
		}
		out = append(out, ansi.Strip(l))
	}
	return out
}

// Reboot syncs and reboots the machine.
func Reboot() error {
	_ = exec.CommandContext(context.Background(), "sync").Run()
	time.Sleep(time.Second)
	// #nosec G204 -- fixed command.
	cmd := exec.CommandContext(context.Background(), "systemctl", "reboot")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// statusBar renders the bottom bar; key names (before "=") are highlighted.
func statusBar(text string, w int) string {
	var b strings.Builder
	b.WriteString(cStatus + " ")
	for i, part := range strings.Split(text, "  ") {
		if i > 0 {
			b.WriteString("  ")
		}
		if k, v, ok := strings.Cut(part, "="); ok && !strings.Contains(k, " ") {
			b.WriteString(cStatusKey + k + cStatus + "=" + v)
		} else {
			b.WriteString(part)
		}
	}
	return cStatus + pad(b.String(), w) + reset
}

// --- painting ----------------------------------------------------------------

// paint renders exactly w×h cells on the canvas color; every reset inside a
// line goes back to that base, so no cell keeps the terminal's own colors.
func paint(lines []string, w, h int) string {
	var b strings.Builder
	for i := 0; i < h; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		if ansi.StringWidth(l) > w {
			l = ansi.Truncate(l, w, "")
		}
		b.WriteString(base)
		b.WriteString(strings.ReplaceAll(l, reset, base))
		b.WriteString(base)
		if n := w - ansi.StringWidth(l); n > 0 {
			b.WriteString(strings.Repeat(" ", n))
		}
		if i < h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func pad(s string, w int) string {
	if n := w - ansi.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func cut(s string, w int) string {
	if w < 4 || ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w-3, "") + "..."
}

func wrap(s string, w int) []string {
	if w < 20 {
		w = 20
	}
	// Continuation lines line up with the text after the leading spaces and "- ".
	lead := len(s) - len(strings.TrimLeft(s, " "))
	if strings.HasPrefix(s[lead:], "- ") {
		lead += 2
	}
	indent := strings.Repeat(" ", lead)
	var out []string
	line := s[:lead]
	for _, word := range strings.Fields(s[lead:]) {
		if strings.TrimSpace(line) != "" && ansi.StringWidth(line)+1+ansi.StringWidth(word) > w {
			out = append(out, line)
			line = indent
		}
		if strings.TrimSpace(line) == "" || line == s[:lead] {
			line += word
		} else {
			line += " " + word
		}
	}
	if strings.TrimSpace(line) != "" {
		out = append(out, line)
	}
	return out
}
