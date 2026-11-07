package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/satya-janghu/kiln/internal/app"
	"github.com/satya-janghu/kiln/internal/battery"
	"github.com/satya-janghu/kiln/internal/mirrors"
	"github.com/satya-janghu/kiln/internal/options"
	"github.com/satya-janghu/kiln/internal/runner"
	"github.com/satya-janghu/kiln/internal/secrets"
	"github.com/satya-janghu/kiln/internal/thermal"
	"github.com/satya-janghu/kiln/internal/tui"
)

func runWizard(ctx context.Context, info BuildInfo, force bool) error {
	app.EnsureScope()
	a, err := app.Load(ctx, info.Version, true)
	if err != nil {
		return err
	}
	defer a.Close()
	return tui.Run(ctx, a, force)
}

func newWizardCmd(info BuildInfo) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "wizard",
		Short: "Start the setup wizard (same as running kiln with no subcommand)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runWizard(cmd.Context(), info, force) },
	}
	cmd.Flags().BoolVar(&force, "i-know-this-is-a-fresh-machine", false,
		"allow applying changes even though /etc/kiln/allow-apply does not exist")
	return cmd
}

func newPlanCmd(info BuildInfo) *cobra.Command {
	var skip []string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what kiln would change on this machine, without changing anything",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := app.Load(cmd.Context(), info.Version, false)
			if err != nil {
				return err
			}
			defer a.Close()
			for _, id := range skip {
				if _, ok := a.Opts.Enabled[id]; !ok {
					return fmt.Errorf("unknown step %q (see `kiln catalog steps`)", id)
				}
				a.Opts.Enabled[id] = false
			}
			pv := a.Runner(nil).Preview(cmd.Context())
			printPreview(cmd, pv)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&skip, "skip", nil, "step IDs to leave out of the plan (comma-separated)")
	return cmd
}

func printPreview(cmd *cobra.Command, pv *runner.Preview) {
	out := cmd.OutOrStdout()
	if len(pv.Warnings) > 0 {
		_, _ = fmt.Fprintln(out, "Warnings:")
		for _, w := range pv.Warnings {
			_, _ = fmt.Fprintln(out, "  ! "+w)
		}
		_, _ = fmt.Fprintln(out)
	}
	for _, s := range pv.Steps {
		status := string(s.Status)
		if s.Status == runner.Done {
			status = "will change"
		}
		line := fmt.Sprintf("%-12s %s", "["+status+"]", s.Title)
		if s.Reason != "" && s.Status != runner.Done {
			line += ": " + s.Reason
		}
		_, _ = fmt.Fprintln(out, line)
		for _, act := range s.Actions {
			_, _ = fmt.Fprintln(out, "    - "+act)
		}
	}
	if len(pv.Reboot) > 0 {
		_, _ = fmt.Fprintln(out, "\nReboot likely: "+strings.Join(pv.Reboot, "; "))
	}
}

func newFactsCmd(info BuildInfo) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "facts",
		Short: "Show what kiln detected: hardware, installer choices and network",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := app.Load(cmd.Context(), info.Version, false)
			if err != nil {
				return err
			}
			defer a.Close()
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(a.Facts)
			}
			_, err = fmt.Fprint(out, FactsText(a))
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

// FactsText renders facts as plain text.
func FactsText(a *app.App) string {
	f := a.Facts
	var b strings.Builder
	w := func(k, v string) { fmt.Fprintf(&b, "%-14s %s\n", k, v) }
	w("System", strings.TrimSpace(f.OS.Pretty+" "+f.Arch+", kernel "+f.Kernel))
	w("Machine", f.MachineName())
	kind := "desktop"
	if f.Laptop {
		kind = "laptop"
	}
	w("Form factor", kind)
	w("CPU", fmt.Sprintf("%s (%s), %d threads", f.CPU.Model, f.CPU.Vendor, f.CPU.Cores))
	w("RAM", fmt.Sprintf("%.1f GiB", float64(f.RAMBytes)/(1<<30)))
	var gpus []string
	for _, g := range f.GPUs {
		gpus = append(gpus, fmt.Sprintf("%s:%s (%s)", g.Vendor, g.Device, g.Driver))
	}
	w("GPUs", strings.Join(gpus, ", "))
	w("Secure Boot", f.SecureBoot)
	w("Desktop", map[bool]string{true: "GNOME", false: "none"}[f.GNOME])
	w("SSH server", map[bool]string{true: "installed", false: "not installed"}[f.SSHServer])
	if len(f.Blends) > 0 {
		w("Blends", strings.Join(f.Blends, ", "))
	}
	if f.User != nil {
		w("Target user", fmt.Sprintf("%s (uid %d)", f.User.Name, f.User.UID))
	} else {
		w("Target user", f.UserProblem)
	}
	for _, n := range f.NICs {
		var addrs []string
		for _, p := range n.Addrs {
			addrs = append(addrs, p.String())
		}
		w("Network", fmt.Sprintf("%s %s (%s)", n.Name, strings.Join(addrs, " "), n.ManagedBy))
	}
	w("LAN subnets", strings.Join(f.LANStrings(), ", "))
	if len(f.Batteries) > 0 {
		w("Battery", fmt.Sprintf("%d%%, on AC: %v", f.BatteryPercent, f.OnAC))
	}
	if f.PlatformProfile != "" {
		w("Platform", f.PlatformProfile+" (choices: "+strings.Join(f.PlatformProfileChoices, " ")+")")
	}
	if f.Virt != "none" && f.Virt != "" {
		w("Virtualized", f.Virt)
	}
	return b.String()
}

