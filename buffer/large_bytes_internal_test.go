package buffer

import (
	"strings"
	"testing"
)

func TestLinesBytesOver(t *testing.T) {
	tests := []struct {
		name string
		data string
		edit func(b *Buffer)
		i, j int
	}{
		{"lf", "aa\nbbb\ncccc\nd\neeeee\n", nil, 1, 3},
		{"crlf", "aa\r\nbbb\r\ncccc\r\nd\r\neeeee\r\n", nil, 1, 3},
		{"mixed", "aa\r\nbbb\ncccc\r\nd\neeeee\n", nil, 0, 4},
		{"a single line", "aa\nbbb\ncccc\n", nil, 1, 1},
		{"after an edit", "aa\nbbb\ncccc\nd\neeeee\n", func(b *Buffer) {
			b.Insert(Pos{Line: 1, Col: 1}, "XX\nYYYY")
			b.Delete(Pos{Line: 4}, Pos{Line: 5})
		}, 0, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := openIndexed(t, tc.data)
			if tc.edit != nil {
				tc.edit(b)
			}
			end := Pos{Line: tc.j, Col: len([]rune(b.Line(tc.j)))}
			actual := int64(len(b.Range(Pos{Line: tc.i}, end)))
			calls := b.Stats().LineCalls
			for limit := int64(0); limit <= actual+2; limit++ {
				n, over := b.LinesBytesOver(tc.i, tc.j, limit)
				if over && actual <= limit {
					t.Fatalf("limit %d: over, but the text is %d bytes", limit, actual)
				}
				if over && n > actual {
					t.Fatalf("limit %d: at least %d bytes, but the text is %d", limit, n, actual)
				}
				// A CRLF is two bytes in the index and one in the text, so the bound is loose by
				// a byte for each line break: it must still catch what is longer than that.
				if slack := int64(tc.j - tc.i); !over && actual > limit+slack {
					t.Fatalf("limit %d: not over, but the text is %d bytes", limit, actual)
				}
			}
			if got := b.Stats().LineCalls - calls; got != 0 {
				t.Errorf("the estimate read %d lines", got)
			}
		})
	}
}

func TestLinesBytesOverDoesNotGuess(t *testing.T) {
	b := openIndexed(t, "aa\nbbb\ncccc\n")
	for _, c := range []struct{ i, j int }{{-1, 1}, {2, 1}, {0, 99}} {
		if _, over := b.LinesBytesOver(c.i, c.j, 0); over {
			t.Errorf("lines %d..%d: over for a range that is not in the document", c.i, c.j)
		}
	}
	m := New("aa\nbbb\ncccc\n")
	if _, over := m.LinesBytesOver(0, 2, 0); over {
		t.Error("a memory buffer says over: it has no index to answer from")
	}
	// A line cut at the line cap has a length the index does not know.
	huge := openIndexed(t, "a\n"+strings.Repeat("x", 200)+"\nb\n")
	if _, over := huge.LinesBytesOver(0, 2, 1); over {
		t.Error("a document with a cut line says over")
	}
}
