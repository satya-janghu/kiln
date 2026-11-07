package facts

import (
	"bufio"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/satya-janghu/kiln/internal/sys"
)

// Gather detects everything. It never fails; missing data stays empty.
func Gather(ctx context.Context, s sys.System) *Facts {
	f := &Facts{Arch: runtime.GOARCH, Packages: map[string]string{}, BatteryPercent: -1}
	f.OS = osRelease(s)
	f.Kernel = trim(read(s, "/proc/sys/kernel/osrelease"))
	if r, err := s.Query(ctx, sys.Cmd{Name: "dpkg", Args: []string{"--print-architecture"}}); err == nil {
		f.Arch = strings.TrimSpace(r.Stdout)
	}
	f.Virt, f.Container = virt(ctx, s)
	f.Root = os.Geteuid() == 0
	// cli.fixRootPath adds the sbin directories and sets KILN_PLAIN_SU.
	f.ViaPlainSu = f.Root && (os.Getenv("KILN_PLAIN_SU") == "1" || !strings.Contains(":"+os.Getenv("PATH")+":", ":/usr/sbin:"))
	f.SSHSession = os.Getenv("SSH_CONNECTION") != ""
	f.User, f.UserProblem = targetUser(s)
	f.Hostname = trim(read(s, "/etc/hostname"))
	if l, err := s.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(l, "zoneinfo/"); i >= 0 {
			f.Timezone = l[i+len("zoneinfo/"):]
		}
	}
	if r, err := s.Query(ctx, sys.Cmd{Name: "systemctl", Args: []string{"get-default"}}); err == nil {
		f.DefaultTarget = strings.TrimSpace(r.Stdout)
	}

	f.DMI = DMI{
		Vendor:  trim(read(s, "/sys/class/dmi/id/sys_vendor")),
		Product: trim(read(s, "/sys/class/dmi/id/product_name")),
		Version: trim(read(s, "/sys/class/dmi/id/product_version")),
		Family:  trim(read(s, "/sys/class/dmi/id/product_family")),
		Board:   trim(read(s, "/sys/class/dmi/id/board_name")),
	}
	f.DMI.ChassisType, _ = strconv.Atoi(trim(read(s, "/sys/class/dmi/id/chassis_type")))
	switch f.DMI.ChassisType {
	case 8, 9, 10, 14, 30, 31, 32:
		f.Laptop = true
	}
	f.CPU = cpu(s)
	f.RAMBytes = memTotal(s)
	f.GPUs = pciGPUs(s)
	f.WiFiPCI = pciWiFi(s)

	if _, err := s.Stat("/sys/firmware/efi"); err == nil {
		f.EFI = true
		f.SecureBoot = "disabled"
		if b, err := s.ReadFile("/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"); err == nil && len(b) >= 5 && b[4] == 1 {
			f.SecureBoot = "enabled"
		}
	} else {
		f.SecureBoot = "n/a"
	}

	power(s, f)
	f.PlatformProfile = trim(read(s, "/sys/firmware/acpi/platform_profile"))
	f.PlatformProfileChoices = strings.Fields(read(s, "/sys/firmware/acpi/platform_profile_choices"))
	f.AMDPState = trim(read(s, "/sys/devices/system/cpu/amd_pstate/status"))
	_, err := s.Stat("/sys/devices/platform/thinkpad_acpi")
	f.ThinkpadACPI = err == nil
	_, err = s.Stat("/proc/acpi/ibm/fan")
	f.ThinkpadFan = err == nil
	_, err = s.Stat("/sys/devices/platform/hp-wmi")
	f.HPWMI = err == nil

	f.Packages = installedPackages(ctx, s)
	f.GNOME = f.Installed("gnome-shell") && f.Installed("gdm3")
	f.SSHServer = f.Installed("openssh-server")
	for p := range f.Packages {
		if strings.HasPrefix(p, "task-") {
			f.Tasks = append(f.Tasks, p)
			if strings.HasSuffix(p, "-blend") {
				f.Blends = append(f.Blends, p)
			}
		}
	}
	sort.Strings(f.Tasks)
	sort.Strings(f.Blends)
	if r, err := s.Query(ctx, sys.Cmd{Name: "tasksel", Args: []string{"--list-tasks"}}); err == nil {
		for _, l := range strings.Split(r.Stdout, "\n") {
			fl := strings.Fields(l)
			if len(fl) >= 2 && fl[0] == "i" && fl[1] == "standard" {
				f.StandardTask = true
			}
		}
	}

	network(ctx, s, f)
	return f
}

func read(s sys.System, p string) string {
	b, err := s.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func trim(v string) string { return strings.TrimSpace(v) }

func osRelease(s sys.System) OSRelease {
	var o OSRelease
	for _, l := range strings.Split(read(s, "/etc/os-release"), "\n") {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "ID":
			o.ID = v
		case "VERSION_ID":
			o.VersionID = v
		case "VERSION_CODENAME":
			o.Codename = v
		case "PRETTY_NAME":
			o.Pretty = v
		}
	}
	return o
}

