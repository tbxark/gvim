package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/theme"
	expanel "github.com/tbxark/gvim/internal/ui/explorer"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// explorerKeys is chromeKeys with the explorer chords the search, clipboard and
// gitignore tests press: Backspace, Space, Ctrl+X, Ctrl+V, Ctrl+I, and the file
// operations on Delete and F2.
func explorerKeys() fakeKeymap {
	k := chromeKeys()
	k["file_explorer|Backspace|"] = Command{Action: "file_explorer_search_backspace"}
	k["file_explorer| |"] = Command{Action: "file_explorer_toggle_select"}
	k["file_explorer|x|ctrl"] = Command{Action: "cut"}
	k["file_explorer|v|ctrl"] = Command{Action: "paste"}
	k["file_explorer|i|ctrl"] = Command{Action: "file_explorer_toggle_gitignored"}
	k["file_explorer|Delete|"] = Command{Action: "file_explorer_delete"}
	k["file_explorer|F2|"] = Command{Action: "file_explorer_rename"}
	return k
}

// newExplorerModel returns a model with the explorer open and focused over a tree
// of the files called names, with the first one selected. The file operations
// are wired to ops. It returns the model, its editor and its tree.
func newExplorerModel(t *testing.T, ops *fakeOps, names ...string) (*Model, *fakeEditor, *fakeTree) {
	t.Helper()
	ed := newFakeEditor("hello")
	tr := newFakeTree(0)
	for _, n := range names {
		tr.rows = append(tr.rows, explorer.Row{Name: n, Path: "/ws/" + n})
	}
	m := New(Deps{Editor: ed, Explorer: tr, Keys: explorerKeys(), Ops: ops, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	press(m, ctrl('e')) // focus_file_explorer
	return m, ed, tr
}

// rowName finds the first drawn cell of name in the frame. It returns that
// cell's style and whether the name is drawn at all.
func rowName(m *Model, name string) (grid.Style, bool) {
	g := m.paint()
	want := []rune(name)
	for y := range g.Height() {
		for x := 0; x+len(want) <= g.Width(); x++ {
			match := true
			for i, r := range want {
				if g.At(x+i, y).Text != string(r) {
					match = false
					break
				}
			}
			if match {
				return g.At(x, y).Style, true
			}
		}
	}
	return grid.Style{}, false
}

func TestTypedLetterSearchesTheTreeAndTypesNothing(t *testing.T) {
	t.Parallel()
	m, ed, tr := newExplorerModel(t, &fakeOps{}, "alpha.txt", "beta.txt", "gamma.txt")
	press(m, typed("g"))
	if tr.selected != 2 {
		t.Errorf("selected row %d after typing g, want 2 (gamma.txt)", tr.selected)
	}
	if len(ed.actions) != 0 {
		t.Errorf("typed letter reached the editor: %v", ed.actions)
	}
}

func TestChordLettersDoNotSearchTheTree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "ctrl", key: ctrl('g')},
		{name: "alt", key: tea.KeyPressMsg{Code: 'g', Mod: tea.ModAlt}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, ed, tr := newExplorerModel(t, &fakeOps{}, "alpha.txt", "beta.txt", "gamma.txt")
			press(m, tt.key)
			if tr.selected != 0 || len(ed.actions) != 0 {
				t.Errorf("selected row %d and editor actions %v, want row 0 and none", tr.selected, ed.actions)
			}
		})
	}
}

func TestBackspaceShortensTheSearch(t *testing.T) {
	t.Parallel()
	m, ed, tr := newExplorerModel(t, &fakeOps{}, "a.txt", "ab.txt", "zz.txt")
	tr.selected = 2
	press(m, typed("a"))
	press(m, typed("b"))
	if tr.selected != 1 {
		t.Fatalf("selected row %d after typing ab, want 1 (ab.txt)", tr.selected)
	}
	press(m, special(tea.KeyBackspace))
	if tr.selected != 0 {
		t.Errorf("selected row %d after Backspace, want 0: the search a first matches a.txt", tr.selected)
	}
	if len(ed.actions) != 0 {
		t.Errorf("explorer keys reached the editor: %v", ed.actions)
	}
}

