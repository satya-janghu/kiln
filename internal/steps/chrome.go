package steps

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

// Google Chrome through extrepo (catalog 16, debian13-notes 16).
const (
	extrepoConfig  = "/etc/extrepo/config.yaml"
	chromeSources  = "/etc/apt/sources.list.d/extrepo_google_chrome.sources"
	chromeDefaults = "/etc/default/google-chrome"
	chromeOwnList  = "/etc/apt/sources.list.d/google-chrome.list"
)

func doChrome(c *Ctx) error {
	if err := c.Install("extrepo"); err != nil {
		return err
	}
	if err := extrepoAllow(c, "non-free"); err != nil {
		return err
	}
	if !chromeEnabled() {
		if err := c.Run("extrepo", "enable", "google_chrome"); err != nil {
			return fmt.Errorf("extrepo couldn't enable google_chrome; check the network connection and run kiln again: %w", err)
		}
	}
	// So the package doesn't add its own duplicate google-chrome.list.
	if err := c.Write(chromeDefaults, 0o644, "repo_add_once=\"false\"\nrepo_reenable_on_distupgrade=\"false\"\n"); err != nil {
		return err
	}
	if err := c.Update(); err != nil {
		return err
	}
	if err := c.Install("google-chrome-stable"); err != nil {
		return err
	}
	switch {
	case !chromeEnabled():
		return fmt.Errorf("%s is missing or disabled; run `extrepo enable google_chrome` and kiln again", chromeSources)
	case !run.Installed(c.S, "google-chrome-stable"):
		return errors.New("google-chrome-stable isn't installed after apt finished; check " + run.LogPath + " and run kiln again")
	case run.Exists(chromeOwnList):
		return fmt.Errorf("the Chrome package added its own %s next to extrepo's source; delete it and run kiln again", chromeOwnList)
	}
	return nil
}

// chromeEnabled: extrepo's sources file exists and isn't disabled. A deb822
// stanza without an Enabled: field is enabled (sources.list(5)), which is
// how extrepo writes it.
func chromeEnabled() bool {
	text := run.Read(chromeSources)
	if strings.TrimSpace(text) == "" {
		return false
	}
	for _, l := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if ok && strings.TrimSpace(k) == "Enabled" && !strings.EqualFold(strings.TrimSpace(v), "yes") {
			return false
		}
	}
	return true
}

var extrepoItem = regexp.MustCompile(`^(#\s*)?-\s*(\S+)\s*$`) // "- main" or "# - non-free"

// extrepoAllow adds or uncomments "- <policy>" under enabled_policies: in
// extrepo's config, leaving the rest of the file as it is.
func extrepoAllow(c *Ctx, policy string) error {
	text := run.Read(extrepoConfig)
	if text == "" {
		return fmt.Errorf("can't read %s; reinstall extrepo (apt-get install --reinstall extrepo) and run kiln again", extrepoConfig)
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "enabled_policies:") {
			start = i
			break
		}
	}
	if start < 0 {
		return fmt.Errorf("%s has no enabled_policies: list; restore it with `apt-get install --reinstall extrepo` and run kiln again", extrepoConfig)
	}
	last, commented := start, -1
	for i := start + 1; i < len(lines); i++ {
		m := extrepoItem.FindStringSubmatch(lines[i])
		if m == nil {
			break
		}
		if m[2] == policy {
			if m[1] == "" {
				return nil // already allowed
			}
			commented = i
		}
		last = i
	}
	if commented >= 0 {
		lines[commented] = "- " + policy
	} else {
		lines = append(lines[:last+1], append([]string{"- " + policy}, lines[last+1:]...)...)
	}
	return c.Write(extrepoConfig, 0o644, strings.Join(lines, "\n")+"\n")
}
