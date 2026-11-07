package editor

import (
	"errors"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestViActionsAreListed(t *testing.T) {
	t.Parallel()
	for _, a := range []Action{"toggle_case", "move_left_in_line", "move_right_in_line", "vi_move_up", "vi_move_down"} {
		if !slices.Contains(Actions(), a) {
			t.Errorf("Actions() does not list %q", a)
		}
	}
}

func TestViMoves(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		text   string
		at     buffer.Pos
		action Action
		want   buffer.Pos
	}{
		{"left in the line", "abc\ndef", buffer.Pos{Line: 0, Col: 2}, "move_left_in_line", buffer.Pos{Line: 0, Col: 1}},
		{"left stops at the line start", "abc\ndef", buffer.Pos{Line: 1, Col: 0}, "move_left_in_line", buffer.Pos{Line: 1, Col: 0}},
		{"left over a wide character", "a€b", buffer.Pos{Line: 0, Col: 2}, "move_left_in_line", buffer.Pos{Line: 0, Col: 1}},
		{"right in the line", "abc\ndef", buffer.Pos{Line: 0, Col: 0}, "move_right_in_line", buffer.Pos{Line: 0, Col: 1}},
		{"right stops on the last character", "abc\ndef", buffer.Pos{Line: 0, Col: 2}, "move_right_in_line", buffer.Pos{Line: 0, Col: 2}},
		{"right pulls a caret past the end back", "abc\ndef", buffer.Pos{Line: 0, Col: 3}, "move_right_in_line", buffer.Pos{Line: 0, Col: 2}},
		{"right on an empty line", "\ndef", buffer.Pos{Line: 0, Col: 0}, "move_right_in_line", buffer.Pos{Line: 0, Col: 0}},
		{"down keeps the column", "abcdef\nghijkl", buffer.Pos{Line: 0, Col: 4}, "vi_move_down", buffer.Pos{Line: 1, Col: 4}},
		{"down onto a shorter line lands on its last character", "abcdef\nxy", buffer.Pos{Line: 0, Col: 4}, "vi_move_down", buffer.Pos{Line: 1, Col: 1}},
		{"down onto an empty line", "abcdef\n\nxyz", buffer.Pos{Line: 0, Col: 4}, "vi_move_down", buffer.Pos{Line: 1, Col: 0}},
		{"down on the last line stays", "abc\ndef", buffer.Pos{Line: 1, Col: 1}, "vi_move_down", buffer.Pos{Line: 1, Col: 1}},
		{"up keeps the column", "abcdef\nghijkl", buffer.Pos{Line: 1, Col: 3}, "vi_move_up", buffer.Pos{Line: 0, Col: 3}},
		{"up onto a shorter line lands on its last character", "xy\nabcdef", buffer.Pos{Line: 1, Col: 4}, "vi_move_up", buffer.Pos{Line: 0, Col: 1}},
		{"up on the first line stays", "abc\ndef", buffer.Pos{Line: 0, Col: 2}, "vi_move_up", buffer.Pos{Line: 0, Col: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.text)
			e.SetCursor(tt.at, false)
			run(t, e, tt.action)
			if got := e.Active().Cursor; got != tt.want {
				t.Fatalf("caret = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestViVerticalKeepsTheWantedColumn: j and k remember the column they started in, so a
// pass over a short line does not pull the caret to the left for good.
func TestViVerticalKeepsTheWantedColumn(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "abcdef\nxy\nghijkl")
	e.SetCursor(buffer.Pos{Line: 0, Col: 4}, false)
	steps := []struct {
		action Action
		want   buffer.Pos
	}{
		{"vi_move_down", buffer.Pos{Line: 1, Col: 1}},
		{"vi_move_down", buffer.Pos{Line: 2, Col: 4}},
		{"vi_move_up", buffer.Pos{Line: 1, Col: 1}},
		{"vi_move_up", buffer.Pos{Line: 0, Col: 4}},
	}
	for _, s := range steps {
		run(t, e, s.action)
		if got := e.Active().Cursor; got != s.want {
			t.Fatalf("after %s the caret is %+v, want %+v", s.action, got, s.want)
		}
	}
}

func TestToggleCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		at         placed
		wantText   string
		wantCursor buffer.Pos
	}{
		{"a lower case letter", "abc", placed{caret: buffer.Pos{Col: 0}}, "Abc", buffer.Pos{Col: 1}},
		{"an upper case letter", "ABC", placed{caret: buffer.Pos{Col: 1}}, "AbC", buffer.Pos{Col: 2}},
		{"a digit only moves on", "a1b", placed{caret: buffer.Pos{Col: 1}}, "a1b", buffer.Pos{Col: 2}},
		{"a non-ASCII letter", "éa", placed{caret: buffer.Pos{Col: 0}}, "Éa", buffer.Pos{Col: 1}},
		{"at the end of the line there is nothing", "ab\ncd", placed{caret: buffer.Pos{Line: 0, Col: 2}}, "ab\ncd", buffer.Pos{Line: 0, Col: 2}},
		{"an empty line", "\ncd", placed{caret: buffer.Pos{Line: 0, Col: 0}}, "\ncd", buffer.Pos{Line: 0, Col: 0}},
		{
			"a selection is swapped as a whole", "Hello World",
			placed{sel: &buffer.Selection{Anchor: buffer.Pos{Col: 0}, Head: buffer.Pos{Col: 5}}},
			"hELLO World", buffer.Pos{Col: 5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.text)
			tt.at.apply(e)
			run(t, e, "toggle_case")
			d := e.Active()
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if d.Cursor != tt.wantCursor {
				t.Fatalf("caret = %+v, want %+v", d.Cursor, tt.wantCursor)
			}
			if d.HasSelection() {
				t.Fatalf("selection = %+v, want none", d.Sel)
			}
		})
	}
}

func TestToggleCaseAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		carets   []caret
		wantText string
		want     carets
	}{
		{"two lines", "ab\ncd", []caret{at(0, 0), at(1, 1)}, "Ab\ncD", wantCarets(plain(0, 1), plain(1, 2))},
		{"a digit caret moves with the edits before it", "a1 b", []caret{at(0, 0), at(0, 1)}, "A1 b", wantCarets(plain(0, 1), plain(0, 2))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := multiDoc(tt.text, tt.carets...)
			run(t, e, "toggle_case")
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %s, want %s", got, tt.want)
			}
			run(t, e, "undo")
			if got := text(e); got != tt.text {
				t.Fatalf("text after one undo = %q, want %q", got, tt.text)
			}
		})
	}
}

// TestToggleCaseRefusedOnReadOnly: toggle_case changes text, so a read-only document
// refuses it and keeps its text. TestEveryActionLeavesReadOnlyTextAlone covers it as well;
// this is the direct row.
func TestToggleCaseRefusedOnReadOnly(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "abc")
	run(t, e, "toggle_read_only")
	r := e.Do("toggle_case", nil)
	if !errors.Is(r.Err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", r.Err)
	}
	if got := text(e); got != "abc" {
		t.Fatalf("text = %q after the refused toggle_case", got)
	}
	if !textChanging["toggle_case"] {
		t.Fatal("toggle_case is not in textChanging")
	}
}
