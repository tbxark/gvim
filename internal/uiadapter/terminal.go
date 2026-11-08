package uiadapter

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	goruntime "runtime"
	"time"

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
	sess, err := terminal.Start(s.ctx, terminal.Options{
		Shell: s.opts.Shell, Args: s.opts.Args,
		Dir: dir, Cols: cols, Rows: rows,
		ScrollbackLines: scrollback, Env: extraEnv,
	})
	if err != nil {
		return nil, err
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
