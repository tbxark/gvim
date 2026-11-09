package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/dock"
	"github.com/tbxark/gvim/internal/tui/frame"
)

// waitDock pumps the dock's commands until cond holds or a generous deadline
// passes. dockSettle returns after a fixed idle window, which a loaded machine can
// exceed while a fake shell's goroutine is scheduled; the restart tests wait for
// the state they need instead of guessing.
func waitDock(t *testing.T, m *Model, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		dockSettle(t, m, nil)
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s (dock open=%v exited=%v running=%v)", what, m.dock.Open(), m.dock.Exited(), m.dock.Running())
}

// exitedDockModel is a model with an open dock whose first shell has exited with
// the given status.
func exitedDockModel(t *testing.T, code int, known bool) (*Model, *shellsStarter) {
	t.Helper()
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
	openDock(t, m)
	waitDock(t, m, "the dock shell to start", func() bool { return m.dock.Running() })
	starter.terms[0].exitCode, starter.terms[0].exitOK = code, known
	close(starter.terms[0].done)
	waitDock(t, m, "the dock shell to exit", func() bool { return m.dock.Exited() })
	return m, starter
}

// restartSegment returns the layout run of {terminal_restart}.
func restartSegment(t *testing.T, m *Model) *run {
	t.Helper()
	l := m.layout()
	for i := range l.Status {
		if l.Status[i].Kind == hitStatusSegment && l.Status[i].Index == frame.StatusSegmentTerminalRestart {
			return &l.Status[i]
		}
	}
	return nil
}

// TestTerminalRestartStatusText checks Fresh's label rules on the live-exit and
// restored paths: resume/restart verb, program basename, non-zero exit code and
// the omitted exit 0.
func TestTerminalRestartStatusText(t *testing.T) {
	t.Parallel()
	m, _ := exitedDockModel(t, 3, true)
	if got := m.statusView().TerminalRestart; got != "⟳ Restart terminal (exit 3)" {
		t.Fatalf("exit-code label = %q", got)
	}
	zero, _ := exitedDockModel(t, 0, true)
	if got := zero.statusView().TerminalRestart; got != "⟳ Restart terminal" {
		t.Fatalf("exit-0 label = %q, want the code omitted", got)
	}
	signal, _ := exitedDockModel(t, -1, true)
	if got := signal.statusView().TerminalRestart; got != "⟳ Restart terminal (signal)" {
		t.Fatalf("signal label = %q", got)
	}
	// A restored exited tab carries the launch record and exit status.
	restored := newTerminalModel(t, newFakeTree(3), dockKeys(), &shellsStarter{})
	restored.RestoreTerminal(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Live: false, Rows: []string{"out"},
		LaunchArgv: []string{"/usr/bin/npm", "run", "dev"}, ExitCode: 2, ExitKnown: true,
	})
	if got := restored.statusView().TerminalRestart; got != "⟳ Restart npm (exit 2)" {
		t.Fatalf("restored launch label = %q", got)
	}
	// A resume record rejoins the agent when terminal.resume_agents is on.
	resume := newTerminalModel(t, newFakeTree(3), dockKeys(), &shellsStarter{})
	resume.SetDock(DockPorts{Starter: &shellsStarter{}, Effects: dock.Effects{ResumeAgents: true}})
	resume.RestoreTerminal(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Live: false, Rows: []string{"out"},
		LaunchArgv: []string{"npm"}, ResumeArgv: []string{"/opt/bin/claude", "--resume", "x"},
		ExitCode: 0, ExitKnown: true,
	})
	if got := resume.statusView().TerminalRestart; got != "⟳ Resume claude" {
		t.Fatalf("resume label = %q", got)
	}
	// With resume_agents off the same record restarts the program.
	resumeOff := newTerminalModel(t, newFakeTree(3), dockKeys(), &shellsStarter{})
	resumeOff.RestoreTerminal(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Live: false, Rows: []string{"out"},
		LaunchArgv: []string{"npm"}, ResumeArgv: []string{"/opt/bin/claude", "--resume", "x"},
	})
	if got := resumeOff.statusView().TerminalRestart; got != "⟳ Restart claude" {
		t.Fatalf("resume-agents-off label = %q", got)
	}
}

