package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
)

// Screen 3: a Huh form per step, built from its options.Field list.

// secretLen is the length of a password kiln generates ("generate one").
const secretLen = 20

const (
	modeKeep     = "keep"
	modeType     = "type"
	modeGenerate = "generate"
)

type binding struct {
	key   string // "step.key"
	field options.Field
	b     *bool
	s     *string
	// secret fields
	mode, pw, pw2 *string
}

type formState struct {
	idx   int
	title string
	f     *huh.Form
	binds []binding
	notes []string // disabled choices that can't be offered at all
}

// stepFields returns a step's fields in declaration order.
func (m *model) stepFields(s step.Step) []options.Field {
	var out []options.Field
	for _, f := range s.Options(m.a.Facts) {
		if fd, ok := m.a.Opts.Fields[s.ID()+"."+f.Key]; ok {
			fd.Key = f.Key
			out = append(out, fd)
		}
	}
	return out
}

// formSize leaves room under the fields for Huh's error lines, which it
// draws below its own height.
func (m *model) formSize() (w, hgt int) {
	return max(20, m.cw()-4-2), max(5, m.bodyH()-2-4)
}

// validator checks a typed value with the same rules as options.Set.
func validator(fd options.Field) func(string) error {
	return func(s string) error {
		nv, err := options.Normalize(fd, s)
		if err != nil {
			if fd.Kind == options.Int {
				return errors.New("enter a whole number")
			}
			return err
		}
		if fd.Validate != nil {
			return fd.Validate(nv)
		}
		return nil
	}
}

func (m *model) openForm(idx int) tea.Cmd {
	s := m.a.Steps[idx]
	fields := m.stepFields(s)
	if len(fields) == 0 {
		return m.setFlash(false, "%s has no options.", s.Title())
	}
	fs := &formState{idx: idx, title: s.Title()}
	var main []huh.Field
	var extra []*huh.Group
	for _, fd := range fields {
		k := s.ID() + "." + fd.Key
		cur := m.a.Opts.Values[k]
		b := binding{key: k, field: fd}
		switch fd.Kind {
		case options.Bool:
			v := new(bool)
			*v, _ = cur.(bool)
			b.b = v
			main = append(main, huh.NewConfirm().Title(fd.Label).Description(fd.Help).
				Affirmative("Yes").Negative("No").Value(v))
		case options.String:
			v := new(string)
			*v, _ = cur.(string)
			b.s = v
			main = append(main, huh.NewInput().Title(fd.Label).Description(fd.Help).Value(v).Validate(validator(fd)))
		case options.Int:
			v := new(string)
			if i, ok := cur.(int); ok {
				*v = strconv.Itoa(i)
			}
			b.s = v
			main = append(main, huh.NewInput().Title(fd.Label).Description(fd.Help).Value(v).
				CharLimit(12).Validate(validator(fd)))
		case options.List:
			v := new(string)
			l, _ := cur.([]string)
			*v = strings.Join(l, "\n")
			b.s = v
			desc := joinNonEmpty(" ", fd.Help, "Separate entries with commas (Alt+Enter starts a new line).")
			main = append(main, huh.NewText().Title(fd.Label).Description(desc).Lines(4).
				ExternalEditor(false).Value(v).Validate(validator(fd)))
		case options.Choice:
			v := new(string)
			*v, _ = cur.(string)
			var opts []huh.Option[string]
			var disabled []string
			found := false
			for _, c := range fd.Choices {
				label := c.Label
				if label == "" {
					label = c.Value
				}
				if c.DisabledReason != "" {
					disabled = append(disabled, label+" isn't available: "+c.DisabledReason)
					continue
				}
				if c.Value == *v {
					found = true
				}
				opts = append(opts, huh.NewOption(label, c.Value))
			}
			if len(opts) == 0 {
				fs.notes = append(fs.notes, fd.Label+": no choice is available on this machine. "+strings.Join(disabled, "; "))
				continue
			}
			if !found {
				*v = opts[0].Value
			}
			b.s = v
			desc := fd.Help
			for _, d := range disabled {
				desc = joinNonEmpty("\n", desc, m.g.skip+" "+d)
			}
			main = append(main, huh.NewSelect[string]().Title(fd.Label).Description(desc).
				Options(opts...).Value(v))
		case options.Secret:
			mode, pw, pw2 := new(string), new(string), new(string)
			sv, _ := cur.(string)
			var opts []huh.Option[string]
			if sv != "" {
				keep := "Keep the one already set"
				if m.generated[k] {
					keep = "Keep the generated one"
				}
				opts = append(opts, huh.NewOption(keep, modeKeep))
				*mode = modeKeep
			} else {
				*mode = modeType
			}
			opts = append(opts, huh.NewOption("Type a password", modeType), huh.NewOption("Generate one (shown once in the summary)", modeGenerate))
			b.mode, b.pw, b.pw2 = mode, pw, pw2
			desc := joinNonEmpty(" ", fd.Help, "It's never saved in state.json or written to the log.")
			main = append(main, huh.NewSelect[string]().Title(fd.Label).Description(desc).Options(opts...).Value(mode))
			check := validator(fd)
			extra = append(extra, huh.NewGroup(
				huh.NewInput().Title(fd.Label).EchoMode(huh.EchoModePassword).Value(pw).Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("type a password, or go back (Shift+Tab) and choose \"Generate one\"")
					}
					return check(s)
				}),
				huh.NewInput().Title("Type it again").EchoMode(huh.EchoModePassword).Value(pw2).Validate(func(s string) error {
					if s != *pw {
						return errors.New("the two passwords don't match")
					}
					return nil
				}),
			).Title(fd.Label).WithHideFunc(func() bool { return *mode != modeType }))
		default:
			continue
		}
		fs.binds = append(fs.binds, b)
	}
	var groups []*huh.Group
	if len(main) > 0 {
		g := huh.NewGroup(main...).Title(s.Title())
		if len(fs.notes) > 0 {
			g = g.Description(strings.Join(fs.notes, "\n"))
		}
		groups = append(groups, g)
	}
	groups = append(groups, extra...)
	if len(groups) == 0 {
		return m.setFlash(true, "%s", strings.Join(fs.notes, " "))
	}
	km := huh.NewDefaultKeyMap()
	km.Quit.SetEnabled(false) // ctrl+c is kiln's (asks first)
	km.Select.Filter.SetEnabled(false)
	km.Text.Editor.SetEnabled(false)
	fw, fh := m.formSize()
	fs.f = huh.NewForm(groups...).
		WithAccessible(false).
		WithShowHelp(false).
		WithShowErrors(true).
		WithKeyMap(km).
		WithTheme(m.huhTheme()).
		WithWidth(fw).
		WithHeight(fh)
	m.form = fs
	m.scr = scrForm
	return fs.f.Init()
}

