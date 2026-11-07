// Package aptsources implements catalog step 2 `apt-sources` (docs/steps-catalog.md).
package aptsources

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/revert"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Paths and values used by this step and by `mirrors`.
const (
	SourcesList   = "/etc/apt/sources.list"
	SourcesDir    = "/etc/apt/sources.list.d"
	DebianSources = "/etc/apt/sources.list.d/debian.sources"
	Keyring       = "/usr/share/keyrings/debian-archive-keyring.pgp"
	DefaultURI    = "http://deb.debian.org/debian"
	SecurityURI   = "http://security.debian.org/debian-security"
	MirrorURI     = "mirror+file:/etc/apt/mirrors/debian.list"
	Components    = "main contrib non-free non-free-firmware"
)

// Step is the `apt-sources` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return stepID }
func (Step) Title() string       { return "Software sources (deb822, backports)" }
func (Step) Category() string    { return "base" }
func (Step) DependsOn() []string { return []string{"preflight"} }

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "backports", Label: "Enable trixie-backports", Kind: options.Bool, Default: true,
			Help: "Packages come from backports only when a step asks for them."},
		{Key: "lock_wait", Label: "Wait for other package managers (minutes)", Kind: options.Int, Default: 10,
			Validate: func(v any) error {
				if n, _ := v.(int); n < 1 || n > 240 {
					return fmt.Errorf("use 1 to 240 minutes")
				}
				return nil
			}},
	}
}

// DebianStanzas builds kiln's debian.sources (main URI may be the mirror list).
func DebianStanzas(mainURI string, backports bool) []apt.Stanza {
	st := func(uri, suites string) apt.Stanza {
		return apt.Stanza{Fields: []apt.Field{
			{Key: "Types", Value: "deb"}, {Key: "URIs", Value: uri}, {Key: "Suites", Value: suites},
			{Key: "Components", Value: Components}, {Key: "Signed-By", Value: Keyring},
		}}
	}
	out := []apt.Stanza{st(mainURI, "trixie trixie-updates"), st(SecurityURI, "trixie-security")}
	if backports {
		out = append(out, st(mainURI, "trixie-backports"))
	}
	return out
}

// DebianSourcesContent renders debian.sources.
func DebianSourcesContent(mainURI string, backports bool) []byte {
	return []byte(apt.FormatDeb822(sys.ManagedHeader("#")+"# Debian archive entries. The mirrors step may point URIs at the mirror list.\n", DebianStanzas(mainURI, backports)))
}

// CurrentMainURI keeps the mirrors step's mirror list if debian.sources already uses it.
func CurrentMainURI(s sys.System) string {
	b, err := s.ReadFile(DebianSources)
	if err != nil {
		return DefaultURI
	}
	for _, st := range apt.ParseDeb822(string(b)) {
		if strings.Contains(st.Get("Suites"), "trixie-updates") && st.Get("URIs") == MirrorURI {
			return MirrorURI
		}
	}
	return DefaultURI
}

// debianArchives are the exact URIs of the archives kiln's debian.sources
// covers (with or without a trailing slash, over http or https). Other
// debian.org archives, such as deb.debian.org/debian-debug, aren't duplicates.
var debianArchives = []string{
	"deb.debian.org/debian",
	"security.debian.org/debian-security",
	"deb.debian.org/debian-security",
}

// isDebianURI reports whether any URI of a stanza's URIs field is one of the
// archives kiln's debian.sources owns.
func isDebianURI(uris string) bool {
	for _, u := range strings.Fields(uris) {
		if u == MirrorURI {
			return true
		}
		rest, ok := strings.CutPrefix(u, "http://")
		if !ok {
			rest, ok = strings.CutPrefix(u, "https://")
		}
		if ok && slices.Contains(debianArchives, strings.TrimSuffix(rest, "/")) {
			return true
		}
	}
	return false
}

func coversTrixie(suites []string) bool {
	for _, s := range suites {
		if strings.HasPrefix(s, "trixie") {
			return true
		}
	}
	return false
}

