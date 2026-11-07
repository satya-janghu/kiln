package netmigrate

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/sys"
)

// ConnDir is where NetworkManager keyfiles live (nm-settings-keyfile(5)).
const ConnDir = "/etc/NetworkManager/system-connections"

func profileName(iface string) string { return "kiln-" + iface }
func keyfilePath(iface string) string { return ConnDir + "/" + profileName(iface) + ".nmconnection" }

// uuidFor is a stable name-based UUID, so a re-run writes the same keyfile.
func uuidFor(iface string) string {
	h := sha256.Sum256([]byte("kiln network-migration " + iface))
	h[6] = (h[6] & 0x0f) | 0x50
	h[8] = (h[8] & 0x3f) | 0x80
	x := hex.EncodeToString(h[:16])
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

// kfEscape escapes a keyfile (GKeyFile) string value.
func kfEscape(v string) string {
	v = strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(v)
	if strings.HasPrefix(v, " ") {
		v = `\s` + v[1:]
	}
	return v
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// validPSK: 8-63 characters, or 64 hex digits (catalog step 6.3).
func validPSK(p string) bool {
	return (len(p) >= 8 && len(p) <= 63) || (len(p) == 64 && isHex(p))
}

// colonHex validates "aa:bb:…" and normalises it to lower case.
func colonHex(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", false
	}
	for _, p := range strings.Split(s, ":") {
		if len(p) != 2 || !isHex(p) {
			return "", false
		}
	}
	return s, true
}

// wifiCredentials reads the SSID and PSK from the stanza or its wpa-conf file.
func wifiCredentials(s sys.System, st []*stanza) (ssid, psk, why string) {
	for _, x := range st {
		if v := x.get("wpa-ssid"); v != "" {
			ssid = unquote(v)
		}
		if v := x.get("wpa-psk"); v != "" {
			psk = unquote(v)
		}
		if v := x.get("wpa-conf"); v != "" && (ssid == "" || psk == "") {
			ssid, psk, why = fromWPAConf(s, unquote(v))
			if why != "" {
				return "", "", why
			}
		}
	}
	switch {
	case ssid == "":
		return "", "", "no wpa-ssid or readable wpa-conf"
	case psk == "":
		return "", "", "no WPA-PSK password (open, WEP or enterprise networks aren't migrated)"
	case !validPSK(psk):
		return "", "", "the WPA password isn't 8-63 characters or 64 hex digits"
	}
	return ssid, psk, ""
}

func fromWPAConf(s sys.System, path string) (ssid, psk, why string) {
	b, err := s.ReadFile(path)
	if err != nil {
		return "", "", "its wpa-conf file " + path + " can't be read"
	}
	if strings.Count(string(b), "network={") != 1 {
		return "", "", path + " doesn't have exactly one network block"
	}
	keyMgmt := ""
	for _, l := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if !ok {
			continue
		}
		switch k {
		case "ssid":
			ssid = unquote(v)
		case "psk":
			psk = unquote(v)
		case "key_mgmt":
			keyMgmt = v
		}
	}
	if keyMgmt != "" && !strings.Contains(keyMgmt, "WPA-PSK") {
		return "", "", path + " isn't a WPA-PSK network"
	}
	if ssid == "" || psk == "" {
		return "", "", path + " has no ssid or psk"
	}
	return ssid, psk, ""
}

// dhcpClient returns the client ifupdown runs: dhclient, then udhcpc, then dhcpcd (ifupdown 0.8.44 inet.defn).
func dhcpClient(s sys.System) string {
	for _, c := range []string{"dhclient", "udhcpc", "dhcpcd"} {
		if _, err := s.Stat("/sbin/" + c); err == nil {
			return c
		}
	}
	return ""
}

// dhcpcdSettings reads clientid/duid/iaid from /etc/dhcpcd.conf for iface
// (global lines, then the `interface <iface>` block; the last one wins).
func dhcpcdSettings(s sys.System, iface string) (useMAC bool, clientID, iaid string) {
	b, err := s.ReadFile("/etc/dhcpcd.conf")
	if err != nil {
		return false, "", ""
	}
	inScope := true
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(strings.TrimSpace(l))
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "interface":
			inScope = len(f) > 1 && f[1] == iface
			continue
		case "profile", "ssid":
			inScope = false
			continue
		}
		if !inScope {
			continue
		}
		switch f[0] {
		case "clientid":
			useMAC, clientID = true, ""
			if len(f) > 1 {
				clientID = f[1]
			}
		case "duid":
			useMAC, clientID = false, ""
		case "iaid":
			if len(f) > 1 {
				iaid = f[1]
			}
		}
	}
	return useMAC, clientID, iaid
}

