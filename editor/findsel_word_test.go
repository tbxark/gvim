package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestSelectionOrWord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name         string
		caret        int
		Anchor, Head int // anchor -1 for no selection
		Want         string
		wantFrom     int
		wantTo       int
		wantOK       bool
	}{
		{Name: "word at the caret", caret: 5, Anchor: -1, Want: "bar", wantFrom: 4, wantTo: 7, wantOK: true},
		{Name: "word just before the caret", caret: 7, Anchor: -1, Want: "bar", wantFrom: 4, wantTo: 7, wantOK: true},
		{Name: "the selection wins", caret: 2, Anchor: 0, Head: 2, Want: "fo", wantFrom: 0, wantTo: 2, wantOK: true},
		{Name: "no word", caret: 8, Anchor: -1},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc("foo bar  x")
			marksCaret(e, 0, tt.caret)
			if tt.Anchor >= 0 {
				e.Active().Sel = &buffer.Selection{Anchor: buffer.Pos{Col: tt.Anchor}, Head: buffer.Pos{Col: tt.Head}}
			}
			text, from, to, ok, err := e.SelectionOrWord()
			if err != nil || ok != tt.wantOK || text != tt.Want || from.Col != tt.wantFrom || to.Col != tt.wantTo {
				t.Fatalf("SelectionOrWord() = %q %v %v %v", text, from, to, ok)
			}
		})
	}
}
