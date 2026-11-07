// Package multimedia implements catalog step 7 `multimedia` (docs/steps-catalog.md):
// codecs, DVD playback, VA-API and the NVIDIA driver (D24, D39).
package multimedia

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

var (
	codecsAll   = []string{"libavcodec-extra"}
	codecsGNOME = []string{"gstreamer1.0-plugins-good", "gstreamer1.0-plugins-bad", "gstreamer1.0-plugins-ugly", "gstreamer1.0-libav"}
	vaAMD       = []string{"mesa-va-drivers"}
	vaInfo      = "vainfo" // for every GPU vendor (D51)
	dvdDebconf  = []string{
		"libdvd-pkg libdvd-pkg/build boolean true",
		"libdvd-pkg libdvd-pkg/post-invoke_hook-install boolean true",
	}
)

const (
	intelNonFree   = "intel-media-va-driver-non-free"
	intelMain      = "intel-media-va-driver"
	libdvdcss      = "/usr/lib/x86_64-linux-gnu/libdvdcss.so.2"
	headers        = "linux-headers-amd64"
	backportsSuite = "trixie-backports"
)

// Step is the `multimedia` step.
type Step struct {
	step.Base
	run *runState
}

// runState remembers, within one process, that this run deliberately left the
// NVIDIA driver out (Secure Boot signing couldn't be prepared, or a downgrade
// kiln doesn't automate), or that the DVD build failed and became a note, so the
// re-check after Apply doesn't count them as missing.
type runState struct {
	mu        sync.Mutex
	skipped   bool
	dvdFailed bool
}

func (r *runState) set(v bool) { r.mu.Lock(); r.skipped = v; r.mu.Unlock() }
func (r *runState) get() bool  { r.mu.Lock(); defer r.mu.Unlock(); return r.skipped }

func (r *runState) setDVDFailed(v bool) { r.mu.Lock(); r.dvdFailed = v; r.mu.Unlock() }
func (r *runState) getDVDFailed() bool  { r.mu.Lock(); defer r.mu.Unlock(); return r.dvdFailed }

// New returns the step.
func New() step.Step { return Step{run: &runState{}} }

func (Step) ID() string       { return "multimedia" }
func (Step) Title() string    { return "Codecs, video acceleration and NVIDIA" }
func (Step) Category() string { return "media" }

// DependsOn: the catalog adds `kernel` "when selected", but DependsOn can't
// depend on options, and `kernel` is off by default (D38), so listing it would
// skip this step on most machines. The headers' suite follows the selection instead.
func (Step) DependsOn() []string { return []string{"apt-sources"} }

func (Step) Packages() []step.PackageRef {
	deb := func(n, comp string) step.PackageRef {
		return step.PackageRef{Name: n, Suite: "trixie", Component: comp, Repo: "debian"}
	}
	var out []step.PackageRef
	for _, p := range append(append(append([]string{}, codecsAll...), codecsGNOME...), append(vaAMD, vaInfo)...) {
		out = append(out, deb(p, "main"))
	}
	out = append(out,
		deb("libdvd-pkg", "contrib"),
		deb(intelNonFree, "non-free"), deb(intelMain, "main"),
		deb("mokutil", "main"), deb("openssl", "main"), deb("dkms", "main"),
		deb(headers, "main"), step.PackageRef{Name: headers, Suite: backportsSuite, Component: "main", Repo: "debian"},
		deb("nvidia-driver", "non-free"),
		step.PackageRef{Name: "nvidia-driver", Suite: "/", Repo: "nvidia"},
		step.PackageRef{Name: "nvidia-kernel-open-dkms", Suite: "/", Repo: "nvidia"},
	)
	return out
}

