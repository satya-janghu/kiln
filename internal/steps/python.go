package steps

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/satya-janghu/kiln/internal/run"
)

var (
	pythonPackages = []string{"python3", "python3-venv", "python3-dev", "pipx", "libxml2-dev", "libxslt1-dev", "libffi-dev"}
	venvPackages   = []string{
		"requests", "httpx[http2]", "aiohttp", "tenacity", "beautifulsoup4", "lxml", "selectolax", "parsel",
		"scrapy", "playwright", "selenium", "pandas", "pyyaml", "python-dotenv", "rich", "typer", "ipython",
	}
	uvTools = []string{"ruff", "pre-commit", "httpie"}
)

const pythonDropIn = `case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) PATH="$HOME/.local/bin:$PATH" ;; esac
alias pyauto='. "$HOME/.venvs/automation/bin/activate"'
`

func doPython(c *Ctx) error {
	var err error
	u := c.U
	uv := filepath.Join(u.Home, ".local", "bin", "uv")
	venv := filepath.Join(u.Home, ".venvs", "automation")
	playwright := filepath.Join(venv, "bin", "playwright")

	// 1. Debian's packages.
	if err = c.Install(pythonPackages...); err != nil {
		return err
	}

	// 2. uv (not packaged in trixie), as the user.
	if err = c.AsUser(u, "pipx", "install", "uv"); err != nil {
		return fmt.Errorf("`pipx install uv` failed; check the network connection and run kiln again: %w", err)
	}

	// 3. The automation venv and its libraries.
	if !run.Exists(filepath.Join(venv, "pyvenv.cfg")) {
		if err = c.AsUser(u, "mkdir", "-p", filepath.Dir(venv)); err != nil {
			return err
		}
		if err = c.AsUser(u, uv, "venv", "--python", "/usr/bin/python3", venv); err != nil {
			return fmt.Errorf("could not create the venv %s: %w", venv, err)
		}
	}
	if err = c.AsUser(u, append([]string{uv, "pip", "install", "--python", venv}, venvPackages...)...); err != nil {
		return fmt.Errorf("installing the Python libraries into %s failed; check the network connection and run kiln again: %w", venv, err)
	}

	// 4. Playwright's Chromium. kiln installs the system libraries it lists
	// (Playwright never runs apt here).
	missing, err := playwrightMissing(c, playwright)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		if err = c.Install(missing...); err != nil {
			return fmt.Errorf("could not install the system libraries Playwright's Chromium needs: %w", err)
		}
	}
	if err = c.AsUser(u, playwright, "install", "chromium"); err != nil {
		return fmt.Errorf("downloading Playwright's Chromium failed; check the network connection and run kiln again: %w", err)
	}

	// 5. uv tools, one per call.
	have := installedUVTools(c, uv)
	for _, t := range uvTools {
		if have[pyName(t)] {
			continue
		}
		if err = c.AsUser(u, uv, "tool", "install", t); err != nil {
			return fmt.Errorf("`uv tool install %s` failed; check the network connection and run kiln again: %w", t, err)
		}
	}

	// 6. Shell setup.
	if err = userDropIn(c, "50-kiln-python.sh", pythonDropIn); err != nil {
		return err
	}

	// Check.
	out, err := c.Exec(quietCmd(run.UserCmd(u, uv, "pip", "list", "--python", venv)))
	if err != nil {
		return fmt.Errorf("`uv pip list` fails for %s: %w", venv, err)
	}
	inVenv := map[string]bool{}
	for _, l := range run.Scan(out) {
		if f := strings.Fields(l); len(f) >= 2 && f[0] != "Package" && !strings.HasPrefix(f[0], "-") {
			inVenv[pyName(f[0])] = true
		}
	}
	var lacking []string
	for _, p := range venvPackages {
		if !inVenv[pyName(p)] {
			lacking = append(lacking, p)
		}
	}
	if len(lacking) > 0 {
		return fmt.Errorf("missing from %s: %s; see %s", venv, strings.Join(lacking, " "), run.LogPath)
	}
	if m, perr := playwrightMissing(c, playwright); perr != nil || len(m) > 0 {
		return fmt.Errorf("playwright still reports missing system libraries (%s); run `%s install-deps --dry-run chromium` to see them", strings.Join(m, " "), playwright)
	}
	if found, _ := filepath.Glob(filepath.Join(u.Home, ".cache", "ms-playwright", "chromium-*")); len(found) == 0 {
		return fmt.Errorf("playwright's Chromium isn't in ~/.cache/ms-playwright; run `%s install chromium` as %s", playwright, u.Name)
	}
	have = installedUVTools(c, uv)
	for _, t := range uvTools {
		if !have[pyName(t)] {
			return fmt.Errorf("`uv tool list` doesn't show %s; run `uv tool install %s` as %s", t, t, u.Name)
		}
	}
	return nil
}

// playwrightMissing runs `playwright install-deps --dry-run chromium` as the
// user. Exit 0 means nothing is missing; exit 1 prints
// "Missing system dependencies (N):" and one indented package per line.
func playwrightMissing(c *Ctx, playwright string) ([]string, error) {
	out, err := c.Exec(quietCmd(run.UserCmd(c.U, playwright, "install-deps", "--dry-run", "chromium")))
	if err == nil {
		return nil, nil
	}
	var ee *run.ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		return nil, fmt.Errorf("checking the system libraries Playwright's Chromium needs failed: %w", err)
	}
	var pkgs []string
	in := false
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "Missing system dependencies"):
			in = true
		case !in || t == "":
		case l == t: // not indented: the list has ended
			in = false
		default:
			pkgs = append(pkgs, strings.Fields(t)[0])
		}
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("playwright reported missing system libraries but kiln couldn't read the list; run `%s install-deps --dry-run chromium` to see it", playwright)
	}
	return pkgs, nil
}

// installedUVTools parses `uv tool list`: tool lines look like "ruff v0.x.y",
// their binaries like "- ruff".
func installedUVTools(c *Ctx, uv string) map[string]bool {
	have := map[string]bool{}
	out, err := c.Exec(quietCmd(run.UserCmd(c.U, uv, "tool", "list")))
	if err != nil {
		return have
	}
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) > 0 && !strings.HasPrefix(l, "-") && !strings.HasPrefix(l, " ") {
			have[pyName(f[0])] = true
		}
	}
	return have
}

var pySep = regexp.MustCompile(`[-_.]+`)

// pyName is PEP 503 name normalisation, without extras ("httpx[http2]" → "httpx").
func pyName(n string) string {
	n, _, _ = strings.Cut(n, "[")
	return pySep.ReplaceAllString(strings.ToLower(n), "-")
}
