package tui

import (
	"errors"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/ctxmenu"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/theme"
)

// ctxEditor is a fake editor with tabs that close, move and activate. A tab named
// "dirty" needs a confirm before it closes.
type ctxEditor struct {
	*fakeEditor
	closed []string
}

func newCtxEditor(names ...string) *ctxEditor {
	e := &ctxEditor{fakeEditor: newFakeEditor("hello world")}
	e.tabs = nil
	for i, n := range names {
		e.tabs = append(e.tabs, TabInfo{Name: n, Path: "/proj/" + n, Active: i == 0, Modified: n == "dirty"})
	}
	return e
}

func (e *ctxEditor) Active() Document {
	if len(e.tabs) == 0 {
		return nil
	}
	return e.fakeEditor.Active()
}

func (e *ctxEditor) ActivateTab(i int) {
	for j := range e.tabs {
		e.tabs[j].Active = j == i
	}
}

func (e *ctxEditor) CloseTab(i int, force bool) error {
	if e.tabs[i].Modified && !force {
		return editor.ErrUnsaved
	}
	e.closed = append(e.closed, e.tabs[i].Name)
	wasActive := e.tabs[i].Active
	e.tabs = slices.Delete(e.tabs, i, i+1)
	if wasActive && len(e.tabs) > 0 {
		e.tabs[min(i, len(e.tabs)-1)].Active = true
	}
	return nil
}

func (e *ctxEditor) CloseNeedsConfirm(i int) (bool, error) { return e.tabs[i].Modified, nil }
func (e *ctxEditor) Quit(bool) []editor.Dirty              { return nil }

// MoveTabTo moves tab from to index to, as the app adapter does.
func (e *ctxEditor) MoveTabTo(from, to int) error {
	if from < 0 || from >= len(e.tabs) || to < 0 || to >= len(e.tabs) {
		return errors.New("no such tab")
	}
	t := e.tabs[from]
	e.tabs = slices.Insert(slices.Delete(e.tabs, from, from+1), to, t)
	return nil
}

func tabNames(e *ctxEditor) []string {
	var out []string
	for _, t := range e.tabs {
		out = append(out, t.Name)
	}
	return out
}

func newCtxModel(t *testing.T, names ...string) (*Model, *ctxEditor) {
	t.Helper()
	ed := newCtxEditor(names...)
	m := New(Deps{Editor: ed, Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, ed
}

func rightClickAt(m *Model, x, y int) {
	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseRight}))
	feedMessages(m, cmd)
}

// ctxTop returns the context menu on top of the overlay stack, or nil.
func ctxTop(m *Model) *ctxmenu.Menu {
	if len(m.overlays) == 0 {
		return nil
	}
	if o, ok := m.overlays[len(m.overlays)-1].(*ctxmenu.Menu); ok {
		return o
	}
	return nil
}

func menuLabels(menu *ctxmenu.Menu) []string {
	var out []string
	for _, it := range menu.Items() {
		if !it.Separator {
			out = append(out, it.Label)
		}
	}
	return out
}

// chooseRow clicks the row labelled label of the open context menu.
func chooseRow(t *testing.T, m *Model, label string) {
	t.Helper()
	menu := ctxTop(m)
	if menu == nil {
		t.Fatal("no context menu is open")
	}
	m.paint()
	x, y, _, _ := menu.Box()
	for i, it := range menu.Items() {
		if it.Label == label {
			_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: x + 3, Y: y + 1 + i, Button: tea.MouseLeft}))
			feedMessages(m, cmd)
			return
		}
	}
	t.Fatalf("no row %q in %v", label, menuLabels(menu))
}