func (Step) Options(f *facts.Facts) []options.Field {
	fields := []options.Field{
		{Key: "codecs", Label: "Codecs", Kind: options.Bool, Default: true,
			Help: "libavcodec-extra; with GNOME also the GStreamer good/bad/ugly/libav plugins."},
		{Key: "dvd", Label: "DVD playback (libdvd-pkg)", Kind: options.Bool, Default: true,
			Help: "Builds libdvdcss2 from videolan.org's source with libdvd-pkg (contrib)."},
		{Key: "vaapi", Label: "Video acceleration (VA-API)", Kind: options.Bool, Default: true,
			Help: "The VA-API driver for the AMD or Intel graphics found."},
	}
	if f == nil || len(f.NVIDIA()) == 0 {
		return fields
	}
	c := choicesFor(f)
	latest := options.ChoiceItem{Value: srcLatest, Label: "NVIDIA latest (NVIDIA's Debian 13 repository, newest branch)"}
	stock := options.ChoiceItem{Value: srcStock, Label: "Debian stock (trixie's nvidia-driver 550)"}
	def := srcLatest
	if !c.latestOK && c.stockOK {
		latest.DisabledReason = "NVIDIA's repository supports Turing (GTX 16xx / RTX 20xx) and newer only; this GPU is older"
		def = srcStock
	}
	if !c.stockOK && c.latestOK {
		stock.DisabledReason = "Debian's 550 driver doesn't support Blackwell (RTX 50xx) and newer GPUs"
	}
	if !c.latestOK && !c.stockOK {
		def = srcStock // mixed GPUs that no single source covers; the step explains and skips NVIDIA
	}
	return append(fields,
		options.Field{Key: "nvidia", Label: "NVIDIA driver", Kind: options.Bool, Default: true,
			Help: "Prepares MOK enrollment first if Secure Boot is on. Unticked, Debian's open nouveau driver stays."},
		options.Field{Key: "nvidia_source", Label: "NVIDIA driver source", Kind: options.Choice, Default: def,
			Help:    "Found: " + strings.Join(c.gpus, ", ") + ". " + debianStockWarning,
			Choices: []options.ChoiceItem{latest, stock}},
	)
}

// The NVIDIA options exist only on machines with an NVIDIA GPU, so read them safely.
func nvidiaOn(env *step.Env) bool {
	v, ok := env.All.Values["multimedia.nvidia"].(bool)
	return ok && v && len(env.Facts.NVIDIA()) > 0
}

func nvidiaSource(env *step.Env) string {
	v, _ := env.All.Values["multimedia.nvidia_source"].(string)
	return v
}

func backportsOn(env *step.Env) bool {
	v, ok := env.All.Values["apt-sources.backports"].(bool)
	return ok && v
}

// headersSuite: the same suite as the kernel metapackage (backports only when
// the backports kernel is selected for this machine).
func headersSuite(env *step.Env) string {
	if env.All.IsEnabled("kernel") && backportsOn(env) {
		return backportsSuite
	}
	return ""
}

func secureBoot(env *step.Env) bool { return env.Facts.SecureBoot == "enabled" }

// intelVA picks the non-free Intel driver when apt can install it, else main's.
func intelVA(ctx context.Context, s sys.System) string {
	if apt.Available(ctx, s, intelNonFree) {
		return intelNonFree
	}
	return intelMain
}

// wanted lists the plain packages (codecs and VA-API) this machine should have.
func wanted(ctx context.Context, env *step.Env) []string {
	var pkgs []string
	if env.Opts.Bool("codecs") {
		pkgs = append(pkgs, codecsAll...)
		if env.Facts.GNOME {
			pkgs = append(pkgs, codecsGNOME...)
		}
	}
	if env.Opts.Bool("vaapi") {
		if len(env.Facts.GPUs) > 0 {
			pkgs = append(pkgs, vaInfo)
		}
		if env.Facts.HasGPUVendor("1002") {
			pkgs = append(pkgs, vaAMD...)
		}
		if env.Facts.HasGPUVendor("8086") && !apt.Installed(ctx, env.Sys, intelNonFree) && !apt.Installed(ctx, env.Sys, intelMain) {
			pkgs = append(pkgs, intelVA(ctx, env.Sys))
		}
	}
	return pkgs
}

func dvdDone(ctx context.Context, s sys.System) bool {
	if !apt.Installed(ctx, s, "libdvd-pkg") || !strings.Contains(apt.InstalledVersion(ctx, s, "libdvdcss2"), "~local") {
		return false
	}
	_, err := s.Stat(libdvdcss)
	return err == nil
}

func nvidiaSourcesContent() []byte {
	st := apt.Stanza{Fields: []apt.Field{
		{Key: "Types", Value: "deb"},
		{Key: "URIs", Value: nvidiaRepoURI},
		{Key: "Suites", Value: "/"}, // a flat repository: no Components
		{Key: "Architectures", Value: "amd64"},
		{Key: "Signed-By", Value: nvidiaKeyring},
	}}
	return []byte(apt.FormatDeb822(sys.ManagedHeader("#")+"# NVIDIA's Debian 13 driver repository (catalog step 7, D39)\n", []apt.Stanza{st}))
}

func sourcesOK(s sys.System) bool {
	b, err := s.ReadFile(nvidiaSources)
	return err == nil && string(b) == string(nvidiaSourcesContent())
}

