package steps

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

const (
	goRoot     = "/usr/local/go"
	goNew      = "/usr/local/go.kiln-new"
	goOld      = "/usr/local/go.kiln-old"
	goPathLine = "export PATH=/usr/local/go/bin:$PATH:$HOME/go/bin\n"
)

// goTools are installed with one `go install <path>@latest` each (a single
// call can't take packages from several modules).
var goTools = [][2]string{
	{"gopls", "golang.org/x/tools/gopls"},
	{"dlv", "github.com/go-delve/delve/cmd/dlv"},
	{"staticcheck", "honnef.co/go/tools/cmd/staticcheck"},
	{"govulncheck", "golang.org/x/vuln/cmd/govulncheck"},
}

type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Files   []struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		SHA256   string `json:"sha256"`
		Kind     string `json:"kind"`
	} `json:"files"`
}

func doGo(c *Ctx) error {
	u := c.U

	// 1. The newest stable release on go.dev.
	version, file, sha, reachable, err := latestGo(c)
	if err != nil {
		return err
	}

	goCmd, env := goRoot+"/bin/go", []string(nil)
	if reachable {
		// 2. Download, verify, extract next to /usr/local/go, swap in by rename.
		if firstLine(run.Read(goRoot+"/VERSION")) != version {
			if err := installGoTree(c, version, file, sha); err != nil {
				return err
			}
		}
	} else {
		// 5. go.dev unreachable: Debian's Go from backports.
		if err := c.InstallOpts(run.InstallOpts{Suite: "trixie-backports"}, "golang-go"); err != nil {
			return fmt.Errorf("go.dev can't be reached and installing Debian's golang-go failed: %w", err)
		}
		c.Note("go.dev couldn't be reached, so kiln installed Debian's golang-go from trixie-backports instead.")
		goCmd, env = "/usr/bin/go", []string{"GOTOOLCHAIN=local"}
	}

	// 3. PATH for login shells and for the user's bash.
	if err := c.Write("/etc/profile.d/kiln-go.sh", 0o644, "# Managed by kiln\n"+goPathLine); err != nil {
		return err
	}
	if err := userDropIn(c, "50-kiln-go.sh", goPathLine); err != nil {
		return err
	}

	// 4. The tools, as the user.
	skipped := map[string]bool{}
	for _, t := range goTools {
		args := append(append([]string{}, env...), goCmd, "install", t[1]+"@latest")
		_, err := c.Exec(run.UserCmd(u, args...))
		var ee *run.ExitError
		if !reachable && errors.As(err, &ee) && strings.Contains(ee.Output, "requires go >=") {
			skipped[t[0]] = true
			c.Note("Skipped the Go tool %s: it needs a newer Go than Debian's golang-go.", t[0])
			continue
		}
		if err != nil {
			return fmt.Errorf("`go install %s@latest` failed; check the network connection and run kiln again: %w", t[1], err)
		}
	}

	// Check.
	if reachable {
		out, _ := c.Exec(run.Cmd{Name: goRoot + "/bin/go", Args: []string{"version"}, Env: []string{"GOTOOLCHAIN=local"}, Dir: "/", Quiet: true})
		if f := strings.Fields(out); len(f) < 3 || f[2] != version {
			return fmt.Errorf("%s/bin/go reports %q, not %s; remove %s and run kiln again", goRoot, strings.TrimSpace(out), version, goRoot)
		}
	} else if !run.Installed(c.S, "golang-go") {
		return errors.New("golang-go isn't installed; see " + run.LogPath)
	}
	for _, t := range goTools {
		p := filepath.Join(u.Home, "go", "bin", t[0])
		if !skipped[t[0]] && !run.Exists(p) {
			return fmt.Errorf("%s is missing after `go install`; see %s", p, run.LogPath)
		}
	}
	return nil
}

// latestGo reads go.dev's download index. reachable is false (with no error)
// when go.dev can't be reached, which selects Debian's Go.
func latestGo(c *Ctx) (version, file, sha string, reachable bool, err error) {
	b, gerr := c.Get("https://go.dev/dl/?mode=json", 16<<20)
	if gerr != nil {
		c.Say("go.dev can't be reached (%v); using Debian's Go instead.", gerr)
		return "", "", "", false, nil
	}
	var rels []goRelease
	if err := json.Unmarshal(b, &rels); err != nil {
		return "", "", "", true, fmt.Errorf("go.dev's download list couldn't be read (%w); try again later", err)
	}
	for _, r := range rels {
		if !r.Stable {
			continue
		}
		for _, f := range r.Files {
			if f.Kind == "archive" && f.OS == "linux" && f.Arch == "amd64" && f.SHA256 != "" {
				return r.Version, f.Filename, f.SHA256, true, nil
			}
		}
		break // only the first stable release counts
	}
	return "", "", "", true, errors.New("go.dev lists no stable linux-amd64 archive; try again later")
}

// installGoTree puts a verified Go release at /usr/local/go. A run that
// stopped halfway leaves go.kiln-new or go.kiln-old, which are cleaned up here.
func installGoTree(c *Ctx, version, file, sha string) error {
	if !run.Exists(goRoot) && run.Exists(goOld+"/VERSION") {
		if err := os.Rename(goOld, goRoot); err != nil {
			return fmt.Errorf("could not put %s back as %s: %w", goOld, goRoot, err)
		}
	}
	for _, d := range []string{goNew, goOld} {
		if err := os.RemoveAll(d); err != nil {
			return fmt.Errorf("could not remove the leftover %s: %w", d, err)
		}
	}
	tgz, err := c.Fetch("https://go.dev/dl/"+file, sha)
	if err != nil {
		return fmt.Errorf("could not download Go %s: %w", version, err)
	}
	if err := os.MkdirAll(goNew, 0o755); err != nil {
		return err
	}
	if err := c.Run("tar", "-C", goNew, "--strip-components=1", "--no-same-owner", "-xzf", tgz); err != nil {
		_ = os.RemoveAll(goNew)
		return fmt.Errorf("could not unpack Go %s: %w", version, err)
	}
	if firstLine(run.Read(goNew+"/VERSION")) != version {
		_ = os.RemoveAll(goNew)
		return fmt.Errorf("the downloaded archive isn't Go %s; nothing was changed", version)
	}
	hadOld := run.Exists(goRoot)
	if hadOld {
		if err := os.Rename(goRoot, goOld); err != nil {
			return fmt.Errorf("could not move the old %s aside: %w", goRoot, err)
		}
	}
	if err := os.Rename(goNew, goRoot); err != nil {
		if hadOld {
			_ = os.Rename(goOld, goRoot)
		}
		return fmt.Errorf("could not put Go %s in place: %w", version, err)
	}
	c.Log("installed Go %s in %s", version, goRoot)
	if hadOld {
		return os.RemoveAll(goOld)
	}
	return nil
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(l)
}