func TestRightClickOnATabOpensTheTabMenu(t *testing.T) {
	m, _ := newCtxModel(t, "a.txt", "b.txt", "c.txt")
	l := m.layout()
	var tab run
	for _, r := range l.Strip.Runs {
		if r.Kind == hitTab && r.Index == 1 {
			tab = r
			break
		}
	}
	rightClickAt(m, tab.X+1, l.StripY)
	menu := ctxTop(m)
	if menu == nil {
		t.Fatal("a right click on a tab opened no menu")
	}
	want := []string{"Close", "Close Others", "Close to the Right", "Close to the Left", "Close All", "Copy Relative Path", "Copy Full Path"}
	if got := menuLabels(menu); !slices.Equal(got, want) {
		t.Fatalf("tab menu = %v, want %v", got, want)
	}
	for _, it := range menu.Items() {
		if it.Disabled {
			t.Errorf("%q is disabled for a middle tab with a path", it.Label)
		}
	}
}

func TestTabMenuDisablesRowsWithNothingToDo(t *testing.T) {
	tests := []struct {
		name     string
		names    []string
		tab      int
		disabled []string
	}{
		{"only tab", []string{"a.txt"}, 0, []string{"Close Others", "Close to the Right", "Close to the Left"}},
		{"first tab", []string{"a.txt", "b.txt"}, 0, []string{"Close to the Left"}},
		{"last tab", []string{"a.txt", "b.txt"}, 1, []string{"Close to the Right"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newCtxModel(t, tt.names...)
			var got []string
			for _, it := range m.tabMenuItems(tt.tab) {
				if it.Disabled {
					got = append(got, it.Label)
				}
			}
			if !slices.Equal(got, tt.disabled) {
				t.Errorf("disabled = %v, want %v", got, tt.disabled)
			}
		})
	}
	m, ed := newCtxModel(t, "a.txt")
	ed.tabs[0].Path = ""
	for _, it := range m.tabMenuItems(0) {
		if (it.Label == "Copy Full Path" || it.Label == "Copy Relative Path") && !it.Disabled {
			t.Errorf("%q is enabled for a tab with no file", it.Label)
		}
	}
}

func TestTabMenuClosesTabs(t *testing.T) {
	tests := []struct {
		row  string
		tab  int
		want []string // the tabs left
	}{
		{"Close", 1, []string{"a.txt", "c.txt", "d.txt"}},
		{"Close Others", 1, []string{"b.txt"}},
		{"Close to the Right", 1, []string{"a.txt", "b.txt"}},
		{"Close to the Left", 2, []string{"c.txt", "d.txt"}},
		{"Close All", 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.row, func(t *testing.T) {
			m, ed := newCtxModel(t, "a.txt", "b.txt", "c.txt", "d.txt")
			l := m.layout()
			var tab run
			for _, r := range l.Strip.Runs {
				if r.Kind == hitTab && r.Index == tt.tab {
					tab = r
					break
				}
			}
			rightClickAt(m, tab.X+1, l.StripY)
			chooseRow(t, m, tt.row)
			if got := tabNames(ed); !slices.Equal(got, tt.want) {
				t.Errorf("tabs after %q = %v, want %v", tt.row, got, tt.want)
			}
			if ctxTop(m) != nil {
				t.Error("the menu stays open after a choice")
			}
		})
	}
}

func TestTabMenuKeepsModifiedTabsAndSaysSo(t *testing.T) {
	m, ed := newCtxModel(t, "a.txt", "dirty", "c.txt")
	m.showContextMenu(tea.Mouse{X: 5, Y: 1}, m.tabMenuItems(0))
	chooseRow(t, m, "Close Others")
	if got := tabNames(ed); !slices.Equal(got, []string{"a.txt", "dirty"}) {
		t.Fatalf("tabs = %v, want the modified tab kept", got)
	}
	if m.notice == "" {
		t.Error("the status row does not say a tab was kept")
	}
}

func TestEscClosesAContextMenu(t *testing.T) {
	m, _ := newCtxModel(t, "a.txt", "b.txt")
	m.showContextMenu(tea.Mouse{X: 5, Y: 1}, m.tabMenuItems(0))
	press(m, special(tea.KeyEscape))
	if ctxTop(m) != nil {
		t.Error("Esc left the menu open")
	}
}

