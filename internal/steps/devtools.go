package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// GitHub CLI repository (D18; debian13-notes, section 10).
const (
	ghKeyURL  = "https://cli.github.com/packages/githubcli-archive-keyring.gpg"
	ghKeySHA  = "6084d5d7bd8e288441e0e94fc6275570895da18e6751f70f057485dc2d1a811b"
	ghKeyring = "/etc/apt/keyrings/githubcli-archive-keyring.gpg"
	ghSources = "/etc/apt/sources.list.d/github-cli.sources"

	ghSourcesContent = `# Managed by kiln
# GitHub CLI (https://github.com/cli/cli/blob/trunk/docs/install_linux.md)
Types: deb
URIs: https://cli.github.com/packages
Suites: stable
Components: main
Architectures: amd64
Signed-By: /etc/apt/keyrings/githubcli-archive-keyring.gpg
`
)

var devPackages = []string{
	"build-essential", "git", "git-lfs", "gh", "curl", "wget", "jq", "vim", "neovim", "tmux",
	"htop", "btop", "ripgrep", "fd-find", "fzf", "bat", "tree", "unzip", "zip", "rsync", "lsof",
	"strace", "bind9-dnsutils", "iproute2", "traceroute", "mtr-tiny", "netcat-openbsd", "cmake",
	"ninja-build", "pkgconf", "libssl-dev", "shellcheck", "bash-completion", "man-db",
	"autoconf", "automake", "libtool", "gdb",
}

// devLinks are the user's names for binaries Debian renames.
var devLinks = [][2]string{{"fd", "/usr/bin/fdfind"}, {"bat", "/usr/bin/batcat"}}

func doDevtools(c *Ctx) error {
	var err error
	u := c.U

	// 1. GitHub's repository for gh.
	f, err := c.Fetch(ghKeyURL, ghKeySHA)
	if err != nil {
		return fmt.Errorf("could not get the GitHub CLI signing key (if the checksum doesn't match, GitHub changed its key and kiln needs an update): %w", err)
	}
	key, err := os.ReadFile(f)
	if err != nil {
		return err
	}
	if err = c.Write(ghKeyring, 0o644, string(key)); err != nil {
		return err
	}
	if err = c.Write(ghSources, 0o644, ghSourcesContent); err != nil {
		return err
	}
	if err = c.Update(); err != nil {
		return err
	}

	// 2. The packages.
	if err = c.Install(devPackages...); err != nil {
		return err
	}

	// 3. As the user: ~/.local/bin with the two links, and git-lfs.
	bin := filepath.Join(u.Home, ".local", "bin")
	if err = c.AsUser(u, "mkdir", "-p", bin); err != nil {
		return fmt.Errorf("could not create %s: %w", bin, err)
	}
	for _, l := range devLinks {
		p := filepath.Join(bin, l[0])
		if fi, lerr := os.Lstat(p); lerr == nil && fi.Mode()&os.ModeSymlink == 0 {
			c.Note("%s is a regular file, not a link, so kiln left it alone (Debian's binary is %s).", p, l[1])
			continue
		}
		if err = c.AsUser(u, "ln", "-sfn", l[1], p); err != nil {
			return fmt.Errorf("could not link %s to %s: %w", p, l[1], err)
		}
	}
	if err = c.AsUser(u, "git", "lfs", "install", "--skip-repo"); err != nil {
		return fmt.Errorf("could not set up git-lfs for %s: %w", u.Name, err)
	}

	// Check.
	var missing []string
	for _, p := range devPackages {
		if !run.Installed(c.S, p) {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("these packages didn't install: %s; see %s", strings.Join(missing, " "), run.LogPath)
	}
	if !c.OK("gh", "--version") {
		return fmt.Errorf("`gh --version` fails; see %s", run.LogPath)
	}
	for _, l := range devLinks {
		p := filepath.Join(bin, l[0])
		if _, err = os.Stat(p); err != nil {
			return fmt.Errorf("%s doesn't lead to a working program (%s); check the link", p, l[1])
		}
	}
	out, err := c.Exec(quietCmd(run.UserCmd(u, "git", "config", "--global", "--get", "filter.lfs.process")))
	if err != nil || strings.TrimSpace(out) == "" {
		return fmt.Errorf("git-lfs isn't set up for %s; run `git lfs install --skip-repo` as %s", u.Name, u.Name)
	}
	return nil
}

// quietCmd keeps a check's output off the progress screen.
func quietCmd(cmd run.Cmd) run.Cmd {
	cmd.Quiet = true
	return cmd
}
