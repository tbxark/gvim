package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestRedrawScreenStartsAClearScreen checks that Redraw Screen returns the clear, so
// the next frame repaints every cell.
func TestRedrawScreenStartsAClearScreen(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	msgs := collect(rootPaletteRun(m, "redraw_screen"))
	if len(msgs) != 1 || reflect.TypeOf(msgs[0]) != reflect.TypeOf(tea.ClearScreen()) {
		t.Fatalf("redraw_screen sent %v, want one clear-screen message", msgs)
	}
}

// TestResumeRepaintsTheScreen checks that the program's resume after a suspend clears
// the screen, because the terminal was given back to the shell in between.
func TestResumeRepaintsTheScreen(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	_, cmd := m.Update(tea.ResumeMsg{})
	msgs := collect(cmd)
	if len(msgs) != 1 || reflect.TypeOf(msgs[0]) != reflect.TypeOf(tea.ClearScreen()) {
		t.Fatalf("resume sent %v, want one clear-screen message", msgs)
	}
}

// TestSuspendProcessSuspendsWhereSupported checks that Suspend Process starts the
// program's suspend on Unix, and that Windows gets a notice instead. The bubbletea
// program sends SIGTSTP to the process group and restores the terminal on SIGCONT.
func TestSuspendProcessSuspendsWhereSupported(t *testing.T) {
	saved := suspendSupported
	t.Cleanup(func() { suspendSupported = saved })

	suspendSupported = true
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	msgs := collect(rootPaletteRun(m, "suspend_process"))
	if len(msgs) != 1 {
		t.Fatalf("suspend_process sent %d messages, want the suspend", len(msgs))
	}
	if _, ok := msgs[0].(tea.SuspendMsg); !ok {
		t.Fatalf("suspend_process sent %T, want tea.SuspendMsg", msgs[0])
	}

	suspendSupported = false
	m = newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	if cmd := rootPaletteRun(m, "suspend_process"); cmd != nil {
		t.Fatal("suspend_process started a command where suspend is not supported")
	}
	if m.notice != "Suspend is not available on Windows" {
		t.Fatalf("notice = %q, want the Windows notice", m.notice)
	}
}
