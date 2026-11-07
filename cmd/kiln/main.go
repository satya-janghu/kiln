// Command kiln sets up a freshly installed Debian 13 machine.
// See docs/product-spec.md for what it does and docs/architecture.md for how.
package main

import (
	"os"

	"github.com/satya-janghu/kiln/internal/cli"
)

// version and commit are set at release time with -ldflags (see Makefile and .goreleaser.yaml).
var (
	version = "dev"
	commit  = "none"
)

func main() {
	os.Exit(cli.Execute(cli.BuildInfo{Version: version, Commit: commit}))
}