func virt(ctx context.Context, s sys.System) (string, bool) {
	if _, err := s.Query(ctx, sys.Cmd{Name: "systemd-detect-virt", Args: []string{"--container", "--quiet"}}); err == nil {
		return "container", true
	}
	for _, p := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := s.Stat(p); err == nil {
			return "container", true
		}
	}
	r, err := s.Query(ctx, sys.Cmd{Name: "systemd-detect-virt", Args: []string{"--vm"}})
	if err == nil {
		return strings.TrimSpace(r.Stdout), false
	}
	return "none", false
}

func targetUser(s sys.System) (*sys.User, string) {
	lookup := func(name string) *sys.User {
		u, err := user.Lookup(name)
		if err != nil {
			return nil
		}
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		return &sys.User{Name: u.Username, UID: uid, GID: gid, Home: u.HomeDir}
	}
	if n := os.Getenv("SUDO_USER"); n != "" && n != "root" {
		if u := lookup(n); u != nil {
			return u, ""
		}
	}
	if id := os.Getenv("PKEXEC_UID"); id != "" {
		if u, err := user.LookupId(id); err == nil && u.Username != "root" {
			return lookup(u.Username), ""
		}
	}
	type cand struct {
		name string
		uid  int
	}
	var cands []cand
	for _, l := range strings.Split(read(s, "/etc/passwd"), "\n") {
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

func cpu(s sys.System) CPU {
	var c CPU
	sc := bufio.NewScanner(strings.NewReader(read(s, "/proc/cpuinfo")))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "vendor_id":
			if c.Vendor == "" {
				switch v {
				case "AuthenticAMD":
					c.Vendor = "AMD"
				case "GenuineIntel":
					c.Vendor = "Intel"
				default:
					c.Vendor = v
				}
			}
		case "model name":
			if c.Model == "" {
				c.Model = v
			}
		case "processor":
			c.Cores++
		}
	}
	return c
}