var errLockBusy = errors.New("lock busy")

// helperApp loads an app for systemd/udev helpers, under the helper's lock (D41).
func helperApp(cmd *cobra.Command, info BuildInfo, lock string) (*app.App, func(), error) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return nil, nil, errors.New("this helper runs as root on the Debian machine")
	}
	release, pid, err := runner.Lock(lock)
	if errors.Is(err, runner.ErrBusy) {
		// Another kiln (pid) is already doing this; it applies the same values,
		// so the helper exits quietly with success (D41).
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "kiln: another kiln (pid %d) is already doing this; nothing to do\n", pid)
		return nil, nil, errLockBusy
	}
	if err != nil {
		return nil, nil, err
	}
	a, err := app.Load(cmd.Context(), info.Version, true)
	if err != nil {
		release()
		return nil, nil, err
	}
	return a, func() { a.Close(); release() }, nil
}

func newBatteryCmd(info BuildInfo) *cobra.Command {
	cmd := &cobra.Command{Use: "battery", Short: "Battery charge-limit helper", Args: cobra.NoArgs}
	cmd.AddCommand(
		&cobra.Command{
			Use: "status", Short: "Show batteries, charge-control interfaces, current limits and who manages them", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				a, err := app.Load(cmd.Context(), info.Version, false)
				if err != nil {
					return err
				}
				defer a.Close()
				txt, err := battery.Status(cmd.Context(), a.Real, a.Facts, a.State)
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(cmd.OutOrStdout(), txt)
				return err
			},
		},
		&cobra.Command{
			Use: "apply", Short: "Re-apply the charge limit chosen in the wizard (used at boot and on power events)", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				a, done, err := helperApp(cmd, info, "battery")
				if errors.Is(err, errLockBusy) {
					return nil
				}
				if err != nil {
					return err
				}
				defer done()
				return battery.ApplyRecorded(cmd.Context(), a.Real, a.Facts, a.State)
			},
		},
		&cobra.Command{
			Use: "watch", Short: "Software charge limit for batteries without thresholds (run by a systemd unit)", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if runtime.GOOS != "linux" || os.Geteuid() != 0 {
					return errors.New("this helper runs as root on the Debian machine")
				}
				a, err := app.Load(cmd.Context(), info.Version, true)
				if err != nil {
					return err
				}
				defer a.Close()
				return battery.Watch(cmd.Context(), a.Real, a.Facts, a.State)
			},
		},
	)
	return cmd
}

func newThermalCmd(info BuildInfo) *cobra.Command {
	cmd := &cobra.Command{Use: "thermal", Short: "Thermal helper", Args: cobra.NoArgs}
	cmd.AddCommand(&cobra.Command{
		Use: "apply", Short: "Validate and enable thinkfan at boot once ThinkPad fan control is active", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, done, err := helperApp(cmd, info, "thermal")
			if errors.Is(err, errLockBusy) {
				return nil
			}
			if err != nil {
				return err
			}
			defer done()
			return thermal.BootApply(cmd.Context(), a.Real, a.Facts, a.State)
		},
	})
	return cmd
}

func newMirrorsCmd(info BuildInfo) *cobra.Command {
	var apply bool
	bench := &cobra.Command{
		Use: "bench", Short: "Rank Debian mirrors by download speed", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := app.Load(cmd.Context(), info.Version, apply)
			if err != nil {
				return err
			}
			defer a.Close()
			if apply {
				if ok, why := a.Gate(true); !ok {
					return errors.New(why)
				}
			}
			v := a.Opts.For("mirrors")
			return mirrors.Bench(cmd.Context(), a.Real, a.Facts, mirrors.BenchOpts{
				Country: v.String("country"), Neighbors: v.Bool("neighbors"), Sample: v.Int("sample"),
				Top: v.Int("top"), TestBytes: int64(v.Int("test_mb")) << 20, Timeout: v.Int("test_seconds"),
				Apply: apply, Say: func(s string) { _, _ = fmt.Fprintln(cmd.OutOrStdout(), s) },
			})
		},
	}
	bench.Flags().BoolVar(&apply, "apply", false, "write the fastest mirrors to the APT mirror list")
	cmd := &cobra.Command{Use: "mirrors", Short: "Debian mirror helper", Args: cobra.NoArgs}
	cmd.AddCommand(bench, &cobra.Command{
		Use: "reset", Short: "Go back to deb.debian.org", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := app.Load(cmd.Context(), info.Version, true)
			if err != nil {
				return err
			}
			defer a.Close()
			if ok, why := a.Gate(true); !ok {
				return errors.New(why)
			}
			return mirrors.Reset(cmd.Context(), a.Real)
		},
	})
	return cmd
}

