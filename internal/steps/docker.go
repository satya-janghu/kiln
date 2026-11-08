package steps

import (
	"crypto/sha1" //nolint:gosec // OpenPGP v4 fingerprints are SHA-1 by definition (RFC 4880 §12.2)
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/satya-janghu/kiln/internal/run"
)

// Docker's Debian install docs (debian13-notes, section 8).
const (
	dockerKeyURL  = "https://download.docker.com/linux/debian/gpg"
	dockerKey     = "/etc/apt/keyrings/docker.asc"
	dockerSources = "/etc/apt/sources.list.d/docker.sources"
	daemonJSON    = "/etc/docker/daemon.json"

	dockerSourcesContent = `# Managed by kiln
# Docker CE (https://docs.docker.com/engine/install/debian/)
Types: deb
URIs: https://download.docker.com/linux/debian
Suites: trixie
Components: stable
Architectures: amd64
Signed-By: /etc/apt/keyrings/docker.asc
`
)

// NVIDIA Container Toolkit (debian13-notes, section 8), as a deb822 flat repository.
const (
	toolkitKeyURL      = "https://nvidia.github.io/libnvidia-container/gpgkey"
	toolkitFingerprint = "C95B321B61E88C1809C4F759DDCAE044F796ECB0"
	toolkitKey         = "/etc/apt/keyrings/nvidia-container-toolkit.asc"
	toolkitSources     = "/etc/apt/sources.list.d/nvidia-container-toolkit.sources"

	toolkitSourcesContent = `# Managed by kiln
# NVIDIA Container Toolkit (https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)
Types: deb
URIs: https://nvidia.github.io/libnvidia-container/stable/deb/amd64/
Suites: /
Signed-By: /etc/apt/keyrings/nvidia-container-toolkit.asc
`
)

var dockerPackages = []string{"docker-ce", "docker-ce-cli", "containerd.io", "docker-buildx-plugin", "docker-compose-plugin"}

