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
		name    string
		text    string
		action  Action
		caret   int
		anchor  int // selection anchor column; -1 for no selection
		head    int // selection head column
		wantSel string
		wantCol int // caret column after the search
		wantErr bool
	}{
		{name: "next from a word goes to the next match", text: text, action: "find_selection_next", caret: 1, anchor: -1, wantSel: "foo", wantCol: 11},
		{name: "next past the last match wraps to the first", text: text, action: "find_selection_next", caret: 16, anchor: -1, wantSel: "foo", wantCol: 3},
		{name: "previous from the first match wraps to the last", text: text, action: "find_selection_previous", caret: 1, anchor: -1, wantSel: "foo", wantCol: 19},
		{name: "previous from the middle match goes to the first", text: text, action: "find_selection_previous", caret: 9, anchor: -1, wantSel: "foo", wantCol: 3},
		{name: "a selection is searched", text: text, action: "find_selection_next", caret: 7, anchor: 4, head: 7, wantSel: "bar", wantCol: 7},
		{name: "a caret with no word beside it has no text", text: "foo ; foo", action: "find_selection_next", caret: 4, anchor: -1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.text)
			marksCaret(e, 0, tt.caret)
			if tt.anchor >= 0 {
				d := e.Active()
				d.Sel = &buffer.Selection{Anchor: buffer.Pos{Col: tt.anchor}, Head: buffer.Pos{Col: tt.head}}
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