// listFiles returns *.list files in sources.list.d.
func listFiles(s sys.System, ext string) []string {
	m, _ := s.Glob(filepath.Join(SourcesDir, "*"+ext))
	return m
}

func activeLines(s sys.System, path string) []apt.OneLine {
	b, err := s.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []apt.OneLine
	for _, l := range strings.Split(string(b), "\n") {
		if o, ok := apt.ParseOneLine(l); ok {
			out = append(out, o)
		}
	}
	return out
}

// problems lists everything that differs from the desired state.
func (s Step) problems(env *step.Env) []string {
	var p []string
	backports := env.Opts.Bool("backports")
	want := string(DebianSourcesContent(CurrentMainURI(env.Sys), backports))
	if b, err := env.Sys.ReadFile(DebianSources); err != nil || string(b) != want {
		p = append(p, DebianSources+" isn't kiln's deb822 layout")
	}
	if len(activeLines(env.Sys, SourcesList)) > 0 {
		p = append(p, SourcesList+" still has active entries")
	}
	for _, f := range listFiles(env.Sys, ".list") {
		if len(activeLines(env.Sys, f)) > 0 {
			p = append(p, f+" uses the old one-line format")
		}
	}
	for _, f := range listFiles(env.Sys, ".sources") {
		if f == DebianSources {
			continue
		}
		b, _ := env.Sys.ReadFile(f)
		for _, st := range apt.ParseDeb822(string(b)) {
			if !st.Enabled() {
				continue
			}
			if strings.HasPrefix(st.Get("URIs"), "cdrom:") {
				p = append(p, f+" has an active cdrom entry")
			}
			if isDebianURI(st.Get("URIs")) && coversTrixie(st.Words("Suites")) {
				p = append(p, f+" duplicates the Debian entries")
			}
		}
	}
	return p
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	if p := s.problems(env); len(p) > 0 {
		return step.Todo("%s", strings.Join(p, "; ")), nil
	}
	return step.Done("deb822 sources with main, contrib, non-free and non-free-firmware"), nil
}

