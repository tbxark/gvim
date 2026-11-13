package uiadapter

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui"
)

// The terminal dock's adapters: a shell from internal/terminal, and the system clipboard
// that terminal_paste reads. The tui package defines the interfaces; this file is the
// only place that imports the concrete terminal package for the dock.

// Both adapters satisfy the ports the dock expects.
var (
	_ tui.Terminal        = (*terminal.Session)(nil)
	_ tui.TerminalStarter = TermStarter{}
	_ tui.ClipboardReader = SystemClipboard{}
)

// clipboardTimeout bounds one clipboard read or write, so a stuck helper cannot hold the
// paste or the copy forever.
const clipboardTimeout = 2 * time.Second

// TermStarterOptions are the terminal.shell and terminal.scrollback_lines settings
// a starter applies to every shell it starts.
type TermStarterOptions struct {
	// Shell is terminal.shell.command, or "" for the user's $SHELL.
	Shell string
	// Args are terminal.shell.args.
	Args []string
	// ScrollbackLines is terminal.scrollback_lines. Zero disables the history;
	// a negative value leaves the terminal package's default.
	ScrollbackLines int
	// SpawnEnv is the active project environment's set-only overlay for the dock
	// shell, or nil.
	SpawnEnv func() []string
	// OSC52, when not nil, is installed on every new session as its clipboard
	// hook: a child OSC 52 sequence calls it with the selection and the decoded
	// text. The host decides whether the feature is on (clipboard.use_osc52 and a
	// trusted workspace); a nil hook leaves the feature inert.
	OSC52 func(selection, text string) bool
	// OSC52Query, when not nil, is installed on every new session as its clipboard
	// read-request handler: a child's OSC 52 "?" query calls it with the selection
	// and receives clipboard text back as terminal input. The host decides whether
	// it is on (clipboard.osc52_paste and a trusted workspace); nil is inert.
	OSC52Query func(selection string) (string, bool)
	// Authority is the workspace authority slot of the session (nil for none). The
	// local backend returns a launch with no backend, so the local PTY starts as
	// before; a remote backend returns a transport the dock drives through
	// terminal.StartWithBackend. The starter never asks what kind the slot is.
	Authority authority.Slot
	// Notice, when not nil, receives a short user-facing notice when a remote
	// working directory is refused and the terminal falls back to the authority's
	// default directory. It is only called for a remote slot.
	Notice func(string)
}

// TermStarter starts shells for the dock. A shell lives until the dock closes it, or
// until ctx is cancelled, whichever comes first.
type TermStarter struct {
	ctx  context.Context
	opts TermStarterOptions
}

// NewTermStarter returns the starter of the dock shells of a session. An optional
// TermStarterOptions gives the shell override and the history size the config asks
// for; without it the user's shell starts with the default history.
func NewTermStarter(ctx context.Context, opts ...TermStarterOptions) TermStarter {
	s := TermStarter{ctx: ctx}
	if len(opts) > 0 {
		s.opts = opts[0]
	}
	return s
}

// remoteDirCheckTimeout bounds the authority stat that validates a restored remote
// working directory before a shell is launched.
const remoteDirCheckTimeout = 5 * time.Second

// Start runs the shell in dir with the given size. It returns a nil Terminal with
// the error, so the dock never holds a typed nil.
func (s TermStarter) Start(dir string, cols, rows int) (tui.Terminal, error) {
	return s.startProgram(s.opts.Shell, s.opts.Args, dir, cols, rows, true)
}

// StartLaunch runs a recorded or configured program instead of the shell: an
// agent session or a resume, as the argv names it. It satisfies the dock's
// Launcher port, so Run Agent and Resume start without a shell in between.
func (s TermStarter) StartLaunch(launch terminal.Launch, dir string, cols, rows int) (tui.Terminal, error) {
	launch = launch.Clean()
	if len(launch.Argv) == 0 {
		return s.Start(dir, cols, rows)
	}
	// An agent/resume program is a local executable: it does not go through a
	// re-parenting authority (a remote login shell would ignore the argv).
	return s.startProgram(launch.Argv[0], launch.Argv[1:], dir, cols, rows, false)
}

