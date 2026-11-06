package editor

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestSelectionOrWord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		caret        int
		anchor, head int // anchor -1 for no selection
		want         string
		wantFrom     int
		wantTo       int
		wantOK       bool
	}{
		{name: "word at the caret", caret: 5, anchor: -1, want: "bar", wantFrom: 4, wantTo: 7, wantOK: true},
		{name: "word just before the caret", caret: 7, anchor: -1, want: "bar", wantFrom: 4, wantTo: 7, wantOK: true},
		{name: "the selection wins", caret: 2, anchor: 0, head: 2, want: "fo", wantFrom: 0, wantTo: 2, wantOK: true},
		{name: "no word", caret: 8, anchor: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withDoc("foo bar  x")
			marksCaret(e, 0, tt.caret)
			if tt.anchor >= 0 {
				e.Active().Sel = &buffer.Selection{Anchor: buffer.Pos{Col: tt.anchor}, Head: buffer.Pos{Col: tt.head}}
			}
			text, from, to, ok, err := e.SelectionOrWord()
			if err != nil || ok != tt.wantOK || text != tt.want || from.Col != tt.wantFrom || to.Col != tt.wantTo {
				t.Fatalf("SelectionOrWord() = %q %v %v %v", text, from, to, ok)
			}
		})
	}
}
