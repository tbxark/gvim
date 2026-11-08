package tui

import "testing"

// TestSidebarCycleVisitsExplorerThenEditor checks the focus cycle with the explorer
// hidden at the start: the next stop shows and focuses the explorer, the stop after
// it is the editor, and the cycle repeats. The previous action walks it backward.
func TestSidebarCycleVisitsExplorerThenEditor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		action string
		steps  []string // "explorer" or "editor", the focus after each press
	}{
		{"next from hidden", "focus_next_sidebar_section", []string{"explorer", "editor", "explorer"}},
		{"previous from hidden", "focus_prev_sidebar_section", []string{"explorer", "editor", "explorer"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t, newFakeEditor("x"), newFakeTree(3), 80, 20)
			for i, want := range tt.steps {
				if err := m.Run(tt.action); err != nil {
					t.Fatalf("step %d: %s returned %v", i, tt.action, err)
				}
				if got := focusName(m); got != want {
					t.Fatalf("step %d: focus is %s, want %s", i, got, want)
				}
			}
		})
	}
}

// TestSidebarPreviousFromEditorReachesExplorer checks that the previous action from
// the editor goes to the explorer, which the cycle reaches last.
func TestSidebarPreviousFromEditorReachesExplorer(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), newFakeTree(3), 80, 20)
	if err := m.Run("focus_prev_sidebar_section"); err != nil {
		t.Fatal(err)
	}
	if got := focusName(m); got != "explorer" {
		t.Fatalf("focus is %s after previous from the editor, want explorer", got)
	}
	if !m.explorerOpen {
		t.Fatal("the explorer was focused but its panel is hidden")
	}
}

// TestSidebarCycleWithoutTreeStaysInEditor checks that a model with no explorer has
// one stop, the editor, so the action does nothing but keep the focus there.
func TestSidebarCycleWithoutTreeStaysInEditor(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 20)
	for _, action := range []string{"focus_next_sidebar_section", "focus_prev_sidebar_section"} {
		if err := m.Run(action); err != nil {
			t.Fatalf("%s returned %v, want it handled", action, err)
		}
		if got := focusName(m); got != "editor" {
			t.Fatalf("%s left focus on %s, want editor", action, got)
		}
	}
}

// TestSidebarCycleClosesTheMenu checks that the cycle leaves an open menu.
func TestSidebarCycleClosesTheMenu(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), newFakeTree(3), 80, 20)
	m.menu = m.menu.Open(0)
	if err := m.Run("focus_next_sidebar_section"); err != nil {
		t.Fatal(err)
	}
	if m.menu.IsOpen() {
		t.Fatal("the menu stayed open when focus moved to the explorer")
	}
}

// focusName names where keyboard focus is: "explorer" or "editor".
func focusName(m *Model) string {
	if m.stage() == stageExplorer {
		return "explorer"
	}
	return "editor"
}
