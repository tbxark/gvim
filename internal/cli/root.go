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

// daemonDefaultName is the NoOptDefVal of --daemon/-a: it marks a flag given with no
// NAME, which means the per-root default socket name.
const daemonDefaultName = "-"

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
		restore    bool
		noRestore  bool
		simple     bool
		stdin      bool
		stdinName  string
		daemonName string
		attachName string
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
by side in a compare view.

A launch without files restores the session saved by an earlier run — the open
tabs, their cursors and unsaved text — and saves it again when the editor quits.
Unsaved buffers are kept across a quit by default (editor.hot_exit); set that
key to false to be asked to save or discard each one instead. A launch with
files opens only those files by default (editor.skip_session_restore_when_files_passed),
restoring and saving no session. --restore forces the session for this run,
--no-restore skips it (still bringing the session's unsaved text back), and
--simple opens just what is named with no session at all.

Standard input becomes a scratch [stdin] buffer (no path: Save runs Save As), and the
keyboard then comes from the terminal. Use --stdin, or a - FILE argument anywhere among
the files; when no files are named, piped stdin is read even without the flag, so
"git show | gvim" works. The read is bounded at 32 MiB. A stdin launch never restores or
saves the session. --stdin-name NAME names the buffer for the language (by extension) and
the Save-As suggestion.

A FILE may be a remote target instead: user@host:/path or
ssh://[user@]host[:port]/path, optionally ending in :LINE[:COL]. ssh:// is exclusive
(a malformed URL is a startup error) and the path may contain colons. A direct remote
launch mounts the SSH authority (the remote editor, terminal, search and LSP) and takes
no local directory or files, is not restored or saved in the local session, and cannot be
combined with --diff.

With --daemon a remote target makes the daemon a jump box: the daemon runs on THIS machine
and hosts the remote workspace over SSH, so only this machine needs gvim. For example
"gvim --daemon=webserver deploy@server:/etc/nginx" starts it, and "gvim --attach=webserver"
reattaches after a detach; a reattach ignores the remote target (the daemon owns SSH).
Each workspace binds one backend, so local and remote workspaces can coexist in one daemon.

--daemon runs the editor as a daemon on a Unix socket (in the foreground; it prints the
socket it listens on) and --attach/-a forwards this terminal to a running daemon. The
socket name defaults to a per-workspace name; the optional NAME is accepted only in the
= form: --daemon=NAME / --attach=NAME (or -a=NAME), because -a NAME would make NAME a
file argument. While a daemon hosts a workspace, every direct editor launch in it is
refused instead of becoming a second writer: the session's unsaved buffers live in the
daemon, and a direct editor could race the daemon's save of the same files. Attach with
"gvim -a" (then the files, e.g. "gvim -a FILE...") or "gvim --attach=NAME", or quit the
daemon first; this covers explicitly named daemons. In the daemon the "Detach from Daemon"
command drops this client and leaves the editor running.`,
		Example: `  gvim
  gvim ~/project
  gvim ~/project main.go:42:7 README.md
  gvim --diff old.go new.go
  gvim --no-restore
  gvim --simple
  git show HEAD | gvim -
  cat notes.txt | gvim --stdin-name notes.md`,
		Args:          cobra.ArbitraryArgs,
		Version:       versionString(),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// "-" is standard input among the file arguments (Fresh's alternative to
			// --stdin); it is not a path to stat. At most one may be given.
			files := make([]string, 0, len(args))
			dashes := 0
			for _, arg := range args {
				if arg == "-" {
					dashes++
					continue
				}
				files = append(files, arg)
			}
			if dashes > 1 {
				return errors.New("only one - (standard input) may be given")
			}
			if dashes == 1 && stdin {
				return errors.New("--stdin and - name the same standard input")
			}
			if dashes == 1 {
				stdin = true
			}
			targets, err := ParseTargets(files, os.Stat)
			if err != nil {
				return err
			}
			opts := app.Options{
				Files:   targets.Files,
				Root:    targets.Root,
				Remote:  targets.Remote,
				Config:  configPath,
				Keymap:  keymapName,
				Theme:   themeName,
				LogFile: logFile,
				Diff:    diff,

				NoPlugins:   noPlugins,
				PluginPaths: pluginPath,

				Restore:   restore,
				NoRestore: noRestore,
				Simple:    simple,

				ReadStdin: stdin,
				StdinName: stdinName,
			}
			// A daemon launch is either the host (--daemon) or a client (-a), never
			// both. An omitted NAME means the per-root default socket name.
			daemonSet, attachSet := cmd.Flags().Changed("daemon"), cmd.Flags().Changed("attach")
			if daemonSet && attachSet {
				return errors.New("--daemon cannot be combined with --attach")
			}
			if daemonSet {
				name := daemonName
				if name == daemonDefaultName {
					name = ""
				}
				opts.Daemon = &app.DaemonOptions{Mode: app.DaemonServe, Name: name}
			}
			if attachSet {
				name := attachName
				if name == daemonDefaultName {
					name = ""
				}
				opts.Daemon = &app.DaemonOptions{Mode: app.DaemonAttach, Name: name}
			}
			if err := opts.Validate(); err != nil {
				// The message names the flag and the valid values: the whole usage text
				// after it would only bury it.
				cmd.SilenceUsage = true
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
	flags.StringVar(&logFile, "log-file", "", "append the status-row notices and the plugin log to `PATH`")
	root.Flags().BoolVar(&diff, "diff", false, "compare the two FILE arguments side by side")
	root.Flags().BoolVar(&noPlugins, "no-plugins", false, "do not discover or load plugins (--plugin still loads)")
	root.Flags().StringArrayVar(&pluginPath, "plugin", nil, "load the plugin at `PATH` (a .lua file or a plugin directory); repeatable")
	root.Flags().BoolVar(&restore, "restore", false, "restore the previous session even when editor.restore_previous_session is off; makes a launch with files a session launch")
	root.Flags().BoolVar(&noRestore, "no-restore", false, "do not restore the previous session for this run; its unsaved text still comes back")
	root.Flags().BoolVar(&noRestore, "no-session", false, "alias for --no-restore")
	root.Flags().BoolVar(&simple, "simple", false, "open just what is named: no session restored or saved and no file explorer")
	root.Flags().BoolVar(&stdin, "stdin", false, "read a [stdin] buffer from standard input (bounded at 32 MiB); the keyboard comes from the terminal")
	root.Flags().StringVar(&stdinName, "stdin-name", "", "name the [stdin] buffer `NAME`, for the language and the Save-As suggestion")
	flags.StringVar(&daemonName, "daemon", "", "run the editor as a daemon on a socket (foreground); use `--daemon=NAME` for a socket name other than this workspace's default")
	flags.Lookup("daemon").NoOptDefVal = daemonDefaultName
	flags.StringVarP(&attachName, "attach", "a", "", "attach this terminal to a running daemon; use `--attach=NAME` (or `-a=NAME`) for a named daemon, `-a` alone for this workspace's default")
	flags.Lookup("attach").NoOptDefVal = daemonDefaultName
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
