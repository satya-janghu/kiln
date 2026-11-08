// Package detect finds out what kind of machine this is. It is the only place
// that reads sysfs, procfs and dpkg state for detection, and it changes nothing.
package detect

import (
	"bufio"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// PCI is one PCI device.
type PCI struct {
	Class, Vendor, Device, Driver string
}

// Facts is everything kiln detected.
type Facts struct {
	OSID, OSVersion, Arch string
	Root                  bool
	PlainSu               bool // root's PATH lacked /usr/sbin (plain `su`)

	User        *run.User
	UserProblem string
	Hostname    string

	Vendor, Product, Version, Family string // DMI

	CPUVendor string // "AMD", "Intel" or the raw vendor_id
	GPUs      []PCI
	WiFiPCI   []PCI    // PCI class 0280, with or without a network interface
	NetDriver []string // drivers of physical network interfaces

	SecureBoot bool
	Battery    string // sysfs path of the first system battery, or ""

	GNOME bool

	LANSubnets []string
	IP         string // the main private IPv4 address, for notes
}

// Gather detects the machine. It never fails; missing data stays empty.
func Gather() *Facts {
	f := &Facts{Arch: runtime.GOARCH}
	for _, l := range strings.Split(run.Read("/etc/os-release"), "\n") {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "ID":
			f.OSID = v
		case "VERSION_ID":
			f.OSVersion = v
		}
	}
	if out, err := exec.CommandContext(context.Background(), "dpkg", "--print-architecture").Output(); err == nil {
		f.Arch = strings.TrimSpace(string(out))
	}
	f.Root = os.Geteuid() == 0
	f.User, f.UserProblem = targetUser()
	f.Hostname = trim(run.Read("/etc/hostname"))

	f.Vendor = trim(run.Read("/sys/class/dmi/id/sys_vendor"))
	f.Product = trim(run.Read("/sys/class/dmi/id/product_name"))
	f.Version = trim(run.Read("/sys/class/dmi/id/product_version"))
	f.Family = trim(run.Read("/sys/class/dmi/id/product_family"))

	f.CPUVendor = cpuVendor()
	f.GPUs = pciDevices("0300", "0302", "0380")
	f.WiFiPCI = pciDevices("0280")
	f.NetDriver = netDrivers()

	if b, err := os.ReadFile("/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"); err == nil && len(b) >= 5 && b[4] == 1 {
		f.SecureBoot = true
	}
	f.Battery = battery()
	f.GNOME = installed("gnome-shell") && installed("gdm3")
	f.LANSubnets, f.IP = network()
	return f
}

// NVIDIA returns the NVIDIA GPUs.
func (f *Facts) NVIDIA() []PCI { return f.gpus("10de") }

// HasGPU reports a GPU from a PCI vendor ("1002" AMD, "8086" Intel, "10de" NVIDIA).
func (f *Facts) HasGPU(vendor string) bool { return len(f.gpus(vendor)) > 0 }

func (f *Facts) gpus(vendor string) []PCI {
	var out []PCI
	for _, g := range f.GPUs {
		if g.Vendor == vendor {
			out = append(out, g)
		}
	}
	return out
}

// Machine is a readable machine name for notes.
func (f *Facts) Machine() string {
	name := f.Product
	if v := f.Vendor; v != "" && !strings.HasPrefix(strings.ToLower(f.Product), strings.ToLower(v)) {
		name = v + " " + f.Product
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "this machine"
	}
	return name
}

// Desktop is "GNOME" or "no desktop".
func (f *Facts) Desktop() string {
	if f.GNOME {
		return "GNOME"
	}
	return "no desktop"
}

func trim(v string) string { return strings.TrimSpace(v) }

func installed(pkg string) bool {
	out, err := exec.CommandContext(context.Background(), "dpkg-query", "-W", "-f=${db:Status-Abbrev}", pkg).Output()
	return err == nil && strings.HasPrefix(string(out), "ii")
}

