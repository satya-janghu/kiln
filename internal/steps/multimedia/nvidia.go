package multimedia

import (
	"context"
	"crypto/sha1" //nolint:gosec // OpenPGP v4 fingerprints are SHA-1 by definition (RFC 4880 §12.2)
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/facts"
	"github.com/satya-janghu/kiln/internal/secrets"
	"github.com/satya-janghu/kiln/internal/step"
	"github.com/satya-janghu/kiln/internal/sys"
)

// NVIDIA's Debian 13 repository (catalog step 7; verified 2026-11-07).
const (
	srcLatest = "nvidia-latest"
	srcStock  = "debian-stock"

	cudaKeyringURL    = "https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/cuda-keyring_1.1-1_all.deb"
	cudaKeyringSHA256 = "d0d4ef986a44400f9db33c600ef33a985175e7cc63d805a10e1839c7a1e78f5f"
	cudaKeyInDeb      = "usr/share/keyrings/cuda-archive-keyring.gpg"
	cudaFingerprint   = "02182E60104FCDC26EAE1B8597A5D4CB8793F200"
	nvidiaRepoURI     = "https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/"
	nvidiaRepoHost    = "developer.download.nvidia.com"
	nvidiaKeyring     = "/etc/apt/keyrings/nvidia-cuda.gpg"
	nvidiaSources     = "/etc/apt/sources.list.d/nvidia-cuda.sources"
	errKeyChanged     = "NVIDIA's repository key changed; kiln needs an update"

	mokKey  = "/var/lib/dkms/mok.key"
	mokPub  = "/var/lib/dkms/mok.pub"
	mokHash = "/root/.kiln-mok.hash"

	// debianStockWarning is Debian's wiki on the 550 series (shown on the option form).
	debianStockWarning = `Debian's wiki: the 550 series "is no longer maintained and contains several security issues".`
)

// gpuGen is what matters for the driver choice.
type gpuGen int

const (
	genPreTuring gpuGen = iota // Maxwell, Pascal, Volta (and older): Debian stock only
	genTuringAda               // Turing to Ada/Hopper: both sources
	genBlackwell               // Blackwell and newer: NVIDIA latest only
)

// blackwellFrom is the lowest Blackwell device ID. Every ID ≥ 0x2900 in the
// 620.70.06 table is a Blackwell (or newer) product and none of them is in the
// 550.163.01 table, while every table ID below it is Turing to Ada/Hopper. An
// ID ≥ 0x2900 that isn't in the table yet is newer than this kiln release.
const blackwellFrom = 0x2900

func classify(device string) gpuGen {
	d := strings.ToLower(strings.TrimPrefix(device, "0x"))
	n, err := strconv.ParseUint(d, 16, 16)
	switch {
	case err == nil && n >= blackwellFrom:
		return genBlackwell
	case openModuleIDs[d]:
		return genTuringAda
	}
	return genPreTuring
}

// sourceChoice is what the detected NVIDIA GPUs allow.
type sourceChoice struct {
	latestOK, stockOK bool
	gpus              []string // "10de:1e84 (Turing or newer)" for messages
}

func choicesFor(f *facts.Facts) sourceChoice {
	c := sourceChoice{latestOK: true, stockOK: true}
	if f == nil {
		return c
	}
	for _, g := range f.NVIDIA() {
		label := "older than Turing"
		switch classify(g.Device) {
		case genPreTuring:
			c.latestOK = false
		case genTuringAda:
			label = "Turing to Ada"
		case genBlackwell:
			c.stockOK = false
			label = "Blackwell or newer"
		}
		c.gpus = append(c.gpus, fmt.Sprintf("10de:%s (%s)", g.Device, label))
	}
	return c
}

