package tui

import (
	"os"

	tea "charm.land/bubbletea/v2"
)

// OpenInputTTY opens the controlling terminal for keyboard input when the process's
// standard input was consumed as buffer content (gvim -/--stdin). It is bubbletea's
// cross-platform OpenTTY: /dev/tty on Unix and CONIN$ on Windows. The caller passes the
// file to RunWithInput and closes it after the program ends.
func OpenInputTTY() (*os.File, error) {
	in, _, err := tea.OpenTTY()
	if err != nil {
		return nil, err
	}
	return in, nil
}
