package reviewdiff

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/theme"
)

func TestReviewContextGapCanCollapseInPlace(t *testing.T) {
	old := make([]string, 85)
	for i := range old {
		old[i] = fmt.Sprintf("line %d", i)
	}
	next := append([]string{}, old...)
	next[4], next[46], next[76] = "change one", "change two", "change three"
	for _, mode := range []ViewMode{ViewUnified, ViewSplit} {
		t.Run(mode.Label(), func(t *testing.T) {
			m := NewModel()
			m.ViewMode = mode
			m.SetDiff(&FileDiff{Path: "a.txt", OldLines: old, NewLines: next}, "")
			const width, height = 100, 6
			for i := range m.LogicalRowCount(width) {
				if m.gapIDAt(i, width) == 1 {
					m.Cursor = i
					break
				}
			}
			if m.gapIDAt(m.Cursor, width) != 1 {
				t.Fatal("interior context control is missing")
			}
			m.Scroll = m.visualRowOfCursor(width) - 2
			before := m.LogicalRowCount(width)
			if !m.ChangeGapAtCursor(width, height, true) {
				t.Fatal("could not open gap")
			}
			if m.LogicalRowCount(width) != before+ExpandStep || m.visualRowOfCursor(width)-m.Scroll != 2 {
				t.Fatalf("opening the gap moved its screen row or revealed the wrong number of lines")
			}
			if !m.ChangeGapAtCursor(width, height, true) {
				t.Fatal("could not close gap")
			}
			if m.LogicalRowCount(width) != before || m.gapIDAt(m.Cursor, width) != 1 || m.visualRowOfCursor(width)-m.Scroll != 2 {
				t.Fatalf("closing the gap did not restore the original context and control position")
			}
		})
	}
}

func TestWordDiffRanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		old, nw     string
		wantOldPart string
		wantNewPart string
	}{
		{
			name:        "single_word_changed",
			old:         "objectVersion = 110;",
			nw:          "objectVersion = 77;",
			wantOldPart: "110",
			wantNewPart: "77",
		},
		{
			name:        "identical",
			old:         "same line",
			nw:          "same line",
			wantOldPart: "",
			wantNewPart: "",
		},
		{
			name:        "added_middle_token",
			old:         "func handle() {",
			nw:          "func handle(ctx Context) {",
			wantOldPart: "",
			wantNewPart: "ctx Context",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			or, nr := WordDiffRanges(tc.old, tc.nw)
			var gotOld, gotNew strings.Builder
			for _, r := range or {
				gotOld.WriteString(tc.old[r.Start:r.End])
			}
			for _, r := range nr {
				gotNew.WriteString(tc.nw[r.Start:r.End])
			}
			if gotOld.String() != tc.wantOldPart {
				t.Errorf("old changed = %q, want %q", gotOld.String(), tc.wantOldPart)
			}
			if gotNew.String() != tc.wantNewPart {
				t.Errorf("new changed = %q, want %q", gotNew.String(), tc.wantNewPart)
			}
		})
	}
}

func TestModelFilterPreservesSelection(t *testing.T) {
	t.Parallel()
	m := NewModel()
	files := []FileEntry{
		{Path: "a/one.go", Status: StatusModified, Added: 2, Deleted: 1},
		{Path: "b/two.go", Status: StatusAdded, Added: 10},
		{Path: "c/three.go", Status: StatusDeleted, Deleted: 5},
	}
	m.SetFiles(files, "main", "", false)
	if f, ok := m.SelectedFile(); !ok || f.Path != "a/one.go" {
		t.Fatalf("initial selected = %v (%v), want a/one.go", f, ok)
	}
	if _, changed := m.MoveFile(1, 10); !changed {
		t.Fatal("MoveFile(1) should change selection")
	}
	if f, _ := m.SelectedFile(); f.Path != "b/two.go" {
		t.Fatalf("selected = %q, want b/two.go", f.Path)
	}

	// Filter keeps b/two.go selected when it still matches.
	m.SetFilter("two")
	if len(m.Filtered) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(m.Filtered))
	}
	if f, ok := m.SelectedFile(); !ok || f.Path != "b/two.go" {
		t.Fatalf("filtered selected = %v (%v), want b/two.go", f, ok)
	}

	// Reloading files with a new file prepended preserves b/two.go by stable path identity.
	m.SetFilter("")
	updated := append([]FileEntry{{Path: "0/zero.go", Status: StatusUntracked}}, files...)
	m.SetFiles(updated, "main", "", false)
	if f, ok := m.SelectedFile(); !ok || f.Path != "b/two.go" {
		t.Fatalf("reloaded selected = %v (%v), want b/two.go", f, ok)
	}
}

