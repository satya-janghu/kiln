// Package mirrors ranks Debian mirrors by throughput (catalog step 3), shared by
// the mirrors step and `kiln mirrors bench|reset`.
package mirrors

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/steps/aptsources"
	"github.com/satya-janghu/kiln/internal/sys"
)

// Paths and URLs (catalog step 3).
const (
	MasterlistURL = "https://mirror-master.debian.org/status/Mirrors.masterlist"
	ListPath      = "/etc/apt/mirrors/debian.list"
	AcquirePath   = "/etc/apt/apt.conf.d/80kiln-acquire"
	// DefaultMirror is deb.debian.org as a candidate; the fallback line uses aptsources.DefaultURI.
	DefaultMirror = "http://deb.debian.org/debian/"
	// MaxAge is how old the mirror list may get before the step measures again.
	MaxAge = 30 * 24 * time.Hour
	// testFile is the throughput test file, listed in the signed trixie InRelease.
	testFile = "main/Contents-all.gz"
	// warmup is skipped when scoring, so TCP slow start doesn't count.
	warmup = 500 * time.Millisecond
	// minBytes is the least a test must receive to get a score.
	minBytes = 1 << 20
	// laggingUpdates is how far trixie-updates/backports may trail deb.debian.org.
	laggingUpdates = 12 * time.Hour
)

// BenchOpts controls a benchmark.
type BenchOpts struct {
	Country   string // ISO code or "auto"
	Sample    int
	Top       int
	TestBytes int64
	Timeout   int // seconds
	Apply     bool
	Say       func(string)
	// Timezone, when set, is used for country "auto" instead of
	// `timedatectl show -p Timezone` (the system step's timezone option).
	Timezone string
}

func (o BenchOpts) say(format string, a ...any) {
	if o.Say != nil {
		o.Say(fmt.Sprintf(format, a...))
	}
}

// Mirror is one candidate.
type Mirror struct {
	URL     string // http://<Site><Archive-http>, ending in '/'
	Site    string
	Country string // ISO code, "" for deb.debian.org
}

// Result is one candidate's outcome.
type Result struct {
	Mirror
	Score float64 // bytes per second after the warm-up; 0 = unusable
	Note  string  // why it was dropped or scored 0
}

// MBps renders a score.
func (r Result) MBps() string { return fmt.Sprintf("%.1f MB/s", r.Score/1e6) }

// AcquireContent is the retries drop-in (apt.conf(5); the default is 3).
func AcquireContent() []byte {
	return []byte(sys.ManagedHeader("//") + "Acquire::Retries \"5\";\n")
}

// Suites returns the suites whose InRelease is compared.
func Suites(backports bool) []string {
	s := []string{"trixie", "trixie-updates"}
	if backports {
		s = append(s, "trixie-backports")
	}
	return s
}

// ResolveCountry turns "auto" into an ISO code via the compiled-in zone table
// (D21: never geo-IP). It returns "" for an unknown timezone.
func ResolveCountry(ctx context.Context, s sys.System, f *facts.Facts, country, tz string) (cc, zone string) {
	if c := strings.ToUpper(strings.TrimSpace(country)); c != "" && c != "AUTO" {
		return c, ""
	}
	zone = strings.TrimSpace(tz)
	if zone == "" {
		if r, err := s.Query(ctx, sys.Cmd{Name: "timedatectl", Args: []string{"show", "-p", "Timezone", "--value"}}); err == nil {
			zone = strings.TrimSpace(r.Stdout)
		}
	}
	if zone == "" && f != nil {
		zone = f.Timezone
	}
	return zoneCountry[zone], zone
}