func TestRightClickOnAnotherTargetReopensTheMenuThere(t *testing.T) {
	m, _ := newCtxModel(t, "a.txt", "b.txt")
	l := m.layout()
	rightClickAt(m, l.Strip.Runs[0].X+1, l.StripY)
	if ctxTop(m) == nil {
		t.Fatal("no menu")
	}
	// A right click on the editor text, outside the menu, closes it and opens the editor menu.
	rightClickAt(m, l.TextX+5, l.BodyBottom)
	menu := ctxTop(m)
	if menu == nil {
		t.Fatal("the second right click opened no menu")
	}
	if got := menuLabels(menu); !slices.Contains(got, "Select All") || slices.Contains(got, "Close") {
		t.Errorf("menu = %v, want the editor menu", got)
	}
	if len(m.overlays) != 1 {
		t.Errorf("%d overlays, want only the new menu", len(m.overlays))
	}
}

func TestEditorMenuRunsClipboardActions(t *testing.T) {
	m, ed := newCtxModel(t, "a.txt")
	l := m.layout()
	rightClickAt(m, l.TextX+2, l.BodyTop)
	menu := ctxTop(m)
	if menu == nil {
		t.Fatal("a right click on the editor text opened no menu")
	}
	if got := menuLabels(menu); !slices.Equal(got, []string{"Cut", "Copy", "Paste", "Select All"}) {
		t.Fatalf("editor menu = %v", got)
	}
	// Nothing is selected, so Cut and Copy cannot be chosen.
	for _, it := range menu.Items() {
		if (it.Label == "Cut" || it.Label == "Copy") != it.Disabled && !it.Separator {
			t.Errorf("%q disabled = %v with no selection", it.Label, it.Disabled)
		}
	}
	chooseRow(t, m, "Select All")
	if n := len(ed.actions); n == 0 || ed.actions[n-1] != "select_all" {
		t.Fatalf("editor actions = %v, want select_all last", ed.actions)
	}
}

func TestRightClickWithNoMenuSaysSo(t *testing.T) {
	m, _ := newCtxModel(t, "a.txt")
	l := m.layout()
	rightClickAt(m, 90, l.StatusY)
	if ctxTop(m) != nil {
		t.Fatal("the status row opened a menu")
	}
	if m.notice != noContextMenuNotice {
		t.Errorf("notice = %q, want the no-menu text", m.notice)
	}
}

func TestExplorerMenuForAFileFolderAndTheBlankArea(t *testing.T) {
	tests := []struct {
		name string
		row  int // body row of the press
		want []string
	}{
		{"an entry", 1, []string{"New File", "New Directory", "Rename", "Cut", "Copy", "Paste", "Delete", "Duplicate", "Copy Full Path", "Copy Relative Path"}},
		{"the blank area", 12, []string{"New File", "New Directory", "Paste"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor("abc")
			m := newTestModel(t, ed, newFakeTree(3), 100, 30)
			m.focusExplorer()
			if !m.explorerOpen {
				t.Fatal("setup: the explorer is hidden")
			}
			rightClickAt(m, 3, explorerBodyTop+tt.row)
			menu := ctxTop(m)
			if menu == nil {
				t.Fatal("a right click in the explorer opened no menu")
			}
			if got := menuLabels(menu); !slices.Equal(got, tt.want) {
				t.Errorf("menu = %v, want %v", got, tt.want)
			}
			if !m.explorerFocus {
				t.Error("the explorer lost the focus")
			}
		})
	}
}

func TestExplorerMenuPasteIsDisabledWithAnEmptyClipboard(t *testing.T) {
	m := newTestModel(t, newFakeEditor("abc"), newFakeTree(3), 100, 30)
	for _, it := range m.explorerMenuItems(expanel.ContextTarget{Paths: []string{"/a/b"}}) {
		if it.Label == "Paste" && !it.Disabled {
			t.Error("Paste is enabled with nothing cut or copied")
		}
	}
	m.clip = newClipboard("/a/b", nil, false)
	for _, it := range m.explorerMenuItems(expanel.ContextTarget{Paths: []string{"/a/b"}}) {
		if it.Label == "Paste" && it.Disabled {
			t.Error("Paste is disabled with a copied entry")
		}
	}
}

