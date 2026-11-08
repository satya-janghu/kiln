package steps

import (
	"crypto/sha1" //nolint:gosec // OpenPGP v4 fingerprints are SHA-1 by definition (RFC 4880 §12.2)
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/satya-janghu/kiln/internal/detect"
	"github.com/satya-janghu/kiln/internal/run"
)

// NVIDIA's Debian 13 repository (catalog 17, debian13-notes 17).
const (
	nvKeyringDeb    = "https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/cuda-keyring_1.1-1_all.deb"
	nvKeyringSHA    = "d0d4ef986a44400f9db33c600ef33a985175e7cc63d805a10e1839c7a1e78f5f"
	nvKeyInDeb      = "usr/share/keyrings/cuda-archive-keyring.gpg"
	nvFingerprint   = "02182E60104FCDC26EAE1B8597A5D4CB8793F200"
	nvRepoURI       = "https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/"
	nvRepoHost      = "developer.download.nvidia.com"
	nvKeyring       = "/etc/apt/keyrings/nvidia-cuda.gpg"
	nvSources       = "/etc/apt/sources.list.d/nvidia-cuda.sources"
	nvKeyChanged    = "NVIDIA's repository key changed; kiln needs an update"
	nvBlackwellFrom = 0x2900 // every ID from here on is Blackwell or newer (debian13-notes 17)
)

// nvidiaSource picks the driver source for this machine's NVIDIA GPUs from
// their PCI device IDs alone (pure detection, also used by the docker step):
//   - "nvidia": every GPU is Turing or newer (NVIDIA's open-module table, or
//     an ID from 0x2900, Blackwell), so NVIDIA's repository;
//   - "debian": every GPU is supported by Debian's nvidia-driver 550
//     (Maxwell to Ada; the list nvidia-detect uses on trixie);
//   - "": no GPU, a GPU too old for either (Kepler and older), or a mix that
//     no single source covers. The card stays on nouveau.
func nvidiaSource(f *detect.Facts) string {
	gpus := f.NVIDIA()
	if len(gpus) == 0 {
		return ""
	}
	open, deb := true, true
	for _, g := range gpus {
		id := strings.ToLower(strings.TrimPrefix(g.Device, "0x"))
		n, err := strconv.ParseUint(id, 16, 16)
		if !nvOpenIDs[id] && (err != nil || n < nvBlackwellFrom) {
			open = false
		}
		if !nvDebian550IDs[id] {
			deb = false
		}
	}
	switch {
	case open:
		return "nvidia"
	case deb:
		return "debian"
	}
	return ""
}

func doNVIDIA(c *Ctx) error {
	src := nvidiaSource(c.F)
	if src == "" {
		var ids []string
		for _, g := range c.F.NVIDIA() {
			ids = append(ids, "10de:"+g.Device)
		}
		if len(ids) > 1 {
			c.Note("NVIDIA driver not installed: this machine's NVIDIA GPUs (%s) aren't all supported by one driver source, so they use Debian's open nouveau driver.", strings.Join(ids, ", "))
		} else {
			c.Note("NVIDIA driver not installed: this card (%s) is too old for a supported NVIDIA driver; it uses nouveau.", strings.Join(ids, ", "))
		}
		return nil
	}

	// Signing first, so no driver is installed that can't load.
	if c.F.SecureBoot {
		if err := enrollMOK(c); err != nil {
			return fmt.Errorf("the NVIDIA driver wasn't installed and nouveau stays in use: kiln couldn't prepare module signing, which Secure Boot needs (kiln never turns Secure Boot off); fix that and run kiln again: %w", err)
		}
	}
	if err := c.Install("linux-headers-amd64"); err != nil {
		return err
	}
	if src == "nvidia" {
		c.Say("Using NVIDIA's repository (Turing or newer GPU)")
		if err := nvAddRepo(c); err != nil {
			return err
		}
		// Same names as Debian's at higher versions, so apt picks NVIDIA's; no
		// pinning package, so it follows the newest branch.
		if err := c.Install("nvidia-driver", "nvidia-kernel-open-dkms"); err != nil {
			return err
		}
	} else {
		c.Say("Using Debian's nvidia-driver 550 (Maxwell to Ada GPU)")
		// With Recommends: they bring libcuda1 and nvidia-smi (D55, forced).
		if err := c.Install("nvidia-driver"); err != nil {
			return err
		}
	}
	c.Reboot("load the NVIDIA driver")
	return nvCheck(c, src)
}

// nvAddRepo installs NVIDIA's key (taken out of cuda-keyring without
// installing it, and checked) and the repository's deb822 source.
func nvAddRepo(c *Ctx) error {
	deb, err := c.Fetch(nvKeyringDeb, nvKeyringSHA)
	if err != nil {
		if strings.Contains(err.Error(), "expected checksum") {
			return errors.New(nvKeyChanged)
		}
		return fmt.Errorf("couldn't download NVIDIA's cuda-keyring package: %w", err)
	}
	dir := deb + ".x"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("couldn't create %s: %w", dir, err)
	}
	if err := c.Run("dpkg-deb", "-x", deb, dir); err != nil {
		return fmt.Errorf("couldn't unpack NVIDIA's cuda-keyring package: %w", err)
	}
	key, rerr := os.ReadFile(filepath.Join(dir, nvKeyInDeb))
	if rerr != nil {
		return fmt.Errorf("NVIDIA's cuda-keyring package has no %s; %s", nvKeyInDeb, nvKeyChanged)
	}
	if fps, ferr := pgpFingerprints(key); ferr != nil || len(fps) != 1 || fps[0] != nvFingerprint {
		return errors.New(nvKeyChanged)
	}
	if err := c.Write(nvKeyring, 0o644, string(key)); err != nil {
		return err
	}
	src := "# Written by kiln: NVIDIA's Debian 13 driver repository (flat, so no Components)\n" +
		"Types: deb\n" +
		"URIs: " + nvRepoURI + "\n" +
		"Suites: /\n" +
		"Architectures: amd64\n" +
		"Signed-By: " + nvKeyring + "\n"
	if err := c.Write(nvSources, 0o644, src); err != nil {
		return err
	}
	return c.Update()
}