func TestBackspaceWithoutSearchStaysInTheExplorer(t *testing.T) {
	t.Parallel()
	m, ed, tr := newExplorerModel(t, &fakeOps{}, "alpha.txt", "beta.txt")
	tr.selected = 1
	press(m, special(tea.KeyBackspace))
	if tr.selected != 1 || len(ed.actions) != 0 {
		t.Errorf("Backspace with no search moved to row %d and sent editor actions %v, want row 1 and none", tr.selected, ed.actions)
	}
}

func TestSpacePicksTheCursorRowAndIsNotSearched(t *testing.T) {
	t.Parallel()
	m, ed, _ := newExplorerModel(t, &fakeOps{}, "alpha.txt", "beta.txt", "gamma.txt")
	press(m, typed(" "))
	press(m, special(tea.KeyDown))
	msg := runMsg(press(m, ctrl('c')))
	if got, ok := msg.(expanel.CopyMsg); !ok || got.Path != "/ws/alpha.txt" {
		t.Errorf("copy after picking alpha.txt sent %#v, want CopyMsg for /ws/alpha.txt", msg)
	}
	if len(ed.actions) != 0 {
		t.Errorf("space reached the editor: %v", ed.actions)
	}
}

func TestEscCancelsAPendingCutInTheRoot(t *testing.T) {
	t.Parallel()
	ops := &fakeOps{}
	m, _, _ := newExplorerModel(t, ops, "alpha.txt", "beta.txt")
	settle(t, m, press(m, ctrl('x')))
	if m.clip == nil || !m.clip.cut || !m.explorerPanel.CutPending() {
		t.Fatalf("setup: clip %+v, cut pending %v, want a pending cut", m.clip, m.explorerPanel.CutPending())
	}
	if st, ok := rowName(m, "alpha.txt"); !ok || !st.Faint {
		t.Fatalf("setup: alpha.txt is not drawn dim under the cut (drawn %v)", ok)
	}
	press(m, special(tea.KeyEscape))
	if m.clip != nil {
		t.Errorf("clipboard kept after Esc: %+v", m.clip)
	}
	if m.explorerPanel.CutPending() {
		t.Error("cut still pending after Esc")
	}
	if st, ok := rowName(m, "alpha.txt"); !ok || st.Faint {
		t.Errorf("alpha.txt still drawn dim after Esc (drawn %v)", ok)
	}
	if !m.explorerFocus {
		t.Error("the Esc that cleared the cut moved the focus to the editor")
	}
	settle(t, m, press(m, ctrl('v')))
	if len(ops.calls) != 0 || m.notice != "Nothing to paste" {
		t.Errorf("paste after Esc ran %v with notice %q, want no file operation and \"Nothing to paste\"", ops.calls, m.notice)
	}
}

func TestEscKeepsAPendingCopy(t *testing.T) {
	t.Parallel()
	m, _, _ := newExplorerModel(t, &fakeOps{}, "alpha.txt")
	settle(t, m, press(m, ctrl('c')))
	press(m, special(tea.KeyEscape))
	if m.clip == nil || m.clip.cut {
		t.Errorf("Esc changed the pending copy: clip %+v, want a copy", m.clip)
	}
}

func TestShowGitignoredCheckboxFollowsTheTree(t *testing.T) {
	t.Parallel()
	m, _, tr := newExplorerModel(t, &fakeOps{}, "alpha.txt")
	if m.Holds("file_explorer_show_gitignored") {
		t.Fatal("checkbox is on before the tree shows gitignored files")
	}
	tr.SetShowIgnored(true)
	if !m.Holds("file_explorer_show_gitignored") {
		t.Error("checkbox is off after the tree shows gitignored files")
	}
}

func TestCtrlIToggleGitignoredUpdatesTheCheckbox(t *testing.T) {
	t.Parallel()
	m, _, tr := newExplorerModel(t, &fakeOps{}, "alpha.txt")
	settle(t, m, press(m, ctrl('i')))
	if !tr.ignored || !m.Holds("file_explorer_show_gitignored") {
		t.Fatalf("first Ctrl+I: tree shows gitignored %v, checkbox %v; want both on", tr.ignored, m.Holds("file_explorer_show_gitignored"))
	}
	settle(t, m, press(m, ctrl('i')))
	if tr.ignored || m.Holds("file_explorer_show_gitignored") {
		t.Errorf("second Ctrl+I: tree shows gitignored %v, checkbox %v; want both off", tr.ignored, m.Holds("file_explorer_show_gitignored"))
	}
}