func (m *model) resizeForm() tea.Cmd {
	if m.form == nil {
		return nil
	}
	fw, fh := m.formSize()
	m.form.f.WithWidth(fw).WithHeight(fh)
	return nil
}

func (m *model) keyForm(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "esc" {
		return m.closeForm(false)
	}
	return m.updateForm(msg)
}

func (m *model) updateForm(msg tea.Msg) tea.Cmd {
	fs := m.form
	if fs == nil {
		return nil
	}
	next, cmd := fs.f.Update(msg)
	if f, ok := next.(*huh.Form); ok {
		fs.f = f
	}
	switch fs.f.State {
	case huh.StateCompleted:
		return tea.Batch(cmd, m.closeForm(true))
	case huh.StateAborted:
		return tea.Batch(cmd, m.closeForm(false))
	}
	return cmd
}

// closeForm applies the form's values. Esc keeps the edits that are valid
// ("esc: back (your edits are kept)").
func (m *model) closeForm(submitted bool) tea.Cmd {
	fs := m.form
	m.form = nil
	m.scr = scrChecklist
	if fs == nil {
		return nil
	}
	var kept []string
	for _, b := range fs.binds {
		if err := m.applyBinding(b, submitted); err != nil {
			kept = append(kept, b.field.Label+" ("+err.Error()+")")
		}
	}
	m.cursorTo(fs.idx)
	var flash tea.Cmd
	switch {
	case len(kept) > 0:
		flash = m.setFlash(true, "Kept the previous value for: %s", strings.Join(kept, "; "))
	case submitted:
		flash = m.setFlash(false, "Options saved for %s.", fs.title)
	}
	return tea.Batch(flash, m.detect())
}

