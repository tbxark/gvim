package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// actionStep runs the actions in order as one step of a block selection test.
func actionStep(as ...Action) func(t *testing.T, e *Editor) {
	return func(t *testing.T, e *Editor) {
		t.Helper()
		run(t, e, as...)
	}
}

// moveStep places the primary caret at p, as a click or a motion would.
func moveStep(p buffer.Pos) func(t *testing.T, e *Editor) {
	return func(_ *testing.T, e *Editor) {
		e.SetCursor(p, false)
	}
}

func TestBlockSelect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name  string
		Text  string
		Start buffer.Pos
		steps []func(t *testing.T, e *Editor)
		Want  carets
	}{
		{
			Name:  "block_select_down puts a caret on the next line",
			Text:  "abcd\nefgh\nijkl",
			Start: caretPos(0, 1),
			steps: []func(*testing.T, *Editor){actionStep("block_select_down")},
			Want: carets{
				Primary: Caret{Cursor: caretPos(1, 1)},
				extras:  []Caret{{Cursor: caretPos(0, 1)}},
			},
		},
		{
			Name:  "down and right grow a rectangle with one selection per line",
			Text:  "abcd\nefgh\nijkl",
			Start: caretPos(0, 1),
			steps: []func(*testing.T, *Editor){
				actionStep("block_select_down", "block_select_down", "block_select_right", "block_select_right"),
			},
			Want: carets{
				Primary: Caret{Cursor: caretPos(2, 3), Sel: caretSel(caretPos(2, 1), caretPos(2, 3))},
				extras: []Caret{
					{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 1), caretPos(0, 3))},
					{Cursor: caretPos(1, 3), Sel: caretSel(caretPos(1, 1), caretPos(1, 3))},
				},
			},
		},
		{
			Name:  "block_select_left shrinks the rectangle backward",
			Text:  "abcd\nefgh",
			Start: caretPos(0, 3),
			steps: []func(*testing.T, *Editor){actionStep("block_select_left")},
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 2), Sel: caretSel(caretPos(0, 3), caretPos(0, 2))},
			},
		},
		{
			Name:  "block_select_up on the first line leaves one caret",
			Text:  "abcd\nefgh",
			Start: caretPos(0, 1),
			steps: []func(*testing.T, *Editor){actionStep("block_select_up")},
			Want:  carets{Primary: Caret{Cursor: caretPos(0, 1)}},
		},
		{
			Name:  "block_select_right at the end of a line stops there",
			Text:  "abcd\nefgh",
			Start: caretPos(0, 4),
			steps: []func(*testing.T, *Editor){actionStep("block_select_right")},
			Want:  carets{Primary: Caret{Cursor: caretPos(0, 4)}},
		},
		{
			Name:  "down onto a shorter line clamps the caret and keeps the rectangle",
			Text:  "abcdef\nab\nabcdef",
			Start: caretPos(0, 4),
			steps: []func(*testing.T, *Editor){actionStep("block_select_down", "block_select_down")},
			Want: carets{
				Primary: Caret{Cursor: caretPos(2, 2), Sel: caretSel(caretPos(2, 4), caretPos(2, 2))},
				extras: []Caret{
					{Cursor: caretPos(0, 2), Sel: caretSel(caretPos(0, 4), caretPos(0, 2))},
					{Cursor: caretPos(1, 2)},
				},
			},
		},
		{
			Name:  "a caret moved by another action starts a new block",
			Text:  "abcd\nefgh",
			Start: caretPos(0, 1),
			steps: []func(*testing.T, *Editor){
				actionStep("block_select_down"),
				moveStep(caretPos(0, 3)),
				actionStep("block_select_right"),
			},
			Want: carets{Primary: Caret{Cursor: caretPos(0, 4), Sel: caretSel(caretPos(0, 3), caretPos(0, 4))}},
		},
		{
			Name:  "remove_secondary_cursors ends the block",
			Text:  "abcd\nefgh",
			Start: caretPos(0, 1),
			steps: []func(*testing.T, *Editor){
				actionStep("block_select_down"),
				actionStep("remove_secondary_cursors"),
				actionStep("block_select_right"),
			},
			Want: carets{Primary: Caret{Cursor: caretPos(1, 2), Sel: caretSel(caretPos(1, 1), caretPos(1, 2))}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.Text)
			e.SetCursor(tt.Start, false)
			for _, step := range tt.steps {
				step(t, e)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestTypingReplacesEachLineOfABlock(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "abcd\nefgh")
	e.SetCursor(caretPos(0, 1), false)
	run(t, e, "block_select_down", "block_select_right", "block_select_right")
	typeKey(t, e, "X")
	if text(e) != "aXd\neXh" {
		t.Fatalf("text = %q, want %q", text(e), "aXd\neXh")
	}
	want := carets{
		Primary: Caret{Cursor: caretPos(1, 2)},
		extras:  []Caret{{Cursor: caretPos(0, 2)}},
	}
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets = %v, want %v", got, want)
	}
}

func TestBlockSelectHorizontalStepMovesByCluster(t *testing.T) {
	t.Parallel()
	// The second cluster is e followed by a combining acute, two runes wide.
	const text = "ae\u0301b"
	tests := []struct {
		Name  string
		Start buffer.Pos
		steps []Action
		Want  carets
	}{
		{
			Name:  "right steps over the whole cluster",
			Start: caretPos(0, 0),
			steps: []Action{"block_select_right", "block_select_right"},
			Want:  carets{Primary: Caret{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))}},
		},
		{
			Name:  "left steps back over the whole cluster",
			Start: caretPos(0, 3),
			steps: []Action{"block_select_left"},
			Want:  carets{Primary: Caret{Cursor: caretPos(0, 1), Sel: caretSel(caretPos(0, 3), caretPos(0, 1))}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(text, tt.Start.Line, tt.Start.Col)
			run(t, e, tt.steps...)
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
			}
		})
	}
}
