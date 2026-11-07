package tui

import (
	"image/color"
	"os"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// glyphs is one of the two glyph sets from docs/tui-design.md ("Where it runs").
type glyphs struct {
	ascii bool

	// double-line panels (D42) and the single-line progress frame
	dTL, dTR, dBL, dBR, dH, dV string
	sTL, sTR, sBL, sBR, sH, sV string

	rule, thinRule, ellipsis, dot, bar, statusSep, times string

	// step status markers
	ok, same, fail, skip, pending, running, warn, err string

	spinner []string
}

var richGlyphs = glyphs{
	dTL: "╔", dTR: "╗", dBL: "╚", dBR: "╝", dH: "═", dV: "║",
	sTL: "┌", sTR: "┐", sBL: "└", sBR: "┘", sH: "─", sV: "│",
	rule: "═", thinRule: "─", ellipsis: "…", dot: " · ", bar: "█", statusSep: "│", times: "×",
	ok: "✓", same: "=", fail: "✗", skip: "–", pending: "·", running: ">", warn: "!", err: "!!",
	spinner: spinner.Dot.Frames,
}

var asciiGlyphs = glyphs{
	ascii: true,
	dTL:   "+", dTR: "+", dBL: "+", dBR: "+", dH: "=", dV: "|",
	sTL: "+", sTR: "+", sBL: "+", sBR: "+", sH: "-", sV: "|",
	rule: "=", thinRule: "-", ellipsis: "...", dot: " - ", bar: "#", statusSep: "|", times: "x",
	ok: "ok", same: "==", fail: "!!", skip: "--", pending: "  ", running: ">>", warn: "!", err: "!!",
	spinner: spinner.Line.Frames,
}

// pickGlyphs chooses the console set on the Linux console and similar
// terminals, or when KILN_ASCII=1 is set.
func pickGlyphs() glyphs {
	switch os.Getenv("TERM") {
	case "linux", "vt100", "dumb":
		return asciiGlyphs
	}
	if os.Getenv("KILN_ASCII") == "1" {
		return asciiGlyphs
	}
	return richGlyphs
}

// styles holds the D42 palette: only the 16 console colors, or their exact
// VGA values on truecolor terminals.
type styles struct {
	base           string // SGR that paints light grey on blue
	fgBase, bgBase string // SGR parameters for the base colors

	grey, blue, white, yellow, red, black color.Color

	white15, yellow11, red9, sel, status, tabOn lipgloss.Style
}

func newStyles(truecolor bool) *styles {
	s := &styles{}
	if truecolor {
		s.fgBase, s.bgBase = "38;2;170;170;170", "48;2;0;0;170"
		s.blue, s.grey = lipgloss.Color("#0000AA"), lipgloss.Color("#AAAAAA")
		s.white, s.yellow = lipgloss.Color("#FFFFFF"), lipgloss.Color("#FFFF55")
		s.red, s.black = lipgloss.Color("#FF5555"), lipgloss.Color("#000000")
	} else {
		s.fgBase, s.bgBase = "37", "44"
		s.blue, s.grey = lipgloss.Color("4"), lipgloss.Color("7")
		s.white, s.yellow = lipgloss.Color("15"), lipgloss.Color("11")
		s.red, s.black = lipgloss.Color("9"), lipgloss.Color("0")
	}
	s.base = "\x1b[0;" + s.fgBase + ";" + s.bgBase + "m"
	s.white15 = lipgloss.NewStyle().Foreground(s.white)
	s.yellow11 = lipgloss.NewStyle().Foreground(s.yellow)
	s.red9 = lipgloss.NewStyle().Foreground(s.red)
	s.sel = lipgloss.NewStyle().Foreground(s.blue).Background(s.grey)
	s.status = lipgloss.NewStyle().Foreground(s.black).Background(s.grey)
	s.tabOn = s.sel
	return s
}

// paint renders exactly w×h cells. Every reset inside a line is turned back
// into "light grey on blue", so no cell keeps the terminal's own colors (D42).
func (s *styles) paint(lines []string, w, h int) string {
	var b strings.Builder
	for i := 0; i < h; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		if ansi.StringWidth(l) > w {
			l = ansi.Truncate(l, w, "")
		}
		pad := w - ansi.StringWidth(l)
		b.WriteString(s.base)
		b.WriteString(s.fixSGR(l))
		b.WriteString(s.base)
		if pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		if i < h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// fixSGR rewrites SGR sequences so resets restore the base colors.
func (s *styles) fixSGR(l string) string {
	if !strings.Contains(l, "\x1b[") {
		return l
	}
	var b strings.Builder
	for i := 0; i < len(l); {
		if l[i] == 0x1b && i+1 < len(l) && l[i+1] == '[' {
			j := i + 2
			for j < len(l) && (l[j] < 0x40 || l[j] > 0x7e) {
				j++
			}
			if j >= len(l) {
				break // unterminated sequence: drop it
			}
			if l[j] == 'm' {
				b.WriteString(s.rewriteSGR(l[i+2 : j]))
			} else {
				b.WriteString(l[i : j+1])
			}
			i = j + 1
			continue
		}
		b.WriteByte(l[i])
		i++
	}
	return b.String()
}

func (s *styles) rewriteSGR(params string) string {
	if params == "" {
		return s.base
	}
	ps := strings.Split(params, ";")
	out := make([]string, 0, len(ps)+2)
	for k := 0; k < len(ps); k++ {
		p := ps[k]
		switch p {
		case "", "0":
			out = append(out, "0", s.fgBase, s.bgBase)
		case "39":
			out = append(out, s.fgBase)
		case "49":
			out = append(out, s.bgBase)
		case "38", "48", "58":
			out = append(out, p)
			n := 0
			if k+1 < len(ps) {
				switch ps[k+1] {
				case "5":
					n = 2
				case "2":
					n = 4
				}
			}
			for ; n > 0 && k+1 < len(ps); n-- {
				k++
				out = append(out, ps[k])
			}
		default:
			out = append(out, p)
		}
	}
	return "\x1b[" + strings.Join(out, ";") + "m"
}

// --- text helpers -----------------------------------------------------------

func width(s string) int { return ansi.StringWidth(s) }

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

// fit cuts s to n cells (with an ellipsis) and pads it to exactly n.
func (m *model) fit(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if width(s) > n {
		tail := m.g.ellipsis
		if width(tail) >= n {
			tail = ""
		}
		s = ansi.Truncate(s, n, tail)
	}
	return s + spaces(n-width(s))
}

// cut cuts s to n cells with an ellipsis, without padding.
func (m *model) cut(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if width(s) <= n {
		return s
	}
	tail := m.g.ellipsis
	if width(tail) >= n {
		tail = ""
	}
	return ansi.Truncate(s, n, tail)
}

// wrap word-wraps plain text to n cells (long words are broken).
func wrap(s string, n int) []string {
	if n <= 0 {
		return nil
	}
	if width(s) <= n {
		return []string{s}
	}
	return strings.Split(ansi.Wrap(s, n, ""), "\n")
}

// wrapIndent wraps s, indenting continuation lines by ind cells.
func wrapIndent(s string, n, ind int) []string {
	first := wrap(s, n)
	if len(first) <= 1 || ind <= 0 || ind >= n {
		return first
	}
	rest := strings.TrimSpace(strings.Join(first[1:], " "))
	out := []string{first[0]}
	for _, l := range wrap(rest, n-ind) {
		out = append(out, spaces(ind)+l)
	}
	return out
}

// clean turns command output into one printable line: no escape sequences,
// no carriage-return redraws, tabs expanded.
func clean(s string) string {
	s = ansi.Strip(s)
	if i := strings.LastIndexByte(strings.TrimRight(s, "\r"), '\r'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimRight(s, "\r\n")
	if !strings.ContainsAny(s, "\t\x00\x07\x08\x0b\x0c\x7f") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch {
		case r == '\t':
			n := 8 - col%8
			b.WriteString(spaces(n))
			col += n
		case r < 0x20 || r == 0x7f:
			// drop other control characters
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// --- building blocks --------------------------------------------------------

// panel draws a double-line frame (console set: +=|) of w×h with an optional title.
func (m *model) panel(title string, body []string, w, h int) []string {
	g := m.g
	if w < 6 || h < 2 {
		return nil
	}
	inner := w - 4
	top := g.dTL + g.dH
	if title != "" {
		t := " " + m.cut(title, w-6) + " "
		top += m.st.white15.Render(t)
	}
	top += repeat(g.dH, w-1-width(top)) + g.dTR
	out := []string{top}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		out = append(out, g.dV+" "+m.fit(l, inner)+" "+g.dV)
	}
	out = append(out, g.dBL+repeat(g.dH, w-2)+g.dBR)
	return out
}

// progressBar is the XP bar: percentage centred above, yellow fill inside a
// single-line frame.
func (m *model) progressBar(frac float64, w int) []string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	g := m.g
	inner := w - 2
	filled := int(frac*float64(inner) + 0.5)
	label := itoa(int(frac*100+0.5)) + "%"
	return []string{
		spaces((w-width(label))/2) + label,
		g.sTL + repeat(g.sH, inner) + g.sTR,
		g.sV + m.st.yellow11.Render(repeat(g.bar, filled)) + spaces(inner-filled) + g.sV,
		g.sBL + repeat(g.sH, inner) + g.sBR,
	}
}

// hint is one XP-style key hint, e.g. ENTER=Continue.
type hint struct{ key, label string }

func h(key, label string) hint { return hint{key, label} }

// statusBar is the full-width light grey bar with black text. Hints are in
// order of importance; the least important are dropped when space runs out.
func (m *model) statusBar(hints []hint, right string) string {
	w := m.w
	rightW := 0
	if right != "" {
		rightW = min(width(right)+3, w*2/5)
	}
	leftW := w - rightW
	parts := make([]string, 0, len(hints))
	for _, x := range hints {
		parts = append(parts, x.key+"="+x.label)
	}
	left := ""
	for len(parts) > 0 {
		left = " " + strings.Join(parts, "  ")
		if width(left) < leftW {
			break
		}
		parts = parts[:len(parts)-1]
		left = ""
	}
	line := m.fit(left, leftW)
	if rightW > 0 {
		line += m.g.statusSep + " " + m.fit(right, rightW-3) + " "
	}
	return m.st.status.Render(line)
}

// sideBySide joins two blocks of lines with a gap.
func sideBySide(left, right []string, leftW, gap int) []string {
	n := max(len(left), len(right))
	out := make([]string, n)
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = l + spaces(leftW-width(l)+gap) + r
	}
	return out
}
