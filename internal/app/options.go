// Package app is the composition root of gvim. It turns the parsed command
// line into a running Bubble Tea program: Options and Run, the runtime
// lifecycle, the config-to-settings mapping, the log file, live grep and the
// default formatter table. The rest of the shell lives in its sibling host
// packages: internal/uiadapter (the UI port adapters), internal/pluginhost (the
// Lua plugin runtime), internal/lsphost (the language servers), internal/restore
// (session restore and crash recovery), internal/keybindings (user keybindings)
// and internal/highlight (the syntax-span cache). app is the only package that
// imports them all. The command-line layer in internal/cli sees only the Options
// type and app.Run; the config-view backend lives in internal/configview.
package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
	"github.com/tbxark/gvim/updatecheck"
)

// FileTarget is one file argument. Line and Col are 1-based. Zero means the
// position was not given, and the file opens at its start.
type FileTarget struct {
	Path string
	Line int
	Col  int
}

// DaemonMode is what the launch does with the daemon: nothing, host the editor as a
// daemon, or attach this terminal to a running daemon.
type DaemonMode int

const (
	// DaemonOff is a direct launch: one process, no daemon.
	DaemonOff DaemonMode = iota
	// DaemonServe hosts the editor as a daemon on a Unix socket (--daemon). The
	// process stays in the foreground and prints the socket it listens on; a
	// forked daemon is not part of this MVP.
	DaemonServe
	// DaemonAttach forwards this terminal to a running daemon (--attach/-a).
	DaemonAttach
)

// DaemonOptions carries a daemon launch. The CLI sets the mode and the socket name
// (empty means the per-root default name); Run fills Detach for the hosted program.
type DaemonOptions struct {
	Mode DaemonMode
	// Name is the socket name. Empty means DefaultName(root).
	Name string
	// Detach sends the detached frame to the attached client and drops it, so the
	// hosted editor keeps running. Run sets it before building the runtime.
	Detach func()
}

