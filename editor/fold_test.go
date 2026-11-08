package editor

import (
	"errors"
	"slices"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/fold"
)

// foldText has one fold, lines 0 to 2, that starts at "a".
const foldText = "a\n  b\n  c\nd\n"

// nestedFoldText has an outer fold, lines 0 to 3, and an inner fold, lines 1 to 3.
const nestedFoldText = "a\n  b\n    c\n    d\ne"

// foldedStarts returns the lines among 0..n-1 whose fold is collapsed.
func foldedStarts(d *Document, n int) []int {
	var out []int
	for line := range n {
		if d.Folded(line) {
			out = append(out, line)
		}
	}
	return out
}

func TestToggleFoldAtCaretLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		caret   int
		wantErr error
		folded  []int
	}{
		{"fold starts on the caret line", 0, nil, []int{0}},
		{"body line starts no fold", 1, ErrNoFold, nil},
		{"blank line starts no fold", 3, ErrNoFold, nil},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, _ := openFixture(t, Options{}, foldText)
			e.SetCursor(buffer.Pos{Line: tt.caret}, false)
			r := e.Do("toggle_fold", nil)
			if tt.wantErr == nil && r.Err != nil {
				t.Fatalf("toggle_fold: %v", r.Err)
			}
			if tt.wantErr != nil && !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("toggle_fold err = %v, want %v", r.Err, tt.wantErr)
			}
			if got := foldedStarts(e.Active(), 4); !slices.Equal(got, tt.folded) {
				t.Fatalf("folded starts = %v, want %v", got, tt.folded)
			}
		})
	}
}

func TestToggleFoldTwiceRestoresTheLines(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, foldText)
	run(t, e, "toggle_fold")
	run(t, e, "toggle_fold")
	d := e.Active()
	if d.Folded(0) {
		t.Fatal("second toggle_fold left the fold collapsed")
	}
	for line := range 4 {
		if d.Hidden(line, 4) {
			t.Fatalf("line %d hidden after unfold", line)
		}
	}
}

func TestFoldHidesBodyLines(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, foldText)
	run(t, e, "toggle_fold")
	d := e.Active()
	if got, want := d.Folds(4), []fold.Range{{Start: 0, End: 2}}; !slices.Equal(got, want) {
		t.Fatalf("Folds() = %v, want %v", got, want)
	}
	hidden := map[int]bool{0: false, 1: true, 2: true, 3: false, 4: false}
	for line, want := range hidden {
		if got := d.Hidden(line, 4); got != want {
			t.Fatalf("Hidden(%d) = %v, want %v", line, got, want)
		}
	}
}

func TestCaretLeavesFoldedLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name   string
		Text   string
		folds  []int      // folded fold starts before the action
		caret  buffer.Pos // caret before the action
		action Action
		Want   buffer.Pos
	}{
		{
			Name: "down from the fold start skips the body",
			Text: foldText, folds: []int{0},
			caret: buffer.Pos{Line: 0}, action: "move_down",
			Want: buffer.Pos{Line: 3},
		},
		{
			Name: "up from below lands on the fold start",
			Text: foldText, folds: []int{0},
			caret: buffer.Pos{Line: 3}, action: "move_up",
			Want: buffer.Pos{Line: 0},
		},
		{
			Name: "down into a fold that runs to the end stays at its start",
			Text: "a\n  b\n  c", folds: []int{0},
			caret: buffer.Pos{Line: 0}, action: "move_down",
			Want: buffer.Pos{Line: 0},
		},
		{
			Name: "a caret placed inside a fold moves to its start",
			Text: foldText, folds: []int{0},
			caret: buffer.Pos{Line: 2, Col: 1}, action: "noop",
			Want: buffer.Pos{Line: 0, Col: 1},
		},
		{
			Name: "a caret in a nested fold moves to the outer start",
			Text: nestedFoldText, folds: []int{0, 1},
			caret: buffer.Pos{Line: 2}, action: "noop",
			Want: buffer.Pos{Line: 0},
		},
		{
			Name: "a caret in a nested fold moves to the inner start when only it is folded",
			Text: nestedFoldText, folds: []int{1},
			caret: buffer.Pos{Line: 2}, action: "noop",
			Want: buffer.Pos{Line: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.Text)
			d := e.Active()
			d.SetFoldedLines(map[int]bool{})
			for _, line := range tt.folds {
				d.FoldedLines()[line] = true
			}
			d.Cursor = tt.caret
			run(t, e, tt.action)
			if d.Cursor != tt.Want {
				t.Fatalf("caret = %+v, want %+v", d.Cursor, tt.Want)
			}
		})
	}
}