// TestTerminalRestartHover checks the hover hit area is the clickable segment:
// moving onto it underlines the painted run, moving off clears it, and the click
// still restarts.
func TestTerminalRestartHover(t *testing.T) {
	t.Parallel()
	m, starter := exitedDockModel(t, 4, true)
	restart := restartSegment(t, m)
	if restart == nil {
		t.Fatalf("no restart segment: %+v", m.layout().Status)
	}
	if m.statusRestartHover {
		t.Fatal("the restart element starts hovered")
	}
	l := m.layout()
	m.Update(tea.MouseMotionMsg{X: restart.X + 1, Y: l.StatusY})
	if !m.statusRestartHover {
		t.Fatal("moving onto the restart segment did not hover it")
	}
	g := m.paint()
	cell := g.At(restart.X, l.StatusY)
	if !cell.Style.Underline || !cell.Style.Bold {
		t.Fatalf("hovered run style = %+v, want underline+bold", cell.Style)
	}
	m.Update(tea.MouseMotionMsg{X: 2, Y: 2})
	if m.statusRestartHover {
		t.Fatal("leaving the status row kept the hover")
	}
	if cell := m.paint().At(restart.X, l.StatusY); cell.Style.Underline {
		t.Fatalf("unhovered run is still underlined: %+v", cell.Style)
	}
	// Clicking the same segment still restarts, and the indicator disappears.
	_, cmd := m.Update(click(restart.X+1, l.StatusY, tea.MouseLeft))
	dockSettle(t, m, cmd)
	waitDock(t, m, "the click to restart the shell", func() bool { return len(starter.terms) == 2 && m.dock.Running() && !m.dock.Exited() })
	if len(starter.terms) != 2 || m.dock.Exited() {
		t.Fatalf("click did not restart: shells=%d exited=%v", len(starter.terms), m.dock.Exited())
	}
	if got := m.statusView().TerminalRestart; got != "" {
		t.Fatalf("a running dock still shows %q", got)
	}
}

// TestTerminalRestartStatusNarrow checks the longer label does not break a narrow
// status row: every painted run stays inside the frame.
func TestTerminalRestartStatusNarrow(t *testing.T) {
	t.Parallel()
	restored := newTerminalModel(t, newFakeTree(3), dockKeys(), &shellsStarter{})
	restored.RestoreTerminal(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Live: false, Rows: []string{"out"},
		LaunchArgv: []string{"/usr/bin/claude", "--x"}, ExitCode: 12, ExitKnown: true,
	})
	restored.Update(tea.WindowSizeMsg{Width: 24, Height: 12})
	l := restored.layout()
	for _, r := range l.Status {
		if r.X < 0 || r.X+max(len([]rune(r.Text)), 1) > 24 {
			t.Fatalf("narrow status run out of frame: %+v", r)
		}
	}
	restored.paint()
}

// TestRestoredRestartClickAndKeyboardParity checks a restored exited tab's status
// indicator: both the click on the painted segment and the keyboard-equivalent
// restart_terminal action restart the shell, and the indicator then clears.
func TestRestoredRestartClickAndKeyboardParity(t *testing.T) {
	t.Parallel()
	restore := func(t *testing.T, starter *shellsStarter) *Model {
		t.Helper()
		m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
		m.RestoreTerminal(terminal.SavedState{
			Version: terminal.SavedStateVersion, Open: true, Live: false, Rows: []string{"out"},
			LaunchArgv: []string{"/usr/bin/npm", "run", "dev"}, ExitCode: 5, ExitKnown: true,
		})
		return m
	}
	clickStarter := &shellsStarter{}
	clickModel := restore(t, clickStarter)
	restart := restartSegment(t, clickModel)
	if restart == nil {
		t.Fatalf("no restored restart segment: %+v", clickModel.layout().Status)
	}
	l := clickModel.layout()
	_, cmd := clickModel.Update(click(restart.X+1, l.StatusY, tea.MouseLeft))
	dockSettle(t, clickModel, cmd)
	waitDock(t, clickModel, "the restored click to restart the shell", func() bool {
		return len(clickStarter.terms) == 1 && clickModel.dock.Running() && !clickModel.dock.Exited()
	})
	if len(clickStarter.terms) != 1 || clickModel.dock.Exited() || !clickModel.dock.Running() {
		t.Fatalf("restored click: shells=%d exited=%v running=%v", len(clickStarter.terms), clickModel.dock.Exited(), clickModel.dock.Running())
	}
	if got := clickModel.statusView().TerminalRestart; got != "" {
		t.Fatalf("after the restored click the indicator is still %q", got)
	}

	keyStarter := &shellsStarter{}
	keyModel := restore(t, keyStarter)
	cmd = keyModel.dispatch(Command{Action: "restart_terminal"})
	if cmd == nil {
		t.Fatal("restart_terminal dispatch returned no command")
	}
	dockSettle(t, keyModel, cmd)
	waitDock(t, keyModel, "the keyboard restart to start the shell", func() bool {
		return len(keyStarter.terms) == 1 && keyModel.dock.Running() && !keyModel.dock.Exited()
	})
	if len(keyStarter.terms) != 1 || keyModel.dock.Exited() || !keyModel.dock.Running() {
		t.Fatalf("keyboard restart: shells=%d exited=%v running=%v", len(keyStarter.terms), keyModel.dock.Exited(), keyModel.dock.Running())
	}
}