func doDocker(c *Ctx) error {
	var err error
	// 1. Debian's and other conflicting packages.
	var present []string
	for _, p := range []string{"docker.io", "docker-compose", "docker-doc", "docker-buildx", "podman-docker", "containerd", "runc"} {
		if run.Installed(c.S, p) {
			present = append(present, p)
		}
	}
	if len(present) > 0 {
		if err = c.AptGet(append([]string{"remove", "-y"}, present...)...); err != nil {
			return fmt.Errorf("could not remove %s, which conflict with Docker's packages: %w", strings.Join(present, " "), err)
		}
	}

	// 2–4. Docker's repository and packages.
	if err = c.Run("install", "-m", "0755", "-d", "/etc/apt/keyrings"); err != nil {
		return err
	}
	key, err := c.Get(dockerKeyURL, 1<<20)
	if err != nil {
		return fmt.Errorf("could not download Docker's signing key: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(key)), "-----BEGIN PGP PUBLIC KEY BLOCK-----") {
		return fmt.Errorf("%s didn't return an OpenPGP key; try again later", dockerKeyURL)
	}
	if err = c.Write(dockerKey, 0o644, string(key)); err != nil {
		return err
	}
	if err = c.Write(dockerSources, 0o644, dockerSourcesContent); err != nil {
		return err
	}
	if err = c.Update(); err != nil {
		return err
	}
	if err = c.Install(dockerPackages...); err != nil {
		return err
	}

	// 5. Log rotation in daemon.json.
	if err = dockerLogConfig(c); err != nil {
		return err
	}

	// 6. The docker group, then prove Docker works.
	u := c.U.Name
	if groups, _ := c.Out("id", "-nG", u); !hasField(groups, "docker") {
		if err = c.Run("adduser", u, "docker"); err != nil {
			return fmt.Errorf("could not add %s to the docker group: %w", u, err)
		}
	}
	if err = dockerWait(c); err != nil {
		return err
	}
	if err = c.Run("docker", "run", "--rm", "hello-world"); err != nil {
		return fmt.Errorf("hello-world failed although Docker is running; check the network connection to Docker Hub: %w", err)
	}

	// 7. Portainer CE (an existing container named portainer is left alone).
	if c.OK("docker", "container", "inspect", "portainer") {
		c.Say("A container named portainer already exists; leaving it alone.")
	} else {
		if err = c.Run("docker", "volume", "create", "portainer_data"); err != nil {
			return fmt.Errorf("could not create Portainer's volume: %w", err)
		}
		if err = c.Run("docker", "run", "-d", "-p", "9443:9443", "--name", "portainer", "--restart=always",
			"-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", "portainer_data:/data",
			"portainer/portainer-ce:lts"); err != nil {
			return fmt.Errorf("could not start Portainer; check the network connection to Docker Hub: %w", err)
		}
	}

	c.Note("Log out and back in for %s's docker group membership to take effect.", u)
	c.Note("The docker group is root-equivalent: anyone in it can control the whole machine.")
	c.Note("Portainer: open https://%s:9443 within 5 minutes of a boot and create the admin account; if it timed out, run `docker restart portainer`.", addrForNotes(c))
	c.Note("Portainer may ask for a setup token first: it is in `docker logs portainer` (the line with setup_token=).")

	// 8. NVIDIA Container Toolkit, when the NVIDIA step will install a driver.
	toolkit := c.Picked("nvidia") && nvidiaSource(c.F) != ""
	if toolkit {
		// Docker itself works without it, so a toolkit problem is a note.
		if terr := installToolkit(c); terr != nil {
			c.Note("NVIDIA Container Toolkit couldn't be set up, so containers can't use the GPU yet: %v", terr)
			toolkit = false
		} else {
			c.Note("GPU containers work after the reboot that loads the NVIDIA driver.")
		}
	}

	// Check.
	if _, err = c.Out("docker", "info"); err != nil {
		return fmt.Errorf("`docker info` fails; check `systemctl status docker` and `journalctl -u docker`: %w", err)
	}
	st, err := c.Out("docker", "inspect", "-f", "{{.State.Running}} {{.HostConfig.RestartPolicy.Name}}", "portainer")
	if err != nil || strings.TrimSpace(st) != "true always" {
		return fmt.Errorf("the portainer container isn't running with restart policy always (it shows %q); check `docker ps -a` and `docker logs portainer`", strings.TrimSpace(st))
	}
	if toolkit {
		rt, _ := c.Out("docker", "info", "-f", "{{json .Runtimes}}")
		if !strings.Contains(rt, `"nvidia"`) {
			return errors.New("`docker info` doesn't list the nvidia runtime; run `nvidia-ctk runtime configure --runtime=docker` and `systemctl restart docker`")
		}
	}
	return nil
}

func hasField(s, want string) bool {
	for _, f := range strings.Fields(s) {
		if f == want {
			return true
		}
	}
	return false
}

// dockerWait waits up to 60 s for `docker info`: Docker starts in the
// background after an install or restart.
func dockerWait(c *Ctx) error {
	if err := pollFor(c, 60*time.Second, func() error { _, err := c.Out("docker", "info"); return err }); err != nil {
		return fmt.Errorf("`docker info` didn't answer within 60 seconds; check `systemctl status docker` and `journalctl -u docker`: %w", err)
	}
	return nil
}

// dockerLogConfig merges kiln's log settings into daemon.json, keeping every
// other key, and restarts Docker if the file changed. The new file is checked
// with `dockerd --validate` (when this dockerd has it) before it is put in place.
func dockerLogConfig(c *Ctx) error {
	old := run.Read(daemonJSON)
	m := map[string]any{}
	if strings.TrimSpace(old) != "" {
		if err := json.Unmarshal([]byte(old), &m); err != nil {
			return fmt.Errorf("%s isn't valid JSON (%w); fix or remove it, then run kiln again", daemonJSON, err)
		}
	}
	m["log-driver"] = "json-file"
	lo, _ := m["log-opts"].(map[string]any)
	if lo == nil {
		lo = map[string]any{}
	}
	lo["max-size"] = "10m" // log-opts values must be strings
	lo["max-file"] = "3"
	m["log-opts"] = lo
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	content := string(b) + "\n"
	if content == old {
		return nil
	}
	if help, _ := c.Out("dockerd", "--help"); strings.Contains(help, "--validate") {
		dir, err := c.TempDir()
		if err != nil {
			return err
		}
		tmp := filepath.Join(dir, "daemon.json")
		if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
			return err
		}
		if err := c.Run("dockerd", "--validate", "--config-file", tmp); err != nil {
			return fmt.Errorf("dockerd rejected the new %s, so kiln left the old one in place; check the file's other settings: %w", daemonJSON, err)
		}
	}
	if err := c.Write(daemonJSON, 0o644, content); err != nil {
		return err
	}
	if err := c.Run("systemctl", "restart", "docker"); err != nil {
		return fmt.Errorf("restarting Docker with the new %s failed; check `journalctl -u docker`: %w", daemonJSON, err)
	}
	return dockerWait(c)
}

