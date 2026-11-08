package uiadapter

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// recordingStarter records the directory of each start and fails it, so no shell
// runs. The dock shows the failure, and the test checks where the start was asked to run.
type recordingStarter struct {
	dirs []string
}

var errNoShellInTest = errors.New("no shell in this test")

func (s *recordingStarter) Start(dir string, cols, rows int) (tui.Terminal, error) {
	s.dirs = append(s.dirs, dir)
	return nil, errNoShellInTest
}

// runStartsWithin runs cmd and the commands it batches, and fails the test when they do
// not return within a few seconds. The messages they return are dropped: the test checks
// what the commands did, not what they report.
func runStartsWithin(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runOnce(cmd)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the command did not return")
	}
}

func runOnce(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			runOnce(c)
		}
	}
}

// TestTerminalPaletteCommandsRunInTheApp runs each terminal command as the palette
// does, in the composed app with the real editor. A command the port does not run
// shows "Not supported in this port" in the status row. Each command starts its
// shell in the workspace root.
func TestTerminalPaletteCommandsRunInTheApp(t *testing.T) {
	tests := []struct {
		action string
		starts int
	}{
		{action: "open_terminal", starts: 1},
		{action: "open_terminal_below", starts: 1},
		{action: "open_terminal_right", starts: 1},
		{action: "focus_terminal", starts: 1},
		{action: "restart_terminal", starts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			tree, err := explorer.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			m := tui.New(NewDeps(editor.New(editor.Options{}), keymap.Default(), theme.Theme{}, tree, nil))
			starter := &recordingStarter{}
			m.SetDock(tui.DockPorts{Starter: starter})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

			_, cmd := m.Update(palette.RunCommandMsg{Action: tt.action})
			runStartsWithin(t, cmd)

			if frame := ansi.Strip(m.View().Content); strings.Contains(frame, "Not supported") {
				t.Fatalf("the palette ran %s as unsupported", tt.action)
			}
			if len(starter.dirs) != tt.starts {
				t.Fatalf("shell starts = %d, want %d", len(starter.dirs), tt.starts)
			}
			if !slices.Equal(starter.dirs, []string{tree.Root()}) {
				t.Fatalf("start directories = %q, want [%q]", starter.dirs, tree.Root())
			}
		})
	}
}
