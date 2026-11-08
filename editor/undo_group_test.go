package editor

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
)

// undoClock is a manually advanced clock for the active buffer. Its base is
// a whole second, so a step under one second stays in the same undo bucket.
type undoClock struct {
	t time.Time
}

func newUndoClock() *undoClock {
	return &undoClock{t: time.Date(2026, time.November, 5, 9, 0, 0, 0, time.UTC)}
}

func (c *undoClock) now() time.Time { return c.t }

func (c *undoClock) step(d time.Duration) { c.t = c.t.Add(d) }

// clockedEditor returns an editor whose active buffer reads c for undo
// grouping. The caller must set the clock again after anything that replaces
// the buffer, such as OpenFile.
func clockedEditor(c *undoClock) *Editor {
	e := New(Options{})
	e.Active().Buffer.SetClock(c.now)
	return e
}

func TestTypedWordIsOneUndoStep(t *testing.T) {
	t.Parallel()
	c := newUndoClock()
	e := clockedEditor(c)
	typeText(t, e, "one")
	run(t, e, "undo")
	if got := text(e); got != "" {
		t.Fatalf("undo of one bucket = %q, want empty", got)
	}
}

func TestClockStepSplitsTypedWords(t *testing.T) {
	t.Parallel()
	c := newUndoClock()
	e := clockedEditor(c)
	typeText(t, e, "one")
	c.step(1200 * time.Millisecond)
	typeText(t, e, "two")

	run(t, e, "undo")
	if got := text(e); got != "one" {
		t.Fatalf("first undo = %q, want %q", got, "one")
	}
	run(t, e, "undo")
	if got := text(e); got != "" {
		t.Fatalf("second undo = %q, want empty", got)
	}
}

func TestKeyboardMovementBreaksTypingGroup(t *testing.T) {
	t.Parallel()
	c := newUndoClock()
	e := clockedEditor(c)
	typeText(t, e, "ab")
	run(t, e, "move_left")
	typeText(t, e, "c")
	if got := text(e); got != "acb" {
		t.Fatalf("after typing = %q, want %q", got, "acb")
	}
	run(t, e, "undo")
	if got := text(e); got != "ab" {
		t.Fatalf("undo after move = %q, want %q", got, "ab")
	}
}

// TestPointerMovementBreaksTypingGroup covers the cursor paths that do not go
// through Do: mouse clicks and drags, and the go-to-line prompt. Each one
// moves the caret inside the same wall-clock bucket as the typing before it,
// so the next typed rune must start a new undo step.
func TestPointerMovementBreaksTypingGroup(t *testing.T) {
	t.Parallel()
	moves := []struct {
		Name string
		move func(e *Editor)
	}{
		{"SetCursor", func(e *Editor) { e.SetCursor(buffer.Pos{}, false) }},
		{"SelectRange", func(e *Editor) { e.SelectRange(buffer.Pos{}, buffer.Pos{}) }},
		{"GotoLine", func(e *Editor) {
			if err := e.GotoLine(1, 1); err != nil {
				t.Fatalf("GotoLine: %v", err)
			}
		}},
	}
	for _, m := range moves {
		t.Run(m.Name, func(t *testing.T) {
			t.Parallel()
			c := newUndoClock()
			e := clockedEditor(c)
			typeText(t, e, "ab")
			m.move(e)
			typeText(t, e, "c")
			if got := text(e); got != "cab" {
				t.Fatalf("after typing = %q, want %q", got, "cab")
			}
			run(t, e, "undo")
			if got := text(e); got != "ab" {
				t.Fatalf("undo after %s = %q, want %q", m.Name, got, "ab")
			}
		})
	}
}

func TestDragSelectionBreaksTypingGroup(t *testing.T) {
	t.Parallel()
	c := newUndoClock()
	e := clockedEditor(c)
	typeText(t, e, "ab")
	e.ExtendSelection(buffer.Pos{}, UnitChar)
	typeText(t, e, "c")
	if got := text(e); got != "c" {
		t.Fatalf("after replacing selection = %q, want %q", got, "c")
	}
	run(t, e, "undo")
	if got := text(e); got != "ab" {
		t.Fatalf("undo after selection = %q, want %q", got, "ab")
	}
}

func TestSaveBreaksTypingGroup(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "undo.txt")
	e := New(Options{})
	if err := e.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	c := newUndoClock()
	e.Active().Buffer.SetClock(c.now)

	typeText(t, e, "ab")
	if err := e.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	typeText(t, e, "c")
	run(t, e, "undo")
	if got := text(e); got != "ab" {
		t.Fatalf("undo after save = %q, want %q", got, "ab")
	}
}
