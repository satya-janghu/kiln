// Package options defines wizard options, their defaults and how values resolve:
// defaults < last run on this machine < wizard edits (docs/options-reference.md).
package options

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Kind is an option's value type.
type Kind int

const (
	Bool Kind = iota
	String
	Int
	Choice
	Secret
	List // comma or newline separated strings
)

// Choice is one allowed value of a Choice option.
type ChoiceItem struct {
	Value, Label string
	// DisabledReason, if non-empty, greys the choice out with this reason.
	DisabledReason string
}

// Field describes one option of a step.
type Field struct {
	Key     string // unique within the step
	Label   string
	Help    string
	Kind    Kind
	Default any // bool, string, int, []string
	Choices []ChoiceItem
	// Validate returns a plain-language error for bad values.
	Validate func(v any) error
}

// Resolved holds every step's enablement and every option's value.
type Resolved struct {
	Enabled map[string]bool
	Values  map[string]any // "step.key"
	Fields  map[string]Field
}

// New returns an empty Resolved.
func New() *Resolved {
	return &Resolved{Enabled: map[string]bool{}, Values: map[string]any{}, Fields: map[string]Field{}}
}

// Declare registers a step's fields with their defaults (call once per step).
func (r *Resolved) Declare(step string, defaultEnabled bool, fields []Field) {
	if _, ok := r.Enabled[step]; !ok {
		r.Enabled[step] = defaultEnabled
	}
	for _, f := range fields {
		k := step + "." + f.Key
		r.Fields[k] = f
		if _, ok := r.Values[k]; !ok {
			r.Values[k] = f.Default
		}
	}
}

// Set assigns a value after normalising and validating it.
func (r *Resolved) Set(key string, v any) error {
	f, ok := r.Fields[key]
	if !ok {
		return fmt.Errorf("unknown option %q", key)
	}
	nv, err := Normalize(f, v)
	if err != nil {
		return fmt.Errorf("%s: %w", f.Label, err)
	}
	if f.Validate != nil {
		if err := f.Validate(nv); err != nil {
			return fmt.Errorf("%s: %w", f.Label, err)
		}
	}
	r.Values[key] = nv
	return nil
}

// Normalize converts JSON/wizard input to the field's Go type.
func Normalize(f Field, v any) (any, error) {
	switch f.Kind {
	case Bool:
		switch t := v.(type) {
		case bool:
			return t, nil
		case string:
			return strconv.ParseBool(t)
		}
	case Int:
		switch t := v.(type) {
		case int:
			return t, nil
		case float64:
			return int(t), nil
		case string:
			return strconv.Atoi(strings.TrimSpace(t))
		}
	case String, Secret:
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s), nil
		}
	case Choice:
		s, ok := v.(string)
		if !ok {
			break
		}
		for _, c := range f.Choices {
			if c.Value == s {
				if c.DisabledReason != "" {
					return nil, fmt.Errorf("%q isn't available: %s", s, c.DisabledReason)
				}
				return s, nil
			}
		}
		return nil, fmt.Errorf("%q isn't one of the choices", s)
	case List:
		switch t := v.(type) {
		case []string:
			return clean(t), nil
		case []any:
			var out []string
			for _, x := range t {
				out = append(out, fmt.Sprint(x))
			}
			return clean(out), nil
		case string:
			return clean(strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == '\n' })), nil
		}
	}
	return nil, fmt.Errorf("unexpected value %v", v)
}

func clean(in []string) []string {
	out := []string{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// View is one step's options.
type View struct {
	r    *Resolved
	step string
}

// For returns a step's view.
func (r *Resolved) For(step string) View { return View{r, step} }

func (v View) get(key string) any {
	val, ok := v.r.Values[v.step+"."+key]
	if !ok {
		panic(fmt.Sprintf("option %s.%s was not declared", v.step, key))
	}
	return val
}

// Bool returns a bool option.
func (v View) Bool(key string) bool { b, _ := v.get(key).(bool); return b }

// String returns a string, choice or secret option.
func (v View) String(key string) string { s, _ := v.get(key).(string); return s }

// Int returns an int option.
func (v View) Int(key string) int { i, _ := v.get(key).(int); return i }

// List returns a list option.
func (v View) List(key string) []string { l, _ := v.get(key).([]string); return l }

// Enabled reports whether a step is ticked.
func (r *Resolved) IsEnabled(step string) bool { return r.Enabled[step] }

// NonDefault returns options that differ from defaults (saved to state.json), excluding secrets.
func (r *Resolved) NonDefault(defaults *Resolved) map[string]any {
	out := map[string]any{}
	keys := make([]string, 0, len(r.Values))
	for k := range r.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if r.Fields[k].Kind == Secret {
			continue
		}
		if fmt.Sprint(r.Values[k]) != fmt.Sprint(defaults.Values[k]) {
			out[k] = r.Values[k]
		}
	}
	return out
}

// Clone deep-copies a Resolved.
func (r *Resolved) Clone() *Resolved {
	c := New()
	for k, v := range r.Enabled {
		c.Enabled[k] = v
	}
	for k, v := range r.Fields {
		c.Fields[k] = v
	}
	for k, v := range r.Values {
		if l, ok := v.([]string); ok {
			v = append([]string(nil), l...)
		}
		c.Values[k] = v
	}
	return c
}