// nvidiaSourceEnabled reports whether nvidia-cuda.sources has an enabled stanza.
func nvidiaSourceEnabled(s sys.System) (exists, enabled bool) {
	b, err := s.ReadFile(nvidiaSources)
	if err != nil {
		return false, false
	}
	for _, st := range apt.ParseDeb822(string(b)) {
		if st.Enabled() {
			return true, true
		}
	}
	return true, false
}

func (s Step) checkNVIDIA(ctx context.Context, env *step.Env) (string, bool) {
	if !apt.Installed(ctx, env.Sys, headers) {
		return headers + " isn't installed", false
	}
	pol := driverPolicy(ctx, env.Sys)
	if pol.installed == "" {
		return "nvidia-driver isn't installed", false
	}
	switch nvidiaSource(env) {
	case srcLatest:
		if !installedKeyOK(env.Sys) {
			return "NVIDIA's repository key " + nvidiaKeyring + " is missing or different", false
		}
		if !sourcesOK(env.Sys) {
			return nvidiaSources + " is missing or different", false
		}
		if !pol.fromNVIDIA {
			return "nvidia-driver " + pol.installed + " isn't from NVIDIA's repository", false
		}
		if !apt.Installed(ctx, env.Sys, "nvidia-kernel-open-dkms") {
			return "nvidia-kernel-open-dkms isn't installed", false
		}
	case srcStock:
		if _, en := nvidiaSourceEnabled(env.Sys); en {
			return nvidiaSources + " is still enabled", false
		}
		if !pol.fromTrixie {
			return "nvidia-driver " + pol.installed + " isn't trixie's", false
		}
	}
	if ok, _ := dkmsBuilt(ctx, env.Sys, env.Facts.Kernel); !ok {
		return "the NVIDIA DKMS module isn't built for kernel " + env.Facts.Kernel, false
	}
	if secureBoot(env) && testMOK(ctx, env.Sys) != mokEnrolledOrPending {
		return "Secure Boot is on, but dkms's signing key isn't enrolled or queued for enrollment", false
	}
	return "NVIDIA " + pol.installed, true
}

func (s Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	if ok, missing := apt.AllInstalled(ctx, env.Sys, wanted(ctx, env)); !ok {
		return step.Todo("not installed yet: %s", strings.Join(missing, ", ")), nil
	}
	if env.Opts.Bool("dvd") && !dvdDone(ctx, env.Sys) && !s.run.getDVDFailed() {
		return step.Todo("libdvdcss2 hasn't been built by libdvd-pkg yet"), nil
	}
	detail := "codecs and video acceleration installed"
	if nvidiaOn(env) && !s.run.get() {
		d, ok := s.checkNVIDIA(ctx, env)
		if !ok {
			return step.Todo("%s", d), nil
		}
		detail += "; " + d
	}
	return step.Done(detail), nil
}

func (s Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	s.run.set(false)
	s.run.setDVDFailed(false)
	var acts []action.Action

	if pkgs := wanted(ctx, env); len(pkgs) > 0 {
		if missing := missingOf(ctx, env.Sys, pkgs); len(missing) > 0 {
			acts = append(acts, action.AptInstall{Pkgs: missing})
		}
		if env.Opts.Bool("codecs") && !apt.Installed(ctx, env.Sys, "libavcodec-extra") {
			env.Notes.Warn("Installing libavcodec-extra replaces libavcodec61 with libavcodec-extra61 (expected: it provides libavcodec61, so nothing breaks).")
		}
	}
	if env.Opts.Bool("vaapi") && env.Facts.HasGPUVendor("8086") {
		env.Notes.Add("Intel VA-API uses intel-media-va-driver, which covers Intel Gen8 (Broadwell) and newer graphics; older Intel graphics aren't covered.")
	}

	// NVIDIA before DVD: a failed libdvdcss2 build must not keep the driver out.
	if len(env.Facts.NVIDIA()) > 0 && !nvidiaOn(env) {
		env.Notes.Add("NVIDIA driver not installed (unticked): Debian's open nouveau driver stays in use.")
	}
	if nvidiaOn(env) {
		nv, err := s.planNVIDIA(ctx, env)
		if err != nil {
			return nil, err
		}
		acts = append(acts, nv...)
	}

	if env.Opts.Bool("dvd") && !dvdDone(ctx, env.Sys) {
		acts = append(acts, s.dvdAction(env))
	}
	return acts, nil
}