func TestExplorerMenuOfSeveralEntriesLeavesOutRename(t *testing.T) {
	m := newTestModel(t, newFakeEditor("abc"), newFakeTree(3), 100, 30)
	var got []string
	for _, it := range m.explorerMenuItems(expanel.ContextTarget{Paths: []string{"/a/b", "/a/c"}}) {
		if !it.Separator {
			got = append(got, it.Label)
		}
	}
	want := []string{"Cut", "Copy", "Paste", "Delete", "Duplicate", "Copy Full Path", "Copy Relative Path"}
	if !slices.Equal(got, want) {
		t.Errorf("menu = %v, want %v", got, want)
	}
}

// TestEditorMenuKeepsTheCarets: a right click with several carets and no selection
// opens the menu without collapsing the caret set, so Paste or Cut act on all of them.
func TestEditorMenuKeepsTheCarets(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "hello world\nsecond line\nthird\n", 60, 12)
	mouseAt(m, 2, 0, 0)
	if r := ed.Do("add_cursor_below", nil); r.Err != nil {
		t.Fatal(r.Err)
	}
	before := ed.Active().Cursors()
	if len(before) == 0 {
		t.Fatal("setup: no extra caret")
	}
	l := m.layout()
	rightClickAt(m, l.TextX+4, l.BodyTop+1)
	if ctxTop(m) == nil {
		t.Fatal("no context menu opened")
	}
	if got := ed.Active().Cursors(); len(got) != len(before) {
		t.Errorf("carets after a right click = %v, want the %d extra caret(s) kept", got, len(before))
	}
}

// TestEditorMenuCutAndCopyNeedASelectionWithSeveralCarets: with several carets and no
// selection, Cut and Copy stay disabled, as Fresh's can_copy and can_cut need an active
// selection. Paste stays enabled, since it acts on every caret.
func TestEditorMenuCutAndCopyNeedASelectionWithSeveralCarets(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "hello world\nsecond line\nthird\n", 60, 12)
	mouseAt(m, 2, 0, 0)
	if r := ed.Do("add_cursor_below", nil); r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(ed.Active().Cursors()) == 0 {
		t.Fatal("setup: no extra caret")
	}
	l := m.layout()
	rightClickAt(m, l.TextX+4, l.BodyTop+1)
	menu := ctxTop(m)
	if menu == nil {
		t.Fatal("no context menu opened")
	}
	disabled := map[string]bool{}
	for _, it := range menu.Items() {
		if !it.Separator {
			disabled[it.Label] = it.Disabled
		}
	}
	if !disabled["Cut"] || !disabled["Copy"] || disabled["Paste"] {
		t.Errorf("several carets, no selection: Cut disabled %v, Copy disabled %v, Paste disabled %v; want Cut and Copy disabled and Paste enabled",
			disabled["Cut"], disabled["Copy"], disabled["Paste"])
	}
}

// ctxROEditor makes the active document of a real editor report read-only.
type ctxROEditor struct{ realEditor }

type ctxRODoc struct{ realDoc }

func (ctxRODoc) ReadOnly() bool { return true }

func (r ctxROEditor) Active() Document {
	if d := r.realEditor.Active(); d != nil {
		return ctxRODoc{d.(realDoc)}
	}
	return nil
}

func TestEditorMenuDisablesEditsOnAReadOnlyDocument(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "hello world\n", 60, 12)
	m.ed = ctxROEditor{m.ed.(realEditor)}
	l := m.layout()
	rightClickAt(m, l.TextX+2, l.BodyTop)
	menu := ctxTop(m)
	if menu == nil {
		t.Fatal("no context menu opened")
	}
	for _, it := range menu.Items() {
		want := it.Label == "Cut" || it.Label == "Paste"
		if want && !it.Disabled {
			t.Errorf("%q is enabled on a read-only document", it.Label)
		}
		if it.Label == "Select All" && it.Disabled {
			t.Error("Select All is disabled on a read-only document")
		}
	}
}