// iaidBytes renders an IAID as 4 colon-hex bytes: decimal (network order) or aa:bb:cc:dd.
func iaidBytes(v string) (string, bool) {
	if h, ok := colonHex(v); ok && len(strings.Split(h, ":")) == 4 {
		return h, true
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return "", false
	}
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(n)) //nolint:gosec // ParseUint above limits n to 32 bits
	return hexColons(b[:]), true
}

// decodeDhclientString decodes a dhclient lease string ("\000\001…").
func decodeDhclientString(v string) []byte {
	v = unquote(strings.TrimSuffix(strings.TrimSpace(v), ";"))
	var out []byte
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c != '\\' || i+1 >= len(v) {
			out = append(out, c)
			continue
		}
		if i+3 < len(v) && isOctal(v[i+1]) && isOctal(v[i+2]) && isOctal(v[i+3]) {
			n, _ := strconv.ParseUint(v[i+1:i+4], 8, 8)
			out = append(out, byte(n))
			i += 3
			continue
		}
		out = append(out, v[i+1])
		i++
	}
	return out
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

func hexColons(b []byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02x", x)
	}
	return strings.Join(parts, ":")
}

// clientIdentity returns the NM dhcp-client-id ("ff:<IAID>:<DUID>" or "mac"),
// the DUID for DHCPv6 ("" if unknown), and a note when the IP may change.
func clientIdentity(s sys.System, nic *facts.NIC) (clientID, duid, note string) {
	mac, ok := colonHex(nic.MAC)
	macParts := strings.Split(mac, ":")
	if !ok || len(macParts) != 6 {
		return "mac", "", "its MAC address couldn't be read, so the DHCP client ID may differ and the IP may change"
	}
	iaid := strings.Join(macParts[2:], ":")
	switch dhcpClient(s) {
	case "dhcpcd":
		useMAC, cid, conf := dhcpcdSettings(s, nic.Name)
		if useMAC {
			if h, ok := colonHex(cid); ok {
				return h, "", ""
			}
			if cid == "" {
				return "mac", "", ""
			}
			return "mac", "", "dhcpcd.conf sets a text client ID kiln doesn't copy, so the IP may change"
		}
		if conf != "" {
			if v, ok := iaidBytes(conf); ok {
				iaid = v
			}
		}
		b, err := s.ReadFile("/var/lib/dhcpcd/duid")
		if d, ok := colonHex(string(b)); err == nil && ok {
			duid = d
		}
	case "dhclient":
		b, err := s.ReadFile("/var/lib/dhcp/dhclient6." + nic.Name + ".leases")
		if err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if v, found := strings.CutPrefix(strings.TrimSpace(l), "default-duid "); found {
					if raw := decodeDhclientString(v); len(raw) > 0 {
						duid = hexColons(raw)
					}
				}
			}
		}
	}
	if duid == "" {
		return "mac", "", "its DHCP DUID couldn't be read, so NetworkManager sends the MAC address as client ID and the IP may change"
	}
	return "ff:" + iaid + ":" + duid, duid, ""
}

// prefixOf turns "address" (+ "netmask") into a prefix.
func prefixOf(addr, netmask string) (netip.Prefix, error) {
	if p, err := netip.ParsePrefix(addr); err == nil {
		return p, nil
	}
	a, err := netip.ParseAddr(addr)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("address %q isn't an IP address", addr)
	}
	if netmask == "" {
		return netip.Prefix{}, fmt.Errorf("address %s has no prefix length or netmask", addr)
	}
	// PrefixFrom keeps the host address (Addr.Prefix would mask it down to the
	// network address, which is not this machine's address).
	if n, aerr := strconv.Atoi(netmask); aerr == nil {
		p := netip.PrefixFrom(a, n)
		if !p.IsValid() {
			return netip.Prefix{}, fmt.Errorf("netmask %q isn't valid for %s", netmask, addr)
		}
		return p, nil
	}
	m, err := netip.ParseAddr(netmask)
	if err != nil || !m.Is4() || !a.Is4() {
		return netip.Prefix{}, fmt.Errorf("netmask %q isn't valid for %s", netmask, addr)
	}
	mask := binary.BigEndian.Uint32(m.AsSlice())
	bits := 0
	for mask&0x80000000 != 0 {
		bits++
		mask <<= 1
	}
	if mask != 0 {
		return netip.Prefix{}, fmt.Errorf("netmask %q isn't a contiguous netmask", netmask)
	}
	return netip.PrefixFrom(a, bits), nil
}

// resolvers reads nameservers from /etc/resolv.conf.
func resolvers(s sys.System) (v4, v6 []string) {
	b, _ := s.ReadFile("/etc/resolv.conf")
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "nameserver" {
			if a, err := netip.ParseAddr(f[1]); err == nil {
				if a.Is4() {
					v4 = append(v4, a.String())
				} else {
					v6 = append(v6, a.String())
				}
			}
		}
	}
	return v4, v6
}