// Options is the parsed command line. The zero value starts gvim with no
// files in the current directory, the platform keymap and the dark theme.
type Options struct {
	// Remote is a parsed user@host:/path or ssh:// target. A remote launch mounts the
	// SSH authority, opens the remote terminal at its Workspace() and does not restore
	// or save the local session; remote file editing is not available in this slice
	// (the terminal is). It excludes local Files, Root, Diff and stdin.
	Remote *authority.RemoteSpec
	// Files open in order, each in its own tab. A missing file opens as an
	// empty buffer that is created on save.
	Files []FileTarget
	// Root is the directory the file explorer shows. Empty means the current
	// directory. A non-empty root is also recorded in the recent list.
	Root string
	// Config is the user config file path. Empty means the default location,
	// ~/.config/gvim/config.json (config.Dir). A non-empty value replaces that
	// default user file. The project file (.gvim/config.json at or above Root)
	// still applies on top of it. A missing file is an error that names the path,
	// so a typo does not silently drop the default user file.
	Config string
	// Keymap is a built-in keymap name. Empty means the config's keymap, or the
	// platform default when no config file sets one.
	Keymap string
	// Theme is a bundled theme name. Empty means the config's theme, which is
	// "dark" unless a config file sets another.
	Theme string
	// LogFile is the log file path. Empty means no log file. The notices of the status row
	// (warnings and errors) and the plugin log are appended to it, one line each. A path that
	// cannot be written is a notice, and the editor runs without a log.
	LogFile string
	// SessionDir holds the saved session: the open tabs, the unsaved text and the
	// explorer state. Run saves it when the program ends. Run uses
	// os.UserCacheDir()/gvim/session when this is empty. Without a directory, the
	// session is not restored either, so a caller that builds the model for a test
	// does not read the user's session.
	SessionDir string
	// AutoSaveInterval is how often the modified, named files are saved. Zero defers to
	// the config: editor.auto_save_enabled turns auto-save on at
	// editor.auto_save_interval_secs, and it is off when the config does not enable it.
	// A non-zero value wins over the config.
	AutoSaveInterval time.Duration
	// Diff opens the two files, in tabs 0 and 1, in the compare view. It needs exactly
	// two files and no folder.
	Diff bool
	// NoPlugins skips plugin discovery and loading, including the user's init.lua.
	// Plugins named by PluginPaths still load.
	NoPlugins bool
	// PluginPaths are plugins to load besides the discovered ones: each is a .lua
	// file or a plugin directory, given with --plugin.
	PluginPaths []string
	// Restore forces the restore of the saved session even when
	// editor.restore_previous_session is off, and makes a launch with files a
	// session launch (see editor.skip_session_restore_when_files_passed). It is
	// --restore.
	Restore bool
	// NoRestore skips the restore of the saved tabs for this run, as
	// editor.restore_previous_session=false does, while the unsaved text of the
	// session still comes back. It is --no-restore (Fresh's --no-session).
	NoRestore bool
	// Simple opens just what is named (or the welcome screen) with no session
	// restored or saved and no file explorer: gvim's file-launch behavior
	// without needing a file. It is --simple.
	Simple bool
	// ReadStdin makes Run read standard input (bounded) into the [stdin] buffer
	// before the TUI starts, then read keys from the terminal. It is --stdin or a
	// "-" argument. A launch with no files and piped stdin sets it too, as Fresh
	// does, so "git show | gvim" works without the flag.
	ReadStdin bool
	// Stdin is the piped content. Run fills it; a caller (or a test) may set it
	// directly to build a [stdin] launch without a pipe. It is normalized like a
	// file on open (CRLF to the buffer's LF, the ending remembered for Save As).
	Stdin []byte
	// StdinName is the file name --stdin-name gives the [stdin] buffer: its
	// extension names the language and Save As suggests it. Fresh has no such
	// flag. Empty means the language is sniffed from the content and Save As is
	// unnamed.
	StdinName string

	// Daemon hosts the editor as a daemon (DaemonServe) or attaches this terminal
	// to one (DaemonAttach). Nil is the direct launch, the default.
	Daemon *DaemonOptions

	// pluginLog receives the plugin log for LogFile; set by buildRuntime.
	pluginLog func(pluginapi.LogEntry)
}

// Validate reports the first option that cannot be used. It does not touch
// the file system: a missing file is valid, and a root is checked when Run
// opens it.
// StdinLaunch reports whether the launch carries standard-input content: the explicit
// --stdin/- flag, a directly-set Stdin, or a --stdin-name. Such a launch is treated as a
// file launch (no session restore or save), because the piped content is not session
// state.
func (o Options) StdinLaunch() bool {
	return o.ReadStdin || len(o.Stdin) > 0 || o.StdinName != ""
}

