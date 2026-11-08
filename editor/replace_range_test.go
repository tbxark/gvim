package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/search"
)

// TestReplaceMatchesRespectsTheSearchRange checks that Replace All changes only the matches
// that the find range holds, the same matches the find bar counts. Without a range every
// match is replaced.
func TestReplaceMatchesRespectsTheSearchRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Text     string
		Sel      bool       // find_in_selection runs on the selection from anchor to head
		Anchor   buffer.Pos // selection anchor, when sel is set
		Head     buffer.Pos // selection head, when sel is set
		repl     string
		wantN    int
		wantText string
	}{
		{
			Name:     "no range replaces every match",
			Text:     "a\na\na\n",
			repl:     "b",
			wantN:    3,
			wantText: "b\nb\nb\n",
		},
		{
			Name:     "find in selection replaces only the match inside it",
			Text:     "a\na\na\n",
			Sel:      true,
			Anchor:   buffer.Pos{Line: 1},
			Head:     buffer.Pos{Line: 1, Col: 1},
			repl:     "b",
			wantN:    1,
			wantText: "a\nb\na\n",
		},
		{
			Name:     "every match inside the range is replaced, and the rest is kept",
			Text:     "a a\na\n",
			Sel:      true,
			Anchor:   buffer.Pos{},
			Head:     buffer.Pos{Col: 3},
			repl:     "b",
			wantN:    2,
			wantText: "b b\na\n",
		},
		{
			Name:     "a replacement that adds lines keeps the range of the other matches",
			Text:     "a a\na\n",
			Sel:      true,
			Anchor:   buffer.Pos{},
			Head:     buffer.Pos{Col: 3},
			repl:     "x\ny",
			wantN:    2,
			wantText: "x\ny x\ny\na\n",
		},
		{
			Name:     "a range with no match changes nothing",
			Text:     "a\nb\na\n",
			Sel:      true,
			Anchor:   buffer.Pos{Line: 1},
			Head:     buffer.Pos{Line: 1, Col: 1},
			repl:     "c",
			wantN:    0,
			wantText: "a\nb\na\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.Text)
			if tt.Sel {
				e.SelectRange(tt.Anchor, tt.Head)
				run(t, e, "find_in_selection")
			}
			n, err := e.ReplaceMatches(search.Query{Text: "a", CaseSensitive: true}, tt.repl)
			if err != nil {
				t.Fatalf("ReplaceMatches: %v", err)
			}
			if n != tt.wantN {
				t.Fatalf("replaced = %d, want %d", n, tt.wantN)
			}
			if got := e.Active().Buffer.Text(); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
		})
	}
}

// TestReplaceMatchesRangeIsOneUndoStep checks that a range-limited Replace All is one undo
// step, as an unlimited one is.
func TestReplaceMatchesRangeIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "a\na\na\n")
	e.SelectRange(buffer.Pos{}, buffer.Pos{Line: 1, Col: 1})
	run(t, e, "find_in_selection")
	if _, err := e.ReplaceMatches(search.Query{Text: "a", CaseSensitive: true}, "b"); err != nil {
		t.Fatalf("ReplaceMatches: %v", err)
	}
	run(t, e, "undo")
	if got := e.Active().Buffer.Text(); got != "a\na\na\n" {
		t.Fatalf("after undo text = %q, want the original", got)
	}
}
