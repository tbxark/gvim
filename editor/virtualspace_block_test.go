package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
)

// blockDoc returns an editor over content with virtual space mode and line wrap
// off, with the caret at start.
func blockDoc(t *testing.T, mode, content string) *Editor {
	t.Helper()
	e := virtualDoc(t, mode, content)
	return e
}

// blockAt starts a block selection at p and runs steps.
func blockAt(t *testing.T, e *Editor, p buffer.Pos, steps ...Action) {
	t.Helper()
	e.SetCursor(p, false)
	run(t, e, steps...)
}

// result runs one action and returns its Result.
func result(t *testing.T, e *Editor, a Action, args map[string]any) Result {
	t.Helper()
	r := e.Do(a, args)
	if r.Err != nil {
		t.Fatalf("%s: %v", a, r.Err)
	}
	return r
}

func TestBlockRectPastEndOfLinesCopy(t *testing.T) {
	t.Parallel()
	const content = "abcde\nab\nabcdef"
	tests := []struct {
		name string
		mode string
		want string
	}{
		{"block mode copies a true rectangle", document.VirtualSpaceBlock, "e \n  \nef"},
		{"on mode copies a true rectangle", document.VirtualSpaceOn, "e \n  \nef"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := blockDoc(t, tt.mode, content)
			// Rectangle columns [4,6) over lines 0..2: line 0 ends at 5, line 1
			// at 2, line 2 at 6.
			blockAt(t, e, buffer.Pos{Line: 0, Col: 4}, "block_select_down", "block_select_down",
				"block_select_right", "block_select_right")
			if got := result(t, e, "copy", nil).Text; got != tt.want {
				t.Fatalf("copied %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBlockRectPastEndOfLinesCut(t *testing.T) {
	t.Parallel()
	e := blockDoc(t, document.VirtualSpaceBlock, "abcde\nab\nabcdef")
	e.SetCursor(buffer.Pos{Line: 0, Col: 4}, false)
	run(t, e, "block_select_down", "block_select_down", "block_select_right", "block_select_right")
	if got := result(t, e, "cut", nil).Text; got != "e \n  \nef" {
		t.Fatalf("cut copied %q", got)
	}
	if got := text(e); got != "abcd\nab\nabcd" {
		t.Fatalf("text = %q, want the real cells of the rectangle deleted", got)
	}
}

func TestBlockRectPastEndOfLinesPaste(t *testing.T) {
	t.Parallel()
	e := blockDoc(t, document.VirtualSpaceBlock, "abcde\nab\nabcdef")
	e.SetCursor(buffer.Pos{Line: 0, Col: 4}, false)
	run(t, e, "block_select_down", "block_select_down", "block_select_right", "block_select_right")
	result(t, e, "paste", map[string]any{"text": "XY\nZ\nQ"})
	if got := text(e); got != "abcdXY\nab  Z\nabcdQ" {
		t.Fatalf("text = %q, want the column pasted with padding", got)
	}
}

func TestBlockRectPastEndOfLinesTyping(t *testing.T) {
	t.Parallel()
	e := blockDoc(t, document.VirtualSpaceBlock, "abcde\nab\nabcdef")
	e.SetCursor(buffer.Pos{Line: 0, Col: 4}, false)
	run(t, e, "block_select_down", "block_select_down", "block_select_right", "block_select_right")
	typeText(t, e, "X")
	if got := text(e); got != "abcdX\nab  X\nabcdX" {
		t.Fatalf("text = %q, want X on each line with the pad", got)
	}
	// The padding and the typed characters are one undo step.
	run(t, e, "undo")
	if got := text(e); got != "abcde\nab\nabcdef" {
		t.Fatalf("after one undo: %q", got)
	}
}

func TestBlockRectPastEndOfLinesTypingIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e := blockDoc(t, document.VirtualSpaceBlock, "ab\ncd")
	e.SetCursor(buffer.Pos{Line: 0, Col: 2}, false)
	run(t, e, "block_select_down", "block_select_right")
	typeText(t, e, "X")
	// Both lines end at the rectangle's left edge, so no padding is needed.
	if got := text(e); got != "abX\ncdX" {
		t.Fatalf("text = %q", got)
	}
	run(t, e, "undo")
	if got := text(e); got != "ab\ncd" {
		t.Fatalf("after undo: %q", got)
	}
}

func TestBlockRectZeroWidthCopiesNothing(t *testing.T) {
	t.Parallel()
	e := blockDoc(t, document.VirtualSpaceBlock, "abcde\nab")
	e.SetCursor(buffer.Pos{Line: 0, Col: 4}, false)
	run(t, e, "block_select_down")
	if r := e.Do("copy", nil); r.Err != ErrNothingSelected {
		t.Fatalf("copy of a zero-width rectangle: err %v, text %q", r.Err, r.Text)
	}
}

func TestBlockRectZeroWidthTypingPadsShortLines(t *testing.T) {
	t.Parallel()
	e := blockDoc(t, document.VirtualSpaceBlock, "abcde\nab")
	e.SetCursor(buffer.Pos{Line: 0, Col: 5}, false)
	run(t, e, "block_select_down")
	// The rectangle is the column past both line ends: line 0 ends at 5, line 1
	// at 2, so line 1 needs three spaces before x.
	typeText(t, e, "x")
	if got := text(e); got != "abcdex\nab   x" {
		t.Fatalf("text = %q, want the column typed with padding", got)
	}
}

func TestBlockRectSingleClipboardLinePastesDownTheColumn(t *testing.T) {
	t.Parallel()
	e := blockDoc(t, document.VirtualSpaceBlock, "ab\ncd\nef")
	e.SetCursor(buffer.Pos{Line: 0, Col: 2}, false)
	run(t, e, "block_select_down", "block_select_down", "block_select_right")
	result(t, e, "paste", map[string]any{"text": "XY"})
	if got := text(e); got != "abXY\ncdXY\nefXY" {
		t.Fatalf("text = %q, want every line to get the one clipboard line", got)
	}
}

func TestBlockRectStartsFromAVirtualCaret(t *testing.T) {
	t.Parallel()
	e := blockDoc(t, document.VirtualSpaceOn, "abc\nabcdef")
	caretLineEnd(t, e, 0)
	run(t, e, "move_right", "move_right") // caret (0,3), virtual 2
	run(t, e, "block_select_down", "block_select_right")
	// The block column is the on-screen column 5, so the rectangle is columns 5..6.
	if got := result(t, e, "copy", nil).Text; got != " \nf" {
		t.Fatalf("copied %q, want the on-screen rectangle", got)
	}
}

func TestBlockModeKeepsTheCaretClamped(t *testing.T) {
	t.Parallel()
	e := blockDoc(t, document.VirtualSpaceBlock, "abc")
	caretLineEnd(t, e, 0)
	run(t, e, "move_right")
	if got := e.Active().VirtualColumns(); got != 0 {
		t.Fatalf("virtual %d in block mode, want the caret clamped", got)
	}
}