func (o Options) Validate() error {
	if o.Keymap != "" {
		if _, err := keymap.ByName(o.Keymap); err != nil {
			return fmt.Errorf("--keymap: unknown keymap %q (want default, macos, macos-gui, emacs or vscode)", o.Keymap)
		}
	}
	if o.Theme != "" {
		if err := theme.ValidateForm(o.Theme); err != nil {
			return fmt.Errorf("--theme: %w", err)
		}
		if theme.IsURLForm(o.Theme) {
			// The same transport policy as the update check, applied before any
			// request: https, or http only on the loopback interface.
			if _, err := updatecheck.ValidateURL(o.Theme); err != nil {
				return fmt.Errorf("--theme: %w", err)
			}
		}
		if theme.IsNamedForm(o.Theme) && !slices.Contains(theme.Names(), o.Theme) {
			// A plain name may also be a user theme: check the user theme dir, so
			// a typo is one line instead of a start-time error.
			registry := theme.NewRegistry(userThemeDir())
			if _, err := registry.Load(o.Theme); err != nil {
				return fmt.Errorf("--theme: unknown theme %q (want one of the bundled themes, a theme in %s, or a theme path)", o.Theme, userThemeDir())
			}
		}
	}
	if o.Diff {
		if len(o.Files) != 2 {
			return errors.New("--diff needs exactly two files")
		}
		if o.Root != "" {
			return errors.New("--diff compares two files, not a folder")
		}
	}
	if o.Restore && o.NoRestore {
		return errors.New("--restore cannot be combined with --no-restore")
	}
	if o.Restore && o.Simple {
		return errors.New("--restore cannot be combined with --simple")
	}
	// A compare launch is a file launch by definition, so --restore would be a
	// no-op there; refuse it rather than silently ignoring it.
	if o.Restore && o.Diff {
		return errors.New("--restore cannot be combined with --diff")
	}
	if o.StdinLaunch() && o.Diff {
		return errors.New("--stdin cannot be combined with --diff")
	}
	if o.Restore && o.StdinLaunch() {
		return errors.New("--restore cannot be combined with --stdin")
	}
	if o.StdinName != "" && !o.ReadStdin && len(o.Stdin) == 0 {
		return errors.New("--stdin-name needs --stdin or a - argument")
	}
	if o.Daemon != nil {
		switch o.Daemon.Mode {
		case DaemonServe, DaemonAttach:
		default:
			return errors.New("daemon mode is invalid")
		}
		// A daemon launch is a hosted session, not a stdin/file/compare launch:
		// those combinations would read two keyboards or restore twice.
		if o.StdinLaunch() {
			return errors.New("--daemon cannot be combined with --stdin")
		}
		if o.Diff {
			return errors.New("--daemon cannot be combined with --diff")
		}
		if o.Restore || o.NoRestore {
			return errors.New("--daemon cannot be combined with --restore/--no-restore")
		}
		if o.Simple {
			return errors.New("--daemon cannot be combined with --simple")
		}
		if o.Daemon.Mode == DaemonServe && len(o.Files) > 0 {
			return errors.New("--daemon opens no files: start it, then attach and open them")
		}
	}
	for _, p := range o.PluginPaths {
		if strings.TrimSpace(p) == "" {
			return errors.New("--plugin: path is empty")
		}
	}
	if o.Remote != nil {
		if err := validateRemote(o.Remote); err != nil {
			return err
		}
		if o.Diff {
			return errors.New("a remote target cannot be combined with --diff")
		}
		if o.Root != "" || len(o.Files) > 0 {
			return errors.New("a remote target cannot be combined with local directories or files")
		}
		if o.StdinLaunch() {
			return errors.New("a remote target cannot be combined with --stdin")
		}
		if o.Restore {
			return errors.New("a remote target does not restore the local session")
		}
		if o.Daemon != nil {
			return errors.New("a remote target cannot be combined with --daemon/--attach")
		}
	}
	for _, f := range o.Files {
		if f.Path == "" {
			return errors.New("file path is empty")
		}
		if f.Line < 0 || f.Col < 0 {
			return fmt.Errorf("%s: line and column must not be negative", f.Path)
		}
		if f.Col > 0 && f.Line == 0 {
			return fmt.Errorf("%s: a column needs a line", f.Path)
		}
	}
	return nil
}

// validateRemote checks a programmatically built remote target. The CLI already parsed it;
// this keeps a caller's --remote contract honest.
func validateRemote(spec *authority.RemoteSpec) error {
	if spec.Host == "" {
		return errors.New("remote target: host is empty")
	}
	if spec.User == "" {
		return errors.New("remote target: user is empty")
	}
	if spec.Path == "" {
		return errors.New("remote target: path is empty")
	}
	if spec.Port < 0 || spec.Port > 65535 {
		return fmt.Errorf("remote target: port %d is invalid", spec.Port)
	}
	if spec.Line < 0 || spec.Col < 0 {
		return errors.New("remote target: line and column must not be negative")
	}
	if spec.Col > 0 && spec.Line == 0 {
		return errors.New("remote target: a column needs a line")
	}
	return nil
}