const stepID = "apt-sources"

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	sy := env.Sys
	// 0. Snapshot the sources first, so they can be put back if apt update
	// fails with the new ones (catalog step 2, gotchas).
	acts := []action.Action{action.Custom{
		Desc: "back up " + SourcesList + " and " + SourcesDir + " so they can be put back if apt update fails afterwards",
		K:    action.Files,
		Fn: func(ctx context.Context, s sys.System) error {
			if err := revert.Begin(ctx, s, env.State, stepID, "software sources", []string{SourcesList, SourcesDir}, nil); err != nil {
				return fmt.Errorf("couldn't back up the software sources before changing them: %w", err)
			}
			return nil
		},
	}}

	// 1. Comment out cdrom lines first (their labels break deb822 conversion).
	// 2. Comment out every active line in sources.list; kiln writes the Debian entries itself.
	for _, f := range append([]string{SourcesList}, listFiles(sy, ".list")...) {
		lines := activeLines(sy, f)
		if len(lines) == 0 {
			continue
		}
		b, _ := sy.ReadFile(f)
		if f == SourcesList {
			acts = append(acts, action.WriteFile{Path: f, Mode: 0o644, Content: commentAll(string(b),
				"# Debian entries now live in sources.list.d/debian.sources (managed by kiln).\n")})
			continue
		}
		if hasCdrom(lines) {
			acts = append(acts, action.WriteFile{Path: f, Mode: 0o644, Content: commentCdrom(string(b))})
		}
	}
	// 3. Convert remaining third-party .list files with apt's own tool.
	var thirdParty bool
	for _, f := range listFiles(sy, ".list") {
		for _, l := range activeLines(sy, f) {
			if !strings.HasPrefix(l.URI, "cdrom:") {
				thirdParty = true
			}
		}
	}
	if thirdParty {
		acts = append(acts, action.Custom{
			Desc: "convert third-party .list files to deb822 with `apt -y modernize-sources`, then make sure every converted file has a Signed-By key",
			K:    action.Packages,
			Fn: func(ctx context.Context, s sys.System) error {
				if _, err := s.Run(ctx, sys.Cmd{Name: "apt", Args: []string{"-y", "modernize-sources"}, Env: apt.Env()}); err != nil {
					return err
				}
				if s.DryRun() {
					return nil
				}
				for _, f := range listFiles(s, ".sources") {
					b, _ := s.ReadFile(f)
					for _, st := range apt.ParseDeb822(string(b)) {
						if st.Enabled() && strings.TrimSpace(st.Get("Signed-By")) == "" && f != DebianSources {
							return fmt.Errorf("%s has no Signed-By key after conversion; add the repository's key under /etc/apt/keyrings and set Signed-By, then run kiln again", f)
						}
					}
				}
				return nil
			},
		})
	}
	// 4. kiln's debian.sources (keeps the mirror list if the mirrors step set it).
	acts = append(acts, action.WriteFile{Path: DebianSources, Mode: 0o644,
		Content: DebianSourcesContent(CurrentMainURI(sy), env.Opts.Bool("backports"))})
	// 5. Disable duplicates and cdrom stanzas in other .sources files (after conversion).
	acts = append(acts, action.Custom{
		Desc: "set `Enabled: no` on any other .sources stanza that duplicates the Debian entries or points at a cdrom",
		K:    action.Files,
		Fn: func(ctx context.Context, s sys.System) error {
			for _, f := range listFiles(s, ".sources") {
				if f == DebianSources {
					continue
				}
				b, err := s.ReadFile(f)
				if err != nil {
					continue
				}
				sts := apt.ParseDeb822(string(b))
				changed := false
				for i := range sts {
					st := &sts[i]
					if !st.Enabled() {
						continue
					}
					u := st.Get("URIs")
					if strings.HasPrefix(u, "cdrom:") || (isDebianURI(u) && coversTrixie(st.Words("Suites"))) {
						st.Set("Enabled", "no")
						changed = true
					}
				}
				if changed {
					if _, err := s.WriteFile(f, []byte(apt.FormatDeb822(sys.ManagedHeader("#"), sts)), sys.WriteOpts{Mode: 0o644}); err != nil {
						return err
					}
				}
			}
			return nil
		},
	})
	acts = append(acts, action.Custom{
		Desc: "refresh package lists (apt-get update), then keep the new sources; put the previous sources back if it fails",
		K:    action.Packages,
		Fn: func(ctx context.Context, s sys.System) error {
			if err := apt.Update(ctx, s); err != nil {
				return err // Apply puts the previous sources back
			}
			return revert.Commit(s, env.State)
		},
	})
	return acts, nil
}

// Apply runs the plan; if anything fails after the sources were backed up
// (most often apt update with the new sources), the previous sources are put
// back, so apt keeps working and no unfinished change is left recorded.
func (s Step) Apply(ctx context.Context, env *step.Env, plan []action.Action) error {
	err := action.RunAll(ctx, env.Sys, plan)
	if err == nil || env.Sys.DryRun() || env.State.Pending == nil || env.State.Pending.Step != stepID {
		return err
	}
	if uerr := revert.Undo(ctx, env.Sys, env.State); uerr != nil {
		return fmt.Errorf("%w; putting the previous software sources back also failed: %w", err, uerr)
	}
	return fmt.Errorf("%w. The previous software sources were put back", err)
}

func hasCdrom(lines []apt.OneLine) bool {
	for _, l := range lines {
		if strings.HasPrefix(l.URI, "cdrom:") {
			return true
		}
	}
	return false
}

func commentAll(content, header string) []byte {
	var b strings.Builder
	if !strings.Contains(content, header) {
		b.WriteString(header)
	}
	for _, l := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if _, ok := apt.ParseOneLine(l); ok {
			l = "# " + l
		}
		b.WriteString(l + "\n")
	}
	return []byte(b.String())
}

func commentCdrom(content string) []byte {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if o, ok := apt.ParseOneLine(l); ok && strings.HasPrefix(o.URI, "cdrom:") {
			l = "# " + l
		}
		b.WriteString(l + "\n")
	}
	return []byte(b.String())
}
