package tui

import (
	"runtime"

	tea "charm.land/bubbletea/v2"
)

// suspendSupported reports whether the process can be suspended. Windows has no
// SIGTSTP, so there Suspend Process says so instead.
var suspendSupported = runtime.GOOS != "windows"

// suspend starts the suspend of the program. bubbletea gives the terminal back, sends
// SIGTSTP to the process group, and restores the terminal on SIGCONT. The program then
// sends a resume message, which repaints the screen (see featureMessage).
func (m *Model) suspend() tea.Cmd {
	if !suspendSupported {
		m.notice = "Suspend is not available on Windows"
		return nil
	}
	return tea.Suspend
}
