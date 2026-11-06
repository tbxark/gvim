package editor

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// paragraphDoc has blank lines at 2 and 5 and no blank line after the last one.
const paragraphDoc = "one\ntwo\n\nthree\nfour\n\nfive"

func TestParagraphMoves(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		caret      buffer.Pos
		action     Action
		wantCursor buffer.Pos
		wantSel    *buffer.Selection
	}{
		{
			name:       "move down to the next blank line",
			text:       paragraphDoc,
			caret:      buffer.Pos{Line: 0, Col: 0},
			action:     "move_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 2, Col: 0},
		},
		{
			name:       "move down again reaches the following blank line",
			text:       paragraphDoc,
			caret:      buffer.Pos{Line: 2, Col: 0},
			action:     "move_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 5, Col: 0},
		},
		{
			name:       "move down past the last blank line goes to the last character",
			text:       paragraphDoc,
			caret:      buffer.Pos{Line: 5, Col: 0},
			action:     "move_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 6, Col: 3},
		},
		{
			name:       "move down at the last character stays there",
			text:       paragraphDoc,
			caret:      buffer.Pos{Line: 6, Col: 3},
			action:     "move_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 6, Col: 3},
		},
		{
			name:       "move up to the blank line above",
			text:       paragraphDoc,
			caret:      buffer.Pos{Line: 6, Col: 0},
			action:     "move_to_paragraph_up",
			wantCursor: buffer.Pos{Line: 5, Col: 0},
		},
		{
			name:       "move up with no blank line above goes to the start",
			text:       paragraphDoc,
			caret:      buffer.Pos{Line: 2, Col: 0},
			action:     "move_to_paragraph_up",
			wantCursor: buffer.Pos{Line: 0, Col: 0},
		},
		{
			name:       "move down with a trailing newline stops at the last text character",
			text:       "a\nb\n",
			caret:      buffer.Pos{Line: 0, Col: 0},
			action:     "move_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 1, Col: 0},
		},
		{
			name:       "select down to the next blank line",
			text:       paragraphDoc,
			caret:      buffer.Pos{Line: 0, Col: 1},
			action:     "select_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 2, Col: 0},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 0, Col: 1},
				Head:   buffer.Pos{Line: 2, Col: 0},
			},
		},
		{
			name:       "select down with no blank line below goes to the end of the document",
			text:       paragraphDoc,
			caret:      buffer.Pos{Line: 6, Col: 0},
			action:     "select_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 6, Col: 4},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 6, Col: 0},
				Head:   buffer.Pos{Line: 6, Col: 4},
			},
		},
		{
			name:       "select down with a trailing newline goes past it",
			text:       "a\nb\n",
			caret:      buffer.Pos{Line: 0, Col: 0},
			action:     "select_to_paragraph_down",
			wantCursor: buffer.Pos{Line: 2, Col: 0},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 0, Col: 0},
				Head:   buffer.Pos{Line: 2, Col: 0},
			},
		},
		{
			name:       "select up keeps the anchor at the caret",
			text:       paragraphDoc,
			caret:      buffer.Pos{Line: 3, Col: 2},
			action:     "select_to_paragraph_up",
			wantCursor: buffer.Pos{Line: 2, Col: 0},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 3, Col: 2},
				Head:   buffer.Pos{Line: 2, Col: 0},
			},
		},
		{
			name:       "select up with no blank line above goes to the start",
			text:       paragraphDoc,
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
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.text)
			e.SetCursor(tt.caret, false)
			run(t, e, tt.action)
			d := e.Active()
			if got := text(e); got != tt.text {
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
