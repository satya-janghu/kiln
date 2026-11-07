package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/satya-janghu/kiln/internal/logx"
)

// logBuf is a live log pane: long lines wrap, it follows the end until the
// user scrolls up, and it keeps only the most recent lines (the full log is
// in /var/log/kiln.log).
type logBuf struct {
	raw     []string
	wrapped []string
	ww, wn  int // width wrapped for, raw lines already wrapped
	top     int
	follow  bool
	lastH   int
}

const logKeep = 5000

func (l *logBuf) add(s string) {
	for _, line := range strings.Split(s, "\n") {
		l.raw = append(l.raw, clean(line))
	}
	if len(l.raw) > logKeep+500 {
		l.raw = append([]string(nil), l.raw[len(l.raw)-logKeep:]...)
		l.ww = 0 // re-wrap
	}
}

func (l *logBuf) len() int { return len(l.raw) }

func (l *logBuf) lines(w int) []string {
	if w != l.ww || l.wn > len(l.raw) {
		l.wrapped, l.wn, l.ww = nil, 0, w
	}
	for _, r := range l.raw[l.wn:] {
		if r == "" {
			l.wrapped = append(l.wrapped, "")
			continue
		}
		l.wrapped = append(l.wrapped, wrap(r, w)...)
	}
	l.wn = len(l.raw)
	return l.wrapped
}

func (l *logBuf) view(w, h int) []string {
	if w <= 0 || h <= 0 {
		return nil
	}
	all := l.lines(w)
	l.lastH = h
	maxTop := max(0, len(all)-h)
	if l.follow {
		l.top = maxTop
	}
	l.top = max(0, min(l.top, maxTop))
	return all[l.top:min(len(all), l.top+h)]
}

func (l *logBuf) key(k string) bool {
	page := max(1, l.lastH-1)
	switch k {
	case "pgup":
		l.follow = false
		l.top -= page
	case "pgdown":
		l.top += page
		if l.top >= len(l.wrapped)-l.lastH {
			l.follow = true
		}
	case "home":
		l.follow, l.top = false, 0
	case "end":
		l.follow = true
	default:
		return false
	}
	return true
}

// --- full log viewer (summary `l`, "already running" `l`) -------------------

type logView struct {
	gen     int // refresh generation, so only one refresh loop runs
	buf     logBuf
	err     string
	loading bool
}

type logLoadedMsg struct {
	text string
	err  error
}

type logRefreshMsg struct{ gen int }

const logTail = 4000

func (m *model) loadLog() tea.Cmd {
	a := m.a
	return func() tea.Msg {
		b, err := a.Real.ReadFile(logx.Path)
		if err != nil {
			return logLoadedMsg{err: err}
		}
		s := string(b)
		lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
		if len(lines) > logTail {
			lines = lines[len(lines)-logTail:]
		}
		return logLoadedMsg{text: strings.Join(lines, "\n")}
	}
}

func (m *model) openLog(back screen) tea.Cmd {
	m.logBack, m.scr = back, scrLog
	m.logv = logView{gen: m.logv.gen + 1, buf: logBuf{follow: true}, loading: true}
	return m.loadLog()
}

func (m *model) onLogLoaded(msg logLoadedMsg) tea.Cmd {
	lv := &m.logv
	lv.loading = false
	if msg.err != nil {
		lv.err = "The log can't be read: " + msg.err.Error()
	} else {
		lv.err = ""
		follow, top := lv.buf.follow, lv.buf.top
		lv.buf = logBuf{follow: follow, top: top}
		lv.buf.add(m.a.Red.Apply(msg.text))
	}
	// The other kiln's log keeps growing: refresh it every 2 seconds.
	if m.scr == scrLog && m.logBack == scrLocked {
		gen := lv.gen
		return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return logRefreshMsg{gen: gen} })
	}
	return nil
}

func (m *model) onLogRefresh(msg logRefreshMsg) tea.Cmd {
	if m.scr == scrLog && m.logBack == scrLocked && msg.gen == m.logv.gen {
		return m.loadLog()
	}
	return nil
}

func (m *model) keyLog(k string) tea.Cmd {
	switch k {
	case "esc", "q", "l":
		m.scr = m.logBack
		return nil
	case "up", "k":
		m.logv.buf.follow = false
		m.logv.buf.top--
	case "down", "j":
		m.logv.buf.top++
		if m.logv.buf.top >= len(m.logv.buf.wrapped)-m.logv.buf.lastH {
			m.logv.buf.follow = true
		}
	default:
		m.logv.buf.key(k)
	}
	return nil
}

func (m *model) viewLog() ([]string, []hint, string) {
	lv := &m.logv
	cw, bh := m.cw(), m.bodyH()
	var body []string
	switch {
	case lv.loading && lv.buf.len() == 0:
		body = []string{m.spin() + " Reading " + logx.Path + "..."}
	case lv.err != "":
		body = wrap(lv.err, cw-4)
	default:
		body = lv.buf.view(cw-4, bh-2)
	}
	title := "Log: " + logx.Path
	if m.logBack == scrLocked {
		title += " (read-only, refreshes every 2 s)"
	}
	hints := []hint{h("ESC", "Back"), h("PGUP/PGDN", "Scroll"), h("END", "Follow"), h("F3", "Quit")}
	return m.panel(title, body, cw, bh), hints, "Full log"
}
