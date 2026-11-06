package ui

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/search"
)

// rangeFake is a searchFake whose editor reports a find range, as the editor does after
// find_in_selection.
type rangeFake struct {
	*searchFake
	sel     buffer.Selection
	limited bool
}

// SearchRange reports the find range of the fake.
func (f *rangeFake) SearchRange() (buffer.Selection, bool) { return f.sel, f.limited }

// The find host keeps only the matches inside the editor's search range, so the find bar
// and its highlights follow find_in_selection.
func TestFindHostLimitsMatchesToTheSearchRange(t *testing.T) {
	line0 := buffer.Selection{Anchor: buffer.Pos{Line: 0, Col: 0}, Head: buffer.Pos{Line: 0, Col: 11}}
	last := buffer.Selection{Anchor: buffer.Pos{Line: 1, Col: 0}, Head: buffer.Pos{Line: 1, Col: 3}}
	tests := []struct {
		name    string
		limited bool
		sel     buffer.Selection
		want    int
	}{
		{"whole document", false, buffer.Selection{}, 3},
		{"first line", true, line0, 2},
		{"last word", true, last, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &rangeFake{
				searchFake: &searchFake{fileEditorFake: &fileEditorFake{fakeEditor: newFakeEditor("foo bar foo", "foo")}},
				sel:        tt.sel,
				limited:    tt.limited,
			}
			m := newRouteModel(t, f)
			got, err := findHost{m: m}.Find(search.Query{Text: "foo", CaseSensitive: true})
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if len(got) != tt.want {
				t.Errorf("%d matches, want %d", len(got), tt.want)
			}
		})
	}
}
