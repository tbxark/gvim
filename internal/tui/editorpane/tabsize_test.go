package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/fold"
)

func TestTabSizeSetsTheTabStopOfTextCaretAndMouse(t *testing.T) {
	tests := []struct {
		name    string
		tabSize int
		x       int // the screen column of the b after "a" and a tab, past the gutter of 6
	}{
		{"zero is the default of four", 0, 6 + 4},
		{"two", 2, 6 + 2},
		{"eight", 8, 6 + 8},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoc("a\tb")
			d.cursor = buffer.Pos{Col: 2}
			p := Pane{Doc: d, TabSize: tc.tabSize}
			if got := cellAt(t, p.Render(30, 1), tc.x, 0).Text; got != "b" {
				t.Errorf("cell %d = %q, want the b after the tab", tc.x, got)
			}
			if x, _, ok := p.Caret(30, 1); !ok || x != tc.x {
				t.Errorf("caret column = %d (%v), want %d", x, ok, tc.x)
			}
			if h := p.Hit(30, 1, tc.x, 0); h.Kind != HitText || h.Pos.Col != 2 {
				t.Errorf("hit on the b = %+v, want text at column 2", h)
			}
		})
	}
}

func TestTabSizeSetsTheIndentGuideColumns(t *testing.T) {
	// "\t\tx" has two indent levels, so the guides sit at the start of each level.
	tests := []struct {
		name    string
		tabSize int
		guide   int
	}{
		{"two", 2, 2},
		{"four", 4, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{Doc: newDoc("\t\tx"), IndentGuides: true, TabSize: tc.tabSize}
			if got := cellAt(t, p.Render(30, 1), 6+tc.guide, 0).Text; got != "│" {
				t.Errorf("cell %d = %q, want the indent guide", 6+tc.guide, got)
			}
		})
	}
}

// widthFoldDoc is a document whose folds record the tab width the pane asked for.
type widthFoldDoc struct {
	*fakeDoc
	width *int
}

func (d widthFoldDoc) Folds(tabWidth int) []fold.Range { *d.width = tabWidth; return nil }
func (d widthFoldDoc) Folded(int) bool                 { return false }

func TestFoldsUseTheTabSize(t *testing.T) {
	tests := []struct {
		name    string
		tabSize int
		want    int
	}{
		{"zero is the default of four", 0, 4},
		{"eight", 8, 8},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got int
			doc := widthFoldDoc{fakeDoc: newDoc("x"), width: &got}
			Pane{Doc: doc, TabSize: tc.tabSize}.Render(20, 1)
			if got != tc.want {
				t.Errorf("folds asked for tab width %d, want %d", got, tc.want)
			}
		})
	}
}
