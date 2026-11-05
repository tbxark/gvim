package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/tbxark/gvim/internal/app"
)

// configDeps are the functions the config commands call. The app package
// supplies the real ones, and tests pass fakes. cwd gives the workspace root.
type configDeps struct {
	cwd   func() (string, error)
	load  func(root string) (app.ConfigView, error)
	paths func(root string) ([]app.ConfigFile, error)
}

// defaultConfigDeps returns the dependencies that read the real working
// directory and the real config files.
func defaultConfigDeps() configDeps {
	return configDeps{cwd: os.Getwd, load: app.LoadConfig, paths: app.ConfigPaths}
}

// newConfigCommand returns the config command group, wired to the real
// dependencies.
func newConfigCommand() *cobra.Command {
	return newConfigCommandWith(defaultConfigDeps())
}

// newConfigCommandWith returns the config command group that uses d.
func newConfigCommandWith(d configDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect the configuration",
	}
	cmd.AddCommand(newConfigShowCommand(d), newConfigPathsCommand(d))
	return cmd
}

// newConfigShowCommand prints the effective configuration as JSON. Problems
// in a config file go to stderr as warnings and do not change the exit code.
func newConfigShowCommand(d configDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration",
		Long: `Print the effective configuration as JSON. The user file and the project
file (.gvim/config.json in the working directory or a parent) are merged over
the built-in defaults. A file that cannot be read is skipped and reported on
stderr as a warning.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := d.cwd()
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}
			view, err := d.load(root)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}
			for _, w := range view.Warnings {
				if err := writeLine(cmd.ErrOrStderr(), "gvim: warning: "+w); err != nil {
					return err
				}
			}
			_, err = cmd.OutOrStdout().Write(view.JSON)
			return err
		},
	}
}

// newConfigPathsCommand prints the user and project config file locations and
// whether each one exists.
func newConfigPathsCommand(d configDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "paths",
		Short: "Print the config file paths",
		Long: `Print the user config file and the project config file, and whether each
exists. The project file is the nearest .gvim/config.json at or above the
working directory. When there is none, its path is shown under the working
directory with "missing".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := d.cwd()
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}
			files, err := d.paths(root)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}
			for _, f := range files {
				status := "missing"
				if f.Exists {
					status = "exists"
				}
				if err := writeLine(cmd.OutOrStdout(), fmt.Sprintf("%-8s %s (%s)", f.Label, f.Path, status)); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// writeLine writes s and a newline to w.
func writeLine(w io.Writer, s string) error {
	_, err := fmt.Fprintln(w, s)
	return err
}
