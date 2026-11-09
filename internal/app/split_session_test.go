package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/session"
)

// twoSplitLayout is a side-by-side layout over two saved files, focused right.
func twoSplitLayout(a, b string) *session.SplitLayout {
	return &session.SplitLayout{
		Root: session.SplitNode{
			Dir: session.SplitSideBySide, Ratio: 35,
			A: &session.SplitNode{File: 0, Path: a, Top: 1},
			B: &session.SplitNode{File: 1, Path: b, Top: 2},
		},
		Focus: 1,
	}
}

// splitSessionFiles writes a.txt and b.txt and returns their absolute paths.
func splitSessionFiles(t *testing.T) (dir, a, b string) {
	t.Helper()
	dir = t.TempDir()
	a = filepath.Join(dir, "a.txt")
	b = filepath.Join(dir, "b.txt")
	apptest.WriteTestFile(t, a, "a1\na2\na3\na4\n")
	apptest.WriteTestFile(t, b, "b1\nb2\nb3\nb4\n")
	return dir, a, b
}

// TestBuildRuntimeRestoresSplitLayout restores a saved two-pane layout over the
// restored tabs: both panes come back, with the saved direction, ratio, per-pane
// top and focused pane, without any pane switching during the capture.
func TestBuildRuntimeRestoresSplitLayout(t *testing.T) {
	isolateConfig(t)
	dir, a, b := splitSessionFiles(t)
	sessDir := filepath.Join(dir, "session")
	layout := twoSplitLayout(a, b)
	must(t, session.Save(sessDir, session.Session{
		Root: dir, Active: 1,
		Files:  []session.FileState{{Path: a}, {Path: b}},
		Layout: layout,
	}))
	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir}, false)
	must(t, err)
	defer rt.close()

	// The editor restored both tabs and made the saved pane's tab active.
	tabs := rt.ed.Tabs()
	if len(tabs) != 2 || !tabs[1].Active {
		t.Fatalf("restored tabs = %+v", tabs)
	}
	got := rt.model.SplitState()
	if got == nil {
		t.Fatal("the restored model lost the split layout")
	}
	if got.Root.Dir != session.SplitSideBySide || got.Root.Ratio != 35 || len(got.Root.Leaves()) != 2 {
		t.Fatalf("restored layout = %+v", got.Root)
	}
	leaves := make([]*session.SplitNode, 0, 2)
	if got.Root.A != nil {
		leaves = append(leaves, got.Root.A, got.Root.B)
	}
	if leaves[0].Path != a || leaves[0].Top != 1 || leaves[1].Path != b || leaves[1].Top != 2 {
		t.Fatalf("restored leaves = %+v / %+v", leaves[0], leaves[1])
	}
	if got.Focus != 1 {
		t.Fatalf("focused pane = %d, want the saved right pane", got.Focus)
	}
	// The focused document is scrolled to its saved top (b.txt top 2).
	if top := rt.ed.Active().Top; top != 2 {
		t.Fatalf("focused top = %d, want 2", top)
	}
	// Capture is pure: it did not switch the active tab or scroll the editor.
	_, activeIdx := apptest.TabNames(rt.ed)
	if activeIdx != 1 || rt.ed.Active().Top != 2 {
		t.Fatalf("capture mutated the editor: active %d top %d", activeIdx, rt.ed.Active().Top)
	}
}

// TestBuildRuntimeSkipsInvalidStaleAndMissingSplitLayouts checks the fallbacks: a
// layout naming a file that no longer opens, or a structurally invalid layout, or
// a hot-exit restore, leaves one pane; nothing panics and no wrong file is placed.
func TestBuildRuntimeSkipsInvalidStaleAndMissingSplitLayouts(t *testing.T) {
	isolateConfig(t)
	dir, a, b := splitSessionFiles(t)
	tests := []struct {
		name   string
		layout *session.SplitLayout
		remove string
		hot    bool
	}{
		{name: "invalid ratio", layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, Ratio: 150, A: &session.SplitNode{File: 0, Path: a}, B: &session.SplitNode{File: 1, Path: b}}}},
		{name: "path mismatch", layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: b}, B: &session.SplitNode{File: 1, Path: a}}}},
		{name: "out of range", layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: a}, B: &session.SplitNode{File: 7, Path: b}}}},
		{name: "invalid dual focus", layout: &session.SplitLayout{Dual: true, Focus: 1, Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: a, ViewMode: "source"}, B: &session.SplitNode{File: 0, Path: a, ViewMode: "rendered"}}}},
		{name: "missing file", layout: twoSplitLayout(a, b), remove: b},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessDir := t.TempDir()
			if tt.remove != "" {
				if err := os.Remove(tt.remove); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { apptest.WriteTestFile(t, tt.remove, "b1\nb2\n") })
			}
			must(t, session.Save(sessDir, session.Session{
				Root: dir, Active: 0,
				Files:  []session.FileState{{Path: a}, {Path: b}},
				Layout: tt.layout,
			}))
			rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir}, false)
			must(t, err)
			defer rt.close()
			if got := rt.model.SplitState(); got != nil {
				t.Fatalf("a bad layout was applied: %+v", got)
			}
			if n := len(rt.ed.Tabs()); n < 1 {
				t.Fatalf("tabs = %d after the fallback", n)
			}

		})
	}
}

