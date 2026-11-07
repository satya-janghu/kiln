package cli

import "github.com/spf13/cobra"

// Each constructor below matches a row of the Commands table in docs/product-spec.md.
// Flags are declared now so the command surface is reviewable; behavior arrives with
// the milestone named in notImplemented.

func newWizardCmd() *cobra.Command {
	var wf wizardFlags
	cmd := &cobra.Command{
		Use:   "wizard",
		Short: "Start the setup wizard (same as running kiln with no subcommand)",
		Args:  cobra.NoArgs,
		RunE:  notImplemented("M2"),
	}
	addWizardFlags(cmd, &wf)
	return cmd
}

func newPlanCmd() *cobra.Command {
	var skip []string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what kiln would change on this machine, without changing anything",
		Args:  cobra.NoArgs,
		RunE:  notImplemented("M1"),
	}
	cmd.Flags().StringSliceVar(&skip, "skip", nil, "step IDs to leave out of the plan (comma-separated)")
	return cmd
}

func newFactsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "facts",
		Short: "Show what kiln detected: hardware, installer choices and network",
		Args:  cobra.NoArgs,
		RunE:  notImplemented("M1"),
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func newBatteryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "battery",
		Short: "Battery charge-limit helper",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show batteries, charge-control interfaces, current limits and who manages them",
			Args:  cobra.NoArgs,
			RunE:  notImplemented("M3"),
		},
		&cobra.Command{
			Use:   "apply",
			Short: "Re-apply the charge limit chosen in the wizard (used at boot and on power events)",
			Args:  cobra.NoArgs,
			RunE:  notImplemented("M3"),
		},
		&cobra.Command{
			Use:   "watch",
			Short: "Software charge limit for batteries without thresholds (run by a systemd unit)",
			Args:  cobra.NoArgs,
			RunE:  notImplemented("M3"),
		},
	)
	return cmd
}

func newThermalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thermal",
		Short: "Thermal helper",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "apply",
		Short: "Re-apply the performance platform profile at boot (machines using tuned)",
		Args:  cobra.NoArgs,
		RunE:  notImplemented("M3"),
	})
	return cmd
}

func newKernelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kernel",
		Short: "Kernel helper",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "fallback",
		Short: "Keep the newest stable 6.12 kernel installed next to the backports kernel",
		Args:  cobra.NoArgs,
		RunE:  notImplemented("M3"),
	})
	return cmd
}

func newMirrorsCmd() *cobra.Command {
	var apply bool
	bench := &cobra.Command{
		Use:   "bench",
		Short: "Rank Debian mirrors by download speed",
		Args:  cobra.NoArgs,
		RunE:  notImplemented("M3"),
	}
	bench.Flags().BoolVar(&apply, "apply", false, "write the fastest mirrors to the APT mirror list")

	cmd := &cobra.Command{
		Use:   "mirrors",
		Short: "Debian mirror helper",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		bench,
		&cobra.Command{
			Use:   "reset",
			Short: "Go back to deb.debian.org",
			Args:  cobra.NoArgs,
			RunE:  notImplemented("M3"),
		},
	)
	return cmd
}

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Stored secrets (root only)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show [name]",
		Short: "Print a stored secret, such as the remote-desktop password",
		Args:  cobra.MaximumNArgs(1),
		RunE:  notImplemented("M3"),
	})
	return cmd
}

// newCatalogCmd is hidden: it exists for /check-packages, not for everyday use.
func newCatalogCmd() *cobra.Command {
	var all bool
	var step string
	packages := &cobra.Command{
		Use:   "packages",
		Short: "List every package a step can declare, with suite, component and repo",
		Args:  cobra.NoArgs,
		RunE:  notImplemented("M1"),
	}
	packages.Flags().BoolVar(&all, "all", false, "list packages for every step")
	packages.Flags().StringVar(&step, "step", "", "list packages for one step ID")
	packages.MarkFlagsMutuallyExclusive("all", "step")
	packages.MarkFlagsOneRequired("all", "step")

	cmd := &cobra.Command{
		Use:    "catalog",
		Short:  "Inspect the steps catalog",
		Hidden: true,
		Args:   cobra.NoArgs,
	}
	cmd.AddCommand(packages)
	return cmd
}
