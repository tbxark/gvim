package editor

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// paragraphDoc has blank lines at 2 and 5 and no blank line after the last one.
const paragraphDoc = "one\ntwo\n\nthree\nfour\n\nfive"

func TestParagraphMoves(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		Text       string
		caret      buffer.Pos
		action     Action
		wantCursor buffer.Pos
		wantSel    *buffer.Selection
	}{
		{
			Name:       "move down to the next blank line",
			Text:       paragraphDoc,
			caret:      buffer.Pos{Line: 0, Col: 0},
			action:     "move_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 2, Col: 0},
		},
		{
			Name:       "move down again reaches the following blank line",
			Text:       paragraphDoc,
			caret:      buffer.Pos{Line: 2, Col: 0},
			action:     "move_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 5, Col: 0},
		},
		{
			Name:       "move down past the last blank line goes to the last character",
			Text:       paragraphDoc,
			caret:      buffer.Pos{Line: 5, Col: 0},
			action:     "move_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 6, Col: 3},
		},
		{
			Name:       "move down at the last character stays there",
			Text:       paragraphDoc,
			caret:      buffer.Pos{Line: 6, Col: 3},
			action:     "move_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 6, Col: 3},
		},
		{
			Name:       "move up to the blank line above",
			Text:       paragraphDoc,
			caret:      buffer.Pos{Line: 6, Col: 0},
			action:     "move_to_paragraph_up",
			wantCursor: buffer.Pos{Line: 5, Col: 0},
		},
		{
			Name:       "move up with no blank line above goes to the start",
			Text:       paragraphDoc,
			caret:      buffer.Pos{Line: 2, Col: 0},
			action:     "move_to_paragraph_up",
			wantCursor: buffer.Pos{Line: 0, Col: 0},
		},
		{
			Name:       "move down with a trailing newline stops at the last text character",
			Text:       "a\nb\n",
			caret:      buffer.Pos{Line: 0, Col: 0},
			action:     "move_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 1, Col: 0},
		},
		{
			Name:       "select down to the next blank line",
			Text:       paragraphDoc,
			caret:      buffer.Pos{Line: 0, Col: 1},
			action:     "select_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 2, Col: 0},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 0, Col: 1},
				Head:   buffer.Pos{Line: 2, Col: 0},
			},
		},
		{
			Name:       "select down with no blank line below goes to the end of the document",
			Text:       paragraphDoc,
			caret:      buffer.Pos{Line: 6, Col: 0},
			action:     "select_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 6, Col: 4},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 6, Col: 0},
				Head:   buffer.Pos{Line: 6, Col: 4},
			},
		},
		{
			Name:       "select down with a trailing newline goes past it",
			Text:       "a\nb\n",
			caret:      buffer.Pos{Line: 0, Col: 0},
			action:     "select_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 2, Col: 0},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 0, Col: 0},
				Head:   buffer.Pos{Line: 2, Col: 0},
			},
		},
		{
			Name:       "select up keeps the anchor at the caret",
			Text:       paragraphDoc,
			caret:      buffer.Pos{Line: 3, Col: 2},
			action:     "select_to_paragraph_up",
			wantCursor: buffer.Pos{Line: 2, Col: 0},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 3, Col: 2},
				Head:   buffer.Pos{Line: 2, Col: 0},
			},
		},
		{
			Name:       "select up with no blank line above goes to the start",
			Text:       paragraphDoc,
			caret:      buffer.Pos{Line: 1, Col: 0},
			action:     "select_to_paragraph_up",
			wantCursor: buffer.Pos{Line: 0, Col: 0},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 1, Col: 0},
				Head:   buffer.Pos{Line: 0, Col: 0},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.Text)
			e.SetCursor(tt.caret, false)
			run(t, e, tt.action)
			d := e.Active()
			if got := text(e); got != tt.Text {
				t.Fatalf("text changed to %q", got)
			}
			if d.Cursor != tt.wantCursor {
				t.Fatalf("caret = %+v, want %+v", d.Cursor, tt.wantCursor)
			}
			if !selectionEqual(d.Sel, tt.wantSel) {
				t.Fatalf("selection = %+v, want %+v", d.Sel, tt.wantSel)
			}
		})
	}
}
