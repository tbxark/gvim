package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestFindSelectionNextAndPrevious(t *testing.T) {
	t.Parallel()
	// The three "foo" matches start at columns 0, 8 and 16.
	const text = "foo bar foo baz foo"
	tests := []struct {
		Name    string
		Text    string
		action  Action
		caret   int
		Anchor  int // selection anchor column; -1 for no selection
		Head    int // selection head column
		wantSel string
		wantCol int // caret column after the search
		wantErr bool
	}{
		{Name: "next from a word goes to the next match", Text: text, action: "find_selection_next", caret: 1, Anchor: -1, wantSel: "foo", wantCol: 11},
		{Name: "next past the last match wraps to the first", Text: text, action: "find_selection_next", caret: 16, Anchor: -1, wantSel: "foo", wantCol: 3},
		{Name: "previous from the first match wraps to the last", Text: text, action: "find_selection_previous", caret: 1, Anchor: -1, wantSel: "foo", wantCol: 19},
		{Name: "previous from the middle match goes to the first", Text: text, action: "find_selection_previous", caret: 9, Anchor: -1, wantSel: "foo", wantCol: 3},
		{Name: "a selection is searched", Text: text, action: "find_selection_next", caret: 7, Anchor: 4, Head: 7, wantSel: "bar", wantCol: 7},
		{Name: "a caret with no word beside it has no text", Text: "foo ; foo", action: "find_selection_next", caret: 4, Anchor: -1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.Text)
			marksCaret(e, 0, tt.caret)
			if tt.Anchor >= 0 {
				d := e.Active()
				d.Sel = &buffer.Selection{Anchor: buffer.Pos{Col: tt.Anchor}, Head: buffer.Pos{Col: tt.Head}}
			}
			r := e.Do(tt.action, nil)
			if (r.Err != nil) != tt.wantErr {
				t.Fatalf("%s err = %v, want error %v", tt.action, r.Err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got := marksSelected(e); got != tt.wantSel {
				t.Fatalf("selection = %q, want %q", got, tt.wantSel)
			}
			if got := e.Active().Cursor.Col; got != tt.wantCol {
				t.Fatalf("caret col = %d, want %d", got, tt.wantCol)
			}
		})
	}
}