// pgpFingerprints returns the v4 fingerprints of the primary keys in a binary keyring.
func pgpFingerprints(b []byte) ([]string, error) {
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
				return nil, errors.New("truncated keyring")
			}
			switch o := int(b[1]); {
			case o < 192:
				n, hl = o, 2
			case o < 224:
				if len(b) < 3 {
					return nil, errors.New("truncated keyring")
				}
				n, hl = (o-192)<<8+int(b[2])+192, 3
			case o == 255:
				if len(b) < 6 {
					return nil, errors.New("truncated keyring")
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
					return nil, errors.New("truncated keyring")
				}
				n, hl = int(b[1]), 2
			case 1:
				if len(b) < 3 {
					return nil, errors.New("truncated keyring")
				}
				n, hl = int(binary.BigEndian.Uint16(b[1:3])), 3
			case 2:
				if len(b) < 5 {
					return nil, errors.New("truncated keyring")
				}
				n, hl = int(binary.BigEndian.Uint32(b[1:5])), 5
			default:
				return nil, errors.New("unexpected indeterminate-length packet in a keyring")
			}
		}
		if n < 0 || hl+n > len(b) {
			return nil, errors.New("truncated keyring")
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

// keyringOK reports whether data holds exactly NVIDIA's Debian 13 key.
func keyringOK(data []byte) bool {
	fps, err := pgpFingerprints(data)
	return err == nil && len(fps) == 1 && fps[0] == cudaFingerprint
}

func installedKeyOK(s sys.System) bool {
	b, err := s.ReadFile(nvidiaKeyring)
	return err == nil && keyringOK(b)
}

// installNVIDIAKey downloads cuda-keyring, verifies it, and extracts only the key.
func installNVIDIAKey(ctx context.Context, s sys.System) error {
	deb, err := s.Fetch(ctx, sys.FetchReq{URL: cudaKeyringURL, SHA256: cudaKeyringSHA256, Name: "cuda-keyring_1.1-1_all.deb", MaxBytes: 1 << 20})
	if err != nil {
		if strings.Contains(err.Error(), "checksum mismatch") {
			return errors.New(errKeyChanged)
		}
		return fmt.Errorf("couldn't download NVIDIA's cuda-keyring package: %w", err)
	}
	dir := filepath.Join(filepath.Dir(deb), "cuda-keyring")
	if merr := s.MkdirAll(dir, 0o700, nil); merr != nil {
		return merr
	}
	// Extract without installing the package.
	if _, xerr := s.Run(ctx, sys.Cmd{Name: "dpkg-deb", Args: []string{"-x", deb, dir}}); xerr != nil {
		return fmt.Errorf("couldn't unpack NVIDIA's cuda-keyring package: %w", xerr)
	}
	if s.DryRun() {
		_, err = s.WriteFile(nvidiaKeyring, []byte("(NVIDIA's key, extracted from cuda-keyring)"), sys.WriteOpts{Mode: 0o644})
		return err
	}
	key, err := s.ReadFile(filepath.Join(dir, cudaKeyInDeb))
	if err != nil {
		return fmt.Errorf("NVIDIA's cuda-keyring package has no %s; %s", cudaKeyInDeb, errKeyChanged)
	}
	if !keyringOK(key) {
		return errors.New(errKeyChanged)
	}
	if merr := s.MkdirAll("/etc/apt/keyrings", 0o755, nil); merr != nil {
		return merr
	}
	_, err = s.WriteFile(nvidiaKeyring, key, sys.WriteOpts{Mode: 0o644})
	return err
}

// policy is the installed version of a package and where it comes from.
type policy struct {
	installed, trixieVersion string
	fromNVIDIA, fromTrixie   bool
}

// parsePolicy reads `apt-cache policy <pkg>`'s version table.
func parsePolicy(out string) policy {
	var p policy
	current, curInstalled := "", false
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		if v, ok := strings.CutPrefix(t, "Installed:"); ok {
			p.installed = strings.TrimSpace(v)
			continue
		}
		f := strings.Fields(strings.TrimPrefix(t, "***"))
		if len(f) < 2 {
			continue
		}
		if _, err := strconv.Atoi(f[0]); err != nil { // a version line: "[***] <version> <priority>"
			current, curInstalled = f[0], strings.HasPrefix(t, "***")
			continue
		}
		// A source line: "<priority> <uri> <suite>/<component> <arch> Packages".
		isNVIDIA := strings.Contains(f[1], nvidiaRepoHost)
		isTrixie := len(f) >= 3 && strings.HasPrefix(f[2], "trixie/")
		if isTrixie && p.trixieVersion == "" {
			p.trixieVersion = current
		}
		if curInstalled {
			p.fromNVIDIA = p.fromNVIDIA || isNVIDIA
			p.fromTrixie = p.fromTrixie || isTrixie
		}
	}
	if p.installed == "(none)" {
		p.installed = ""
	}
	return p
}

func driverPolicy(ctx context.Context, s sys.System) policy {
	return parsePolicy(apt.Policy(ctx, s, "nvidia-driver"))
}

// dkmsBuilt reports whether an nvidia DKMS module is installed for the running
// kernel, or for a newer installed kernel that's waiting for a reboot.
func dkmsBuilt(ctx context.Context, s sys.System, running string) (ok, pendingReboot bool) {
	r, err := s.Query(ctx, sys.Cmd{Name: "dkms", Args: []string{"status"}, Env: []string{"LC_ALL=C"}})
	if err != nil {
		return false, false
	}
	for _, l := range strings.Split(r.Stdout, "\n") {
		if !strings.HasPrefix(l, "nvidia") || !strings.Contains(l, ": installed") {
			continue
		}
		parts := strings.Split(l, ",")
		if len(parts) < 2 {
			continue
		}
		k := strings.TrimSpace(parts[1])
		if k == running {
			return true, false
		}
		if _, err := s.Stat("/boot/vmlinuz-" + k); err == nil {
			pendingReboot = true
		}
	}
	return pendingReboot, pendingReboot
}

