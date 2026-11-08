package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/theme"
)

// TestSidebarStateCollectsAndRestores checks the session round trip through the
// model: the collected state has the live widths, folds and scrolls, and a model
// created with it restores them, the Contents section's state landing on the first
// Markdown scan (its open flag from the session wins over auto_open).
func TestSidebarStateCollectsAndRestores(t *testing.T) {
	t.Parallel()
	cfg := DefaultMarkdownTOC()
	ed := newTOCEditor("notes.md", "# Title", "body", "", "## Setup", "text")
	m := newTOCModel(t, ed, &cfg)
	m.explorerCollapsed = true
	m.explorerWidth = explorer.ColumnsWidth(42)
	m.explorerSide = explorer.SideRight
	if ft, ok := m.tree.(*fakeTree); ok {
		_ = ft.Select("/ws/f1")
	}
	m.tree.ScrollBy(2)
	m.diag.open = true
	m.diag.collapsed = true
	m.diag.rows = 6
	m.contents.SetRows(12)
	m.contents.ToggleCollapsed()
	m.contents.SetScroll(1)
	st := m.SidebarState()
	if !st.ContentsOpen || !st.ContentsCollapsed || st.ContentsRows != 12 || st.ContentsScroll != 1 {
		t.Fatalf("collected contents state = %+v", st)
	}
	if st.Width != explorer.ColumnsWidth(42) || st.Side != explorer.SideRight || !st.ExplorerCollapsed || st.ExplorerScroll != 2 || st.ExplorerSelected != "/ws/f1" {
		t.Fatalf("collected explorer state = %+v", st)
	}
	if !st.DiagnosticsOpen || !st.DiagnosticsCollapsed || st.DiagnosticsRows != 6 {
		t.Fatalf("collected diagnostics state = %+v", st)
	}

	restoredEd := newTOCEditor("notes.md", "# Title", "body", "", "## Setup", "text")
	restored := New(Deps{
		Editor: restoredEd, Explorer: newFakeTree(3), ShowExplorer: true,
		Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, MarkdownTOC: &cfg,
		Sidebar: &st,
	})
	restored.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if restored.explorerWidth != st.Width || restored.explorerSide != st.Side || !restored.explorerCollapsed {
		t.Errorf("restored explorer = width %v side %v collapsed %v", restored.explorerWidth, restored.explorerSide, restored.explorerCollapsed)
	}
	if got := restored.tree.ScrollOffset(); got != st.ExplorerScroll {
		t.Errorf("restored explorer scroll = %d, want %d", got, st.ExplorerScroll)
	}
	if !restored.diag.open || !restored.diag.collapsed || restored.diag.rows != 6 {
		t.Errorf("restored diagnostics = %+v", restored.diag)
	}
	if !restored.contents.Open() || !restored.contents.Collapsed() || restored.contents.Rows() != 12 || restored.contents.Scroll() != 1 {
		t.Errorf("restored contents = open %v collapsed %v rows %d scroll %d",
			restored.contents.Open(), restored.contents.Collapsed(), restored.contents.Rows(), restored.contents.Scroll())
	}

	// A session that closed the section keeps it closed on restore, even though
	// auto_open is on: the saved layout wins for the restored buffer.
	closed := st
	closed.ContentsOpen = false
	closedEd := newTOCEditor("notes.md", "# Title", "body")
	restoredClosed := New(Deps{
		Editor: closedEd, Explorer: newFakeTree(3), ShowExplorer: true,
		Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, MarkdownTOC: &cfg,
		Sidebar: &closed,
	})
	restoredClosed.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if restoredClosed.contents.Open() {
		t.Error("a section the session closed reopened on restore")
	}
}
