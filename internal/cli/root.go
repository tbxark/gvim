// Package cli is the command-line layer of gvim: the cobra command tree, flag
// parsing and exit codes. It holds no state of its own. Every command is built
// by a function call, and flags are bound to local variables, so nothing is
// registered at init time. The program itself runs through the Runner that the
// caller passes in.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/tbxark/gvim/internal/app"
)

// Runner starts gvim with the parsed options and returns when it stops. The
// production Runner is app.Run.
type Runner func(ctx context.Context, opts app.Options) error

// Exit codes. exitError carries a non-default code; any other error exits 1.
const (
	exitOK             = 0
	exitFailure        = 1
	exitNotImplemented = 2
	exitInterrupted    = 130
)

// exitError is an error that Execute reports with a specific exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func (e *exitError) Unwrap() error { return e.err }

// keymapNames lists the built-in keymaps for help text and completion. Names
// are checked by app.Options.Validate.
var keymapNames = []string{"default", "macos", "macos-gui", "emacs", "vscode"}

// NewRootCommand returns the gvim command tree. The root opens the editor with
// the positional arguments and the flags. run is called once per invocation
// with the parsed options, after they validate.
func NewRootCommand(run Runner) *cobra.Command {
	var (
		configPath string
		keymapName string
		themeName  string
		logFile    string
		diff       bool
		noPlugins  bool
		pluginPath []string
	)
	root := &cobra.Command{
		Use:   "gvim [DIR] [FILE[:LINE[:COL]]]...",
		Short: "gvim is a terminal editor",
		Long: `gvim is a terminal editor. Run it with no arguments to start in the
current directory with an empty buffer.

A DIR argument sets the project root that the file explorer shows. At most one
directory may be given. Each FILE opens in its own tab. A FILE that does not
exist opens as an empty buffer and is created on save. FILE may end in :LINE or
:LINE:COL to place the cursor. With --diff, exactly two FILE arguments open side
by side in a compare view.`,
		Example: `  gvim
  gvim ~/project
  gvim ~/project main.go:42:7 README.md
  gvim --diff old.go new.go`,
		Args:          cobra.ArbitraryArgs,
		Version:       versionString(),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := ParseTargets(args, os.Stat)
			if err != nil {
				return err
			}
			opts := app.Options{
				Files:   targets.Files,
				Root:    targets.Root,
				Config:  configPath,
				Keymap:  keymapName,
				Theme:   themeName,
				LogFile: logFile,
				Diff:    diff,

				NoPlugins:   noPlugins,
				PluginPaths: pluginPath,
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			// Runtime errors are not usage errors, so they print no usage text.
			cmd.SilenceUsage = true
			if run == nil {
				return errors.New("no runner configured")
			}
			return run(cmd.Context(), opts)
		},
	}

	flags := root.PersistentFlags()
	flags.StringVar(&configPath, "config", "", "config file `PATH` (default: the platform config directory)")
	flags.StringVar(&keymapName, "keymap", "", "keymap `NAME`: default, macos, macos-gui, emacs or vscode (default: the platform keymap)")
	flags.StringVar(&themeName, "theme", "", "theme `NAME` (default: dark)")
	flags.StringVar(&logFile, "log-file", "", "log file `PATH` (default: no log file)")
	root.Flags().BoolVar(&diff, "diff", false, "compare the two FILE arguments side by side")
	root.Flags().BoolVar(&noPlugins, "no-plugins", false, "do not discover or load plugins (--plugin still loads)")
	root.Flags().StringArrayVar(&pluginPath, "plugin", nil, "load the plugin at `PATH` (a .lua file or a plugin directory); repeatable")
	_ = root.RegisterFlagCompletionFunc("keymap", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return keymapNames, cobra.ShellCompDirectiveNoFileComp
	})

	root.AddCommand(newVersionCommand(), newConfigCommand(&configPath, &keymapName, &themeName), newGrammarCommand())
	// Cobra adds the completion command only when the root has subcommands,
	// so this call comes after AddCommand.
	root.InitDefaultCompletionCmd()
	return root
}

// Execute builds the command tree, parses args (without the program name) and
// runs it. Output goes to stdout and stderr. An error is printed once to stderr
// and makes the exit code 1, unless the error sets another code. Help and
// version output exit 0. A canceled context exits 130 without a message, which
// is the conventional code for an interrupt.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer, run Runner) int {
	// A nil slice makes cobra read os.Args, which is not wanted here.
	if args == nil {
		args = []string{}
	}
	root := NewRootCommand(run)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return exitCode(stderr, root.ExecuteContext(ctx))
}

// exitCode turns the result of a command into a process exit status and prints
// the error, if there is one.
func exitCode(stderr io.Writer, err error) int {
	if err == nil {
		return exitOK
	}
	if errors.Is(err, context.Canceled) {
		return exitInterrupted
	}
	code := exitFailure
	if ee, ok := errors.AsType[*exitError](err); ok {
		code = ee.code
	}
	_, _ = fmt.Fprintln(stderr, "gvim:", err)
	return code
}