func memTotal(s sys.System) uint64 {
	for _, l := range strings.Split(read(s, "/proc/meminfo"), "\n") {
		if strings.HasPrefix(l, "MemTotal:") {
			f := strings.Fields(l)
			if len(f) >= 2 {
				kb, _ := strconv.ParseUint(f[1], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

func pciGPUs(s sys.System) []PCIDevice { return pciDevices(s, "0300", "0302", "0380") }

// pciWiFi lists PCI network controllers of class 0280 ("Network controller"
// subclass 80, used by Wi-Fi cards; pci.ids). They are listed even when their
// driver couldn't load its firmware and so created no network interface.
func pciWiFi(s sys.System) []PCIDevice { return pciDevices(s, "0280") }

func pciDevices(s sys.System, classes ...string) []PCIDevice {
	var out []PCIDevice
	devs, _ := s.Glob("/sys/bus/pci/devices/*")
	for _, d := range devs {
		class := strings.TrimPrefix(trim(read(s, d+"/class")), "0x")
		if len(class) < 4 {
			continue
		}
		c := class[:4]
		if !slices.Contains(classes, c) {
			continue
		}
		dev := PCIDevice{
			Slot:   filepath.Base(d),
			Class:  c,
			Vendor: strings.TrimPrefix(trim(read(s, d+"/vendor")), "0x"),
			Device: strings.TrimPrefix(trim(read(s, d+"/device")), "0x"),
		}
		if l, err := s.Readlink(d + "/driver"); err == nil {
			dev.Driver = filepath.Base(l)
		}
		out = append(out, dev)
	}
	return out
}

func power(s sys.System, f *Facts) {
	supplies, _ := s.Glob("/sys/class/power_supply/*")
	for _, p := range supplies {
		switch trim(read(s, p+"/type")) {
		case "Battery":
			if strings.EqualFold(trim(read(s, p+"/scope")), "Device") {
				continue // peripheral battery (mouse, keyboard)
			}
			f.Batteries = append(f.Batteries, Battery{Name: filepath.Base(p), Path: p})
			if f.BatteryPercent < 0 {
				if v, err := strconv.Atoi(trim(read(s, p+"/capacity"))); err == nil {
					f.BatteryPercent = v
				}
			}
		case "Mains":
			f.HasAC = true
			if trim(read(s, p+"/online")) == "1" {
				f.OnAC = true
			}
		}
	}
	if len(f.Batteries) == 0 {
		f.OnAC = true // desktops: always on mains
	}
}

func installedPackages(ctx context.Context, s sys.System) map[string]string {
	out := map[string]string{}
	r, err := s.Query(ctx, sys.Cmd{Name: "dpkg-query", Args: []string{"-W", "-f=${db:Status-Abbrev}\t${Package}\t${Version}\n"}, Env: []string{"LC_ALL=C"}})
	if err != nil {
		return out
	}
	for _, l := range strings.Split(r.Stdout, "\n") {
		p := strings.Split(l, "\t")
		if len(p) == 3 && strings.HasPrefix(p[0], "ii") {
			out[p[1]] = p[2]
		}
	}
	return out
}

// virtualPrefixes are never LAN or physical (catalog step 14).
var virtualPrefixes = []string{"lo", "docker", "br-", "veth", "virbr", "tun", "tap", "wg", "tailscale"}

// IsVirtualName reports interface names kiln always ignores.
func IsVirtualName(n string) bool {
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func network(ctx context.Context, s sys.System, f *Facts) {
	ifup := IfupdownInterfaces(s)
	f.IfupdownIface = ifup
	inIfup := map[string]bool{}
	for _, n := range ifup {
		inIfup[n] = true
	}
	nmState := map[string]string{}
	if r, err := s.Query(ctx, sys.Cmd{Name: "nmcli", Args: []string{"-t", "-f", "DEVICE,STATE", "device"}}); err == nil {
		f.NMRunning = true
		for _, l := range strings.Split(r.Stdout, "\n") {
			d, st, ok := strings.Cut(l, ":")
			if ok {
				nmState[d] = st
			}
		}
	}
	type ipAddr struct {
		Ifname    string   `json:"ifname"`
		Flags     []string `json:"flags"`
		Address   string   `json:"address"`
		OperState string   `json:"operstate"`
		AddrInfo  []struct {
			Family    string `json:"family"`
			Local     string `json:"local"`
			PrefixLen int    `json:"prefixlen"`
			Scope     string `json:"scope"`
		} `json:"addr_info"`
	}
	var addrs []ipAddr
	if r, err := s.Query(ctx, sys.Cmd{Name: "ip", Args: []string{"-j", "addr"}}); err == nil {
		_ = json.Unmarshal([]byte(r.Stdout), &addrs)
	}
	for _, a := range addrs {
		n := NIC{Name: a.Ifname, MAC: a.Address, Up: a.OperState == "UP"}
		base := "/sys/class/net/" + a.Ifname
		if _, err := s.Stat(base + "/device"); err == nil && !IsVirtualName(a.Ifname) {
			n.Physical = true
		}
		if _, err := s.Stat(base + "/wireless"); err == nil {
			n.Wireless = true
		} else if _, err := s.Stat(base + "/phy80211"); err == nil {
			n.Wireless = true
		}
		if l, err := s.Readlink(base + "/device/driver"); err == nil {
			n.Driver = filepath.Base(l)
		}
		n.Vendor = strings.TrimPrefix(trim(read(s, base+"/device/vendor")), "0x")
		for _, ai := range a.AddrInfo {
			if ai.Scope != "global" {
				continue
			}
			if p, err := netip.ParsePrefix(ai.Local + "/" + strconv.Itoa(ai.PrefixLen)); err == nil {
				n.Addrs = append(n.Addrs, p)
			}
		}
		switch {
		case inIfup[a.Ifname]:
			n.ManagedBy = "ifupdown"
		case nmState[a.Ifname] != "" && nmState[a.Ifname] != "unmanaged":
			n.ManagedBy = "NetworkManager"
		default:
			n.ManagedBy = "none"
		}
		if n.Name == "lo" {
			continue
		}
		f.NICs = append(f.NICs, n)
		if n.Physical && n.Wireless {
			f.WiFi = append(f.WiFi, n)
		}
	}
	type route struct {
		Gateway string `json:"gateway"`
		Dev     string `json:"dev"`
	}
	var routes []route
	if r, err := s.Query(ctx, sys.Cmd{Name: "ip", Args: []string{"-j", "route", "show", "default"}}); err == nil {
		_ = json.Unmarshal([]byte(r.Stdout), &routes)
	}
	if len(routes) > 0 {
		f.DefaultIface, f.Gateway = routes[0].Dev, routes[0].Gateway
	}
	f.LANSubnets = LANSubnets(f.NICs)
}

// LANSubnets returns private IPv4 (RFC 1918) and IPv6 ULA subnets on physical NICs (D20).
func LANSubnets(nics []NIC) []netip.Prefix {
	seen := map[netip.Prefix]bool{}
	var out []netip.Prefix
	ula := netip.MustParsePrefix("fc00::/7")
	for _, n := range nics {
		if !n.Physical || IsVirtualName(n.Name) {
			continue
		}
		for _, a := range n.Addrs {
			ok := (a.Addr().Is4() && a.Addr().IsPrivate()) || ula.Contains(a.Addr())
			// A single-address prefix (/32, /128, e.g. a DHCPv6 lease) isn't a
			// subnet; the on-link prefix it belongs to is listed separately.
			if !ok || a.Bits() == a.Addr().BitLen() {
				continue
			}
			p := a.Masked()
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// IfupdownInterfaces lists non-lo interfaces with iface stanzas in
// /etc/network/interfaces and /etc/network/interfaces.d/*.
func IfupdownInterfaces(s sys.System) []string {
	files := []string{"/etc/network/interfaces"}
	more, _ := s.Glob("/etc/network/interfaces.d/*")
	files = append(files, more...)
	seen := map[string]bool{}
	var out []string
	for _, file := range files {
		for _, l := range strings.Split(read(s, file), "\n") {
			fl := strings.Fields(l)
			if len(fl) >= 2 && fl[0] == "iface" && fl[1] != "lo" && !seen[fl[1]] {
				seen[fl[1]] = true
				out = append(out, fl[1])
			}
		}
	}
	return out
}
