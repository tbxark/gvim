package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/fold"
)

// foldSpy is a document with folds: lines 1 to 3 fold into line 0, and are folded. It
// counts the questions about folds, and is large or not by large.
type foldSpy struct {
	*spyDoc
	large         bool
	folds, folded int
}

func (d *foldSpy) Large() bool { return d.large }
func (d *foldSpy) Folds(int) []fold.Range {
	d.folds++
	return []fold.Range{{Start: 0, End: 3}}
}
func (d *foldSpy) Folded(int) bool {
	d.folded++
	return true
}

func TestPaneLargeDocAsksForNoFolds(t *testing.T) {
	text := func(i int) string { return "    line " + strings.Repeat("x", i%7) }
	tests := []struct {
		name      string
		large     bool
		wantAsked bool
		wantRow1  string // the number on the second row
	}{
		{"large", true, false, "2"},
		{"small", false, true, "5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &foldSpy{spyDoc: newSpy(40, text), large: tt.large}
			p := Pane{Doc: spy, IndentGuides: true, Opts: Options{GuideActive: true}}
			g := p.Render(40, 5)
			if asked := spy.folds+spy.folded > 0; asked != tt.wantAsked {
				t.Errorf("Folds asked %d times, Folded %d times, want asked = %v", spy.folds, spy.folded, tt.wantAsked)
			}
			if got := strings.Fields(g.Plain()[1])[0]; got != tt.wantRow1 {
				t.Errorf("second row number = %q, want %q (lines are hidden only when folds are used)", got, tt.wantRow1)
			}
		})
	}
}

func TestPaneLargeDocGuidesReadOnlyTheHeadOfTheLinesAround(t *testing.T) {
	long := "    " + strings.Repeat("y", 100_000)
	spy := newSpy(300, func(i int) string {
		if i%2 == 0 {
			return long
		}
		return "        short"
	})
	spy.cursor = buffer.Pos{Line: 100}
	Pane{Doc: spy, Top: 100, IndentGuides: true, Opts: Options{GuideActive: true}}.Render(60, 8)
	if spy.decoded > 600*headRunes {
		t.Errorf("the guide scans walked %d runes of other lines, want about %d at most", spy.decoded, 600*headRunes)
	}
}