func TestDiffExpandContextAndFullFile(t *testing.T) {
	t.Parallel()
	oldLines := make([]string, 40)
	newLines := make([]string, 40)
	for i := range 40 {
		line := fmt.Sprintf("line %02d", i+1)
		oldLines[i] = line
		newLines[i] = line
	}
	newLines[1] = "line 02 changed"
	newLines[37] = "line 38 changed"

	m := NewModel()
	m.SetDiff(&FileDiff{
		Path:     "sample.go",
		Status:   StatusModified,
		BaseRef:  "HEAD",
		OldLines: oldLines,
		NewLines: newLines,
	}, "")

	// Hunk-only view should have 2 hunks and an expandable gap between them.
	if cur, total := m.CurrentHunkPos(100); cur != 1 || total != 2 {
		t.Fatalf("CurrentHunkPos = (%d, %d), want (1, 2)", cur, total)
	}
	beforeCount := m.LogicalRowCount(100)
	gapRow := -1
	gapID := -1
	for i, r := range m.unified {
		if r.Kind == RowHunkHeader && r.GapID >= 0 && r.HiddenCount > 0 {
			gapRow = i
			gapID = r.GapID
			break
		}
	}
	if gapRow < 0 {
		t.Fatal("expected an expandable hunk header gap between distant hunks")
	}
	m.Cursor = gapRow
	if !m.ExpandAtCursor(100) {
		t.Fatalf("ExpandAtCursor on gap %d returned false", gapID)
	}
	if after := m.LogicalRowCount(100); after <= beforeCount {
		t.Fatalf("row count after expand = %d, want > %d", after, beforeCount)
	}

	// Full-file toggle shows every line of the file without hunk headers.
	m.ToggleFullFile()
	for _, r := range m.unified {
		if r.Kind == RowHunkHeader {
			t.Fatal("full-file mode should not emit hunk headers")
		}
	}
	if len(m.unified) != 42 { // 38 context + 2 del + 2 add
		t.Fatalf("full-file unified rows = %d, want 42", len(m.unified))
	}
}

func TestHunkNavigationAndEditTarget(t *testing.T) {
	t.Parallel()
	oldLines := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	newLines := []string{"a", "B", "c", "d", "e", "f", "g", "h", "i", "j", "K", "l"}

	m := NewModel()
	m.SetDiff(&FileDiff{
		Path:     "foo.go",
		Status:   StatusModified,
		BaseRef:  "HEAD",
		OldLines: oldLines,
		NewLines: newLines,
	}, "")

	// Starts on hunk 1.
	if p, line, ok := m.EditTargetAtCursor(100); !ok || p != "foo.go" || line != 2 {
		t.Fatalf("initial EditTarget = (%q, %d, %v), want (foo.go, 2, true)", p, line, ok)
	}
	// NextHunk lands on hunk 2 (line 11).
	if jumped, needNext := m.NextHunk(100, 20); !jumped || needNext {
		t.Fatalf("NextHunk = (%v, %v), want (true, false)", jumped, needNext)
	}
	if p, line, ok := m.EditTargetAtCursor(100); !ok || p != "foo.go" || line != 11 {
		t.Fatalf("hunk 2 EditTarget = (%q, %d, %v), want (foo.go, 11, true)", p, line, ok)
	}
	// NextHunk at last hunk requests crossing to the next file.
	if jumped, needNext := m.NextHunk(100, 20); jumped || !needNext {
		t.Fatalf("NextHunk at end = (%v, %v), want (false, true)", jumped, needNext)
	}
	// PrevHunk goes back to hunk 1.
	if jumped, needPrev := m.PrevHunk(100, 20); !jumped || needPrev {
		t.Fatalf("PrevHunk = (%v, %v), want (true, false)", jumped, needPrev)
	}
	if cur, total := m.CurrentHunkPos(100); cur != 1 || total != 2 {
		t.Fatalf("CurrentHunkPos = (%d, %d), want (1, 2)", cur, total)
	}
}

