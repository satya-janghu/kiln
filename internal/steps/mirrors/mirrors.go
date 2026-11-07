// Package mirrors implements catalog step 3 `mirrors` (docs/steps-catalog.md).
// The ranking logic lives in internal/mirrors, shared with `kiln mirrors bench|reset`.
package mirrors

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/mirrors"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/steps/aptsources"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Step is the `mirrors` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return "mirrors" }
func (Step) Title() string       { return "Fastest download mirrors" }
func (Step) Category() string    { return "base" }
func (Step) DependsOn() []string { return []string{"apt-sources"} }

func intRange(lo, hi int, unit string) func(any) error {
	return func(v any) error {
		if n, _ := v.(int); n < lo || n > hi {
			return fmt.Errorf("use %d to %d %s", lo, hi, unit)
		}
		return nil
	}
}

// Options uses the keys the CLI reads for `kiln mirrors bench`.
func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "country", Label: "Country", Kind: options.String, Default: "auto",
			Help: "Two-letter country code (e.g. IN, DE), or auto to use the timezone. kiln never asks a geo-IP service.",
			Validate: func(v any) error {
				s, _ := v.(string)
				if strings.EqualFold(s, "auto") || (len(s) == 2 && strings.Trim(strings.ToUpper(s), "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == "") {
					return nil
				}
				return errors.New("use a two-letter country code such as IN or DE, or auto")
			}},
		{Key: "neighbors", Label: "Include neighboring countries", Kind: options.Bool, Default: true},
		{Key: "sample", Label: "Worldwide sample", Kind: options.Int, Default: 4,
			Help: "Random mirrors from outside the region.", Validate: intRange(0, 50, "mirrors")},
		{Key: "top", Label: "Mirrors to keep", Kind: options.Int, Default: 5,
			Help:     "The fastest N share priority 1, so apt spreads downloads across them; deb.debian.org is the fallback.",
			Validate: intRange(1, 20, "mirrors")},
		{Key: "test_mb", Label: "Test size (MB)", Kind: options.Int, Default: 40,
			Help: "Per mirror, one at a time. The test file is main/Contents-all.gz (about 38 MB).", Validate: intRange(1, 1024, "MB")},
		{Key: "test_seconds", Label: "Time cap per mirror (seconds)", Kind: options.Int, Default: 6,
			Validate: intRange(2, 120, "seconds")},
	}
}

func backports(env *step.Env) bool {
	b, _ := env.All.Values["apt-sources.backports"].(bool)
	return b
}

// timezone is the system step's timezone option when set (catalog: system.timezone,
// else timedatectl).
func timezone(env *step.Env) string {
	tz, _ := env.All.Values["system.timezone"].(string)
	if strings.Contains(tz, "/") || tz == "UTC" {
		return tz
	}
	return ""
}

func (s Step) opts(env *step.Env) mirrors.BenchOpts {
	v := env.Opts
	return mirrors.BenchOpts{
		Country: v.String("country"), Neighbors: v.Bool("neighbors"), Sample: v.Int("sample"),
		Top: v.Int("top"), TestBytes: int64(v.Int("test_mb")) << 20, Timeout: v.Int("test_seconds"),
		Timezone: timezone(env),
		Say: func(t string) {
			env.Say("%s", t)
			if env.Output != nil {
				env.Output(t)
			}
		},
	}
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	uris, mod, err := mirrors.ListedMirrors(env.Sys)
	if err != nil || len(uris) == 0 {
		return step.Todo("no mirror list yet (%s)", mirrors.ListPath), nil
	}
	if age := time.Since(mod); age > mirrors.MaxAge {
		return step.Todo("the mirror list is %d days old", int(age.Hours()/24)), nil
	}
	want := string(aptsources.DebianSourcesContent(aptsources.MirrorURI, backports(env)))
	if b, err := env.Sys.ReadFile(aptsources.DebianSources); err != nil || string(b) != want {
		return step.Todo("%s doesn't point at the mirror list", aptsources.DebianSources), nil
	}
	if b, err := env.Sys.ReadFile(mirrors.AcquirePath); err != nil || string(b) != string(mirrors.AcquireContent()) {
		return step.Todo("%s is missing or different", mirrors.AcquirePath), nil
	}
	if err := mirrors.CheckFresh(ctx, env.Sys, uris, backports(env)); err != nil {
		return step.Todo("%s", err.Error()), nil
	}
	return step.Done(fmt.Sprintf("%d mirrors from %s (re-measure with `kiln mirrors bench --apply`)", len(uris), mirrors.ListPath)), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	o := s.opts(env)
	bp := backports(env)
	where := "the country from the timezone"
	if !strings.EqualFold(o.Country, "auto") {
		where = strings.ToUpper(o.Country)
	}
	if o.Neighbors {
		where += " and its neighbors"
	}
	var results []mirrors.Result
	var prev mirrors.PrevList // the mirror list before this run's write
	return []action.Action{
		action.Custom{
			Desc: fmt.Sprintf("measure Debian mirrors one at a time (%s, %d random others and deb.debian.org; drop any not up to date with deb.debian.org; up to %d MB or %d s each), then write the fastest %d to %s with priority 1 and deb.debian.org as the priority 99 fallback",
				where, o.Sample, o.TestBytes>>20, o.Timeout, o.Top, mirrors.ListPath),
			K: action.Files,
			Fn: func(ctx context.Context, s sys.System) error {
				if s.DryRun() {
					return nil // never measure in a preview
				}
				res, err := mirrors.Rank(ctx, s, env.Facts, o, bp)
				if err != nil {
					return fmt.Errorf("mirrors couldn't be measured, kept deb.debian.org: %w", err)
				}
				results = res
				list, err := mirrors.ListContent(results, o.Top, time.Now())
				if err != nil {
					return err
				}
				prev = mirrors.SaveList(s) // so a failed apt update below puts it back
				_, err = s.WriteFile(mirrors.ListPath, list, sys.WriteOpts{Mode: 0o644})
				return err
			},
		},
		action.WriteFile{Path: mirrors.AcquirePath, Mode: 0o644, Content: mirrors.AcquireContent()},
		action.Custom{
			Desc: fmt.Sprintf("point the main and backports entries in %s at %s, then run apt-get update; if that fails, put the previous %s and %s back",
				aptsources.DebianSources, aptsources.MirrorURI, aptsources.DebianSources, mirrors.ListPath),
			K: action.Packages,
			Fn: func(ctx context.Context, s sys.System) error {
				return mirrors.UseSources(ctx, s, bp, prev)
			},
		},
	}, nil
}
