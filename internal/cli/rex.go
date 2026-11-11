package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tbxark/gvim/internal/rexplugin"
)

// newRexCommand returns the rex command group, wired to the real installer.
func newRexCommand() *cobra.Command {
	return newRexCommandWith(rexplugin.Installer{})
}

// newRexCommandWith returns the rex command group that installs with installer.
func newRexCommandWith(installer rexplugin.Installer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rex",
		Short: "Install gvim's Rex integration",
		Long: `Install gvim's Rex integration.

The plugin goes in Rex's config directory and adds "Gvim: Open File…" and
"Gvim: Review Diff" to Rex's command palette: gvim opens in a floating window
over the session, at the working directory of the pane the action was called
from, or on a file of it. A managed block in Rex's init.lua loads it.

"rex install" writes the plugin and reloads Rex's config; "rex uninstall" takes
it away again. A file the command did not write is never replaced or removed.`,
	}
	cmd.AddCommand(newRexInstallCommand(installer), newRexUninstallCommand(installer))
	return cmd
}

// newRexInstallCommand writes the integration and reports what it touched.
func newRexInstallCommand(installer rexplugin.Installer) *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install the Rex integration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep, err := installer.Install()
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}
			w := cmd.OutOrStdout()
			if err := writeLine(w, "Installed gvim's Rex integration."); err != nil {
				return err
			}
			if err := writeLine(w, fmt.Sprintf("  Rex plugin   %s", rep.RexPlugin)); err != nil {
				return err
			}
			if err := writeLine(w, fmt.Sprintf("  Rex config   %s%s", rep.RexConfig, reloaded(rep.Changed, rep.Reloaded))); err != nil {
				return err
			}
			if err := writeLine(w, ""); err != nil {
				return err
			}
			return writeLine(w, "In Rex: cmd+shift+p, then \"Gvim: Open File…\" (cmd+shift+e).")
		},
	}
}

// newRexUninstallCommand removes the integration and reports what it touched.
func newRexUninstallCommand(installer rexplugin.Installer) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the Rex integration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep, err := installer.Uninstall()
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}
			w := cmd.OutOrStdout()
			if err := writeLine(w, "Removed gvim's Rex integration."); err != nil {
				return err
			}
			if err := writeLine(w, fmt.Sprintf("  Rex config   %s%s", rep.RexConfig, reloaded(rep.Changed, rep.Reloaded))); err != nil {
				return err
			}
			return writeLine(w, "  plugins      removed")
		},
	}
}

// reloaded says whether the config changed and whether Rex took it.
func reloaded(changed, ok bool) string {
	switch {
	case !changed:
		return " (unchanged)"
	case ok:
		return " (reloaded)"
	default:
		return " (rex config reload did not run; reload Rex's config)"
	}
}