func dnsList(v []string) string { return strings.Join(v, ";") + ";" }

// profile is a migration's keyfile and what it says, for the plan text.
type profile struct {
	content []byte
	summary string
	note    string
}

// buildProfile translates iface's stanzas into a keyfile (catalog step 6.3).
func buildProfile(s sys.System, f *facts.Facts, c *config, nic *facts.NIC, ssid, psk string) (profile, error) {
	name := nic.Name
	in4, in6 := c.stanza(name, "inet"), c.stanza(name, "inet6")
	var b strings.Builder
	var sum []string
	p := profile{}
	b.WriteString(sys.ManagedHeader("#"))
	b.WriteString("[connection]\n")
	b.WriteString("id=" + profileName(name) + "\nuuid=" + uuidFor(name) + "\n")
	if nic.Wireless {
		b.WriteString("type=wifi\n")
	} else {
		b.WriteString("type=ethernet\n")
	}
	b.WriteString("interface-name=" + name + "\nautoconnect=false\n\n")
	if nic.Wireless {
		b.WriteString("[wifi]\nmode=infrastructure\nssid=" + kfEscape(ssid) + "\n\n")
		b.WriteString("[wifi-security]\nkey-mgmt=wpa-psk\npsk=" + kfEscape(psk) + "\n\n")
		sum = append(sum, "Wi-Fi "+ssid)
	} else {
		b.WriteString("[ethernet]\n\n")
	}
	clientID, duid, note := clientIdentity(s, nic)
	v4dns, v6dns := resolvers(s)

	b.WriteString("[ipv4]\n")
	switch {
	case in4 == nil || in4.method == "manual":
		b.WriteString("method=disabled\n")
		sum = append(sum, "no IPv4")
	case in4.method == "dhcp":
		b.WriteString("method=auto\ndhcp-client-id=" + clientID + "\n")
		sum = append(sum, "IPv4 DHCP")
		p.note = note
	case in4.method == "static":
		pfx, err := prefixOf(in4.get("address"), in4.get("netmask"))
		if err != nil {
			return p, fmt.Errorf("%s: %w", name, err)
		}
		gw := in4.get("gateway")
		if gw == "" && f.DefaultIface == name {
			gw = f.Gateway
		}
		addr := pfx.String()
		if gw != "" {
			addr += "," + gw
		}
		b.WriteString("method=manual\naddress1=" + addr + "\n")
		dns := strings.Fields(in4.get("dns-nameservers"))
		if len(dns) == 0 {
			dns = v4dns
		}
		if len(dns) > 0 {
			b.WriteString("dns=" + dnsList(dns) + "\n")
		}
		if ds := strings.Fields(in4.get("dns-search")); len(ds) > 0 {
			b.WriteString("dns-search=" + dnsList(ds) + "\n")
		}
		sum = append(sum, "IPv4 "+pfx.String())
	}
	b.WriteString("\n[ipv6]\n")
	switch {
	case in6 == nil || in6.method == "manual":
		b.WriteString("method=ignore\n")
	case in6.method == "auto" || in6.method == "dhcp":
		m := "auto"
		if in6.method == "dhcp" {
			m = "dhcp"
		}
		b.WriteString("method=" + m + "\naddr-gen-mode=eui64\n")
		if duid != "" {
			b.WriteString("dhcp-duid=" + duid + "\n")
		}
		sum = append(sum, "IPv6 "+in6.method)
	case in6.method == "static":
		pfx, err := prefixOf(in6.get("address"), in6.get("netmask"))
		if err != nil {
			return p, fmt.Errorf("%s: %w", name, err)
		}
		addr := pfx.String()
		if gw := in6.get("gateway"); gw != "" {
			addr += "," + gw
		}
		b.WriteString("method=manual\naddr-gen-mode=eui64\naddress1=" + addr + "\n")
		dns := strings.Fields(in6.get("dns-nameservers"))
		if len(dns) == 0 {
			dns = v6dns
		}
		if len(dns) > 0 {
			b.WriteString("dns=" + dnsList(dns) + "\n")
		}
		sum = append(sum, "IPv6 "+pfx.String())
	}
	if in4 != nil && in4.method == "dhcp" {
		sum = append(sum, "DHCP client ID "+clientIDLabel(clientID))
	}
	p.content = []byte(b.String())
	p.summary = strings.Join(sum, ", ")
	return p, nil
}

func clientIDLabel(id string) string {
	if id == "mac" {
		return "from the MAC address"
	}
	return "copied from the current DHCP client"
}
