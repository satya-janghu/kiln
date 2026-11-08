// Package steps holds every item kiln does, in catalog order
// (docs/steps-catalog.md is the behaviour contract), and runs them.
package steps

import (
	"fmt"
	"runtime/debug"

	"github.com/satya-janghu/kiln/internal/detect"
	"github.com/satya-janghu/kiln/internal/run"
)

// Ctx is what a step gets: the run, the detected facts, the target user and
// which checkboxes are ticked.
type Ctx struct {
	*run.S
	F      *detect.Facts
	U      *run.User
	picked map[string]bool
}

// NewCtx builds a step context.
func NewCtx(s *run.S, f *detect.Facts, picked map[string]bool) *Ctx {
	return &Ctx{S: s, F: f, U: f.User, picked: picked}
}

// Picked reports whether a checkbox is ticked.
func (c *Ctx) Picked(id string) bool { return c.picked[id] }

// Step is one catalog item.
type Step struct {
	ID     string
	Title  string
	Desc   string // one line under the title on the checklist
	Always bool   // done on every run, no checkbox
	Show   func(*detect.Facts) bool
	Needs  []string // these must have succeeded, or this step is skipped
	Do     func(*Ctx) error
}

// Shown reports whether the step fits this machine.
func (s Step) Shown(f *detect.Facts) bool { return s.Show == nil || s.Show(f) }

func hasNVIDIA(f *detect.Facts) bool  { return len(f.NVIDIA()) > 0 }
func hasBattery(f *detect.Facts) bool { return f.Battery != "" }

var base = []string{"sources"}

// List is every item, in catalog order.
func List() []Step {
	return []Step{
		{ID: "prepare", Title: "Prepare", Always: true, Do: doPrepare,
			Desc: "Wait for Debian's first-boot updates, finish any half-done install"},
		{ID: "sources", Title: "Package sources", Always: true, Needs: []string{"prepare"}, Do: doSources,
			Desc: "Debian sources with non-free, security, updates and backports"},
		{ID: "upgrade", Title: "Full upgrade", Always: true, Needs: base, Do: doUpgrade,
			Desc: "Bring every installed package up to date"},
		{ID: "firmware", Title: "Firmware and microcode", Always: true, Needs: base, Do: doFirmware,
			Desc: "Firmware for the detected hardware and CPU microcode"},

		{ID: "sudo", Title: "Passwordless sudo", Needs: base, Do: doSudo,
			Desc: "Your user can use sudo without a password, right away"},
		{ID: "system", Title: "System basics", Needs: base, Do: doSystem,
			Desc: "zram swap as big as RAM, log size cap, file-watch limits, time sync"},
		{ID: "ssh", Title: "SSH", Needs: base, Do: doSSH,
			Desc: "SSH server with password logins for you and root"},
		{ID: "firewall", Title: "Firewall and fail2ban", Needs: base, Do: doFirewall,
			Desc: "UFW allowing SSH and Cockpit; fail2ban bans repeated SSH login failures"},
		{ID: "autoupdates", Title: "Automatic updates", Needs: base, Do: doAutoUpdates,
			Desc: "Every package updated every 12 hours, never an automatic reboot"},
		{ID: "alwayson", Title: "Always on", Do: doAlwaysOn,
			Desc: "Never sleeps or suspends; closing the lid does nothing"},
		{ID: "power", Title: "Balanced power profile", Needs: base, Do: doPower,
			Desc: "Debian's balanced profile (tuned, or power-profiles-daemon with GNOME)"},
		{ID: "docker", Title: "Docker", Needs: base, Do: doDocker,
			Desc: "Docker with Compose, Portainer on port 9443, GPU support for NVIDIA cards"},
		{ID: "cockpit", Title: "Cockpit", Needs: base, Do: doCockpit,
			Desc: "Web admin page on port 9090"},
		{ID: "devtools", Title: "Command-line and developer tools", Needs: base, Do: doDevtools,
			Desc: "gh, git, build tools, editors, tmux, htop, ripgrep, fzf and more"},
		{ID: "node", Title: "Node.js", Needs: []string{"devtools"}, Do: doNode,
			Desc: "nvm and Node 26 for your user"},
		{ID: "python", Title: "Python", Needs: []string{"devtools"}, Do: doPython,
			Desc: "uv, an automation venv, Playwright Chromium, ruff, pre-commit, httpie"},
		{ID: "go", Title: "Go", Needs: []string{"devtools"}, Do: doGo,
			Desc: "Latest Go from go.dev with gopls, dlv, staticcheck, govulncheck"},
		{ID: "codecs", Title: "Codecs and video acceleration", Needs: base, Do: doCodecs,
			Desc: "Every non-free codec and GPU video decoding"},
		{ID: "flathub", Title: "Flathub", Needs: base, Do: doFlathub,
			Desc: "Flatpak apps from Flathub"},
		{ID: "chrome", Title: "Google Chrome", Needs: base, Do: doChrome,
			Desc: "Chrome from Google's repository"},
		{ID: "nvidia", Title: "NVIDIA driver", Needs: base, Show: hasNVIDIA, Do: doNVIDIA,
			Desc: "The right driver for this card, picked automatically"},
		{ID: "fwupd", Title: "Firmware updates (fwupd)", Needs: base, Do: doFwupd,
			Desc: "BIOS and device firmware updates from LVFS, applied once"},
		{ID: "battery", Title: "Battery limit (70-80%)", Needs: base, Show: hasBattery, Do: doBattery,
			Desc: "Keeps the battery between 70% and 80% to make it last"},
	}
}

