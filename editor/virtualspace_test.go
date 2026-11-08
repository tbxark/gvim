package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
)

// virtualDoc returns an editor over content with virtual space in mode, and line
// wrap off (a wrapped line has no empty region past its end, so virtual space
// needs unwrapped lines).
func virtualDoc(t *testing.T, mode, content string) *Editor {
	t.Helper()
	e := docWith(Options{VirtualSpace: mode}, content)
	e.SetLanguageSettings(func(language, path string) LanguageSettings {
		return LanguageSettings{TabSize: 4, LineWrap: false}
	})
	return e
}

// caretLineEnd puts the caret at the end of line line.
func caretLineEnd(t *testing.T, e *Editor, line int) {
	t.Helper()
	d := e.Active()
	e.SetCursor(buffer.Pos{Line: line, Col: d.Buffer.LineLen(line)}, false)
}

func TestVirtualSpaceRightAndLeft(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "abc\ndef")
	caretLineEnd(t, e, 0)
	run(t, e, "move_right")
	if d := e.Active(); d.Cursor != (buffer.Pos{Line: 0, Col: 3}) || d.VirtualColumns() != 1 {
		t.Fatalf("after one right: caret %+v virtual %d, want (0,3) and 1", d.Cursor, d.VirtualColumns())
	}
	run(t, e, "move_right", "move_right")
	if got := e.Active().VirtualColumns(); got != 3 {
		t.Fatalf("after three rights: virtual %d, want 3", got)
	}
	run(t, e, "move_left")
	if got := e.Active().VirtualColumns(); got != 2 {
		t.Fatalf("after one left: virtual %d, want 2", got)
	}
	run(t, e, "move_left", "move_left")
	if got := e.Active().VirtualColumns(); got != 0 {
		t.Fatalf("back on the text: virtual %d, want 0", got)
	}
	run(t, e, "move_left")
	if d := e.Active(); d.Cursor != (buffer.Pos{Line: 0, Col: 2}) {
		t.Fatalf("left from the line end: caret %+v, want (0,2)", d.Cursor)
	}
}

func TestVirtualSpaceTypingPadsTheGap(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "abc")
	caretLineEnd(t, e, 0)
	run(t, e, "move_right", "move_right")
	typeText(t, e, "x")
	if got := text(e); got != "abc  x" {
		t.Fatalf("text = %q, want the gap padded to %q", got, "abc  x")
	}
	if d := e.Active(); d.VirtualColumns() != 0 || d.Cursor != (buffer.Pos{Line: 0, Col: 6}) {
		t.Fatalf("after typing: virtual %d caret %+v", d.VirtualColumns(), d.Cursor)
	}
	// The padding and the typed text are one undo step.
	run(t, e, "undo")
	if got := text(e); got != "abc" {
		t.Fatalf("after one undo: text = %q, want abc", got)
	}
}

func TestVirtualSpaceBackspaceWalksBack(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "abc")
	caretLineEnd(t, e, 0)
	run(t, e, "move_right", "move_right", "move_right")
	for want := 2; want >= 0; want-- {
		run(t, e, "delete_backward")
		if got := e.Active().VirtualColumns(); got != want {
			t.Fatalf("backspace: virtual %d, want %d", got, want)
		}
		if got := text(e); got != "abc" {
			t.Fatalf("backspace changed the text: %q", got)
		}
	}
	// Once on the text, backspace deletes a character.
	run(t, e, "delete_backward")
	if got := text(e); got != "ab" {
		t.Fatalf("text = %q, want ab", got)
	}
}

func TestVirtualSpaceEnterAndDelete(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "abc\ndef")
	caretLineEnd(t, e, 0)
	run(t, e, "move_right", "move_right")
	// Enter at a virtual caret is a line break at the real end of the line: the
	// gap is not materialized.
	run(t, e, "insert_newline")
	if got := text(e); got != "abc\n\ndef" {
		t.Fatalf("text = %q, want a plain newline at the line end", got)
	}
	if d := e.Active(); d.Cursor.Line != 1 || d.VirtualColumns() != 0 {
		t.Fatalf("after Enter: caret %+v virtual %d", d.Cursor, d.VirtualColumns())
	}
}

