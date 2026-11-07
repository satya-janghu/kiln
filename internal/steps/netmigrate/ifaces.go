package netmigrate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/satya-janghu/kiln/internal/sys"
)

// Paths (ifupdown 0.8.44).
const (
	Interfaces  = "/etc/network/interfaces"
	InterfacesD = "/etc/network/interfaces.d"
)

// opt is one option line inside an iface stanza.
type opt struct {
	key, val string
	line     int
}

// stanza is one `iface <name> <family> <method>` block.
type stanza struct {
	file                  string
	start, end            int // physical line range, inclusive
	iface, family, method string
	opts                  []opt
}

func (s *stanza) get(key string) string {
	for _, o := range s.opts {
		if o.key == key {
			return o.val
		}
	}
	return ""
}

// autoLine is an `auto`/`allow-*`/`no-*` line naming interfaces.
type autoLine struct {
	file   string
	start  int
	end    int
	kind   string
	ifaces []string
}

// config is everything ifupdown reads.
type config struct {
	files   []string
	lines   map[string][]string
	stanzas []*stanza
	autos   []autoLine
	manual  string // first construct kiln won't translate ("file line N: text"), "" if none
}

// hooks are commands ifupdown runs; kiln can't carry them over to NetworkManager.
var hooks = map[string]bool{"pre-up": true, "up": true, "post-up": true, "pre-down": true, "down": true, "post-down": true}

var methods = map[string]map[string]bool{
	"inet":  {"dhcp": true, "static": true, "manual": true},
	"inet6": {"auto": true, "dhcp": true, "static": true, "manual": true},
}

func defaultSource(kw, arg string) bool {
	arg = strings.TrimSuffix(arg, "/")
	switch kw {
	case "source":
		return arg == InterfacesD+"/*" || arg == "interfaces.d/*"
	case "source-directory":
		return arg == InterfacesD || arg == "interfaces.d"
	}
	return false
}

// logical joins backslash-continued lines.
type logical struct {
	start, end int
	text       string
}

func logicalLines(lines []string) []logical {
	var out []logical
	for i := 0; i < len(lines); i++ {
		start, text := i, strings.TrimRight(lines[i], " \t")
		for strings.HasSuffix(text, "\\") && i+1 < len(lines) {
			i++
			text = strings.TrimSuffix(text, "\\") + " " + strings.TrimSpace(lines[i])
		}
		out = append(out, logical{start: start, end: i, text: text})
	}
	return out
}

// parse reads /etc/network/interfaces and interfaces.d (read-only).
func parse(s sys.System) *config {
	c := &config{lines: map[string][]string{}}
	files := []string{Interfaces}
	more, _ := s.Glob(filepath.Join(InterfacesD, "*"))
	files = append(files, more...)
	flag := func(file string, l logical, why string) {
		if c.manual == "" {
			c.manual = fmt.Sprintf("%s line %d (%s) %s", file, l.start+1, strings.TrimSpace(l.text), why)
		}
	}
	for _, file := range files {
		b, err := s.ReadFile(file)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		c.files = append(c.files, file)
		c.lines[file] = lines
		var cur *stanza
		for _, l := range logicalLines(lines) {
			t := strings.TrimSpace(l.text)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			f := strings.Fields(t)
			switch {
			case f[0] == "iface":
				cur = nil
				if len(f) < 4 {
					flag(file, l, "is incomplete")
					continue
				}
				cur = &stanza{file: file, start: l.start, end: l.end, iface: f[1], family: f[2], method: f[3]}
				c.stanzas = append(c.stanzas, cur)
				if cur.iface == "lo" {
					continue
				}
				if strings.ContainsAny(cur.iface, ".:") {
					flag(file, l, "is a VLAN or alias interface")
				} else if !methods[cur.family][cur.method] {
					flag(file, l, "uses a method kiln doesn't translate")
				}
			case f[0] == "mapping":
				cur = nil
				flag(file, l, "is a mapping stanza")
			case f[0] == "source" || f[0] == "source-directory":
				cur = nil
				if len(f) < 2 || !defaultSource(f[0], f[1]) {
					flag(file, l, "includes other files")
				}
			case f[0] == "auto" || strings.HasPrefix(f[0], "allow-") || f[0] == "no-auto-down" || f[0] == "no-scripts":
				cur = nil
				c.autos = append(c.autos, autoLine{file: file, start: l.start, end: l.end, kind: f[0], ifaces: f[1:]})
			case cur != nil:
				cur.end = l.end
				cur.opts = append(cur.opts, opt{key: f[0], val: strings.TrimSpace(strings.TrimPrefix(t, f[0])), line: l.start})
				if cur.iface == "lo" {
					continue
				}
				k := f[0]
				if hooks[k] || strings.HasPrefix(k, "bridge") || strings.HasPrefix(k, "bond") || strings.HasPrefix(k, "vlan") {
					flag(file, l, "needs bridge, bond, VLAN or hook handling")
				}
			}
		}
	}
	return c
}

