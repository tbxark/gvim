package buffer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"
)

// TestSnapshotCodeAvoidsUIGoroutineState keeps the snapshot side of large file
// mode honest (AMENDMENTS C16, I13): the code a Snapshot runs on another
// goroutine must not touch the fields that only the UI goroutine may use.
func TestSnapshotCodeAvoidsUIGoroutineState(t *testing.T) {
	forbiddenSel := []string{"dec", "err", "refusal", "grew", "txnRefused", "closed"}
	fset := token.NewFileSet()
	for _, name := range []string{"snapshot.go", "large_snapshot.go", "large_write.go"} {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				if n.Name == "bigStore" {
					t.Errorf("%s: snapshot code refers to bigStore", fset.Position(n.Pos()))
				}
			case *ast.SelectorExpr:
				if slices.Contains(forbiddenSel, n.Sel.Name) {
					t.Errorf("%s: snapshot code uses the UI-goroutine field %s", fset.Position(n.Pos()), n.Sel.Name)
				}
			}
			return true
		})
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestTeeCounterCountsPerChunk(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		chunk  int64
		want   []int32
	}{
		{"one write over several chunks", []string{"a\nb\nc\nd\ne\nf\n"}, 4, []int32{2, 2, 2}},
		{"writes cut mid chunk", []string{"a\n", "b", "\nc\nd", "\n"}, 4, []int32{2, 2}},
		{"no newlines", []string{"abcdefgh"}, 3, []int32{0, 0, 0}},
		{"nothing", nil, 3, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tee := &teeCounter{w: discard{}, chunk: tc.chunk}
			for _, w := range tc.writes {
				if n, err := tee.Write([]byte(w)); n != len(w) || err != nil {
					t.Fatalf("Write(%q) = %d, %v", w, n, err)
				}
			}
			if !slices.Equal(tee.counts, tc.want) {
				t.Fatalf("counts = %v, want %v", tee.counts, tc.want)
			}
		})
	}
}