// startProgram runs one program in dir with the dock's environment and clipboard
// hook. An empty program runs the user's shell.
func (s TermStarter) startProgram(shell string, args []string, dir string, cols, rows int, viaAuthority bool) (tui.Terminal, error) {
	scrollback := s.opts.ScrollbackLines
	if scrollback == 0 {
		// config's 0 means "no history"; the terminal package's 0 means its
		// default, so the switch is spelled -1 here.
		scrollback = -1
	}
	var extraEnv []string
	if s.opts.SpawnEnv != nil {
		extraEnv = s.opts.SpawnEnv()
	}
	// The workspace authority decides where the terminal runs. The local backend
	// returns a launch with no transport (the local PTY path below); a remote backend
	// returns a transport that the same Session drives through StartWithBackend.
	remote := s.opts.Authority != nil && s.opts.Authority.State().Remote != nil
	if viaAuthority && s.opts.Authority != nil {
		// A restored/restarted shell's saved cwd may no longer exist on the remote
		// host. Verify it through the authority before the launch command (SSH does
		// a remote cd && exec, which would otherwise leave the shell dead) and fall
		// back to the authority's default directory. This only ever stats the
		// authority's file system: no local path is read and nothing is executed to
		// probe the directory. A Windows-flavoured path is refused before the stat:
		// this remote backend is a POSIX (SSH) host, so the path can never be valid
		// there. A local authority gets the same platform-flavour check as a local
		// start.
		if remote {
			dir = s.checkedRemoteDir(s.ctx, dir)
		} else {
			dir = s.checkedLocalDir(dir)
		}
		launch, err := s.opts.Authority.Terminal().Launch(s.ctx, authority.TerminalOptions{
			Dir: dir, Shell: shell, Args: args, Env: extraEnv, Cols: cols, Rows: rows,
		})
		if err != nil {
			return nil, err
		}
		if launch.Shell != "" {
			shell = launch.Shell
		}
		if len(launch.Args) > 0 {
			args = launch.Args
		}
		if launch.Dir != "" {
			dir = launch.Dir
		}
		if len(launch.Env) > 0 {
			extraEnv = launch.Env
		}
		if launch.Backend != nil {
			sess, err := terminal.StartWithBackend(s.ctx, terminal.Options{
				Dir: launch.Dir, Cols: cols, Rows: rows,
				ScrollbackLines: scrollback,
			}, launch.Backend)
			if err != nil {
				_ = launch.Backend.Close()
				return nil, err
			}
			s.installOSC52(sess)
			return sess, nil
		}
	}
	// A local start (no authority, a local authority, or a remote workspace's local
	// agent executable) validates the directory against this host's path flavour:
	// a saved Windows path must not be handed to a POSIX PTY (or the reverse), and
	// the fallback is the default local directory, never a translation.
	dir = s.checkedLocalDir(dir)
	sess, err := terminal.Start(s.ctx, terminal.Options{
		Shell: shell, Args: args,
		Dir: dir, Cols: cols, Rows: rows,
		ScrollbackLines: scrollback, Env: extraEnv,
	})
	if err != nil {
		return nil, err
	}
	s.installOSC52(sess)
	return sess, nil
}

// installOSC52 sets the clipboard hooks of the options, when there are any, on sess.
func (s TermStarter) installOSC52(sess *terminal.Session) {
	if s.opts.OSC52 != nil {
		sess.SetOSC52Handler(s.opts.OSC52)
	}
	if s.opts.OSC52Query != nil {
		sess.SetOSC52QueryHandler(s.opts.OSC52Query)
	}
}