// ifaces returns the non-lo interfaces with stanzas, in file order.
func (c *config) ifaces() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range c.stanzas {
		if s.iface != "lo" && !seen[s.iface] {
			seen[s.iface] = true
			out = append(out, s.iface)
		}
	}
	return out
}

func (c *config) stanzasFor(iface string) []*stanza {
	var out []*stanza
	for _, s := range c.stanzas {
		if s.iface == iface {
			out = append(out, s)
		}
	}
	return out
}

func (c *config) stanza(iface, family string) *stanza {
	for _, s := range c.stanzas {
		if s.iface == iface && s.family == family {
			return s
		}
	}
	return nil
}

// filesFor lists the files that mention iface (stanzas or auto lines).
func (c *config) filesFor(iface string) []string {
	set := map[string]bool{}
	for _, s := range c.stanzasFor(iface) {
		set[s.file] = true
	}
	for _, a := range c.autos {
		for _, n := range a.ifaces {
			if n == iface {
				set[a.file] = true
			}
		}
	}
	var out []string
	for _, f := range c.files {
		if set[f] {
			out = append(out, f)
		}
	}
	return out
}

// commentOut returns file's content with iface's stanzas commented out and
// iface removed from its auto/allow-* lines. lo and everything else stay.
func (c *config) commentOut(file, iface string) []byte {
	lines := c.lines[file]
	type mark int
	const (
		keep mark = iota
		stanzaStart
		inStanza
		autoStart
		inAuto
	)
	marks := make([]mark, len(lines))
	rest := map[int][]string{}
	for _, s := range c.stanzasFor(iface) {
		if s.file != file {
			continue
		}
		marks[s.start] = stanzaStart
		for i := s.start + 1; i <= s.end; i++ {
			marks[i] = inStanza
		}
	}
	for _, a := range c.autos {
		if a.file != file {
			continue
		}
		var others []string
		found := false
		for _, n := range a.ifaces {
			if n == iface {
				found = true
			} else {
				others = append(others, n)
			}
		}
		if !found {
			continue
		}
		marks[a.start] = autoStart
		for i := a.start + 1; i <= a.end; i++ {
			marks[i] = inAuto
		}
		if len(others) > 0 {
			// After the whole (possibly continued) line, so a trailing
			// backslash in the commented copy can't swallow it.
			rest[a.end] = append([]string{a.kind}, others...)
		}
	}
	var b strings.Builder
	for i, l := range lines {
		switch marks[i] {
		case stanzaStart:
			b.WriteString("# Moved to NetworkManager by kiln (profile kiln-" + iface + "):\n")
			b.WriteString("# " + l + "\n")
		case inStanza, autoStart, inAuto:
			b.WriteString("# " + l + "\n")
		default:
			b.WriteString(l + "\n")
		}
		if r, ok := rest[i]; ok {
			b.WriteString(strings.Join(r, " ") + "\n")
		}
	}
	return []byte(b.String())
}
