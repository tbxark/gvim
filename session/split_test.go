package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
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

func TestSplitNodeLeavesReadingOrder(t *testing.T) {
	t.Parallel()
	n := &SplitNode{Dir: SplitSideBySide, A: leaf(2), B: &SplitNode{Dir: SplitStacked, A: leaf(0), B: leaf(1)}}
	if got, want := n.Leaves(), []int{2, 0, 1}; !slices.Equal(got, want) {
		t.Fatalf("Leaves = %v, want %v", got, want)
	}
	if (*SplitNode)(nil).Leaves() != nil {
		t.Fatal("a nil node has leaves")
	}
}

// The zero fields of a layout and of the search state are left out of the file, as
// their omitzero tags say; omitempty would keep false and 0 under encoding/json/v2.
func TestZeroFieldsAreOmitted(t *testing.T) {
	t.Parallel()
	for _, v := range []any{SplitLayout{}, SplitNode{}, Search{}} {
		data, err := jsonv2.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "{}" && string(data) != `{"root":{}}` {
			t.Errorf("%T zero value encodes as %s, want no fields", v, data)
		}
	}
}

// TestSplitLayoutDualFieldsRoundTrip: the Markdown dual marker and each leaf's view mode
// are JSON fields; a valid dual layout passes Valid, and a dual marker on any
// other shape is rejected instead of restoring the wrong panes.
func TestSplitLayoutDualFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	layout := &SplitLayout{
		Dual:  true,
		Focus: 0,
		Root: SplitNode{
			Dir: SplitSideBySide,
			A:   &SplitNode{File: 0, Path: "/w/notes.md", ViewMode: "source"},
			B:   &SplitNode{File: 0, Path: "/w/notes.md", ViewMode: "rendered"},
		},
	}
	if !layout.Valid(1) {
		t.Fatal("a valid dual layout was rejected")
	}
	b, err := json.Marshal(layout)
	if err != nil {
		t.Fatal(err)
	}
	var got SplitLayout
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Dual || got.Root.A == nil || got.Root.B == nil ||
		got.Root.A.ViewMode != "source" || got.Root.B.ViewMode != "rendered" {
		t.Fatalf("round-trip dual layout = %+v", got)
	}
	bad := *layout
	bad.Focus = 1
	if bad.Valid(1) {
		t.Fatal("a dual layout focused on the preview was accepted")
	}
	for _, mode := range [][2]string{{"", "rendered"}, {"source", ""}, {"rendered", "source"}} {
		bad = *layout
		bad.Root.A = &SplitNode{File: 0, Path: "/w/notes.md", ViewMode: mode[0]}
		bad.Root.B = &SplitNode{File: 0, Path: "/w/notes.md", ViewMode: mode[1]}
		if bad.Valid(1) {
			t.Fatalf("a dual layout with view modes %q was accepted", mode)
		}
	}
	bad = *layout
	bad.Root.B = &SplitNode{File: 1, Path: "/w/other.md", ViewMode: "rendered"}
	if bad.Valid(2) {
		t.Fatal("a dual layout over two files was accepted")
	}
}
