package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/tbxark/gvim/internal/app"
	"github.com/tbxark/gvim/internal/configview"
)

// configDeps are the functions the config commands call. The app package
// supplies the real ones, and tests pass fakes. cwd gives the workspace root.
// load gets the root flags that change the effective config (--config, --keymap
// and --theme) as a configview.Options, paths gets the --config path, and trusted
// resolves the workspace's recorded trust level read-only. An unset flag is empty.
type configDeps struct {
	cwd     func() (string, error)
	load    func(root string, opts configview.Options) (configview.ConfigView, error)
	paths   func(root, configPath string) ([]configview.ConfigFile, error)
	trusted func(root string) (bool, error)
}

// defaultConfigDeps returns the dependencies that read the real working
// directory and the real config files.
func defaultConfigDeps() configDeps {
	return configDeps{
		cwd: os.Getwd, load: configview.Load, paths: configview.Paths,
		trusted: app.ProjectConfigTrusted,
	}
}

// newConfigCommand returns the config command group, wired to the real
// dependencies. The pointers are the values of the root --config, --keymap and
// --theme flags, read when a command runs.
func newConfigCommand(configPath, keymapName, themeName *string) *cobra.Command {
	return newConfigCommandWith(defaultConfigDeps(), configPath, keymapName, themeName)
}

// newConfigCommandWith returns the config command group that uses d. The
// pointers are the values of the --config, --keymap and --theme flags, read when
// a command runs.
func newConfigCommandWith(d configDeps, configPath, keymapName, themeName *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect the configuration",
	}
	cmd.AddCommand(newConfigShowCommand(d, configPath, keymapName, themeName), newConfigPathsCommand(d, configPath))
	return cmd
}

// newConfigShowCommand prints the effective configuration as JSON. The --keymap
// and --theme flags override the config files, as they do when the editor starts.
// Problems in a config file go to stderr as warnings and do not change the exit
// code.
func newConfigShowCommand(d configDeps, configPath, keymapName, themeName *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration",
		Long: `Print the effective configuration as JSON. The user file and, when the
workspace is trusted, the project file (.gvim/config.json in the working
directory or a parent) are merged over the built-in defaults. An untrusted or
undecided workspace omits the project layer and says so on stderr, exactly as
the editor gates it. A file that cannot be read is skipped and reported as a
warning.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := d.cwd()
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}
			trusted := false
			if d.trusted != nil {
				if ok, err := d.trusted(root); err == nil {
					trusted = ok
				}
			}
			view, err := d.load(root, configview.Options{Config: *configPath, Keymap: *keymapName, Theme: *themeName, Trusted: &trusted})
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
func newConfigPathsCommand(d configDeps, configPath *string) *cobra.Command {
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
			files, err := d.paths(root, *configPath)
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
