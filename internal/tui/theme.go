package tui

import (
	"fmt"
	"strconv"
)

// The Okabe Night palette (docs/color-system.md, D57): dark only and
// colorblind-safe. Screens use these semantic tokens only, never raw colors.
// Bubble Tea downsamples them to what the terminal supports, so on the
// Linux console they become the nearest of its 16 colors.
//
// Color is never the only signal: every status also has a word.
var (
	bgCanvas   = "#090E13"
	bgRaised   = "#151A20"
	bgSelected = "#00253F"

	borderDefault = "#2D3239"

	textPrimary   = "#EDF0F4"
	textSecondary = "#B0B5BB"
	textMuted     = "#94999E"

	primaryText = "#82CEFC"
	accentText  = "#FCB538"

	successText = "#80DAB4"
	warningText = "#E9DD31"
	dangerText  = "#FFAE86"
	infoText    = "#82CEFC"
)

// SGR sequences built from the tokens.
var (
	// base is the canvas every cell is painted with.
	base = sgr(fg(textPrimary), bg(bgCanvas))
	// reset ends a colored span; paint() turns it back into base.
	reset = "\x1b[0m"

	cTitle     = sgr("1", fg(primaryText))
	cRule      = sgr(fg(borderDefault))
	cSecondary = sgr(fg(textSecondary))
	cMuted     = sgr(fg(textMuted))
	cPrimary   = sgr(fg(primaryText))
	cAccent    = sgr("1", fg(accentText))
	cSuccess   = sgr(fg(successText))
	cWarning   = sgr("1", fg(warningText))
	cDanger    = sgr("1", fg(dangerText))
	cInfo      = sgr(fg(infoText))
	cSelected  = sgr(fg(textPrimary), bg(bgSelected))
	cStatus    = sgr(fg(textSecondary), bg(bgRaised))
	cStatusKey = sgr("1", fg(accentText), bg(bgRaised))
)

func sgr(parts ...string) string {
	s := "\x1b["
	for i, p := range parts {
		if i > 0 {
			s += ";"
		}
		s += p
	}
	return s + "m"
}

func fg(hex string) string { return "38;2;" + rgb(hex) }
func bg(hex string) string { return "48;2;" + rgb(hex) }

func rgb(hex string) string {
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		panic(fmt.Sprintf("bad color token %q", hex))
	}
	return fmt.Sprintf("%d;%d;%d", v>>16&0xff, v>>8&0xff, v&0xff)
}