func targetUser() (*run.User, string) {
	lookup := func(name string) *run.User {
		u, err := user.Lookup(name)
		if err != nil {
			return nil
		}
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		return &run.User{Name: u.Username, UID: uid, GID: gid, Home: u.HomeDir}
	}
	if n := os.Getenv("SUDO_USER"); n != "" && n != "root" {
		if u := lookup(n); u != nil {
			return u, ""
		}
	}
	if id := os.Getenv("PKEXEC_UID"); id != "" {
		if u, err := user.LookupId(id); err == nil && u.Username != "root" {
			if lu := lookup(u.Username); lu != nil {
				return lu, ""
			}
		}
	}
	type cand struct {
		name string
		uid  int
	}
	var cands []cand
	for _, l := range strings.Split(run.Read("/etc/passwd"), "\n") {
		p := strings.Split(l, ":")
		if len(p) < 7 {
			continue
		}
		uid, err := strconv.Atoi(p[2])
		if err != nil || uid < 1000 || uid >= 60000 {
			continue
		}
		if strings.HasSuffix(p[6], "nologin") || strings.HasSuffix(p[6], "/false") {
			continue
		}
		cands = append(cands, cand{p[0], uid})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].uid < cands[j].uid })
	if len(cands) > 0 {
		if u := lookup(cands[0].name); u != nil {
			return u, ""
		}
	}
	return nil, "no regular user account (UID 1000 or higher) was found; create one with `adduser <name>` and run kiln again"
}

func cpuVendor() string {
	sc := bufio.NewScanner(strings.NewReader(run.Read("/proc/cpuinfo")))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok || strings.TrimSpace(k) != "vendor_id" {
			continue
		}
		switch v = strings.TrimSpace(v); v {
		case "AuthenticAMD":
			return "AMD"
		case "GenuineIntel":
			return "Intel"
		default:
			return v
		}
	}
	return ""
}

func pciDevices(classes ...string) []PCI {
	var out []PCI
	devs, _ := filepath.Glob("/sys/bus/pci/devices/*")
	for _, d := range devs {
		class := strings.TrimPrefix(trim(run.Read(d+"/class")), "0x")
		if len(class) < 4 || !slices.Contains(classes, class[:4]) {
			continue
		}
		p := PCI{
			Class:  class[:4],
			Vendor: strings.TrimPrefix(trim(run.Read(d+"/vendor")), "0x"),
			Device: strings.TrimPrefix(trim(run.Read(d+"/device")), "0x"),
		}
		if l, err := os.Readlink(d + "/driver"); err == nil {
			p.Driver = filepath.Base(l)
		}
		out = append(out, p)
	}
	return out
}

// virtual interface names are never LAN or physical.
var virtual = []string{"lo", "docker", "br-", "veth", "virbr", "tun", "tap", "wg", "tailscale"}

func isVirtual(n string) bool {
	for _, p := range virtual {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func netDrivers() []string {
	var out []string
	ifs, _ := filepath.Glob("/sys/class/net/*")
	for _, i := range ifs {
		if isVirtual(filepath.Base(i)) {
			continue
		}
		if l, err := os.Readlink(i + "/device/driver"); err == nil {
			d := filepath.Base(l)
			if !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
	}
	return out
}

func battery() string {
	supplies, _ := filepath.Glob("/sys/class/power_supply/*")
	for _, p := range supplies {
		if trim(run.Read(p+"/type")) != "Battery" {
			continue
		}
		if strings.EqualFold(trim(run.Read(p+"/scope")), "Device") {
			continue // a mouse or keyboard battery
		}
		return p
	}
	return ""
}

// network returns the private subnets on physical interfaces and the main
// private IPv4 address.
func network() ([]string, string) {
	type addrInfo struct {
		Family    string `json:"family"`
		Local     string `json:"local"`
		PrefixLen int    `json:"prefixlen"`
		Scope     string `json:"scope"`
	}
	type ipAddr struct {
		Ifname   string     `json:"ifname"`
		AddrInfo []addrInfo `json:"addr_info"`
	}
	var addrs []ipAddr
	if out, err := exec.CommandContext(context.Background(), "ip", "-j", "addr").Output(); err == nil {
		_ = json.Unmarshal(out, &addrs)
	}
	ula := netip.MustParsePrefix("fc00::/7")
	seen := map[string]bool{}
	var subnets []string
	ip := ""
	for _, a := range addrs {
		if isVirtual(a.Ifname) || !run.Exists("/sys/class/net/"+a.Ifname+"/device") {
			continue
		}
		for _, ai := range a.AddrInfo {
			if ai.Scope != "global" {
				continue
			}
			p, err := netip.ParsePrefix(ai.Local + "/" + strconv.Itoa(ai.PrefixLen))
			if err != nil {
				continue
			}
			private := (p.Addr().Is4() && p.Addr().IsPrivate()) || ula.Contains(p.Addr())
			if !private {
				continue
			}
			if ip == "" && p.Addr().Is4() {
				ip = p.Addr().String()
			}
			if p.Bits() == p.Addr().BitLen() {
				continue // a single address, not a subnet
			}
			if m := p.Masked().String(); !seen[m] {
				seen[m] = true
				subnets = append(subnets, m)
			}
		}
	}
	return subnets, ip
}
