// Package cli defines kiln's command-line interface.
//
// The command tree matches docs/product-spec.md. Commands whose behavior has not
// been built yet return an error naming the milestone that delivers them
// (docs/roadmap.md), so nothing ever pretends to have done work.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// BuildInfo identifies the running binary. It is filled in from -ldflags.
type BuildInfo struct {
	Version string
	Commit  string
}

// Execute runs kiln with the process arguments and returns the exit code.
func Execute(info BuildInfo) int {
	return run(info, os.Args[1:], os.Stdout, os.Stderr)
}

func run(info BuildInfo, args []string, stdout, stderr io.Writer) int {
	root := newRootCmd(info)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.Execute(); err != nil {
		// Errors are printed once, here, so every failure looks the same.
		// If stderr itself can't be written there is nowhere left to report that,
		// and the non-zero exit code still signals the failure.
		_, _ = fmt.Fprintf(stderr, "kiln: %v\n", err)
		return 1
	}
	return 0
}

// wizardFlags are shared by `kiln` and `kiln wizard`, which are the same command.
type wizardFlags struct {
	freshMachine bool
}

func addWizardFlags(cmd *cobra.Command, f *wizardFlags) {
	cmd.Flags().BoolVar(&f.freshMachine, "i-know-this-is-a-fresh-machine", false,
		"allow applying changes even though /etc/kiln/allow-apply does not exist")
}

func newRootCmd(info BuildInfo) *cobra.Command {
	var wf wizardFlags
	root := &cobra.Command{
		Use:   "kiln",
		Short: "Set up a freshly installed Debian 13 machine",
		Long: "kiln turns a fresh Debian 13 (trixie) amd64 install into a configured, always-on machine.\n" +
			"Run it at the machine's console. With no subcommand it starts the setup wizard.",
		Version:       fmt.Sprintf("%s (commit %s)", info.Version, info.Commit),
		Args:          cobra.NoArgs,
		SilenceUsage:  true, // a runtime error is not a usage error; don't dump help
		SilenceErrors: true, // run() prints the error
		RunE:          notImplemented("M2"),
	}
	addWizardFlags(root, &wf)
	// Keep the command surface exactly as documented in docs/product-spec.md.
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newWizardCmd(),
		newPlanCmd(),
		newFactsCmd(),
		newBatteryCmd(),
		newThermalCmd(),
		newKernelCmd(),
		newMirrorsCmd(),
		newSecretsCmd(),
		newCatalogCmd(),
	)
	return root
}

// notImplemented returns a RunE that fails with a plain-language message naming
// the milestone (docs/roadmap.md) that will deliver the command.
func notImplemented(milestone string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		return fmt.Errorf("%q is not built yet; it arrives in milestone %s (see docs/roadmap.md)",
			cmd.CommandPath(), milestone)
	}
}
