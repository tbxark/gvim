package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
)

// caretBufferEnd puts the caret at the end of the buffer.
func caretBufferEnd(t *testing.T, e *Editor) {
	t.Helper()
	e.SetCursor(e.Active().Buffer.End(), false)
}

func TestVirtualLinesArrowDownParksBelowTheBufferEnd(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "abc\ndef")
	caretBufferEnd(t, e)
	run(t, e, "move_down")
	d := e.Active()
	if got := d.VirtualLines(); got != 1 {
		t.Fatalf("VirtualLines = %d after one down, want 1", got)
	}
	if got := d.VirtualLineColumn(); got != 3 {
		t.Fatalf("VirtualLineColumn = %d, want the line end column 3", got)
	}
	run(t, e, "move_down")
	if got := e.Active().VirtualLines(); got != 2 {
		t.Fatalf("VirtualLines = %d after two downs, want 2", got)
	}
	// Up steps back and ends on the buffer end's text.
	run(t, e, "move_up")
	if got := e.Active().VirtualLines(); got != 1 {
		t.Fatalf("VirtualLines = %d after up, want 1", got)
	}
	run(t, e, "move_up")
	d = e.Active()
	if d.VirtualLines() != 0 || d.Cursor != d.Buffer.End() {
		t.Fatalf("after the last up: lines %d caret %+v, want the buffer end", d.VirtualLines(), d.Cursor)
	}
}

func TestVirtualLinesKeepTheGoalColumn(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "abc\ndef")
	caretLineEnd(t, e, 1)
	run(t, e, "move_right", "move_right") // caret (1,3), virtual 2
	run(t, e, "move_down")
	d := e.Active()
	if d.VirtualLines() != 1 || d.VirtualLineColumn() != 5 {
		t.Fatalf("lines %d column %d, want the on-screen column 5", d.VirtualLines(), d.VirtualLineColumn())
	}
	// From the first virtual line, Up restores the horizontal virtual caret.
	run(t, e, "move_up")
	if d.VirtualLines() != 0 || d.VirtualColumns() != 2 {
		t.Fatalf("after up: lines %d virtual %d, want the round-2 caret", d.VirtualLines(), d.VirtualColumns())
	}
}

func TestVirtualLinesTypingMaterializesNewlinesAndPadding(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "ab")
	if !e.SetVirtualLines(2, 3) {
		t.Fatal("SetVirtualLines was refused")
	}
	typeText(t, e, "x")
	if got := text(e); got != "ab\n\n   x" {
		t.Fatalf("text = %q, want two newlines and three spaces before x", got)
	}
	d := e.Active()
	if d.VirtualLines() != 0 || d.Cursor != (buffer.Pos{Line: 2, Col: 4}) {
		t.Fatalf("after typing: caret %+v lines %d", d.Cursor, d.VirtualLines())
	}
	// The line breaks, the padding and the typed character are one undo step.
	run(t, e, "undo")
	if got := text(e); got != "ab" {
		t.Fatalf("after one undo: %q", got)
	}
}

func TestVirtualLinesBackspaceStepsBack(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "ab")
	if !e.SetVirtualLines(2, 3) {
		t.Fatal("SetVirtualLines was refused")
	}
	run(t, e, "delete_backward")
	if got := e.Active().VirtualLines(); got != 1 {
		t.Fatalf("VirtualLines = %d after backspace, want 1", got)
	}
	run(t, e, "delete_backward")
	d := e.Active()
	if d.VirtualLines() != 0 || d.VirtualColumns() != 1 {
		t.Fatalf("after the last backspace: lines %d virtual %d, want the horizontal caret", d.VirtualLines(), d.VirtualColumns())
	}
	if got := text(e); got != "ab" {
		t.Fatalf("backspace changed the text: %q", got)
	}
	run(t, e, "delete_backward") // virtual 1 -> 0
	if got := text(e); got != "ab" {
		t.Fatalf("text = %q, want it unchanged by walking back", got)
	}
	run(t, e, "delete_backward")
	if got := text(e); got != "a" {
		t.Fatalf("text = %q, want the last character deleted once back on the text", got)
	}
}

func TestVirtualLinesEnterIsAPlainNewline(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "ab")
	if !e.SetVirtualLines(1, 2) {
		t.Fatal("SetVirtualLines was refused")
	}
	run(t, e, "insert_newline")
	if got := text(e); got != "ab\n" {
		t.Fatalf("text = %q, want a plain newline at the buffer end", got)
	}
	if got := e.Active().VirtualLines(); got != 0 {
		t.Fatalf("VirtualLines = %d after Enter, want 0", got)
	}
}

func TestVirtualLinesCopyKeepsTheLastLine(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "ab\ncd")
	if !e.SetVirtualLines(2, 1) {
		t.Fatal("SetVirtualLines was refused")
	}
	if got := result(t, e, "copy", nil).Text; got != "cd" {
		t.Fatalf("copied %q, want the last line", got)
	}
	if got := e.Active().VirtualLines(); got != 2 {
		t.Fatalf("copy cleared the virtual lines: %d", got)
	}
}

func TestVirtualLinesNotInBlockOrOffMode(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{document.VirtualSpaceOff, document.VirtualSpaceBlock} {
		t.Run(mode, func(t *testing.T) {
			e := virtualDoc(t, mode, "ab")
			caretBufferEnd(t, e)
			run(t, e, "move_down")
			if got := e.Active().VirtualLines(); got != 0 {
				t.Fatalf("VirtualLines = %d in mode %q, want none", got, mode)
			}
			if e.SetVirtualLines(1, 0) {
				t.Fatalf("SetVirtualLines was allowed in mode %q", mode)
			}
		})
	}
}

func TestVirtualLinesNeedUnwrappedLines(t *testing.T) {
	t.Parallel()
	e := docWith(Options{VirtualSpace: document.VirtualSpaceOn}, "ab")
	caretBufferEnd(t, e)
	if e.SetVirtualLines(1, 0) {
		t.Fatal("virtual lines were allowed with wrap on")
	}
	run(t, e, "move_down")
	if got := e.Active().VirtualLines(); got != 0 {
		t.Fatalf("VirtualLines = %d with wrap on", got)
	}
}

func TestVirtualLinesEndOnOtherMoves(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "ab\ncd")
	if !e.SetVirtualLines(2, 1) {
		t.Fatal("SetVirtualLines was refused")
	}
	run(t, e, "move_line_start")
	if got := e.Active().VirtualLines(); got != 0 {
		t.Fatalf("a plain move kept %d virtual lines", got)
	}
}

func TestSetVirtualLinesGoesToTheBufferEnd(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "ab\ncd")
	e.SetCursor(buffer.Pos{Line: 0, Col: 1}, false)
	if !e.SetVirtualLines(1, 5) {
		t.Fatal("SetVirtualLines was refused")
	}
	d := e.Active()
	if d.Cursor != d.Buffer.End() || d.VirtualLines() != 1 || d.VirtualLineColumn() != 5 {
		t.Fatalf("caret %+v lines %d column %d, want the buffer end at column 5", d.Cursor, d.VirtualLines(), d.VirtualLineColumn())
	}
}

func TestVirtualLinesSelectionEndsThem(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "ab\ncd")
	if !e.SetVirtualLines(1, 1) {
		t.Fatal("SetVirtualLines was refused")
	}
	run(t, e, "select_up")
	d := e.Active()
	if d.VirtualLines() != 0 || !d.HasSelection() {
		t.Fatalf("shift+up: lines %d selection %v, want a clamped linear selection", d.VirtualLines(), d.HasSelection())
	}
}
