package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func mouseAtOrigin() tea.Mouse { return tea.Mouse{X: 5, Y: 1} }

// openExplorerMenu right-clicks the row with the index row of the explorer.
func openExplorerMenu(t *testing.T, m *Model, row int) {
	t.Helper()
	rightClickAt(m, 3, explorerBodyTop+row)
	if ctxTop(m) == nil {
		t.Fatalf("a right click on explorer row %d opened no menu", row)
	}
}

func TestExplorerMenuSelectsTheRowItActsOn(t *testing.T) {
	m, _, tr := newExplorerModel(t, &fakeOps{}, "alpha.txt", "beta.txt", "gamma.txt")
	openExplorerMenu(t, m, 1)
	if sel, _ := tr.Selected(); sel.Name != "beta.txt" {
		t.Fatalf("the right press selected %q, want beta.txt", sel.Name)
	}
}

func TestExplorerMenuDuplicateCopiesIntoTheSameFolder(t *testing.T) {
	ops := &fakeOps{}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt")
	openExplorerMenu(t, m, 1)
	chooseRow(t, m, "Duplicate")
	settle(t, m, nil)
	if want := []string{"copy /ws/beta.txt /ws/beta.txt"}; !equalStrings(ops.calls, want) {
		t.Errorf("ops = %v, want %v", ops.calls, want)
	}
}

func TestExplorerMenuCopyAndCutFillTheClipboardAndPasteRuns(t *testing.T) {
	ops := &fakeOps{}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt")
	openExplorerMenu(t, m, 0)
	chooseRow(t, m, "Copy")
	if m.clip == nil || m.clip.cut || !slices.Equal(m.clip.paths, []string{"/ws/alpha.txt"}) {
		t.Fatalf("clipboard after Copy = %+v", m.clip)
	}
	openExplorerMenu(t, m, 1)
	chooseRow(t, m, "Cut")
	if m.clip == nil || !m.clip.cut || !slices.Equal(m.clip.paths, []string{"/ws/beta.txt"}) {
		t.Fatalf("clipboard after Cut = %+v", m.clip)
	}
	openExplorerMenu(t, m, 0)
	for _, it := range ctxTop(m).Items() {
		if it.Label == "Paste" && it.Disabled {
			t.Fatal("Paste is disabled with a cut entry")
		}
	}
	chooseRow(t, m, "Paste")
}

func TestExplorerMenuRenameAndDeleteOpenTheirPrompts(t *testing.T) {
	for _, row := range []string{"Rename", "Delete"} {
		m, _, _ := newExplorerModel(t, &fakeOps{}, "alpha.txt")
		openExplorerMenu(t, m, 0)
		chooseRow(t, m, row)
		if len(m.overlays) != 1 {
			t.Errorf("%s: %d overlays, want the prompt", row, len(m.overlays))
		}
		if ctxTop(m) != nil {
			t.Errorf("%s: the menu is still open", row)
		}
	}
}

func TestExplorerMenuCopiesPaths(t *testing.T) {
	m, _, _ := newExplorerModel(t, &fakeOps{}, "alpha.txt")
	for row, want := range map[string]string{
		"Copy Full Path":     "/ws/alpha.txt",
		"Copy Relative Path": "alpha.txt",
	} {
		openExplorerMenu(t, m, 0)
		chooseRow(t, m, row)
		if m.notice != "Copied "+want {
			t.Errorf("%s: notice %q, want %q", row, m.notice, "Copied "+want)
		}
	}
}

func TestTabMenuCopiesPaths(t *testing.T) {
	m, ed := newCtxModel(t, "a.txt", "b.txt")
	ed.tabs[1].Path = "/ws/dir/b.txt"
	m.explorerOpen = true
	for row, want := range map[string]string{
		"Copy Full Path":     "/ws/dir/b.txt",
		"Copy Relative Path": "dir/b.txt",
	} {
		m.showContextMenu(mouseAtOrigin(), m.tabMenuItems(1))
		chooseRow(t, m, row)
		if m.notice != "Copied "+want {
			t.Errorf("%s: notice %q, want %q", row, m.notice, "Copied "+want)
		}
	}
}
