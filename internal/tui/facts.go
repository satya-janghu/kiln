package tui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/runner"
)

// Screen 1: welcome and facts.

func (m *model) keyFacts(k string) tea.Cmd {
	if m.scr == scrAllFacts {
		switch k {
		case "esc", "f", "q":
			m.scr = scrFacts
		case "enter":
			return m.toChecklist()
		default:
			m.fact.key(k)
		}
		return nil
	}
	switch k {
	case "enter":
		return m.toChecklist()
	case "q":
		return m.quitRequest()
	case "f":
		m.scr, m.fact = scrAllFacts, scroll{}
	}
	return nil
}

func gpuVendor(id string) string {
	switch id {
	case "1002":
		return "AMD"
	case "10de":
		return "NVIDIA"
	case "8086":
		return "Intel"
	}
	return "vendor " + id
}

func gpuText(g facts.PCIDevice) string {
	s := gpuVendor(g.Vendor) + " " + g.Vendor + ":" + g.Device
	if g.Driver != "" {
		s += " (" + g.Driver + ")"
	}
	return s
}

func nicText(n facts.NIC) string {
	var addrs []string
	for _, p := range n.Addrs {
		addrs = append(addrs, p.String())
	}
	if !n.Up && len(addrs) == 0 {
		return n.Name + " no link"
	}
	s := n.Name
	if len(addrs) > 0 {
		s += " " + strings.Join(addrs, " ")
	}
	if n.ManagedBy != "" && n.ManagedBy != "none" {
		s += " (" + n.ManagedBy + ")"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

type factRow struct {
	label, left, right string
	warn               bool
}

func (m *model) factRows() []factRow {
	f := m.a.Facts
	var rows []factRow
	machine := f.MachineName() // already adds the version when it's informative
	kind := "Desktop"
	if f.Laptop {
		kind = "Laptop"
	}
	if len(f.Batteries) > 0 {
		if f.OnAC {
			kind += ", on AC"
		} else {
			kind += ", on battery"
		}
		if f.BatteryPercent >= 0 {
			kind += fmt.Sprintf(", battery %d%%", f.BatteryPercent)
		}
	}
	rows = append(rows, factRow{label: "Machine", left: machine, right: kind})
	rows = append(rows, factRow{label: "CPU / RAM", left: fmt.Sprintf("%s, %d cores / %.1f GiB", joinNonEmpty(" ", f.CPU.Model), f.CPU.Cores, float64(f.RAMBytes)/(1<<30))})
	var gpus []string
	for _, g := range f.GPUs {
		gpus = append(gpus, gpuText(g))
	}
	if len(gpus) == 0 {
		gpus = []string{"none found"}
	}
	sb := map[string]string{"enabled": "on", "disabled": "off"}[f.SecureBoot]
	if sb == "" {
		sb = f.SecureBoot
	}
	rows = append(rows, factRow{label: "Graphics", left: strings.Join(gpus, ", "), right: "Secure Boot: " + sb})
	blend := "none"
	if len(f.Blends) > 0 {
		blend = strings.Join(f.Blends, ", ")
	}
	rows = append(rows, factRow{label: "Installed", left: fmt.Sprintf("GNOME desktop: %s   SSH server: %s   Blend: %s", yesNo(f.GNOME), yesNo(f.SSHServer), blend)})
	var nics []string
	for _, n := range f.NICs {
		if n.Physical {
			nics = append(nics, nicText(n))
		}
	}
	if len(nics) == 0 {
		nics = []string{"no physical network interface found"}
	}
	for i := 0; i < len(nics); i += 2 {
		label := ""
		if i == 0 {
			label = "Network"
		}
		r := factRow{label: label, left: nics[i], right: ""}
		if i+1 < len(nics) {
			r.right = nics[i+1]
		}
		rows = append(rows, r)
	}
	user := f.UserProblem
	if f.User != nil {
		user = fmt.Sprintf("%s (uid %d)", f.User.Name, f.User.UID)
	}
	how := "Not running as root"
	switch {
	case f.Root && os.Getenv("SUDO_USER") != "":
		how = "Running as root via sudo"
	case f.Root && f.ViaPlainSu:
		how = "Running as root via su"
	case f.Root:
		how = "Running as root"
	}
	rows = append(rows, factRow{label: "User", left: user, right: how})
	if st := m.a.State; st != nil && !st.LastRun.IsZero() {
		res := ""
		if st.Run != nil {
			res = st.Run.Result
		}
		rows = append(rows, factRow{label: "Last run", left: joinNonEmpty(", ", st.LastRun.Local().Format("Jan 2 15:04"), res) +
			" (summary: " + runner.SummaryPath + ")"})
	}
	warns := m.factWarnings()
	if len(warns) == 0 {
		rows = append(rows, factRow{label: "Warnings", left: "none", right: ""})
	}
	for i, w := range warns {
		label := ""
		if i == 0 {
			label = "Warnings"
		}
		rows = append(rows, factRow{label: label, left: w, warn: true})
	}
	return rows
}

func (m *model) factWarnings() []string {
	f := m.a.Facts
	var w []string
	if f.SSHSession {
		w = append(w, "You're connected over SSH. The SSH step may disconnect you; kiln is meant to run at the machine.")
	}
	if !m.gateOK {
		w = append(w, "kiln won't change this machine: "+m.gateWhy+". You can still look at the plan.")
	}
	if f.User == nil && f.UserProblem != "" {
		w = append(w, "No target user: "+f.UserProblem)
	}
	if f.OS.ID != "" && (f.OS.ID != "debian" || f.OS.VersionID != "13") {
		w = append(w, "This is "+joinNonEmpty(" ", f.OS.Pretty)+"; kiln is made for Debian 13 (trixie).")
	}
	if f.Virt != "" && f.Virt != "none" {
		w = append(w, "This is a virtual machine ("+f.Virt+"); kiln is made for bare-metal machines.")
	}
	if m.pre.p.OnBattery && !m.pre.p.LowBattery {
		w = append(w, fmt.Sprintf("On battery (%d%%). Plug in the charger before you start.", f.BatteryPercent))
	}
	return append(w, m.pre.notes...)
}

func (m *model) viewFacts() ([]string, []hint, string) {
	f := m.a.Facts
	cw := m.cw()
	osText := joinNonEmpty(" ", f.OS.Pretty, f.Arch)
	if osText == "" {
		osText = "unknown system"
	}
	head := m.st.white15.Render("kiln: set up this machine")
	head += spaces(cw-width(head)-width(osText)) + osText
	rule := repeat(m.g.thinRule, cw)
	out := []string{head, rule}
	const lw = 12
	valW := cw - lw - 1
	twoCols := cw >= 100
	rightCol := valW * 55 / 100
	for _, r := range m.factRows() {
		label := m.fit(r.label, lw)
		if r.right == "" {
			for i, l := range wrap(r.left, valW) {
				if i > 0 {
					label = spaces(lw)
				}
				if r.warn {
					l = m.st.yellow11.Render(l)
				}
				out = append(out, label+" "+l)
			}
			continue
		}
		if twoCols && width(r.left) < rightCol-2 {
			out = append(out, label+" "+m.fit(r.left, rightCol)+m.cut(r.right, valW-rightCol))
			continue
		}
		if width(r.left)+3+width(r.right) <= valW {
			out = append(out, label+" "+r.left+spaces(valW-width(r.left)-width(r.right))+r.right)
			continue
		}
		out = append(out, label+" "+m.cut(r.left, valW))
		out = append(out, spaces(lw)+" "+m.cut(r.right, valW))
	}
	out = append(out, rule)
	bh := m.bodyH()
	if len(out) > bh {
		out = out[:bh]
	}
	hints := []hint{h("ENTER", "Continue"), h("F", "All facts"), h("F3", "Quit"), h("?", "Help")}
	return out, hints, "This machine"
}

// allFacts is the long form (like `kiln facts`).
func (m *model) allFacts() []string {
	f := m.a.Facts
	var out []string
	add := func(k, v string) {
		for i, l := range wrap(v, max(10, m.cw()-22)) {
			if i == 0 {
				out = append(out, m.fit(k, 18)+"  "+l)
			} else {
				out = append(out, spaces(20)+l)
			}
		}
	}
	add("System", joinNonEmpty(" ", f.OS.Pretty, f.Arch)+", kernel "+f.Kernel)
	add("Machine", f.MachineName())
	add("Family / board", joinNonEmpty(" / ", f.DMI.Family, f.DMI.Board))
	add("Chassis type", fmt.Sprintf("%d (laptop: %s)", f.DMI.ChassisType, yesNo(f.Laptop)))
	add("CPU", fmt.Sprintf("%s (%s), %d cores", f.CPU.Model, f.CPU.Vendor, f.CPU.Cores))
	add("AMD P-State", f.AMDPState)
	add("RAM", fmt.Sprintf("%.1f GiB", float64(f.RAMBytes)/(1<<30)))
	for _, g := range f.GPUs {
		add("GPU", gpuText(g)+" at "+g.Slot)
	}
	add("Firmware", map[bool]string{true: "UEFI", false: "BIOS"}[f.EFI]+", Secure Boot: "+f.SecureBoot)
	add("Virtualization", f.Virt)
	add("Container", yesNo(f.Container))
	add("Hostname", f.Hostname)
	add("Timezone", f.Timezone)
	add("Default target", f.DefaultTarget)
	add("GNOME desktop", yesNo(f.GNOME))
	add("SSH server", yesNo(f.SSHServer))
	add("Standard tools", yesNo(f.StandardTask))
	add("Tasks", strings.Join(f.Tasks, ", "))
	add("Blends", strings.Join(f.Blends, ", "))
	if f.User != nil {
		add("Target user", fmt.Sprintf("%s (uid %d, home %s)", f.User.Name, f.User.UID, f.User.Home))
	} else {
		add("Target user", f.UserProblem)
	}
	add("Running as root", yesNo(f.Root))
	add("Over SSH", yesNo(f.SSHSession))
	for _, n := range f.NICs {
		kind := "virtual"
		if n.Physical {
			kind = "physical"
			if n.Wireless {
				kind = "Wi-Fi"
			}
		}
		add("Network", nicText(n)+", "+kind+", "+joinNonEmpty(" ", n.MAC, n.Driver))
	}
	add("Default route", joinNonEmpty(" via ", f.DefaultIface, f.Gateway))
	add("LAN subnets", strings.Join(f.LANStrings(), ", "))
	add("ifupdown", strings.Join(f.IfupdownIface, ", "))
	add("NetworkManager", map[bool]string{true: "running", false: "not running"}[f.NMRunning])
	if len(f.Batteries) > 0 {
		var names []string
		for _, b := range f.Batteries {
			names = append(names, b.Name)
		}
		add("Batteries", strings.Join(names, ", "))
		add("Battery", fmt.Sprintf("%d%%, on AC: %s", f.BatteryPercent, yesNo(f.OnAC)))
	}
	if f.PlatformProfile != "" {
		add("Platform profile", f.PlatformProfile+" (choices: "+strings.Join(f.PlatformProfileChoices, " ")+")")
	}
	add("ThinkPad ACPI", yesNo(f.ThinkpadACPI)+", fan control: "+yesNo(f.ThinkpadFan))
	add("HP WMI", yesNo(f.HPWMI))
	add("Packages", fmt.Sprintf("%d installed", len(f.Packages)))
	return out
}

func (m *model) viewAllFacts() ([]string, []hint, string) {
	cw, bh := m.cw(), m.bodyH()
	lines := m.allFacts()
	body := m.fact.window(lines, bh-2)
	hints := []hint{h("ESC", "Back"), h("ENTER", "Continue"), h("PGUP/PGDN", "Scroll"), h("F3", "Quit")}
	return m.panel(titled("All facts", m.fact.position(len(lines))), body, cw, bh), hints, "This machine"
}
