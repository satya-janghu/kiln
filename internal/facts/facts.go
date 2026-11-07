// Package facts detects the machine: hardware, installer choices, network and
// the target user (rule 6). Gathering is read-only.
package facts

import (
	"net/netip"
	"strings"

	"github.com/satya-janghu/kiln/internal/sys"
)

// OSRelease is /etc/os-release.
type OSRelease struct {
	ID, VersionID, Codename, Pretty string
}

// DMI identifies the machine.
type DMI struct {
	Vendor, Product, Version, Family, Board string
	ChassisType                             int
}

// CPU describes the processor.
type CPU struct {
	Vendor string // "AMD", "Intel" or the raw vendor_id
	Model  string
	Cores  int
}

// PCIDevice is one PCI function.
type PCIDevice struct {
	Slot   string `json:"slot"`
	Class  string `json:"class"`  // e.g. "0300"
	Vendor string `json:"vendor"` // e.g. "1002"
	Device string `json:"device"`
	Driver string `json:"driver,omitempty"`
}

// NIC is a network interface.
type NIC struct {
	Name      string         `json:"name"`
	Physical  bool           `json:"physical"`
	Wireless  bool           `json:"wireless"`
	MAC       string         `json:"mac"`
	Driver    string         `json:"driver,omitempty"`
	Vendor    string         `json:"vendor,omitempty"` // PCI/USB vendor id
	Up        bool           `json:"up"`
	Addrs     []netip.Prefix `json:"addrs"`
	ManagedBy string         `json:"managed_by"` // ifupdown, NetworkManager, systemd-networkd, none
}

// Battery is a system battery.
type Battery struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Facts is everything kiln detected.
type Facts struct {
	OS            OSRelease `json:"os"`
	Arch          string    `json:"arch"`
	Kernel        string    `json:"kernel"`
	Virt          string    `json:"virt"` // "none" or the hypervisor
	Container     bool      `json:"container"`
	Root          bool      `json:"root"`
	ViaPlainSu    bool      `json:"via_plain_su"`
	SSHSession    bool      `json:"ssh_session"`
	User          *sys.User `json:"user,omitempty"`
	UserProblem   string    `json:"user_problem,omitempty"`
	Hostname      string    `json:"hostname"`
	Timezone      string    `json:"timezone"`
	DefaultTarget string    `json:"default_target"`

	DMI      DMI         `json:"dmi"`
	Laptop   bool        `json:"laptop"`
	CPU      CPU         `json:"cpu"`
	RAMBytes uint64      `json:"ram_bytes"`
	GPUs     []PCIDevice `json:"gpus"`
	WiFi     []NIC       `json:"-"`

	EFI        bool   `json:"efi"`
	SecureBoot string `json:"secure_boot"` // enabled, disabled, n/a

	Batteries      []Battery `json:"batteries"`
	OnAC           bool      `json:"on_ac"`
	HasAC          bool      `json:"has_ac"`
	BatteryPercent int       `json:"battery_percent"` // -1 when unknown

	PlatformProfile        string   `json:"platform_profile"`
	PlatformProfileChoices []string `json:"platform_profile_choices"`
	AMDPState              string   `json:"amd_pstate"`
	ThinkpadACPI           bool     `json:"thinkpad_acpi"`
	ThinkpadFan            bool     `json:"thinkpad_fan"`
	HPWMI                  bool     `json:"hp_wmi"`

	NICs          []NIC          `json:"nics"`
	DefaultIface  string         `json:"default_iface"`
	Gateway       string         `json:"gateway"`
	LANSubnets    []netip.Prefix `json:"lan_subnets"`
	IfupdownIface []string       `json:"ifupdown_ifaces"`
	NMRunning     bool           `json:"nm_running"`

	Packages     map[string]string `json:"-"` // installed package -> version
	GNOME        bool              `json:"gnome"`
	SSHServer    bool              `json:"ssh_server"`
	StandardTask bool              `json:"standard_task"`
	Tasks        []string          `json:"tasks"`
	Blends       []string          `json:"blends"`
}

// Installed reports whether a package is installed.
func (f *Facts) Installed(pkg string) bool { _, ok := f.Packages[pkg]; return ok }

// HasGPUVendor reports a GPU from vendor ("1002" AMD, "10de" NVIDIA, "8086" Intel).
func (f *Facts) HasGPUVendor(v string) bool {
	for _, g := range f.GPUs {
		if g.Vendor == v {
			return true
		}
	}
	return false
}

// NVIDIA returns NVIDIA GPUs.
func (f *Facts) NVIDIA() []PCIDevice {
	var out []PCIDevice
	for _, g := range f.GPUs {
		if g.Vendor == "10de" {
			out = append(out, g)
		}
	}
	return out
}

// IsLenovoThinkPad reports a ThinkPad.
func (f *Facts) IsThinkPad() bool {
	return f.DMI.Vendor == "LENOVO" && (contains(f.DMI.Version, "ThinkPad") || contains(f.DMI.Family, "ThinkPad"))
}

// placeholderDMI are firmware filler strings that carry no information.
var placeholderDMI = []string{"type1productconfigid", "to be filled by o.e.m.", "default string", "system product name", "system version", "not applicable", "none", "0123456789"}

func isPlaceholder(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, p := range placeholderDMI {
		if v == p {
			return true
		}
	}
	return false
}

// MachineName is a readable machine name: vendor (unless the product already
// starts with it), product, and the version when it's informative.
func (f *Facts) MachineName() string {
	parts := []string{}
	product := strings.TrimSpace(f.DMI.Product)
	if v := strings.TrimSpace(f.DMI.Vendor); v != "" && !isPlaceholder(v) && !strings.HasPrefix(strings.ToLower(product), strings.ToLower(v)) {
		parts = append(parts, v)
	}
	if product != "" && !isPlaceholder(product) {
		parts = append(parts, product)
	}
	if v := strings.TrimSpace(f.DMI.Version); v != "" && !isPlaceholder(v) && !strings.Contains(product, v) {
		parts = append(parts, v)
	}
	if len(parts) == 0 {
		return "this machine"
	}
	return strings.Join(parts, " ")
}

// LANSummary returns LAN subnets as strings.
func (f *Facts) LANStrings() []string {
	out := make([]string, 0, len(f.LANSubnets))
	for _, p := range f.LANSubnets {
		out = append(out, p.String())
	}
	return out
}

// PrimaryIPv4 is the first private IPv4 address on a physical NIC (for notes).
func (f *Facts) PrimaryIPv4() string {
	for _, n := range f.NICs {
		if n.Name != f.DefaultIface && f.DefaultIface != "" {
			continue
		}
		for _, a := range n.Addrs {
			if a.Addr().Is4() {
				return a.Addr().String()
			}
		}
	}
	for _, n := range f.NICs {
		if !n.Physical {
			continue
		}
		for _, a := range n.Addrs {
			if a.Addr().Is4() {
				return a.Addr().String()
			}
		}
	}
	return ""
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	n, m := len(s), len(sub)
	for i := 0; i+m <= n; i++ {
		match := true
		for j := 0; j < m; j++ {
			a, b := s[i+j], sub[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
