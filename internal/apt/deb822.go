package apt

import "strings"

// Field is one deb822 field.
type Field struct{ Key, Value string }

// Stanza is one deb822 paragraph.
type Stanza struct {
	Fields []Field
}

// Get returns a field value (case-insensitive key).
func (s Stanza) Get(key string) string {
	for _, f := range s.Fields {
		if strings.EqualFold(f.Key, key) {
			return f.Value
		}
	}
	return ""
}

// Set sets or appends a field.
func (s *Stanza) Set(key, value string) {
	for i, f := range s.Fields {
		if strings.EqualFold(f.Key, key) {
			s.Fields[i].Value = value
			return
		}
	}
	s.Fields = append(s.Fields, Field{key, value})
}

// Words splits a whitespace-separated field.
func (s Stanza) Words(key string) []string { return strings.Fields(s.Get(key)) }

// Enabled reports whether the stanza is enabled (deb822 "Enabled: no").
func (s Stanza) Enabled() bool { return !strings.EqualFold(strings.TrimSpace(s.Get("Enabled")), "no") }

// ParseDeb822 parses paragraphs, ignoring comments.
func ParseDeb822(data string) []Stanza {
	var out []Stanza
	var cur Stanza
	flush := func() {
		if len(cur.Fields) > 0 {
			out = append(out, cur)
		}
		cur = Stanza{}
	}
	for _, line := range strings.Split(data, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"):
			continue
		case trimmed == "":
			flush()
		case (line[0] == ' ' || line[0] == '\t') && len(cur.Fields) > 0:
			last := &cur.Fields[len(cur.Fields)-1]
			if trimmed == "." {
				trimmed = ""
			}
			last.Value += "\n" + trimmed
		default:
			k, v, ok := strings.Cut(line, ":")
			if ok {
				cur.Fields = append(cur.Fields, Field{strings.TrimSpace(k), strings.TrimSpace(v)})
			}
		}
	}
	flush()
	return out
}

// FormatDeb822 renders stanzas, with an optional comment header.
func FormatDeb822(header string, st []Stanza) string {
	var b strings.Builder
	b.WriteString(header)
	for i, s := range st {
		if i > 0 || header != "" {
			b.WriteString("\n")
		}
		for _, f := range s.Fields {
			lines := strings.Split(f.Value, "\n")
			b.WriteString(f.Key + ": " + lines[0] + "\n")
			for _, l := range lines[1:] {
				if l == "" {
					l = "."
				}
				b.WriteString(" " + l + "\n")
			}
		}
	}
	return b.String()
}

// OneLine is a parsed one-line sources.list entry.
type OneLine struct {
	Type, URI, Suite string
	Components       []string
	Options          string
	Raw              string
}

// ParseOneLine parses active "deb"/"deb-src" lines; ok=false for comments/blank.
func ParseOneLine(line string) (OneLine, bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return OneLine{}, false
	}
	o := OneLine{Raw: line}
	f := strings.Fields(t)
	if len(f) < 3 || (f[0] != "deb" && f[0] != "deb-src") {
		return OneLine{}, false
	}
	o.Type = f[0]
	rest := f[1:]
	if strings.HasPrefix(rest[0], "[") {
		var opts []string
		for len(rest) > 0 {
			opts = append(opts, rest[0])
			done := strings.HasSuffix(rest[0], "]")
			rest = rest[1:]
			if done {
				break
			}
		}
		o.Options = strings.Join(opts, " ")
	}
	if len(rest) < 2 {
		return OneLine{}, false
	}
	o.URI, o.Suite, o.Components = rest[0], rest[1], rest[2:]
	return o, true
}
