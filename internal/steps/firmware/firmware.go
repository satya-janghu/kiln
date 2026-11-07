// Package firmware implements catalog step 5 `firmware-packages` (docs/steps-catalog.md).
package firmware

import (
	"context"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/action"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// ID is the step ID (the kernel step reads this step's options).
const ID = "firmware-packages"

// Step is the `firmware-packages` step.
type Step struct{ step.Base }

// New returns the step.
func New() step.Step { return Step{} }

func (Step) ID() string          { return ID }
func (Step) Title() string       { return "CPU microcode and device firmware" }
func (Step) Category() string    { return "hardware" }
func (Step) DependsOn() []string { return []string{"apt-sources"} }

// MiscFirmware is installed without its Recommends (they pull firmware the machine may not have).
const MiscFirmware = "firmware-misc-nonfree"

const isenkram = "isenkram-cli"

// allFirmware is every package this step can choose (all in non-free-firmware).
var allFirmware = []string{
	"amd64-microcode", "intel-microcode", MiscFirmware,
	"firmware-amd-graphics", "firmware-intel-graphics", "firmware-intel-misc", "firmware-nvidia-graphics",
	"firmware-realtek", "firmware-iwlwifi", "firmware-atheros", "firmware-mediatek", "firmware-brcm80211",
}

func (Step) Packages() []step.PackageRef {
	out := make([]step.PackageRef, 0, len(allFirmware)+1)
	for _, p := range allFirmware {
		out = append(out, step.PackageRef{Name: p, Suite: "trixie", Component: "non-free-firmware", Repo: "debian"})
	}
	return append(out, step.PackageRef{Name: isenkram, Suite: "trixie", Component: "main", Repo: "debian"})
}

func (Step) Options(*facts.Facts) []options.Field {
	return []options.Field{
		{Key: "microcode", Label: "CPU microcode", Kind: options.Bool, Default: true,
			Help: "amd64-microcode or intel-microcode, whichever matches the CPU."},
		{Key: "nonfree_firmware", Label: "Non-free firmware", Kind: options.Bool, Default: true,
			Help: "GPU, Wi-Fi and network firmware matched to the detected hardware."},
	}
}

// Selection is what this machine gets.
type Selection struct {
	Microcode []string // amd64-microcode / intel-microcode
	Firmware  []string // firmware-* packages, MiscFirmware first when selected
	Report    bool     // install isenkram-cli and report missing firmware
}

// Select picks packages from the detected hardware.
func Select(f *facts.Facts, microcode, nonfree bool) Selection {
	var sel Selection
	if microcode {
		switch f.CPU.Vendor {
		case "AMD":
			sel.Microcode = []string{"amd64-microcode"}
		case "Intel":
			sel.Microcode = []string{"intel-microcode"}
		}
	}
	if !nonfree {
		return sel
	}
	sel.Report = true
	seen := map[string]bool{}
	add := func(p ...string) {
		for _, x := range p {
			if !seen[x] {
				seen[x] = true
				sel.Firmware = append(sel.Firmware, x)
			}
		}
	}
	add(MiscFirmware)
	if f.HasGPUVendor("1002") {
		add("firmware-amd-graphics")
	}
	if f.HasGPUVendor("8086") {
		add("firmware-intel-graphics", "firmware-intel-misc")
	}
	if f.HasGPUVendor("10de") {
		add("firmware-nvidia-graphics")
	}
	for _, n := range f.NICs {
		if !n.Physical {
			continue
		}
		if isRealtek(n) {
			add("firmware-realtek")
		}
		if !n.Wireless {
			continue
		}
		switch d := n.Driver; {
		case d == "iwlwifi":
			add("firmware-iwlwifi")
		case strings.HasPrefix(d, "ath"):
			add("firmware-atheros")
		case strings.HasPrefix(d, "mt7"):
			add("firmware-mediatek")
		case d == "brcmfmac":
			add("firmware-brcm80211")
		}
	}
	return sel
}

// isRealtek matches a Realtek PCI vendor ID or a Realtek network driver
// (r8169/r8152 Ethernet, rtw88/rtw89/rtl8xxx Wi-Fi; USB devices have no PCI vendor file).
func isRealtek(n facts.NIC) bool {
	if n.Vendor == "10ec" {
		return true
	}
	for _, p := range []string{"r8169", "r8152", "rtw88", "rtw89", "rtl8"} {
		if strings.HasPrefix(n.Driver, p) {
			return true
		}
	}
	return false
}

// SelectFor applies the step's options from a resolved option set.
func SelectFor(f *facts.Facts, o options.View) Selection {
	return Select(f, o.Bool("microcode"), o.Bool("nonfree_firmware"))
}

func (sel Selection) all() []string {
	out := append(append([]string(nil), sel.Microcode...), sel.Firmware...)
	if sel.Report {
		out = append(out, isenkram)
	}
	return out
}

func (Step) Check(ctx context.Context, env *step.Env) (step.Status, error) {
	sel := SelectFor(env.Facts, env.Opts)
	if ok, missing := apt.AllInstalled(ctx, env.Sys, sel.all()); !ok {
		return step.Todo("not installed: %s", strings.Join(missing, ", ")), nil
	}
	return step.Done("installed: " + strings.Join(sel.all(), ", ")), nil
}

func (Step) Plan(ctx context.Context, env *step.Env) ([]action.Action, error) {
	sel := SelectFor(env.Facts, env.Opts)
	_, missing := apt.AllInstalled(ctx, env.Sys, sel.all())
	miss := map[string]bool{}
	for _, p := range missing {
		miss[p] = true
	}
	var acts []action.Action
	if miss[MiscFirmware] {
		acts = append(acts, action.AptInstall{Pkgs: []string{MiscFirmware}, NoRecommends: true})
	}
	var rest []string
	newFirmware := miss[MiscFirmware]
	for _, p := range missing {
		if p == MiscFirmware {
			continue
		}
		rest = append(rest, p)
		if p != isenkram {
			newFirmware = true
		}
	}
	if len(rest) > 0 {
		acts = append(acts, action.AptInstall{Pkgs: rest})
	}
	if newFirmware {
		// The firmware triggers rebuild only the newest kernel's initramfs.
		acts = append(acts, action.Run{
			Cmd:  sys.Cmd{Name: "update-initramfs", Args: []string{"-u", "-k", "all"}, Timeout: 20 * time.Minute},
			Desc: "rebuild the initramfs of every installed kernel (update-initramfs -u -k all) so each one boots with the new firmware",
		})
	}
	for _, m := range sel.Microcode {
		if miss[m] {
			env.Notes.Reboot("CPU microcode installed (%s)", m)
		}
	}
	if sel.Report {
		acts = append(acts, action.Custom{
			Desc: "list firmware the loaded drivers still ask for with `isenkram-autoinstall-firmware -l` (list only; kiln never lets isenkram install or add sources) and add it to the summary",
			Fn: func(ctx context.Context, s sys.System) error {
				if s.DryRun() {
					return nil
				}
				report(ctx, env, s)
				return nil
			},
		})
	}
	return acts, nil
}

// report runs isenkram in list mode: package names on stdout (`echo
// $binpkgs`), every "info:" line on stderr; it exits 1 only when no loaded
// driver requests firmware, and 0 otherwise, including "No new firmware
// package" (isenkram 0.69+deb13u1 isenkram-autoinstall-firmware; seen on the
// HP test machine). So only stdout decides, whatever the exit code.
func report(ctx context.Context, env *step.Env, s sys.System) {
	r, err := s.Query(ctx, sys.Cmd{Name: "isenkram-autoinstall-firmware", Args: []string{"-l"}, Timeout: 10 * time.Minute})
	pkgs := strings.Fields(r.Stdout)
	if len(pkgs) == 0 {
		if err != nil {
			env.Log.Info("isenkram found no missing firmware", "detail", strings.TrimSpace(r.Stderr))
		}
		return
	}
	env.Notes.Add("Firmware still missing for loaded drivers: %s. Install with `sudo apt install %s` if you need that hardware.",
		strings.Join(pkgs, ", "), strings.Join(pkgs, " "))
}
