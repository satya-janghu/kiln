package steps

import (
	"fmt"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

const (
	sourcesList   = "/etc/apt/sources.list"
	debianSources = "/etc/apt/sources.list.d/debian.sources"
	sourcesNote   = "# Debian entries now live in sources.list.d/debian.sources"
)

// debianSourcesContent is kiln's deb822 debian.sources. Backports needs no
// extra field: its Release file already says `NotAutomatic: yes` with
// `ButAutomaticUpgrades: yes`, so nothing comes from it without
// `-t trixie-backports` (debian13-notes).
const debianSourcesContent = `# Managed by kiln
Types: deb
URIs: http://deb.debian.org/debian
Suites: trixie trixie-updates
Components: main contrib non-free non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.pgp

Types: deb
URIs: http://security.debian.org/debian-security
Suites: trixie-security
Components: main contrib non-free non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.pgp

Types: deb
URIs: http://deb.debian.org/debian
Suites: trixie-backports
Components: main contrib non-free non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.pgp
`

// activeLine reports a sources.list line apt would read.
func activeLine(l string) bool {
	t := strings.TrimSpace(l)
	return t != "" && !strings.HasPrefix(t, "#")
}

// commentOutSources comments out every active line (cdrom ones included)
// under kiln's note.
func commentOutSources(content string) string {
	var b strings.Builder
	if !strings.Contains(content, sourcesNote) {
		b.WriteString(sourcesNote + "\n")
	}
	for _, l := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if activeLine(l) {
			l = "# " + l
		}
		b.WriteString(l + "\n")
	}
	return b.String()
}

func doSources(c *Ctx) error {
	if run.Exists(sourcesList) {
		if err := c.Write(sourcesList, 0o644, commentOutSources(run.Read(sourcesList))); err != nil {
			return err
		}
	}
	if err := c.Write(debianSources, 0o644, debianSourcesContent); err != nil {
		return err
	}
	if err := c.Update(); err != nil {
		return err
	}
	// Check: apt-get update succeeded (above) and sources.list is inactive.
	for _, l := range strings.Split(run.Read(sourcesList), "\n") {
		if activeLine(l) {
			return fmt.Errorf("%s still has an active line (%q); comment it out and run kiln again", sourcesList, strings.TrimSpace(l))
		}
	}
	return nil
}