// checkedRemoteDir validates a saved/restarted working directory on a remote
// authority. Local slots, empty directories and non-remote authorities are left
// untouched. The remote backend is a POSIX host (SSH today), so a path that is
// not POSIX-absolute is refused before any stat — a Windows path must never be
// used as a remote POSIX start cwd. When the directory is unusable (wrong flavour,
// missing, not a directory, or the authority is disconnected), it reports a
// notice (when configured) and returns "" so the authority starts in its own
// default directory; it never falls back to a local path.
func (s TermStarter) checkedRemoteDir(ctx context.Context, dir string) string {
	slot := s.opts.Authority
	if slot == nil || dir == "" || slot.State().Remote == nil {
		return dir
	}
	if !strings.HasPrefix(dir, "/") {
		s.remoteDirFallback(dir, "it is not a POSIX path for this remote workspace")
		return ""
	}
	if !slot.Connected() {
		s.remoteDirFallback(dir, "the workspace is disconnected")
		return ""
	}
	checkCtx, cancel := context.WithTimeout(ctx, remoteDirCheckTimeout)
	defer cancel()
	st, err := slot.Files().Stat(checkCtx, dir)
	if err != nil || st.Kind != "dir" {
		s.remoteDirFallback(dir, "the directory no longer exists")
		return ""
	}
	return dir
}

// dirAbsoluteForPlatform reports whether dir is absolute for goos: POSIX-absolute
// on Unix, drive-absolute or UNC on Windows. The platform is explicit so the
// check is testable on any host.
func dirAbsoluteForPlatform(dir, goos string) bool {
	if goos == "windows" {
		if strings.HasPrefix(dir, "\\\\") {
			return true
		}
		if len(dir) < 3 {
			return false
		}
		c := dir[0]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !isLetter || dir[1] != ':' {
			return false
		}
		return dir[2] == '/' || dir[2] == '\\'
	}
	return strings.HasPrefix(dir, "/")
}

// checkedLocalDir refuses a saved directory whose flavour does not match this
// host: a Windows path on a Unix host (or a POSIX path on a Windows host) is
// reported and replaced by "" (the default local directory). It never translates
// the path and never reads the local file system to guess.
func (s TermStarter) checkedLocalDir(dir string) string {
	if dir == "" || dirAbsoluteForPlatform(dir, goruntime.GOOS) {
		return dir
	}
	if s.opts.Notice != nil {
		s.opts.Notice(uiadapterTextVars("gvim.notice.terminal_dir_invalid", "Terminal directory %{dir} is not an absolute path on this platform; starting in the default directory", map[string]any{"dir": fmt.Sprintf("%q", dir)}))
	}
	return ""
}

// remoteDirFallback tells the user why a saved remote directory was not used. The
// directory is quoted so no control byte from a record can reach the status row.
func (s TermStarter) remoteDirFallback(dir, reason string) {
	if s.opts.Notice != nil {
		s.opts.Notice(uiadapterTextVars("gvim.notice.remote_terminal_dir_unavailable", "Remote terminal directory %{dir} is unavailable (%{reason}); starting in the remote default directory", map[string]any{"dir": fmt.Sprintf("%q", dir), "reason": reason}))
	}
}

// SystemClipboard reads the system clipboard with the platform's own tool.
type SystemClipboard struct{}

// Read returns the text of the system clipboard. It fails when no tool is installed
// for the platform.
func (SystemClipboard) Read() (string, error) {
	cmds := clipboardCommands(goruntime.GOOS)
	if len(cmds) == 0 {
		return "", fmt.Errorf("reading the clipboard is not supported on %s yet", goruntime.GOOS)
	}
	for _, argv := range cmds {
		path, err := exec.LookPath(argv[0])
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
		out, err := exec.CommandContext(ctx, path, argv[1:]...).Output()
		cancel()
		if err != nil {
			return "", fmt.Errorf("%s: %w", argv[0], err)
		}
		return string(out), nil
	}
	return "", errors.New("no clipboard tool found; install pbpaste, wl-paste, xclip or xsel")
}

// clipboardCommands lists the commands that print the clipboard on goos, in the order
// they are tried. Each entry is the program and its arguments.
func clipboardCommands(goos string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbpaste"}}
	case "linux":
		return [][]string{
			{"wl-paste", "--no-newline"},
			{"xclip", "-selection", "clipboard", "-out"},
			{"xsel", "--clipboard", "--output"},
		}
	}
	return nil
}