func newSecretsCmd(info BuildInfo) *cobra.Command {
	cmd := &cobra.Command{Use: "secrets", Short: "Stored secrets (root only)", Args: cobra.NoArgs}
	cmd.AddCommand(&cobra.Command{
		Use: "show [name]", Short: "Print a stored secret, such as the Secure Boot one-time password", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if os.Geteuid() != 0 {
				return errors.New("only root can read stored secrets: sudo kiln secrets show")
			}
			a, err := app.Load(cmd.Context(), info.Version, false)
			if err != nil {
				return err
			}
			defer a.Close()
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				ents, _ := a.Real.ReadDir(secrets.Dir)
				if len(ents) == 0 {
					_, werr := fmt.Fprintln(out, "No stored secrets.")
					return werr
				}
				for _, e := range ents {
					_, _ = fmt.Fprintln(out, e.Name())
				}
				return nil
			}
			v, lerr := secrets.Load(a.Real, args[0])
			if lerr != nil {
				return lerr
			}
			_, err = fmt.Fprintln(out, v)
			return err
		},
	})
	return cmd
}

// newCatalogCmd is hidden: it exists for /check-packages.
func newCatalogCmd(info BuildInfo) *cobra.Command {
	var all bool
	var only string
	packages := &cobra.Command{
		Use: "packages", Short: "List every package a step can declare, with suite, component and repo", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := app.Load(cmd.Context(), info.Version, false)
			if err != nil {
				return err
			}
			defer a.Close()
			var lines []string
			for _, s := range a.Steps {
				if !all && s.ID() != only {
					continue
				}
				for _, p := range s.Packages() {
					lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s\t%s", s.ID(), p.Name, p.Suite, p.Component, p.Repo))
				}
			}
			sort.Strings(lines)
			_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
			return err
		},
	}
	packages.Flags().BoolVar(&all, "all", false, "list packages for every step")
	packages.Flags().StringVar(&only, "step", "", "list packages for one step ID")
	packages.MarkFlagsMutuallyExclusive("all", "step")
	packages.MarkFlagsOneRequired("all", "step")
	steps := &cobra.Command{
		Use: "steps", Short: "List step IDs in catalog order", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := app.Load(cmd.Context(), info.Version, false)
			if err != nil {
				return err
			}
			defer a.Close()
			for _, s := range a.Steps {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-18s %-9s %s\n", s.ID(), s.Category(), s.Title())
			}
			return nil
		},
	}
	opts := &cobra.Command{
		Use: "options", Short: "List every option key with its type and default (for docs/options-reference.md)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := app.Load(cmd.Context(), info.Version, false)
			if err != nil {
				return err
			}
			defer a.Close()
			kinds := map[options.Kind]string{options.Bool: "bool", options.String: "string", options.Int: "int",
				options.Choice: "choice", options.Secret: "secret", options.List: "list"}
			out := cmd.OutOrStdout()
			for _, s := range a.Steps {
				on := "on"
				if !a.Defaults.IsEnabled(s.ID()) {
					on = "off"
				}
				_, _ = fmt.Fprintf(out, "%s\t(step)\t%s\t%s\n", s.ID(), "bool", on)
				for _, f := range s.Options(a.Facts) {
					def := fmt.Sprint(f.Default)
					if f.Kind == options.Secret {
						def = "(empty)"
					}
					if f.Kind == options.Choice {
						var cs []string
						for _, c := range f.Choices {
							cs = append(cs, c.Value)
						}
						def += " [" + strings.Join(cs, "|") + "]"
					}
					_, _ = fmt.Fprintf(out, "%s.%s\t%s\t%s\t%s\n", s.ID(), f.Key, f.Label, kinds[f.Kind], def)
				}
			}
			return nil
		},
	}
	cmd := &cobra.Command{Use: "catalog", Short: "Inspect the steps catalog", Hidden: true, Args: cobra.NoArgs}
	cmd.AddCommand(packages, steps, opts)
	return cmd
}
