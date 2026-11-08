package sidebar

import "testing"

// sumRows is the body rows the layout gave, plus one per header and the bottom
// border. With every section collapsed the difference from the column's height
// is the ground under the headers.
func sumRows(secs []Section) int {
	total := 1 // the bottom border
	for _, s := range secs {
		total += 1 + s.Rows()
	}
	return total
}

func TestLayoutSharesRows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		height    int
		specs     []Spec
		wantRows  []int
		collapsed []bool
	}{
		{
			name:     "explorer shares, contents takes its requested rows",
			height:   20,
			specs:    []Spec{{Kind: KindExplorer}, {Kind: KindContents, Rows: 10}},
			wantRows: []int{7, 10},
		},
		{
			name:      "contents collapsed gives its rows to the explorer",
			height:    20,
			specs:     []Spec{{Kind: KindExplorer}, {Kind: KindContents, Rows: 10, Collapsed: true}},
			wantRows:  []int{17, 0},
			collapsed: []bool{false, true},
		},
		{
			name:      "a collapsed explorer gives the column to the contents",
			height:    20,
			specs:     []Spec{{Kind: KindExplorer, Collapsed: true}, {Kind: KindContents, Rows: 10}},
			wantRows:  []int{0, 17},
			collapsed: []bool{true, false},
		},
		{
			name:      "every section collapsed is headers over ground",
			height:    20,
			specs:     []Spec{{Kind: KindExplorer, Collapsed: true}, {Kind: KindContents, Rows: 10, Collapsed: true}},
			wantRows:  []int{0, 0},
			collapsed: []bool{true, true},
		},
		{
			name:     "a fixed section shrinks to leave the sharing one a row",
			height:   6,
			specs:    []Spec{{Kind: KindExplorer}, {Kind: KindContents, Rows: 10}},
			wantRows: []int{1, 2},
		},
		{
			name:      "a squeezed column collapses from the bottom up",
			height:    3,
			specs:     []Spec{{Kind: KindExplorer}, {Kind: KindContents, Rows: 10}},
			wantRows:  []int{0, 0},
			collapsed: []bool{true, true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bottom := tt.height - 1
			secs := Layout(0, bottom, tt.specs)
			if len(secs) != len(tt.specs) {
				t.Fatalf("sections = %+v, want %d", secs, len(tt.specs))
			}
			open := false
			for i, s := range secs {
				if got := s.Rows(); got != tt.wantRows[i] {
					t.Errorf("section %d rows = %d, want %d", i, got, tt.wantRows[i])
				}
				wantCollapsed := len(tt.collapsed) > 0 && tt.collapsed[i]
				if s.Collapsed != wantCollapsed {
					t.Errorf("section %d collapsed = %v, want %v", i, s.Collapsed, wantCollapsed)
				}
				if !s.Collapsed {
					open = true
				}
			}
			// Open sections fill the column exactly; ground takes the rest when
			// every section is folded away.
			total := sumRows(secs)
			if open && total != tt.height {
				t.Errorf("rows add up to %d, want the column height %d", total, tt.height)
			}
			if !open && total > tt.height {
				t.Errorf("headers and border take %d rows of %d", total, tt.height)
			}
			if secs[0].HeaderY != 0 {
				t.Errorf("the first header is on row %d, want 0", secs[0].HeaderY)
			}
			last := secs[len(secs)-1]
			if last.BodyBottom >= bottom && !last.Collapsed {
				t.Errorf("the last section reaches row %d, the bottom border is %d", last.BodyBottom, bottom)
			}
			for i := 1; i < len(secs); i++ {
				want := secs[i-1].BodyBottom + 1
				if secs[i-1].Collapsed || secs[i-1].Rows() == 0 {
					want = secs[i-1].HeaderY + 1
				}
				if secs[i].HeaderY != want {
					t.Errorf("header of section %d is on row %d, want %d", i, secs[i].HeaderY, want)
				}
			}
		})
	}
}

// TestLayoutTooShortForEverySection checks the fallback: when even the header
// rows and the bottom border do not fit, the top section keeps its header and
// the rest is dropped.
func TestLayoutTooShortForEverySection(t *testing.T) {
	t.Parallel()
	specs := []Spec{{Kind: KindExplorer}, {Kind: KindContents, Rows: 10}}
	if got := Layout(0, 1, specs); len(got) != 1 || got[0].Kind != KindExplorer {
		t.Fatalf("height 2: sections = %+v, want only the explorer", got)
	}
	if got := Layout(0, 0, specs); got != nil {
		t.Fatalf("height 1: sections = %+v, want none", got)
	}
	if got := Layout(5, 4, specs); got != nil {
		t.Fatalf("empty column: sections = %+v, want none", got)
	}
	if got := Layout(0, 5, nil); got != nil {
		t.Fatalf("no specs: sections = %+v, want none", got)
	}
}

// TestLayoutSingleSectionIsTheWholeColumn checks that a one-section column is
// the box the explorer always was: one header, every body row under it, the
// bottom border last.
func TestLayoutSingleSectionIsTheWholeColumn(t *testing.T) {
	t.Parallel()
	secs := Layout(2, 29, []Spec{{Kind: KindExplorer}})
	if len(secs) != 1 {
		t.Fatalf("sections = %+v, want one", secs)
	}
	s := secs[0]
	if s.HeaderY != 2 || s.BodyTop != 3 || s.BodyBottom != 28 || s.Rows() != 26 {
		t.Fatalf("section = %+v, want header 2 body 3..28", s)
	}
	if s.Collapsed {
		t.Fatal("the section is collapsed")
	}
}
