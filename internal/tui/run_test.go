package tui

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/theme"
)

// TestRunResetsTheCaretShapeAtExit checks that the program leaves the caret shape the
// terminal had: the frame asks for a blinking block, and the renderer does not undo it.
func TestRunResetsTheCaretShapeAtExit(t *testing.T) {
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	in, release := io.Pipe()
	t.Cleanup(func() { _ = release.Close() })
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := runProgram(ctx, m, &out, tea.WithInput(in), tea.WithOutput(&out), tea.WithWindowSize(80, 24))
	if err == nil {
		t.Error("a canceled run reported no error")
	}
	got := out.String()
	if !strings.Contains(got, "\x1b[1 q") {
		t.Fatalf("the frame never set the blinking block: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0 q") {
		t.Errorf("the output ends with %q, want the caret shape reset", got[max(len(got)-24, 0):])
	}
}