// dvdAction installs libdvd-pkg and builds libdvdcss2. The build downloads
// from videolan.org and compiles, so it can fail for reasons outside kiln; a
// failure becomes a summary note and the run continues.
func (s Step) dvdAction(env *step.Env) action.Action {
	install := action.AptInstall{Pkgs: []string{"libdvd-pkg"}, Debconf: dvdDebconf}
	build := action.Run{Desc: "build and install libdvdcss2 from videolan.org's source (dpkg-reconfigure -f noninteractive libdvd-pkg)", K: action.Packages,
		Cmd: sys.Cmd{Name: "dpkg-reconfigure", Args: []string{"-f", "noninteractive", "libdvd-pkg"}, Env: apt.Env(), Output: env.Output}}
	return action.Custom{
		Desc: install.Describe() + ", then " + build.Describe() + "; if that fails, DVD playback is left out and the summary says why",
		K:    action.Packages,
		Fn: func(ctx context.Context, sy sys.System) error {
			err := action.RunAll(ctx, sy, []action.Action{install, build})
			if err == nil {
				return nil
			}
			if sy.DryRun() {
				return err
			}
			s.run.setDVDFailed(true)
			env.Notes.Add("DVD playback isn't set up: building libdvdcss2 with libdvd-pkg failed (%v). Everything else was installed. Try `sudo dpkg-reconfigure libdvd-pkg` later, or run kiln again.", err)
			return nil
		},
	}
}

func missingOf(ctx context.Context, s sys.System, pkgs []string) []string {
	_, missing := apt.AllInstalled(ctx, s, pkgs)
	return missing
}

// guard runs a after the Secure Boot preparation only if NVIDIA wasn't skipped.
func (s Step) guard(desc string, k action.Kind, fn func(ctx context.Context, sy sys.System) error) action.Action {
	return action.Custom{Desc: desc, K: k, Fn: func(ctx context.Context, sy sys.System) error {
		if s.run.get() {
			return nil
		}
		return fn(ctx, sy)
	}}
}

