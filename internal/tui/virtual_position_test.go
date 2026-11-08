package tui

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// virtualColsDoc is a document with a horizontal virtual caret.
type virtualColsDoc struct {
	*fakeDoc
	cols int
}

func (d virtualColsDoc) VirtualColumns() int { return d.cols }

// virtualLinesDoc is a document with a caret on a virtual line below the end.
type virtualLinesDoc struct {
	*fakeDoc
	lines, col int
}

func (d virtualLinesDoc) VirtualLines() int      { return d.lines }
func (d virtualLinesDoc) VirtualLineColumn() int { return d.col }

func TestVirtualPosition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		doc  func() Document
		want [2]int
	}{
		{
			name: "on text",
			doc: func() Document {
				d := &fakeDoc{lines: []string{"abc"}}
				d.cursor = buffer.Pos{Line: 0, Col: 1}
				return d
			},
			want: [2]int{0, 1},
		},
		{
			name: "horizontal virtual columns read as spaces",
			doc: func() Document {
				d := &fakeDoc{lines: []string{"abc"}}
				d.cursor = buffer.Pos{Line: 0, Col: 3}
				return virtualColsDoc{fakeDoc: d, cols: 2}
			},
			want: [2]int{0, 5},
		},
		{
			name: "a virtual line advances the line and takes its column",
			doc: func() Document {
				d := &fakeDoc{lines: []string{"abc"}}
				d.cursor = buffer.Pos{Line: 0, Col: 3}
				return virtualLinesDoc{fakeDoc: d, lines: 2, col: 7}
			},
			want: [2]int{2, 7},
		},
		{
			name: "a virtual line wins over horizontal columns",
			doc: func() Document {
				d := &fakeDoc{lines: []string{"abc"}}
				d.cursor = buffer.Pos{Line: 0, Col: 3}
				return virtualLinesDoc{fakeDoc: d, lines: 1, col: 4}
			},
			want: [2]int{1, 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			line, col := virtualPosition(tt.doc())
			if line != tt.want[0] || col != tt.want[1] {
				t.Fatalf("virtualPosition = (%d,%d), want (%d,%d)", line, col, tt.want[0], tt.want[1])
			}
		})
	}
}
