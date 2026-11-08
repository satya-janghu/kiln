// Command kiln sets up a fresh Debian 13 install in one go (docs/product-spec.md).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/satya-janghu/kiln/internal/detect"
	"github.com/satya-janghu/kiln/internal/run"
	"github.com/satya-janghu/kiln/internal/tui"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	for _, a := range os.Args[1:] {
		switch a {
		case "--version", "-v", "version":
			fmt.Printf("kiln %s (%s)\n", version, commit)
			return
		case "--help", "-h", "help":
			fmt.Println("kiln sets up a fresh Debian 13 install. Run it as root at the machine's console, with no arguments.")
			return
		}
	}
	os.Exit(start())
}

func start() int {
	// Never change anything on a development machine (CLAUDE.md).
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "kiln runs only on Debian 13 (Linux). Nothing was changed.")
		return 1
	}
	// Plain `su` keeps the user's PATH, which lacks the sbin directories.
	if p := os.Getenv("PATH"); !strings.Contains(":"+p+":", ":/usr/sbin:") {
		_ = os.Setenv("PATH", "/usr/local/sbin:/usr/sbin:/sbin:"+p)
	}

	f := detect.Gather()
	switch {
	case !f.Root:
		fmt.Fprintln(os.Stderr, "kiln must run as root. Log in as root or use `su -` (or sudo), then run it again.")
		return 1
	case f.OSID != "debian" || f.OSVersion != "13":
		fmt.Fprintf(os.Stderr, "kiln runs only on Debian 13 (trixie). This system reports ID=%s VERSION_ID=%s. Nothing was changed.\n", f.OSID, f.OSVersion)
		return 1
	case f.Arch != "amd64":
		fmt.Fprintf(os.Stderr, "kiln runs only on amd64 (64-bit PC) machines. This one is %s. Nothing was changed.\n", f.Arch)
		return 1
	case f.User == nil:
		fmt.Fprintf(os.Stderr, "kiln can't continue: %s.\n", f.UserProblem)
		return 1
	}

	logf, err := run.OpenLog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kiln can't write its log %s: %v\n", run.LogPath, err)
		return 1
	}
	defer func() { _ = logf.Close() }()

	res, err := tui.Run(f, logf, version)
	if len(res.Summary) > 0 {
		_, _ = fmt.Fprintln(logf, "---- summary ----")
		for _, l := range res.Summary {
			_, _ = fmt.Fprintln(logf, l)
		}
		for _, l := range res.Summary {
			fmt.Println(l)
		}
		fmt.Printf("\nThe full log is in %s.\n", run.LogPath)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "kiln's screen stopped: %v. Everything that ran is in %s.\n", err, run.LogPath)
		return 1
	}
	if res.Reboot {
		_ = logf.Sync()
		// install.sh can't remove its temporary directory when the machine
		// reboots under it, so kiln does.
		if dir := filepath.Dir(os.Args[0]); strings.HasPrefix(dir, "/var/tmp/kiln.") {
			_ = os.RemoveAll(dir) // #nosec G703 -- only install.sh's own /var/tmp/kiln.* directory
		}
		if err := tui.Reboot(); err != nil {
			fmt.Fprintf(os.Stderr, "Reboot failed: %v. Run `systemctl reboot` yourself.\n", err)
			return 1
		}
	}
	return 0
}