func (s Step) planNVIDIA(ctx context.Context, env *step.Env) ([]action.Action, error) {
	src := nvidiaSource(env)
	c := choicesFor(env.Facts)
	if !c.latestOK && !c.stockOK {
		env.Notes.Add("NVIDIA driver skipped: this machine has GPUs (%s) that no single driver source supports. Debian's nouveau driver stays in use.", strings.Join(c.gpus, ", "))
		return []action.Action{action.Custom{Desc: "skip the NVIDIA driver (no single source supports these GPUs)", Fn: func(_ context.Context, sy sys.System) error {
			if !sy.DryRun() {
				s.run.set(true)
			}
			return nil
		}}}, nil
	}
	if (src == srcLatest && !c.latestOK) || (src == srcStock && !c.stockOK) {
		return nil, fmt.Errorf("the NVIDIA driver source %q doesn't support this GPU (%s); pick the other source in the multimedia options", src, strings.Join(c.gpus, ", "))
	}
	if _, ok := s.checkNVIDIA(ctx, env); ok {
		return nil, nil // the chosen driver is already in effect
	}

	var acts []action.Action
	// 1. Secure Boot: prepare signing before any driver is installed (both sources).
	if secureBoot(env) && testMOK(ctx, env.Sys) != mokEnrolledOrPending {
		acts = append(acts, action.Custom{
			Desc: "Secure Boot is on: install mokutil openssl dkms, run `dkms generate_mok`, and if its key isn't enrolled or queued, queue it with a one-time password (mokutil --generate-hash, password on stdin; mokutil --import " + mokPub + " --hash-file " + mokHash + ", then delete the hash file). If this fails, the NVIDIA driver is skipped",
			K:    action.Packages,
			Fn: func(ctx context.Context, sy sys.System) error {
				otp, err := prepareMOK(ctx, sy, env)
				if err != nil {
					if sy.DryRun() {
						return err
					}
					s.run.set(true)
					env.Notes.Add("NVIDIA driver skipped: Secure Boot is on and kiln couldn't prepare module signing (%v). Debian's nouveau driver stays in use; kiln never turns Secure Boot off. Run kiln again after fixing it.", err)
					return nil
				}
				if otp != "" {
					env.Notes.Add("Secure Boot: the NVIDIA module signing key is queued for enrollment. At next boot choose *Enroll MOK* → *Continue* → enter the one-time password shown on the summary screen (also: sudo kiln secrets show mok).")
					env.Notes.Reboot("enroll the NVIDIA module signing key (MOK) at the boot screen")
				}
				return nil
			},
		})
	}
	// 2. Kernel headers from the kernel metapackage's suite.
	hs := headersSuite(env)
	from := "trixie"
	if hs != "" {
		from = hs
	}
	acts = append(acts, s.guard("install "+headers+" (from "+from+", matching the kernel metapackage)", action.Packages,
		func(ctx context.Context, sy sys.System) error {
			return apt.Install(ctx, sy, []string{headers}, apt.InstallOpts{Suite: hs})
		}))

	pol := driverPolicy(ctx, env.Sys)
	switch src {
	case srcLatest:
		if !installedKeyOK(env.Sys) {
			acts = append(acts, s.guard(fmt.Sprintf("download %s, check its sha256 is %s, extract %s with dpkg-deb -x (the package isn't installed), check the key's fingerprint is %s, and install it as %s (0644)",
				cudaKeyringURL, cudaKeyringSHA256, cudaKeyInDeb, cudaFingerprint, nvidiaKeyring), action.Files, installNVIDIAKey))
		}
		if !sourcesOK(env.Sys) {
			content := nvidiaSourcesContent()
			acts = append(acts, s.guard("write "+nvidiaSources+" (deb822, Suites: /, Signed-By: "+nvidiaKeyring+")", action.Files,
				func(_ context.Context, sy sys.System) error {
					_, err := sy.WriteFile(nvidiaSources, content, sys.WriteOpts{Mode: 0o644})
					return err
				}))
		}
		acts = append(acts,
			s.guard("refresh package lists (apt-get update)", action.Packages, apt.Update),
			s.guard("install nvidia-driver nvidia-kernel-open-dkms from NVIDIA's repository (Origin: NVIDIA, newest branch, no pinning; desktop-only set, no CUDA)", action.Packages,
				func(ctx context.Context, sy sys.System) error {
					if err := apt.Install(ctx, sy, []string{"nvidia-driver", "nvidia-kernel-open-dkms"}, apt.InstallOpts{}); err != nil {
						return err
					}
					env.Notes.Reboot("load the NVIDIA driver")
					return nil
				}),
		)
	case srcStock:
		if exists, en := nvidiaSourceEnabled(env.Sys); exists && en {
			acts = append(acts, s.guard("set `Enabled: no` in "+nvidiaSources+" so apt doesn't pick NVIDIA's newer driver", action.Files, disableNVIDIASource),
				s.guard("refresh package lists (apt-get update)", action.Packages, apt.Update))
		}
		if pol.installed != "" && !pol.fromTrixie {
			ver := pol.trixieVersion
			if ver == "" {
				ver = "<trixie's 550 version>"
			}
			msg := fmt.Sprintf("nvidia-driver %s from NVIDIA's repository is installed, and going back to Debian's 550 is a downgrade kiln doesn't automate. To switch: keep %s disabled (kiln did that), then run `apt install nvidia-driver=%s` together with the matching versions of its NVIDIA dependencies, and reboot.", pol.installed, nvidiaSources, ver)
			acts = append(acts, action.Custom{Desc: "leave nvidia-driver " + pol.installed + " from NVIDIA installed (kiln doesn't downgrade it; the summary says how)", Fn: func(_ context.Context, sy sys.System) error {
				if !sy.DryRun() {
					s.run.set(true)
				}
				env.Notes.Add("%s", msg)
				return nil
			}})
			return acts, nil
		}
		acts = append(acts, s.guard("install nvidia-driver from trixie (550, non-free, DKMS)", action.Packages,
			func(ctx context.Context, sy sys.System) error {
				if err := apt.Install(ctx, sy, []string{"nvidia-driver"}, apt.InstallOpts{}); err != nil {
					return err
				}
				env.Notes.Reboot("load the NVIDIA driver")
				return nil
			}))
	}
	return acts, nil
}

func disableNVIDIASource(_ context.Context, s sys.System) error {
	b, err := s.ReadFile(nvidiaSources)
	if err != nil {
		return nil // nothing to disable
	}
	sts := apt.ParseDeb822(string(b))
	for i := range sts {
		sts[i].Set("Enabled", "no")
	}
	_, err = s.WriteFile(nvidiaSources, []byte(apt.FormatDeb822(sys.ManagedHeader("#")+"# Disabled: the multimedia step uses Debian's driver (Debian stock)\n", sts)), sys.WriteOpts{Mode: 0o644})
	return err
}