func TestToggleFoldUnfoldsAStaleStart(t *testing.T) {
	t.Parallel()
	// After the text above a fold changes, a collapsed start may no longer begin a
	// fold. Toggling it must still expand it, so it cannot get stuck collapsed.
	e := docWith(Options{}, "x\ny")
	d := e.Active()
	d.SetFoldedLines(map[int]bool{0: true})
	run(t, e, "toggle_fold")
	if d.Folded(0) {
		t.Fatal("toggle_fold did not clear a collapsed start with no fold")
	}
}

func TestSelectionLeavesFoldedLinesAtBothEnds(t *testing.T) {
	t.Parallel()
	// The anchor is inside the collapsed fold and the head is below it. Both ends
	// must leave the hidden body, not only the head.
	e := docWith(Options{}, foldText)
	d := e.Active()
	d.SetFoldedLines(map[int]bool{0: true})
	e.SelectRange(caretPos(2, 1), caretPos(3, 0))
	run(t, e, "noop")
	if d.Cursor != caretPos(3, 0) {
		t.Fatalf("head = %+v, want 3:0", d.Cursor)
	}
	if d.Sel == nil || d.Sel.Anchor != caretPos(0, 1) {
		t.Fatalf("anchor = %+v, want 0:1 (the fold start, column kept)", d.Sel)
	}
}

func TestCollapsingAFoldMovesExtraCaretsOutOfItsBody(t *testing.T) {
	t.Parallel()
	// Three carets: the primary on the fold start, and two in its body. Collapsing
	// the fold moves the body carets to the fold start, where they meet the primary
	// and merge into one caret.
	e := docWith(Options{}, foldText)
	e.SetCursor(caretPos(2, 1), false)
	run(t, e, "add_cursor_above", "add_cursor_above")
	if got := caretState(e); len(got.extras) != 2 {
		t.Fatalf("setup carets = %v, want two extra carets", got)
	}
	run(t, e, "toggle_fold")
	want := carets{Primary: Caret{Cursor: caretPos(0, 1)}}
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets after collapsing = %v, want %v", got, want)
	}
}

func TestFoldRevealRunsOnTheTabThatBecomesActive(t *testing.T) {
	t.Parallel()
	// The first tab has a collapsed fold and its caret in the body. The action
	// switches to it from the second tab, so the reveal must run on the first tab.
	e := docWith(Options{}, foldText)
	folded := e.Active()
	folded.SetFoldedLines(map[int]bool{0: true})
	folded.Cursor = caretPos(1, 0)
	e.docs = append(e.docs, newDocument("", "x\n"))
	e.switchTo(1)
	run(t, e, "prev_buffer")
	if e.Active() != folded {
		t.Fatal("prev_buffer did not activate the folded tab")
	}
	if folded.Cursor != caretPos(0, 0) {
		t.Fatalf("caret in the folded tab = %+v, want 0:0", folded.Cursor)
	}
}

// TestFoldsAreKeptPerVersionAndTabWidth checks that the ranges are not found again for a
// frame when the text and the tab width are the same (every repaint and every key asks),
// and that an edit, an undo and a tab width that changes the ranges are seen.
func TestFoldsAreKeptPerVersionAndTabWidth(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "a\n  b\n  c\nd\n")
	d := e.Active()
	same := func(x, y []fold.Range) bool { return len(x) > 0 && len(y) > 0 && &x[0] == &y[0] }

	first := d.Folds(4)
	if want := []fold.Range{{Start: 0, End: 2}}; !slices.Equal(first, want) {
		t.Fatalf("Folds = %v, want %v", first, want)
	}
	if again := d.Folds(4); !same(first, again) {
		t.Error("Folds found the ranges again for the same text and tab width")
	}

	e.SetCursor(buffer.Pos{Line: 3}, false)
	if r := e.Do("insert_char", map[string]any{"text": "\n  x"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if got, want := d.Folds(4), []fold.Range{{Start: 0, End: 4}}; !slices.Equal(got, want) {
		t.Errorf("Folds after an edit = %v, want %v", got, want)
	}
	run(t, e, "undo")
	if got, want := d.Folds(4), []fold.Range{{Start: 0, End: 2}}; !slices.Equal(got, want) {
		t.Errorf("Folds after undo = %v, want %v", got, want)
	}

	// The tab is indented more than the two spaces under it only when it is 4 wide.
	tabs, _ := openFixture(t, Options{}, "\ta\n  b\n")
	td := tabs.Active()
	if got := td.Folds(4); len(got) != 0 {
		t.Fatalf("Folds(4) = %v, want none", got)
	}
	if got, want := td.Folds(1), []fold.Range{{Start: 0, End: 1}}; !slices.Equal(got, want) {
		t.Fatalf("Folds(1) = %v, want %v", got, want)
	}
	if got := td.Folds(4); len(got) != 0 {
		t.Fatalf("Folds(4) again = %v, want none", got)
	}
}
