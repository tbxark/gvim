package tui

import (
	"testing"

	"github.com/tbxark/gvim/editor"
)

// TestFormatBufferRunsOffTheUpdateLoop checks that format_buffer, like a shell command, is
// prepared in Update and runs in a command. A formatter can take up to the shell timeout,
// so Update must not run it.
func TestFormatBufferRunsOffTheUpdateLoop(t *testing.T) {
	t.Parallel()
	f := &shellFake{fakeEditor: newFakeEditor("hello"), job: editor.ShellJob{Command: "fmt", Replace: true}}
	m := newShellModel(f)
	cmd, ok := m.uiAction("format_buffer", nil)
	if !ok || cmd == nil {
		t.Fatalf("uiAction = %v, %v; want a command", cmd, ok)
	}
	if len(f.actions) != 0 {
		t.Fatalf("Do ran %v; format_buffer must not run in Do", f.actions)
	}
	if len(f.begun) != 1 || f.begun[0] != "format_buffer" {
		t.Fatalf("BeginShell calls = %v, want one format_buffer", f.begun)
	}
}