// mokStatus is what `mokutil --test-key` says about dkms's signing key.
type mokStatus int

const (
	mokUnknown mokStatus = iota
	mokNotEnrolled
	mokEnrolledOrPending
	mokBlocked
)

// testMOK runs `mokutil --test-key`: exit 0 = "is not enrolled" (also not in
// MokNew); exit 1 = already enrolled, in db, or in the enrollment request
// (MokNew), or blocked (mokutil 0.7.2 test_key/print_skip_message). Because
// --test-key already checks the pending request, `--list-new` adds nothing.
func testMOK(ctx context.Context, s sys.System) mokStatus {
	r, err := s.Query(ctx, sys.Cmd{Name: "mokutil", Args: []string{"--test-key", mokPub}, Env: []string{"LC_ALL=C"}})
	out := r.Stdout + r.Stderr
	var ee *sys.ExitError
	switch {
	case err == nil && strings.Contains(out, "is not enrolled"):
		return mokNotEnrolled
	case errors.As(err, &ee) && ee.Code == 1 && strings.Contains(out, "blocked"):
		return mokBlocked
	case errors.As(err, &ee) && ee.Code == 1 && strings.Contains(out, "already"):
		return mokEnrolledOrPending
	}
	return mokUnknown
}

// mokPlaceholder stands in for the one-time password in a preview.
const mokPlaceholder = "(one-time password, generated when applied)"

// prepareMOK makes dkms's signing key and queues its enrollment (D24). It
// returns the one-time password when an enrollment was queued.
func prepareMOK(ctx context.Context, s sys.System, env *step.Env) (string, error) {
	if err := apt.Install(ctx, s, []string{"mokutil", "openssl", "dkms"}, apt.InstallOpts{}); err != nil {
		return "", err
	}
	if _, err := s.Run(ctx, sys.Cmd{Name: "dkms", Args: []string{"generate_mok"}}); err != nil {
		return "", fmt.Errorf("dkms couldn't create its signing key: %w", err)
	}
	if !s.DryRun() {
		for _, p := range []string{mokKey, mokPub} {
			if _, err := s.Stat(p); err != nil {
				return "", fmt.Errorf("dkms generate_mok didn't create %s", p)
			}
		}
	}
	st := testMOK(ctx, s)
	switch st {
	case mokEnrolledOrPending:
		return "", nil
	case mokBlocked:
		return "", fmt.Errorf("the firmware blocks dkms's key %s (dbx/MokListX)", mokPub)
	case mokUnknown:
		if !s.DryRun() {
			return "", errors.New("mokutil --test-key gave an unexpected answer")
		}
	}
	// Shown once on the summary screen and kept in /etc/kiln/secrets/mok (0600);
	// never in notes, logs or the world-readable summary file. A preview only
	// records the steps with a placeholder: no real password is generated or
	// registered for display.
	otp := mokPlaceholder
	if !s.DryRun() {
		var gerr error
		if otp, gerr = env.Secrets.GenerateAndRegister("mok", 8); gerr != nil {
			return "", gerr
		}
	}
	if serr := secrets.Save(s, "mok", otp); serr != nil {
		return "", serr
	}
	// The password goes on stdin (mokutil reads it twice with getline when stdin
	// isn't a terminal), never in argv; Quiet keeps the printed hash out of the log.
	r, err := s.Run(ctx, sys.Cmd{Name: "mokutil", Args: []string{"--generate-hash"}, Stdin: []byte(otp + "\n" + otp + "\n"), Redact: []string{otp}, Quiet: true})
	if err != nil {
		return "", fmt.Errorf("mokutil couldn't hash the one-time password: %w", err)
	}
	hash := ""
	for _, l := range strings.Split(r.Stdout, "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "$") {
			hash = t
		}
	}
	if hash == "" && !s.DryRun() {
		return "", errors.New("mokutil --generate-hash printed no hash")
	}
	if _, err := s.WriteFile(mokHash, []byte(hash+"\n"), sys.WriteOpts{Mode: 0o600, NoBackup: true}); err != nil {
		return "", err
	}
	_, ierr := s.Run(ctx, sys.Cmd{Name: "mokutil", Args: []string{"--import", mokPub, "--hash-file", mokHash}})
	_ = s.RemoveAll(mokHash) // never backed up
	if ierr != nil {
		return "", fmt.Errorf("mokutil couldn't queue the key for enrollment: %w", ierr)
	}
	return otp, nil
}
