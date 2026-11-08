package uiadapter

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	goruntime "runtime"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui"
)

// The terminal dock's adapters: a shell from internal/terminal, and the system clipboard
// that terminal_paste reads. The ui package defines the interfaces; this file is the
// only place that imports the concrete term package for the dock.

// Both adapters satisfy the ports the dock expects.
var (
	_ tui.Terminal        = (*terminal.Session)(nil)
	_ tui.TerminalStarter = TermStarter{}
	_ tui.ClipboardReader = SystemClipboard{}
)

// clipboardReadTimeout bounds one clipboard read, so a stuck helper cannot hold the
// paste forever.
const clipboardReadTimeout = 2 * time.Second

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
	// Authority is the workspace authority slot of the session (nil for none). The
	// local backend returns a launch with no backend, so the local PTY starts as
	// before; a remote backend returns a transport the dock drives through
	// terminal.StartWithBackend. The starter never asks what kind the slot is.
	Authority authority.Slot
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
	if viaAuthority && s.opts.Authority != nil {
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
			if s.opts.OSC52 != nil {
				sess.SetOSC52Handler(s.opts.OSC52)
			}
			return sess, nil
		}
	}
	sess, err := terminal.Start(s.ctx, terminal.Options{
		Shell: shell, Args: args,
		Dir: dir, Cols: cols, Rows: rows,
		ScrollbackLines: scrollback, Env: extraEnv,
	})
	if err != nil {
		return nil, err
	}
	if s.opts.OSC52 != nil {
		sess.SetOSC52Handler(s.opts.OSC52)
	}
	return sess, nil
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
		ctx, cancel := context.WithTimeout(context.Background(), clipboardReadTimeout)
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