// Status of a step after the run.
type Status int

// Step states.
const (
	Pending Status = iota
	Running
	Done
	Failed
	Skipped
)

// Result is what happened to one step.
type Result struct {
	ID     string
	Title  string
	Status Status
	Detail string // the error or the reason it was skipped
}

// Execute runs the always-done items and the ticked ones in order. A failed
// step never stops the run; the steps that need it are skipped. stop is
// checked between steps (Ctrl+C means "stop after the current step").
func Execute(c *Ctx, list []Step, stop func() bool, ev func(i int, r Result)) []Result {
	res := make([]Result, len(list))
	byID := map[string]int{}
	for i, st := range list {
		byID[st.ID] = i
		res[i] = Result{ID: st.ID, Title: st.Title}
	}
	defer c.Cleanup()
	for i, st := range list {
		r := &res[i]
		switch {
		case !st.Shown(c.F):
			r.Status, r.Detail = Skipped, "not for this machine"
		case !st.Always && !c.Picked(st.ID):
			r.Status, r.Detail = Skipped, "not selected"
		case stop():
			r.Status, r.Detail = Skipped, "stopped before it started"
		default:
			for _, n := range st.Needs {
				if j, ok := byID[n]; ok && res[j].Status != Done {
					r.Status, r.Detail = Skipped, fmt.Sprintf("needs %q, which didn't complete", res[j].Title)
					break
				}
			}
		}
		if r.Status == Skipped {
			c.Log("== %s: skipped (%s)", st.Title, r.Detail)
			ev(i, *r)
			continue
		}
		r.Status = Running
		ev(i, *r)
		c.Say("== %s", st.Title)
		if err := safeDo(st, c); err != nil {
			r.Status, r.Detail = Failed, err.Error()
			c.Say("%s failed: %v", st.Title, err)
		} else {
			r.Status = Done
		}
		ev(i, *r)
	}
	return res
}

func safeDo(st Step, c *Ctx) (err error) {
	defer func() {
		if p := recover(); p != nil {
			c.Log("panic in %s: %v\n%s", st.ID, p, debug.Stack())
			err = fmt.Errorf("kiln hit an internal error (%v); the details are in %s", p, run.LogPath)
		}
	}()
	return st.Do(c)
}