// installToolkit adds NVIDIA's Container Toolkit repository, installs the
// toolkit and registers the nvidia runtime with Docker.
func installToolkit(c *Ctx) error {
	key, err := c.Get(toolkitKeyURL, 1<<20)
	if err != nil {
		return fmt.Errorf("could not download the NVIDIA Container Toolkit signing key: %w", err)
	}
	fps, err := armoredFingerprints(key)
	if err != nil || len(fps) != 1 || fps[0] != toolkitFingerprint {
		return fmt.Errorf("the NVIDIA Container Toolkit signing key changed (got %v); kiln needs an update", fps)
	}
	if err := c.Write(toolkitKey, 0o644, string(key)); err != nil {
		return err
	}
	if err := c.Write(toolkitSources, 0o644, toolkitSourcesContent); err != nil {
		return err
	}
	if err := c.Update(); err != nil {
		return err
	}
	if err := c.Install("nvidia-container-toolkit"); err != nil {
		return err
	}
	if err := c.Run("nvidia-ctk", "runtime", "configure", "--runtime=docker"); err != nil {
		return fmt.Errorf("could not register the nvidia runtime with Docker: %w", err)
	}
	if err := c.Run("systemctl", "restart", "docker"); err != nil {
		return fmt.Errorf("restarting Docker after adding the nvidia runtime failed; check `journalctl -u docker`: %w", err)
	}
	if err := dockerWait(c); err != nil {
		return err
	}
	// Put the log settings back in case nvidia-ctk dropped them.
	return dockerLogConfig(c)
}

// armoredFingerprints returns the v4 fingerprints of the primary keys in an
// ASCII-armored OpenPGP public key block.
func armoredFingerprints(armored []byte) ([]string, error) {
	var b64 strings.Builder
	in, body := false, false
	for _, l := range strings.Split(string(armored), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case l == "-----BEGIN PGP PUBLIC KEY BLOCK-----":
			in = true
		case !in:
		case strings.HasPrefix(l, "-----END"):
			in = false
		case !body: // armor headers end at the first blank line
			body = l == ""
		case strings.HasPrefix(l, "="): // CRC24 line
		default:
			b64.WriteString(l)
		}
	}
	b, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil || len(b) == 0 {
		return nil, errors.New("not an ASCII-armored OpenPGP key")
	}
	var out []string
	for len(b) > 0 {
		h := b[0]
		if h&0x80 == 0 {
			return nil, errors.New("not an OpenPGP key")
		}
		var tag, n, hl int
		if h&0x40 != 0 { // new packet format
			if len(b) < 2 {
				return nil, errors.New("truncated key")
			}
			tag = int(h & 0x3f)
			switch o := int(b[1]); {
			case o < 192:
				n, hl = o, 2
			case o < 224 && len(b) >= 3:
				n, hl = (o-192)<<8+int(b[2])+192, 3
			case o == 255 && len(b) >= 6:
				n, hl = int(binary.BigEndian.Uint32(b[2:6])), 6
			default:
				return nil, errors.New("unexpected packet length in the key")
			}
		} else { // old packet format
			tag = int(h>>2) & 0x0f
			switch t := h & 3; {
			case t == 0 && len(b) >= 2:
				n, hl = int(b[1]), 2
			case t == 1 && len(b) >= 3:
				n, hl = int(binary.BigEndian.Uint16(b[1:3])), 3
			case t == 2 && len(b) >= 5:
				n, hl = int(binary.BigEndian.Uint32(b[1:5])), 5
			default:
				return nil, errors.New("unexpected packet length in the key")
			}
		}
		if n < 0 || hl+n > len(b) {
			return nil, errors.New("truncated key")
		}
		if tag == 6 { // public-key packet
			p := b[hl : hl+n]
			if len(p) == 0 || p[0] != 4 || n > 0xffff {
				return nil, errors.New("not an OpenPGP v4 key")
			}
			sum := sha1.New()                              //nolint:gosec // see the import comment
			sum.Write([]byte{0x99, byte(n >> 8), byte(n)}) //nolint:gosec // n <= 0xffff is checked above
			sum.Write(p)
			out = append(out, strings.ToUpper(hex.EncodeToString(sum.Sum(nil))))
		}
		b = b[hl+n:]
	}
	return out, nil
}
