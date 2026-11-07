// Package cli defines kiln's command-line interface (docs/product-spec.md).
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

// BuildInfo identifies the running binary. It is filled in from -ldflags.
type BuildInfo struct {
	Version string
	Commit  string
}

// Execute runs kiln with the process arguments and returns the exit code.
func Execute(info BuildInfo) int {
	fixRootPath()
	return run(info, os.Args[1:], os.Stdout, os.Stderr)
}

// fixRootPath: plain `su` (not `su -`) keeps the user's PATH, which on Debian
// has no sbin directories, so sshd, visudo, chpasswd and adduser wouldn't be
// found. Add them before anything runs (facts, the scope re-exec, steps), and
// leave a marker so facts.ViaPlainSu still reports it after the re-exec.
func fixRootPath() {
	if os.Geteuid() != 0 {
		return
	}
	path := os.Getenv("PATH")
	if strings.Contains(":"+path+":", ":/usr/sbin:") {
		return
	}
	_ = os.Setenv("KILN_PLAIN_SU", "1")
	_ = os.Setenv("PATH", "/usr/local/sbin:/usr/sbin:/sbin:"+path)
}

func run(info BuildInfo, args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	root := newRootCmd(info)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		// If stderr itself can't be written there is nowhere left to report that;
		// the non-zero exit code still signals the failure.
		_, _ = fmt.Fprintf(stderr, "kiln: %v\n", err)
		return 1
	}
	return 0
}

func newRootCmd(info BuildInfo) *cobra.Command {
	var force bool
	root := &cobra.Command{
		Use:   "kiln",
		Short: "Set up a freshly installed Debian 13 machine",
		Long: "kiln turns a fresh Debian 13 (trixie) amd64 install into a configured, always-on machine.\n" +
			"Run it at the machine's console. With no subcommand it starts the setup wizard.",
		Version:       fmt.Sprintf("%s (commit %s)", info.Version, info.Commit),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return runWizard(cmd.Context(), info, force) },
	}
	root.Flags().BoolVar(&force, "i-know-this-is-a-fresh-machine", false,
		"allow applying changes even though /etc/kiln/allow-apply does not exist")
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(
		newWizardCmd(info),
		newPlanCmd(info),
		newFactsCmd(info),
		newBatteryCmd(info),
		newThermalCmd(info),
		newMirrorsCmd(info),
		newSecretsCmd(info),
		newCatalogCmd(info),
	)
	return root
}
