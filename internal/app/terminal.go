package app

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

// The terminal dock's adapters: a shell from internal/term, and the system clipboard
// that terminal_paste reads. The ui package defines the interfaces; this file is the
// only place that imports the concrete term package for the dock.

// Both adapters satisfy the ports the dock expects.
var (
	_ tui.Terminal        = (*terminal.Session)(nil)
	_ tui.TerminalStarter = termStarter{}
	_ tui.ClipboardReader = systemClipboard{}
)

// clipboardReadTimeout bounds one clipboard read, so a stuck helper cannot hold the
// paste forever.
const clipboardReadTimeout = 2 * time.Second

// termStarter starts shells for the dock. A shell lives until the dock closes it, or
// until ctx is cancelled, whichever comes first.
type termStarter struct {
	ctx context.Context
}

// Start runs the user's shell in dir with the given size. It returns a nil Terminal
// with the error, so the dock never holds a typed nil.
func (s termStarter) Start(dir string, cols, rows int) (tui.Terminal, error) {
	sess, err := terminal.Start(s.ctx, terminal.Options{Dir: dir, Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// systemClipboard reads the system clipboard with the platform's own tool.
type systemClipboard struct{}

// Read returns the text of the system clipboard. It fails when no tool is installed
// for the platform.
func (systemClipboard) Read() (string, error) {
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
