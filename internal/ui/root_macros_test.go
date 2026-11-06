package ui

import (
	"slices"
	"testing"
)

// TestListMacrosShowsTheRecordedSteps checks that List Macros lists the steps of the
// kept macro in order, and that it says so when no macro has been recorded.
func TestListMacrosShowsTheRecordedSteps(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	rootPaletteRun(m, "list_macros")
	if m.notice != "No macro recorded" {
		t.Fatalf("notice = %q, want the no-macro notice", m.notice)
	}

	m.root.macro.last = []macroStep{
		{cmd: Command{Action: "insert_char"}},
		{cmd: Command{Action: "save"}},
	}
	rootPaletteRun(m, "list_macros")
	rows, ok := overlayRows(m)
	if !ok {
		t.Fatal("list_macros did not open a list overlay")
	}
	sameLines(t, "macro steps", rows, []string{"1. insert_char=", "2. save="})
}

// TestPlayMacroReplaysTheKeptMacro checks that Play Macro replays the one macro the
// recorder keeps, through the editor, as the typed commands would run.
func TestPlayMacroReplaysTheKeptMacro(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 24)
	m.root.macro.last = []macroStep{
		{cmd: Command{Action: "insert_char", Args: map[string]any{"text": "a"}}},
		{cmd: Command{Action: "insert_newline"}},
	}
	rootPaletteRun(m, "prompt_play_macro")
	if got, want := ed.actionNames(), []string{"insert_char", "insert_newline"}; !slices.Equal(got, want) {
		t.Fatalf("editor ran %q, want %q", got, want)
	}
}

// TestPlayMacroWithNoMacroSaysSo checks that playing with nothing recorded runs no
// editor action and says there is no macro.
func TestPlayMacroWithNoMacroSaysSo(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 24)
	rootPaletteRun(m, "prompt_play_macro")
	if len(ed.actions) != 0 {
		t.Fatalf("editor ran %q with no macro", ed.actionNames())
	}
	if m.notice != "No macro recorded" {
		t.Fatalf("notice = %q, want the no-macro notice", m.notice)
	}
}