// TestHotExitRestoreSkipsTheSplitLayout checks that restore_previous_session=false
// (hot-exit) brings the unsaved text back without the saved pane layout.
func TestHotExitRestoreSkipsTheSplitLayout(t *testing.T) {
	isolateConfig(t)
	dir, a, b := splitSessionFiles(t)
	sessDir := filepath.Join(dir, "session")
	layout := twoSplitLayout(a, b)
	must(t, session.Save(sessDir, session.Session{
		Root: dir, Active: 0,
		Files: []session.FileState{
			{Path: a, StashFile: writeStashForTest(t, sessDir, a)},
			{Path: b},
		},
		Layout: layout,
	}))
	// buildRuntime's hot path is selected by the config/Options: use NoRestore
	// with an unsaved stash so the tabs come back for their text only.
	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir, NoRestore: true}, false)
	must(t, err)
	defer rt.close()
	if got := rt.model.SplitState(); got != nil {
		t.Fatalf("a hot-exit restore applied the split layout: %+v", got)
	}
}

// writeStashForTest writes a session stash with the text of path's file and returns
// its name.
func writeStashForTest(t *testing.T, sessDir, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	must(t, err)
	name, err := session.StashContent(sessDir, string(data))
	must(t, err)
	return name
}

// TestMalformedLayoutJSONStartsWithoutSplits checks invalid-layout safety at the
// file level: a session whose layout field is not a split tree fails to load like
// any corrupt session, and the editor starts with one pane instead of crashing.
func TestMalformedLayoutJSONStartsWithoutSplits(t *testing.T) {
	isolateConfig(t)
	dir, a, b := splitSessionFiles(t)
	sessDir := filepath.Join(dir, "session")
	if err := os.MkdirAll(sessDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "{\"version\":1,\"root\":\"" + dir + "\",\"active\":0,\"files\":[{\"path\":\"" + a + "\"},{\"path\":\"" + b + "\"}],\"layout\":\"not-a-tree\"}"
	if err := os.WriteFile(filepath.Join(sessDir, "session.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir}, false)
	must(t, err)
	defer rt.close()
	if got := rt.model.SplitState(); got != nil {
		t.Fatalf("a malformed layout was applied: %+v", got)
	}
}

// TestBuildRuntimeRestoresMarkdownDualLayout restores a saved dual split: one Markdown
// file in both panes, source left/rendered right, focus on the source, with the per-leaf
// view modes intact.
func TestBuildRuntimeRestoresMarkdownDualLayout(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	md := filepath.Join(dir, "notes.md")
	apptest.WriteTestFile(t, md, "# Title\n\ntext\n")
	sessDir := filepath.Join(dir, "session")
	layout := &session.SplitLayout{
		Dual:  true,
		Focus: 0,
		Root: session.SplitNode{
			Dir: session.SplitSideBySide,
			A:   &session.SplitNode{File: 0, Path: md, ViewMode: "source"},
			B:   &session.SplitNode{File: 0, Path: md, ViewMode: "rendered"},
		},
	}
	must(t, session.Save(sessDir, session.Session{
		Root: dir, Active: 0,
		Files:  []session.FileState{{Path: md}},
		Layout: layout,
	}))
	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir}, false)
	must(t, err)
	defer rt.close()
	got := rt.model.SplitState()
	if got == nil || !got.Dual || got.Focus != 0 || len(got.Root.Leaves()) != 2 {
		t.Fatalf("restored dual layout = %+v", got)
	}
	if got.Root.A == nil || got.Root.B == nil || got.Root.A.ViewMode != "source" || got.Root.B.ViewMode != "rendered" {
		t.Fatalf("restored dual modes = %+v", got.Root)
	}
}

// TestBuildRuntimeCapturesMarkdownDualLayout opens the dual view through the public action
// and checks the session layout now records it (previous behavior returned nil for dual).
func TestBuildRuntimeCapturesMarkdownDualLayout(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	md := filepath.Join(dir, "notes.md")
	apptest.WriteTestFile(t, md, "# Title\n\ntext\n")
	sessDir := filepath.Join(dir, "session")
	must(t, session.Save(sessDir, session.Session{
		Root: dir, Active: 0,
		Files: []session.FileState{{Path: md}},
	}))
	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir}, false)
	must(t, err)
	defer rt.close()
	rt.model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	must(t, rt.model.Run("markdown_mode_dual"))
	// Focus the preview; the captured layout must still name the source as focus 0.
	must(t, rt.model.Run("next_pane"))
	got := rt.model.SplitState()
	if got == nil || !got.Dual || got.Focus != 0 {
		t.Fatalf("captured dual layout = %+v", got)
	}
	if got.Root.A == nil || got.Root.B == nil || got.Root.A.ViewMode != "source" || got.Root.B.ViewMode != "rendered" {
		t.Fatalf("captured dual modes = %+v", got.Root)
	}
}