// nvCheck: the DKMS module is built for the newest installed kernel; with
// NVIDIA's repository the installed driver comes from it; with Secure Boot the
// signing key is enrolled or queued.
func nvCheck(c *Ctx, src string) error {
	k := newestKernel(c)
	if k == "" {
		return errors.New("no installed kernel found in /boot; reinstall linux-image-amd64 and run kiln again")
	}
	status, _ := c.Out("dkms", "status")
	built := false
	for _, l := range run.Scan(status) {
		// "nvidia-current/550.163.01, 6.12.57+deb13-amd64, x86_64: installed"
		if strings.HasPrefix(l, "nvidia") && strings.Contains(l, ", "+k+", ") && strings.Contains(l, ": installed") {
			built = true
		}
	}
	if !built {
		return fmt.Errorf("the NVIDIA kernel module wasn't built for kernel %s; `dkms status` and /var/lib/dkms/*/*/build/make.log show why. Fix that and run kiln again", k)
	}
	if src == "nvidia" && !nvFromRepo(c) {
		return fmt.Errorf("nvidia-driver isn't installed from NVIDIA's repository; check `apt-cache policy nvidia-driver` and %s, then run kiln again", nvSources)
	}
	if c.F.SecureBoot && mokState(c) != mokQueued {
		return errors.New("the module signing key Secure Boot needs isn't enrolled or queued for enrollment; run kiln again")
	}
	return nil
}

// nvFromRepo reads `apt-cache policy nvidia-driver`: the installed (***)
// version must list NVIDIA's repository among its sources.
func nvFromRepo(c *Ctx) bool {
	out, err := c.Out("apt-cache", "policy", "nvidia-driver")
	if err != nil {
		return false
	}
	inInstalled := false
	for _, l := range run.Scan(out) {
		if strings.HasPrefix(l, "***") {
			inInstalled = true
			continue
		}
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		if _, err := strconv.Atoi(f[0]); err != nil {
			inInstalled = false // the next version line
			continue
		}
		if inInstalled && strings.Contains(f[1], nvRepoHost) {
			return true
		}
	}
	return false
}

// newestKernel is the highest kernel release with an image in /boot.
func newestKernel(c *Ctx) string {
	imgs, _ := filepath.Glob("/boot/vmlinuz-*")
	best := ""
	for _, p := range imgs {
		k := strings.TrimPrefix(filepath.Base(p), "vmlinuz-")
		if best == "" || c.OK("dpkg", "--compare-versions", k, "gt", best) {
			best = k
		}
	}
	return best
}

// pgpFingerprints returns the v4 fingerprints of the primary keys in a binary keyring.
func pgpFingerprints(b []byte) ([]string, error) {
	short := errors.New("truncated keyring")
	var out []string
	for len(b) > 0 {
		h := b[0]
		if h&0x80 == 0 {
			return nil, errors.New("not a binary OpenPGP keyring")
		}
		var tag, n, hl int
		if h&0x40 != 0 { // new packet format
			tag = int(h & 0x3f)
			if len(b) < 2 {
				return nil, short
			}
			switch o := int(b[1]); {
			case o < 192:
				n, hl = o, 2
			case o < 224:
				if len(b) < 3 {
					return nil, short
				}
				n, hl = (o-192)<<8+int(b[2])+192, 3
			case o == 255:
				if len(b) < 6 {
					return nil, short
				}
				n, hl = int(binary.BigEndian.Uint32(b[2:6])), 6
			default:
				return nil, errors.New("unexpected partial-length packet in a keyring")
			}
		} else { // old packet format
			tag = int(h>>2) & 0x0f
			switch h & 3 {
			case 0:
				if len(b) < 2 {
					return nil, short
				}
				n, hl = int(b[1]), 2
			case 1:
				if len(b) < 3 {
					return nil, short
				}
				n, hl = int(binary.BigEndian.Uint16(b[1:3])), 3
			case 2:
				if len(b) < 5 {
					return nil, short
				}
				n, hl = int(binary.BigEndian.Uint32(b[1:5])), 5
			default:
				return nil, errors.New("unexpected indeterminate-length packet in a keyring")
			}
		}
		if n < 0 || hl+n > len(b) {
			return nil, short
		}
		body := b[hl : hl+n]
		if tag == 6 { // public-key packet
			if len(body) == 0 || body[0] != 4 || n > 0xffff {
				return nil, errors.New("the key isn't an OpenPGP v4 key")
			}
			sum := sha1.New() //nolint:gosec // see the import comment
			sum.Write([]byte{0x99, byte(n >> 8), byte(n & 0xff)})
			sum.Write(body)
			out = append(out, strings.ToUpper(hex.EncodeToString(sum.Sum(nil))))
		}
		b = b[hl+n:]
	}
	return out, nil
}