func TestSearchAndSplitFallback(t *testing.T) {
	t.Parallel()
	m := NewModel()
	m.SetDiff(&FileDiff{
		Path:     "app.go",
		Status:   StatusModified,
		BaseRef:  "HEAD",
		OldLines: []string{"alpha", "beta", "gamma"},
		NewLines: []string{"alpha", "beta_new", "gamma_new"},
	}, "")

	// Split falls back to Unified when narrow, activates when wide.
	m.ToggleViewMode()
	if got := m.EffectiveViewMode(40); got != ViewUnified {
		t.Fatalf("narrow EffectiveViewMode = %v, want ViewUnified", got)
	}
	if got := m.EffectiveViewMode(100); got != ViewSplit {
		t.Fatalf("wide EffectiveViewMode = %v, want ViewSplit", got)
	}

	m.SetSearch("new", 100, 10)
	if len(m.SearchMatches) != 2 {
		t.Fatalf("search matches = %d, want 2", len(m.SearchMatches))
	}
	firstRow := m.Cursor
	if !m.NextMatch(100, 10) || m.Cursor == firstRow {
		t.Fatalf("NextMatch should move cursor from %d, got %d", firstRow, m.Cursor)
	}
	if !m.PrevMatch(100, 10) || m.Cursor != firstRow {
		t.Fatalf("PrevMatch should return cursor to %d, got %d", firstRow, m.Cursor)
	}
}

func TestPaintAndHitTesting(t *testing.T) {
	t.Parallel()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatalf("theme.Load(dark): %v", err)
	}
	st := NewStyles(th)
	m := NewModel()
	m.SetFiles([]FileEntry{
		{Path: "src/main.go", Status: StatusModified, Added: 3, Deleted: 1},
		{Path: "README.md", Status: StatusAdded, Added: 10},
	}, "main", "", false)
	m.SetDiff(&FileDiff{
		Path:     "src/main.go",
		Status:   StatusModified,
		BaseRef:  "HEAD",
		OldLines: []string{"package main", "func old() {}"},
		NewLines: []string{"package main", "func newFn() {}", "func extra() {}"},
	}, "")

	var g grid.Grid
	g.Resize(100, 20)

	sideRect := SidebarRect{X: 0, W: 30, Top: 1, Bottom: 18}
	m.PaintSidebar(&g, sideRect, st, icons.Plain)

	// Hit-test first and second file rows in sidebar.
	hit0 := m.HitSidebar(sideRect, icons.Plain, 5, sideRect.ListTop())
	if hit0.Kind != SidebarHitFile || hit0.FilteredIdx != 0 {
		t.Fatalf("hit0 = %+v, want SidebarHitFile idx 0", hit0)
	}
	hit1 := m.HitSidebar(sideRect, icons.Plain, 5, sideRect.ListTop()+1)
	if hit1.Kind != SidebarHitFile || hit1.FilteredIdx != 1 {
		t.Fatalf("hit1 = %+v, want SidebarHitFile idx 1", hit1)
	}
	if hf := m.HitSidebar(sideRect, icons.Plain, 5, sideRect.FilterY()); hf.Kind != SidebarHitFilter {
		t.Fatalf("filter hit = %+v, want SidebarHitFilter", hf)
	}

	// Header bar buttons hit-test cleanly.
	hl := m.PaintHeaderBar(&g, 30, 1, 70, st, icons.Plain)
	if len(hl.Buttons) == 0 {
		t.Fatal("expected clickable header buttons")
	}
	for _, btn := range hl.Buttons {
		if got := hl.Hit(btn.X, 1); got != btn.Action {
			t.Errorf("hl.Hit(%d, 1) = %v, want %v", btn.X, got, btn.Action)
		}
	}

	// Paint Unified and Split diff into the right rect.
	diffRect := DiffRect{X: 30, Y: 2, W: 70, H: 17}
	m.PaintDiff(&g, diffRect, st)
	if dh := m.HitDiff(diffRect, 35, 3); dh.Kind != DiffHitRow {
		t.Fatalf("diff row hit = %+v, want DiffHitRow", dh)
	}

	m.ToggleViewMode()
	m.PaintDiff(&g, diffRect, st)
	if dh := m.HitDiff(diffRect, 35, 3); dh.Kind != DiffHitRow {
		t.Fatalf("split diff row hit = %+v, want DiffHitRow", dh)
	}
}