func TestVirtualSpaceUpDownKeepsTheColumn(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "abcdef\n\nabcdef")
	// Put the caret at column 5 of line 0 (one cell before its end), then step
	// down through the short line: the wanted column is 5, so line 1 has all of
	// it in virtual space.
	d := e.Active()
	e.SetCursor(buffer.Pos{Line: 0, Col: 5}, false)
	run(t, e, "move_down")
	if d.Cursor != (buffer.Pos{Line: 1, Col: 0}) || d.VirtualColumns() != 5 {
		t.Fatalf("down to the empty line: caret %+v virtual %d, want (1,0) and 5", d.Cursor, d.VirtualColumns())
	}
	run(t, e, "move_down")
	if d.Cursor != (buffer.Pos{Line: 2, Col: 5}) || d.VirtualColumns() != 0 {
		t.Fatalf("down to the long line: caret %+v virtual %d, want (2,5) and 0", d.Cursor, d.VirtualColumns())
	}
	// Up again keeps the column too.
	run(t, e, "move_up")
	if d.Cursor != (buffer.Pos{Line: 1, Col: 0}) || d.VirtualColumns() != 5 {
		t.Fatalf("up: caret %+v virtual %d, want (1,0) and 5", d.Cursor, d.VirtualColumns())
	}
}

func TestVirtualSpacePastePads(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "abc")
	caretLineEnd(t, e, 0)
	run(t, e, "move_right")
	if r := e.Do("paste", map[string]any{"text": "Z"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if got := text(e); got != "abc Z" {
		t.Fatalf("text = %q, want %q", got, "abc Z")
	}
}

func TestVirtualSpaceDisabledKeepsTheCaretClamped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mode string
	}{
		{"off", document.VirtualSpaceOff},
		{"block is caret-clamped for now", document.VirtualSpaceBlock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := virtualDoc(t, tt.mode, "abc")
			caretLineEnd(t, e, 0)
			run(t, e, "move_right", "move_right")
			if d := e.Active(); d.VirtualColumns() != 0 || d.Cursor.Col != 3 {
				t.Fatalf("virtual %d caret %+v, want the caret clamped at (0,3)", d.VirtualColumns(), d.Cursor)
			}
		})
	}
}

func TestVirtualSpaceNeedsUnwrappedLines(t *testing.T) {
	t.Parallel()
	e := docWith(Options{VirtualSpace: document.VirtualSpaceOn}, "abc")
	// The default language settings wrap lines, so virtual space is off.
	caretLineEnd(t, e, 0)
	run(t, e, "move_right")
	if got := e.Active().VirtualColumns(); got != 0 {
		t.Fatalf("virtual %d with wrap on, want 0", got)
	}
}

func TestVirtualSpaceSwitchAndOtherMovesClearIt(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "abcdef")
	// move_line_end moves the caret and ends the virtual position.
	d := e.Active()
	e.SetCursor(buffer.Pos{Line: 0, Col: 6}, false)
	d.SetVirtualColumns(3)
	if got := d.VirtualColumns(); got != 3 {
		t.Fatalf("setup: virtual %d", got)
	}
	run(t, e, "move_line_start")
	if got := d.VirtualColumns(); got != 0 {
		t.Fatalf("a move that is not virtual-aware left virtual %d, want 0", got)
	}
	// Copy keeps it (it does not move the caret)...
	e.SetCursor(buffer.Pos{Line: 0, Col: 6}, false)
	d.SetVirtualColumns(2)
	run(t, e, "copy")
	if got := d.VirtualColumns(); got != 2 {
		t.Fatalf("copy cleared virtual space: %d", got)
	}
	// ...undo does not.
	run(t, e, "undo")
	if got := d.VirtualColumns(); got != 0 {
		t.Fatalf("undo kept virtual space: %d", got)
	}
}

func TestVirtualSpacePerBufferOverride(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOff, "abc")
	caretLineEnd(t, e, 0)
	run(t, e, "move_right")
	if got := e.Active().VirtualColumns(); got != 0 {
		t.Fatalf("virtual %d before the override", got)
	}
	if err := e.UpdateViewSettings(e.ActiveID(), func(s *ViewSettings) {
		on := true
		s.VirtualSpace = &on
	}); err != nil {
		t.Fatal(err)
	}
	run(t, e, "move_right")
	if got := e.Active().VirtualColumns(); got != 1 {
		t.Fatalf("virtual %d after the per-buffer toggle, want 1", got)
	}
}

// TestVirtualSpaceDeleteForwardJoinsClearingTheGap checks that deleting at a
// virtual caret keeps the text operation (join the next line) and drops the
// virtual position, instead of deleting at a column that no longer exists.
func TestVirtualSpaceDeleteForwardJoinsClearingTheGap(t *testing.T) {
	t.Parallel()
	e := virtualDoc(t, document.VirtualSpaceOn, "abc\ndef")
	caretLineEnd(t, e, 0)
	run(t, e, "move_right", "move_right")
	run(t, e, "delete_forward")
	if got := text(e); got != "abcdef" {
		t.Fatalf("text = %q, want the lines joined", got)
	}
	if got := e.Active().VirtualColumns(); got != 0 {
		t.Fatalf("virtual %d after delete, want 0", got)
	}
}