var (
	siteRe = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]+)?$`)
	pathRe = regexp.MustCompile(`^/[A-Za-z0-9._~/-]*$`)
)

// Candidates parses the masterlist and selects mirrors (catalog step 3.1).
func Candidates(ctx context.Context, s sys.System, cc string, sample int) ([]Mirror, error) {
	b, err := s.HTTPGet(ctx, MasterlistURL, 16<<20)
	if err != nil {
		return nil, fmt.Errorf("couldn't download the Debian mirror list (%w); check the network, or keep deb.debian.org by unticking this step", err)
	}
	region := map[string]bool{}
	if cc != "" {
		region[cc] = true
	}
	seen := map[string]bool{"deb.debian.org": true}
	var local, others []Mirror
	for _, st := range apt.ParseDeb822(string(b)) {
		site, path := strings.TrimSpace(st.Get("Site")), strings.TrimSpace(st.Get("Archive-http"))
		if site == "" || path == "" || !hasWord(st.Get("Archive-architecture"), "amd64") {
			continue
		}
		if !siteRe.MatchString(site) || !pathRe.MatchString(path) || seen[strings.ToLower(site)] {
			continue
		}
		seen[strings.ToLower(site)] = true
		if !strings.HasSuffix(path, "/") {
			path += "/"
		}
		country := ""
		if f := strings.Fields(st.Get("Country")); len(f) > 0 {
			country = strings.ToUpper(f[0])
		}
		m := Mirror{URL: "http://" + site + path, Site: site, Country: country}
		if region[country] {
			local = append(local, m)
		} else {
			others = append(others, m)
		}
	}
	rand.Shuffle(len(others), func(i, j int) { others[i], others[j] = others[j], others[i] }) //nolint:gosec // picking a sample of mirrors, not a secret
	if sample < 0 {
		sample = 0
	}
	if sample > len(others) {
		sample = len(others)
	}
	out := append(local, others[:sample]...)
	return append(out, Mirror{URL: DefaultMirror, Site: "deb.debian.org"}), nil
}

func hasWord(field, w string) bool {
	for _, x := range strings.Fields(field) {
		if strings.EqualFold(x, w) {
			return true
		}
	}
	return false
}

// release is what kiln reads from an InRelease file.
type release struct {
	date, validUntil time.Time
	contentsAll      int64 // size of main/Contents-all.gz, 0 if not listed
}

func parseTime(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	for _, layout := range []string{time.RFC1123, time.RFC1123Z} {
		if t, err := time.Parse(layout, v); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unreadable date %q", v)
}

func parseRelease(b []byte) (release, error) {
	var r release
	inSHA := false
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.HasPrefix(l, "-----BEGIN PGP SIGNATURE") {
			break
		}
		if inSHA && strings.HasPrefix(l, " ") {
			f := strings.Fields(l)
			if len(f) == 3 && f[2] == testFile {
				r.contentsAll, _ = strconv.ParseInt(f[1], 10, 64)
			}
			continue
		}
		inSHA = false
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch k {
		case "Date":
			if r.date.IsZero() {
				t, err := parseTime(v)
				if err != nil {
					return r, err
				}
				r.date = t
			}
		case "Valid-Until":
			if r.validUntil.IsZero() {
				t, err := parseTime(v)
				if err != nil {
					return r, err
				}
				r.validUntil = t
			}
		case "SHA256":
			inSHA = true
		}
	}
	if r.date.IsZero() {
		return r, errors.New("the release file has no Date field")
	}
	return r, nil
}

func fetchRelease(ctx context.Context, s sys.System, base, suite string) (release, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	b, err := s.HTTPGet(ctx, base+"dists/"+suite+"/InRelease", 16<<20)
	if err != nil {
		return release{}, err
	}
	return parseRelease(b)
}

// reference reads deb.debian.org's release files, the yardstick for freshness.
func reference(ctx context.Context, s sys.System, backports bool) (map[string]release, error) {
	ref := map[string]release{}
	for _, suite := range Suites(backports) {
		r, err := fetchRelease(ctx, s, DefaultMirror, suite)
		if err != nil {
			return nil, fmt.Errorf("couldn't read %s from deb.debian.org to compare mirrors against (%w); check the network", suite, err)
		}
		ref[suite] = r
	}
	return ref, nil
}

// fresh applies the freshness rule (catalog step 3.2) to one mirror.
func fresh(ctx context.Context, s sys.System, base string, ref map[string]release, backports bool, now time.Time) error {
	for _, suite := range Suites(backports) {
		r, err := fetchRelease(ctx, s, base, suite)
		if err != nil {
			return fmt.Errorf("%s: %w", suite, err)
		}
		want := ref[suite]
		switch {
		case suite == "trixie" && !r.date.Equal(want.date):
			return fmt.Errorf("trixie is dated %s, deb.debian.org has %s (missing a point release)", r.date.UTC().Format(time.RFC3339), want.date.UTC().Format(time.RFC3339))
		case suite != "trixie" && r.date.Before(want.date.Add(-laggingUpdates)):
			return fmt.Errorf("%s is more than 12 hours behind deb.debian.org", suite)
		case !r.validUntil.IsZero() && r.validUntil.Before(now):
			return fmt.Errorf("%s expired at %s", suite, r.validUntil.UTC().Format(time.RFC3339))
		}
	}
	return nil
}

// CheckFresh reports the first listed mirror that no longer passes the freshness rule.
func CheckFresh(ctx context.Context, s sys.System, uris []string, backports bool) error {
	ref, err := reference(ctx, s, backports)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, u := range uris {
		if sameMirror(u, DefaultMirror) {
			continue
		}
		if err := fresh(ctx, s, withSlash(u), ref, backports, now); err != nil {
			return fmt.Errorf("%s is no longer up to date: %w", u, err)
		}
	}
	return nil
}

func withSlash(u string) string {
	if strings.HasSuffix(u, "/") {
		return u
	}
	return u + "/"
}

func sameMirror(a, b string) bool { return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/") }

// benchClient is used only for the throughput test, which needs a Range
// request and timing that sys.System's HTTPGet doesn't offer. It only reads.
var benchClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DisableCompression: true}}

// measure downloads up to n bytes of the test file for at most timeout and
// returns bytes per second after the warm-up (catalog step 3.3).
func measure(ctx context.Context, base string, n int64, timeout time.Duration) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"dists/trixie/"+testFile, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "kiln")
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", n-1))
	resp, err := benchClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("no response: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	t0 := time.Now()
	var total, warmBytes int64
	var warm, end time.Time
	buf := make([]byte, 64<<10)
	body := io.LimitReader(resp.Body, n)
	for {
		k, rerr := body.Read(buf)
		now := time.Now()
		if k > 0 {
			total += int64(k)
			end = now
			if warm.IsZero() && now.Sub(t0) >= warmup {
				warm, warmBytes = now, total
			}
		}
		if rerr != nil {
			// A deadline, EOF or a dropped connection all end the test; only
			// the amount received decides the score.
			break
		}
	}
	if total < minBytes {
		return 0, fmt.Errorf("only %d KB arrived in %s", total>>10, timeout)
	}
	if warm.IsZero() || !end.After(warm) {
		// Everything arrived within the warm-up: score the whole transfer.
		if !end.After(t0) {
			return 0, errors.New("the transfer couldn't be timed")
		}
		return float64(total) / end.Sub(t0).Seconds(), nil
	}
	return float64(total-warmBytes) / end.Sub(warm).Seconds(), nil
}

// Rank selects, filters and measures mirrors one at a time (catalog step 3,
// actions 1-3). Results are sorted fastest first; dropped mirrors score 0.
func Rank(ctx context.Context, s sys.System, f *facts.Facts, o BenchOpts, backports bool) ([]Result, error) {
	if s.DryRun() {
		return nil, errors.New("mirrors are never measured in a preview")
	}
	cc, zone := ResolveCountry(ctx, s, f, o.Country, o.Timezone)
	switch {
	case cc != "" && zone != "":
		o.say("Timezone %s → country %s", zone, cc)
	case cc == "":
		o.say("Timezone %q isn't in kiln's table; testing a worldwide sample and deb.debian.org", zone)
	}
	cands, err := Candidates(ctx, s, cc, o.Sample)
	if err != nil {
		return nil, err
	}
	ref, err := reference(ctx, s, backports)
	if err != nil {
		return nil, err
	}
	n := o.TestBytes
	if size := ref["trixie"].contentsAll; size > 0 && size < n {
		n = size
	}
	if n < minBytes {
		n = minBytes
	}
	timeout := time.Duration(o.Timeout) * time.Second
	now := time.Now()
	out := make([]Result, 0, len(cands))
	for i, m := range cands {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r := Result{Mirror: m}
		if m.URL != DefaultMirror {
			if ferr := fresh(ctx, s, m.URL, ref, backports, now); ferr != nil {
				r.Note = "dropped: " + ferr.Error()
				o.say("[%d/%d] %s: %s", i+1, len(cands), m.Site, r.Note)
				out = append(out, r)
				continue
			}
		}
		o.say("[%d/%d] measuring %s …", i+1, len(cands), m.Site)
		score, merr := measure(ctx, m.URL, n, timeout)
		r.Score = score
		if merr != nil {
			r.Note = merr.Error()
			o.say("[%d/%d] %s: unusable (%s)", i+1, len(cands), m.Site, r.Note)
		} else {
			o.say("[%d/%d] %s: %s", i+1, len(cands), m.Site, r.MBps())
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

// ListContent renders /etc/apt/mirrors/debian.list (apt-transport-mirror(1)):
// the top mirrors share priority:1 so apt spreads files across them, and
// deb.debian.org is the priority:99 fallback.
func ListContent(results []Result, top int, now time.Time) ([]byte, error) {
	var b strings.Builder
	b.WriteString(sys.ManagedHeader("#"))
	b.WriteString("# Fastest Debian mirrors, measured " + now.UTC().Format("2006-01-02 15:04 UTC") + ".\n")
	b.WriteString("# Measure again with: sudo kiln mirrors bench --apply   Go back with: sudo kiln mirrors reset\n")
	kept, hasDefault := 0, false
	for _, r := range results {
		if kept >= top {
			break
		}
		if r.Score <= 0 {
			continue
		}
		u := r.URL
		if sameMirror(u, DefaultMirror) {
			u, hasDefault = aptsources.DefaultURI, true
		}
		b.WriteString(u + "\tpriority:1\n")
		kept++
	}
	if kept == 0 {
		return nil, errors.New("no mirror could be measured, so kiln kept deb.debian.org; check the network and run `sudo kiln mirrors bench --apply` later")
	}
	if !hasDefault {
		b.WriteString(aptsources.DefaultURI + "\tpriority:99\n")
	}
	return []byte(b.String()), nil
}

// ListedMirrors returns the URIs in the mirror list and its modification time.
func ListedMirrors(s sys.System) ([]string, time.Time, error) {
	fi, err := s.Stat(ListPath)
	if err != nil {
		return nil, time.Time{}, err
	}
	b, err := s.ReadFile(ListPath)
	if err != nil {
		return nil, time.Time{}, err
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		u, _, _ := strings.Cut(l, "\t")
		out = append(out, strings.TrimSpace(u))
	}
	return out, fi.ModTime(), nil
}

// BackportsInSources reports whether debian.sources has a trixie-backports stanza.
func BackportsInSources(s sys.System) bool {
	b, err := s.ReadFile(aptsources.DebianSources)
	if err != nil {
		return false
	}
	for _, st := range apt.ParseDeb822(string(b)) {
		if hasWord(st.Get("Suites"), "trixie-backports") && st.Enabled() {
			return true
		}
	}
	return false
}

func usesMirrorList(s sys.System) bool {
	return aptsources.CurrentMainURI(s) == aptsources.MirrorURI
}

// PrevList is the mirror list as it was before a new one is written, so a
// failed apt update can put it back together with debian.sources.
type PrevList struct {
	content []byte
	existed bool
}

// SaveList reads the current mirror list; call it before writing a new one.
func SaveList(s sys.System) PrevList {
	b, err := s.ReadFile(ListPath)
	return PrevList{content: b, existed: err == nil}
}

// restore puts the previous mirror list back, or removes the new one (kiln's
// own file, so without a backup) if there was none before.
func (p PrevList) restore(s sys.System) error {
	if p.existed {
		_, err := s.WriteFile(ListPath, p.content, sys.WriteOpts{Mode: 0o644})
		return err
	}
	return s.RemoveAll(ListPath)
}

// UseSources points main and backports at the mirror list, runs apt update,
// and puts the previous debian.sources and mirror list (prev, saved with
// SaveList before the new list was written) back if that fails (catalog
// step 3.5, 3.7).
func UseSources(ctx context.Context, s sys.System, backports bool, prev PrevList) error {
	prevSources, rerr := s.ReadFile(aptsources.DebianSources)
	if rerr != nil && !s.DryRun() {
		return fmt.Errorf("%s is missing (%w); run the apt-sources step first", aptsources.DebianSources, rerr)
	}
	hadMirrors := usesMirrorList(s)
	if _, err := s.WriteFile(aptsources.DebianSources, aptsources.DebianSourcesContent(aptsources.MirrorURI, backports), sys.WriteOpts{Mode: 0o644}); err != nil {
		return err
	}
	uerr := apt.Update(ctx, s)
	if uerr == nil {
		return nil
	}
	if _, err := s.WriteFile(aptsources.DebianSources, prevSources, sys.WriteOpts{Mode: 0o644}); err != nil {
		return fmt.Errorf("mirrors couldn't be used (%w), and the previous %s couldn't be put back (%w); restore it from %s", uerr, aptsources.DebianSources, err, sys.BackupPath(aptsources.DebianSources))
	}
	if err := prev.restore(s); err != nil {
		return fmt.Errorf("mirrors couldn't be used (%w), and the previous %s couldn't be put back (%w); restore it from %s", uerr, ListPath, err, sys.BackupPath(ListPath))
	}
	if hadMirrors {
		return fmt.Errorf("the new mirrors couldn't be used, kept the previous mirror list in %s: %w", aptsources.DebianSources, uerr)
	}
	return fmt.Errorf("mirrors couldn't be used, kept deb.debian.org: %w", uerr)
}

// Apply writes the mirror list and the retries drop-in, then switches the sources.
func Apply(ctx context.Context, s sys.System, results []Result, top int, backports bool) error {
	list, err := ListContent(results, top, time.Now())
	if err != nil {
		return err
	}
	prev := SaveList(s)
	if _, err := s.WriteFile(ListPath, list, sys.WriteOpts{Mode: 0o644}); err != nil {
		return err
	}
	if _, err := s.WriteFile(AcquirePath, AcquireContent(), sys.WriteOpts{Mode: 0o644}); err != nil {
		return err
	}
	return UseSources(ctx, s, backports, prev)
}

// Bench ranks mirrors and optionally writes the mirror list (kiln mirrors bench).
func Bench(ctx context.Context, s sys.System, f *facts.Facts, o BenchOpts) error {
	if o.Apply {
		if _, err := s.Stat(aptsources.DebianSources); err != nil {
			return fmt.Errorf("%s doesn't exist yet; run `sudo kiln` first so the apt-sources step sets it up", aptsources.DebianSources)
		}
	}
	backports := BackportsInSources(s)
	res, err := Rank(ctx, s, f, o, backports)
	if err != nil {
		return err
	}
	o.say("")
	o.say("Ranking (fastest first):")
	for i, r := range res {
		line := fmt.Sprintf("%2d. %-40s %s", i+1, r.Site, r.MBps())
		if r.Note != "" {
			line += "  (" + r.Note + ")"
		}
		o.say("%s", line)
	}
	if !o.Apply {
		o.say("")
		o.say("Nothing was changed. Run `sudo kiln mirrors bench --apply` to use the top %d.", o.Top)
		return nil
	}
	if err := Apply(ctx, s, res, o.Top, backports); err != nil {
		return err
	}
	o.say("APT now uses the top %d mirrors from %s.", o.Top, ListPath)
	return nil
}

// Reset goes back to deb.debian.org (kiln mirrors reset).
func Reset(ctx context.Context, s sys.System) error {
	if _, err := s.Stat(aptsources.DebianSources); err != nil {
		return fmt.Errorf("%s doesn't exist; nothing to reset", aptsources.DebianSources)
	}
	if !usesMirrorList(s) {
		return nil
	}
	backports := BackportsInSources(s)
	if _, err := s.WriteFile(aptsources.DebianSources, aptsources.DebianSourcesContent(aptsources.DefaultURI, backports), sys.WriteOpts{Mode: 0o644}); err != nil {
		return err
	}
	if err := s.Remove(ListPath); err != nil {
		return err
	}
	return apt.Update(ctx, s)
}