func (m *model) applyBinding(b binding, submitted bool) error {
	o := m.a.Opts
	switch b.field.Kind {
	case options.Bool:
		return o.Set(b.key, *b.b)
	case options.String, options.Int, options.List, options.Choice:
		if err := validator(b.field)(*b.s); err != nil {
			return err
		}
		return o.Set(b.key, *b.s)
	case options.Secret:
		switch *b.mode {
		case modeType:
			pw := strings.TrimSpace(*b.pw)
			if pw == "" {
				if submitted {
					return errors.New("no password typed")
				}
				return nil
			}
			if *b.pw != *b.pw2 {
				return errors.New("the two passwords don't match")
			}
			if err := o.Set(b.key, pw); err != nil {
				return err
			}
			m.registerSecret(b.key, pw)
			m.generated[b.key] = false
		case modeGenerate:
			v, err := m.a.Secrets.GenerateAndRegister(b.key, secretLen)
			if err != nil {
				return fmt.Errorf("couldn't generate one: %w", err)
			}
			if serr := o.Set(b.key, v); serr != nil {
				return serr
			}
			m.registerSecret(b.key, v)
			m.generated[b.key] = true
		}
	}
	return nil
}

// registerSecret makes sure a secret never reaches the log or the screen
// unredacted: the secrets store (for sys.Cmd.Redact) and the log redactor.
func (m *model) registerSecret(name, v string) {
	m.a.Secrets.Add(name, v)
	m.a.Red.Add(v)
}

func (m *model) viewForm() ([]string, []hint, string) {
	fs := m.form
	cw, bh := m.cw(), m.bodyH()
	if fs == nil {
		return nil, nil, ""
	}
	body := strings.Split(fs.f.View(), "\n")
	hints := []hint{h("ENTER", "Next"), h("SHIFT+TAB", "Previous"), h("ESC", "Back (keeps your edits)"), h("F3", "Quit")}
	return m.panel("Options: "+fs.title, body, cw, bh), hints, "Options"
}

// huhTheme is the XP look for Huh: no colors outside the D42 palette and
// console-safe glyphs.
func (m *model) huhTheme() huh.Theme {
	st, g := m.st, m.g
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := huh.ThemeBase(isDark)
		edge := lipgloss.Border{Left: g.sV}
		t.Focused.Base = lipgloss.NewStyle().PaddingLeft(1).BorderStyle(edge).BorderLeft(true).BorderForeground(st.white)
		t.Focused.Card = t.Focused.Base
		t.Focused.Title = lipgloss.NewStyle().Foreground(st.white)
		t.Focused.NoteTitle = t.Focused.Title
		t.Focused.Description = lipgloss.NewStyle().Foreground(st.grey)
		t.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(st.yellow).SetString(" " + g.err)
		t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(st.yellow)
		t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(st.white).SetString("> ")
		t.Focused.Option = lipgloss.NewStyle().Foreground(st.grey)
		t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(st.white)
		t.Focused.NextIndicator = lipgloss.NewStyle().MarginLeft(1).Foreground(st.white).SetString(">")
		t.Focused.PrevIndicator = lipgloss.NewStyle().MarginRight(1).Foreground(st.white).SetString("<")
		t.Focused.MultiSelectSelector = lipgloss.NewStyle().Foreground(st.white).SetString("> ")
		t.Focused.SelectedPrefix = lipgloss.NewStyle().SetString("[x] ")
		t.Focused.UnselectedPrefix = lipgloss.NewStyle().SetString("[ ] ")
		t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(st.white)
		t.Focused.TextInput.Placeholder = lipgloss.NewStyle().Foreground(st.grey)
		t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(st.white)
		t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(st.white)
		button := lipgloss.NewStyle().Padding(0, 2).MarginRight(1)
		t.Focused.FocusedButton = button.Foreground(st.blue).Background(st.grey)
		t.Focused.BlurredButton = button.Foreground(st.grey)
		t.Focused.Next = t.Focused.FocusedButton

		t.Blurred = t.Focused
		t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Card = t.Blurred.Base
		t.Blurred.Title = lipgloss.NewStyle().Foreground(st.grey)
		t.Blurred.SelectSelector = lipgloss.NewStyle().SetString("  ")
		t.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")
		t.Blurred.NextIndicator = lipgloss.NewStyle()
		t.Blurred.PrevIndicator = lipgloss.NewStyle()
		t.Blurred.TextInput.Text = lipgloss.NewStyle().Foreground(st.grey)
		t.Blurred.FocusedButton = t.Focused.FocusedButton
		t.Blurred.BlurredButton = t.Focused.BlurredButton

		t.Group.Title = lipgloss.NewStyle().Foreground(st.white)
		t.Group.Description = lipgloss.NewStyle().Foreground(st.yellow)
		return t
	})
}
