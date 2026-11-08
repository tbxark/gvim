package tui

import (
	"reflect"
	"testing"
)

// TestExplorerStateReportsThePanelAndTheTree checks the state a session keeps: the
// panel's shown flag, the tree root and the two listing settings. With no explorer
// the state is closed and empty.
func TestExplorerStateReportsThePanelAndTheTree(t *testing.T) {
	t.Parallel()
	tree := newFakeTree(2)
	tree.ignored = true
	m := newTestModel(t, newFakeEditor("x"), tree, 80, 20)
	if st := m.ExplorerState(); st.Open || st.Root != "/ws" || !st.ShowIgnored {
		t.Fatalf("hidden panel state = %+v, want closed, root /ws, show-ignored on", st)
	}
	must(t, m.Run("focus_next_sidebar_section"))
	if st := m.ExplorerState(); !st.Open {
		t.Fatalf("shown panel state = %+v, want open", st)
	}

	none := newTestModel(t, newFakeEditor("x"), nil, 80, 20)
	if st := none.ExplorerState(); !reflect.DeepEqual(st, ExplorerState{}) {
		t.Fatalf("no explorer state = %+v, want the zero value", st)
	}
}
