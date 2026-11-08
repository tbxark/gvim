package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// leaf is a valid leaf for file i.
func leaf(i int) *SplitNode { return &SplitNode{File: i, Path: "/w/" + string(rune('a'+i)) + ".txt"} }

func TestSplitLayoutValid(t *testing.T) {
	t.Parallel()
	nested := &SplitNode{
		Dir: SplitStacked, Ratio: 60,
		A: &SplitNode{Dir: SplitSideBySide, A: leaf(0), B: leaf(1)},
		B: leaf(2),
	}
	tests := []struct {
		name   string
		layout *SplitLayout
		files  int
		want   bool
	}{
		{name: "one leaf pane", layout: &SplitLayout{Root: *leaf(0)}, files: 1, want: true},
		{name: "nested split", layout: &SplitLayout{Root: *nested, Focus: 2}, files: 3, want: true},
		{name: "even ratio", layout: &SplitLayout{Root: SplitNode{Dir: SplitSideBySide, A: leaf(0), B: leaf(1)}}, files: 2, want: true},
		{name: "nil", layout: nil, files: 2, want: false},
		{name: "bad direction", layout: &SplitLayout{Root: SplitNode{Dir: "diagonal", A: leaf(0), B: leaf(1)}}, files: 2, want: false},
		{name: "ratio over", layout: &SplitLayout{Root: SplitNode{Dir: SplitSideBySide, Ratio: 100, A: leaf(0), B: leaf(1)}}, files: 2, want: false},
		{name: "negative ratio", layout: &SplitLayout{Root: SplitNode{Dir: SplitSideBySide, Ratio: -1, A: leaf(0), B: leaf(1)}}, files: 2, want: false},
		{name: "file out of range", layout: &SplitLayout{Root: SplitNode{Dir: SplitSideBySide, A: leaf(0), B: leaf(2)}}, files: 2, want: false},
		{name: "negative file", layout: &SplitLayout{Root: *leaf(-1)}, files: 2, want: false},
		{name: "empty path", layout: &SplitLayout{Root: SplitNode{Dir: SplitSideBySide, A: &SplitNode{File: 0, Path: "/a"}, B: &SplitNode{File: 1}}}, files: 2, want: false},
		{name: "missing child", layout: &SplitLayout{Root: SplitNode{Dir: SplitSideBySide, A: leaf(0)}}, files: 2, want: false},
		{name: "focus out of range", layout: &SplitLayout{Root: SplitNode{Dir: SplitSideBySide, A: leaf(0), B: leaf(1)}, Focus: 2}, files: 2, want: false},
		{name: "negative focus", layout: &SplitLayout{Root: *leaf(0), Focus: -1}, files: 2, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.layout.Valid(tt.files); got != tt.want {
				t.Fatalf("Valid = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitLayoutBounds(t *testing.T) {
	t.Parallel()
	// A right-deep tree with 17 leaves is over the pane bound.
	root := leaf(0)
	for i := 1; i < MaxSplitLeaves+1; i++ {
		root = &SplitNode{Dir: SplitStacked, A: root, B: leaf(i)}
	}
	if got := (&SplitLayout{Root: *root}).Valid(MaxSplitLeaves + 1); got {
		t.Fatal("a layout over the leaf bound validated")
	}
	// A tree deeper than the depth bound is rejected even when it has few leaves.
	deep := &SplitNode{File: 0, Path: "/a"}
	for i := 0; i < MaxSplitDepth+1; i++ {
		deep = &SplitNode{Dir: SplitStacked, A: deep, B: &SplitNode{File: 0, Path: "/a"}}
	}
	if got := (&SplitLayout{Root: *deep}).Valid(1); got {
		t.Fatal("a layout over the depth bound validated")
	}
}

func TestSplitLayoutRoundTripAndLegacy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout := &SplitLayout{
		Root: SplitNode{
			Dir: SplitSideBySide, Ratio: 40,
			A: &SplitNode{File: 0, Path: "/w/a.txt", Top: 3},
			B: &SplitNode{File: 1, Path: "/w/b.txt", Top: 7, TopRow: 2},
		},
		Focus: 1,
	}
	if err := Save(dir, Session{
		Root: "/w", Explorer: true, Active: 2,
		Files:  []FileState{{Path: "/w/a.txt"}, {Path: "/w/b.txt"}},
		Layout: layout,
	}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(dir)
	if err != nil || !ok {
		t.Fatalf("Load = %v, %v", ok, err)
	}
	if got.Layout == nil || !got.Layout.Valid(2) {
		t.Fatalf("round-trip layout = %+v", got.Layout)
	}
	if got.Layout.Root.Ratio != 40 || got.Layout.Focus != 1 || got.Layout.Root.B.TopRow != 2 {
		t.Fatalf("round-trip values = %+v", got.Layout)
	}

	// A session written before the layout existed decodes with a nil layout and
	// still loads: the additive field changed nothing for old files.
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{"version":1,"root":"/w","active":0,"files":[{"path":"/w/a.txt"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old, ok, err := Load(dir)
	if err != nil || !ok || old.Layout != nil {
		t.Fatalf("legacy session = %+v, %v, %v", old, ok, err)
	}
}

func TestValidateSplitLayoutHelper(t *testing.T) {
	t.Parallel()
	l := &SplitLayout{Root: SplitNode{Dir: SplitSideBySide, A: leaf(0), B: leaf(1)}, Focus: 0}
	if !ValidateSplitLayout(l, 2) {
		t.Fatal("a valid layout was rejected")
	}
	if ValidateSplitLayout(nil, 2) {
		t.Fatal("a nil layout validated")
	}
	if !strings.Contains(leavesString(l), "0") {
		// Leaves() is exercised for the reading-order contract.
		t.Fatalf("leaves = %v", l.Root.Leaves())
	}
}

// leavesString renders the reading-order leaves, for the helper test.
func leavesString(l *SplitLayout) string {
	var b strings.Builder
	for _, f := range l.Root.Leaves() {
		b.WriteString(string(rune('0' + f)))
	}
	return b.String()
}
