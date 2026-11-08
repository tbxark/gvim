package tui

import (
	"errors"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/theme"
)

// bookmarkErrEditor is a fake editor whose jump fails, as it does for a slot that is
// not set.
type bookmarkErrEditor struct {
	*fakeEditor
}

func (e bookmarkErrEditor) Do(a editor.Action, args map[string]any) editor.Result {
	res := e.fakeEditor.Do(a, args)
	if a == "jump_to_bookmark" {
		res.Err = errors.New("bookmark '3' not set")
	}
	return res
}

// answerSlot types text into the slot prompt that is open, presses Enter, and feeds
// the prompt's answer back to the model, as the program does.
func answerSlot(t *testing.T, m *Model, text string) {
	t.Helper()
	for _, r := range text {
		press(m, typed(string(r)))
	}
	cmd := press(m, special(tea.KeyEnter))
	for _, msg := range collect(cmd) {
		m.Update(msg)
	}
}

// TestSetBookmarkPromptSetsTheSlot checks that Set Bookmark asks for a slot from 0 to
// 9, and that the answer sets that slot in the editor.
func TestSetBookmarkPromptSetsTheSlot(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 24)
	rootPaletteRun(m, "prompt_set_bookmark")
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the slot prompt", len(m.overlays))
	}
	answerSlot(t, m, "7")
	if !slices.Contains(ed.actionNames(), "set_bookmark") {
		t.Fatalf("editor ran %q, want set_bookmark", ed.actionNames())
	}
	if got := ed.args[len(ed.args)-1]["char"]; got != "7" {
		t.Fatalf("set_bookmark char = %v, want 7", got)
	}
	if len(m.overlays) != 0 {
		t.Fatal("the slot prompt is still open after its answer")
	}
}

// TestBookmarkPromptRejectsANonDigit checks that a slot that is not a digit keeps the
// prompt open and runs nothing.
func TestBookmarkPromptRejectsANonDigit(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 24)
	rootPaletteRun(m, "prompt_set_bookmark")
	answerSlot(t, m, "a")
	if slices.Contains(ed.actionNames(), "set_bookmark") {
		t.Fatal("set_bookmark ran for a slot that is not a digit")
	}
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the prompt still open", len(m.overlays))
	}
}

// TestJumpToBookmarkPromptJumps checks that Jump to Bookmark asks for a slot and runs
// jump_to_bookmark for it.
func TestJumpToBookmarkPromptJumps(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 24)
	rootPaletteRun(m, "prompt_jump_to_bookmark")
	answerSlot(t, m, "2")
	if got := ed.args[len(ed.args)-1]["char"]; got != "2" {
		t.Fatalf("jump_to_bookmark char = %v, want 2", got)
	}
	if ed.actionNames()[len(ed.actionNames())-1] != "jump_to_bookmark" {
		t.Fatalf("editor ran %q, want jump_to_bookmark last", ed.actionNames())
	}
}

// TestJumpToUnsetBookmarkShowsTheError checks that a jump the editor refuses shows its
// error on the status row.
func TestJumpToUnsetBookmarkShowsTheError(t *testing.T) {
	t.Parallel()
	m := New(Deps{Editor: bookmarkErrEditor{fakeEditor: newFakeEditor("x")}, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	rootPaletteRun(m, "prompt_jump_to_bookmark")
	answerSlot(t, m, "3")
	if m.notice != "bookmark '3' not set" {
		t.Fatalf("notice = %q, want the editor's error", m.notice)
	}
}

// TestBookmarkPromptIsALinePrompt checks that the slot prompt is a line prompt, so the
// root's prompt routing applies to it.
func TestBookmarkPromptIsALinePrompt(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	rootPaletteRun(m, "prompt_set_bookmark")
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the slot prompt", len(m.overlays))
	}
	if _, ok := m.overlays[0].(lineOverlay); !ok {
		t.Fatalf("overlay is %T, want a line prompt", m.overlays[0])
	}
}
