package steps

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

const (
	nvmInstallURL = "https://raw.githubusercontent.com/nvm-sh/nvm/v0.41.0/install.sh"
	nodeVersion   = "26"

	// Fixed scripts fed to `bash -s` as the user. Never under `set -e`: nvm
	// breaks with errexit.
	nvmInstallScript = `. "$HOME/.nvm/nvm.sh"
nvm install "$1"
nvm alias default "$1"
node -v && npm -v
`
	nvmCheckScript = `. "$HOME/.nvm/nvm.sh"
printf 'nvm=%s\n' "$(nvm --version)"
printf 'node=%s\n' "$(node -v)"
`
)

func doNode(c *Ctx) error {
	u := c.U

	// nvm's installer adds its lines to ~/.bashrc only if the file exists.
	if _, err := ensureBashrc(c); err != nil {
		return err
	}

	// 1. The owner's `curl -o- <install.sh> | bash`, without the pipe: the
	// script goes to a file under /var/tmp that the user can read, then
	// `bash <file>` runs as the user.
	script, err := c.Get(nvmInstallURL, 1<<20)
	if err != nil {
		return fmt.Errorf("could not download nvm's installer: %w", err)
	}
	sum := sha256.Sum256(script)
	c.Log("downloaded %s (sha256 %s)", nvmInstallURL, hex.EncodeToString(sum[:]))
	path, err := userTempFile(c, "kiln-nvm-install-*.sh", script)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(path) }()
	if err := c.AsUser(u, "bash", path); err != nil {
		return fmt.Errorf("nvm's installer failed; check the network connection and run kiln again: %w", err)
	}

	// 2. Node 26 as the default.
	cmd := run.UserCmd(u, "bash", "-s", "--", nodeVersion)
	cmd.Stdin = nvmInstallScript
	if _, err := c.Exec(cmd); err != nil {
		return fmt.Errorf("installing Node %s with nvm failed; check the network connection and run kiln again: %w", nodeVersion, err)
	}

	// Check.
	cmd = quietCmd(run.UserCmd(u, "bash", "-s"))
	cmd.Stdin = nvmCheckScript
	out, _ := c.Exec(cmd)
	got := map[string]string{}
	for _, l := range run.Scan(out) {
		if k, v, ok := strings.Cut(l, "="); ok {
			got[k] = strings.TrimSpace(v)
		}
	}
	if got["nvm"] != "0.41.0" {
		return fmt.Errorf("nvm reports version %q, not 0.41.0; see %s", got["nvm"], run.LogPath)
	}
	if !strings.HasPrefix(got["node"], "v"+nodeVersion+".") {
		return fmt.Errorf("node reports %q, not a v%s version; see %s", got["node"], nodeVersion, run.LogPath)
	}
	if bashrc := filepath.Join(u.Home, ".bashrc"); !strings.Contains(run.Read(bashrc), "$NVM_DIR/nvm.sh") {
		return fmt.Errorf("%s doesn't load nvm (nvm's installer didn't add its lines); see the installer's output in %s and run kiln again", bashrc, run.LogPath)
	}
	return nil
}

// userTempFile writes data to a new file under /var/tmp (never /tmp, a tmpfs
// on trixie) that only the target user can read. The caller removes it.
func userTempFile(c *Ctx, pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp("/var/tmp", pattern)
	if err != nil {
		return "", fmt.Errorf("could not create a file in /var/tmp: %w", err)
	}
	name := f.Name()
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Chown(c.U.UID, c.U.GID)
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("could not write %s: %w", name, werr)
	}
	return name, nil
}
