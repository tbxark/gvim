package restore

import (
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// twoFileEditor opens a.txt and b.txt in a session editor and returns their paths.
func twoFileEditor(t *testing.T) (*editor.Editor, []string) {
	t.Helper()
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")}
	for _, p := range paths {
		apptest.WriteTestFile(t, p, "one\ntwo\nthree\n")
	}
	ed := apptest.NewSessionEditor()
	t.Cleanup(ed.Close)
	for _, p := range paths {
		apptest.Must(t, ed.OpenFile(p))
	}
	return ed, paths
}

func TestWriteSessionLayoutRoundTrip(t *testing.T) {
	t.Parallel()
	ed, paths := twoFileEditor(t)
	dir := t.TempDir()
	layout := &session.SplitLayout{
		Root: session.SplitNode{
			Dir: session.SplitSideBySide, Ratio: 55,
			A: &session.SplitNode{File: 0, Path: paths[0], Top: 1},
			B: &session.SplitNode{File: 1, Path: paths[1]},
		},
		Focus: 1,
	}
	apptest.Must(t, WriteSession(ed, dir, tui.ExplorerState{}, nil, layout))
	got, ok, err := session.Load(dir)
	apptest.Must(t, err)
	if !ok || got.Layout == nil {
		t.Fatalf("saved session = %+v, %v", got, ok)
	}
	if got.Layout.Root.Ratio != 55 || got.Layout.Focus != 1 || len(got.Layout.Root.Leaves()) != 2 {
		t.Fatalf("saved layout = %+v", got.Layout)
	}
	// A plain WriteSession (the old call shape) still saves no layout.
	dir2 := t.TempDir()
	apptest.Must(t, WriteSession(ed, dir2, tui.ExplorerState{}, nil))
	plain, _, err := session.Load(dir2)
	apptest.Must(t, err)
	if plain.Layout != nil {
		t.Fatalf("a layout was saved without one being passed: %+v", plain.Layout)
	}
}

func TestWriteSessionDropsStaleLayout(t *testing.T) {
	t.Parallel()
	ed, paths := twoFileEditor(t)
	tests := []struct {
		name   string
		layout *session.SplitLayout
	}{
		{name: "path mismatch", layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: paths[1]}, B: &session.SplitNode{File: 1, Path: paths[0]}}}},
		{name: "file out of range", layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: paths[0]}, B: &session.SplitNode{File: 5, Path: "/missing"}}}},
		{name: "bad direction", layout: &session.SplitLayout{Root: session.SplitNode{Dir: "diagonal", A: &session.SplitNode{File: 0, Path: paths[0]}, B: &session.SplitNode{File: 1, Path: paths[1]}}}},
		{name: "empty path leaf", layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0}, B: &session.SplitNode{File: 1, Path: paths[1]}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			apptest.Must(t, WriteSession(ed, dir, tui.ExplorerState{}, nil, tt.layout))
			got, _, err := session.Load(dir)
			apptest.Must(t, err)
			if got.Layout != nil {
				t.Fatalf("a stale layout was saved: %+v", got.Layout)
			}
		})
	}
}

func TestSplitLayoutForRejectsMismatch(t *testing.T) {
	t.Parallel()
	files := []session.FileState{{Path: "/w/a"}, {Path: "/w/b"}}
	ok := &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: "/w/a"}, B: &session.SplitNode{File: 1, Path: "/w/b"}}}
	if SplitLayoutFor(files, ok) != ok {
		t.Fatal("a valid layout was dropped")
	}
	bad := *ok
	bad.Root = session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: "/w/b"}, B: &session.SplitNode{File: 1, Path: "/w/a"}}
	if SplitLayoutFor(files, &bad) != nil {
		t.Fatal("a mismatched layout was accepted")
	}
	if SplitLayoutFor(nil, nil) != nil || SplitLayoutFor(files, nil) != nil {
		t.Fatal("nil inputs were accepted")
	}
}
