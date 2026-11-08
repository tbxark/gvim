package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/menubar"
	"github.com/tbxark/gvim/theme"
)

func hoverAt(m *Model, x, y int) {
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: x, Y: y}))
}

// TestHoverThroughTheRootClearsTheMenuHighlight drives the pointer through the
// root model: a row under it is highlighted, and every area that is not a row
// leaves nothing highlighted while the dropdown stays open.
func TestHoverThroughTheRootClearsTheMenuHighlight(t *testing.T) {
	cases := []struct {
		name string
		x, y int
	}{
		{"separator", 3, 4},
		{"top border", 3, 1},
		{"left border", 0, 3},
		{"bar blank space", 70, 0},
		{"editor text", 40, 12},
		{"status row", 40, 23},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := menuModel(t, false)
			m.dispatch(Command{Action: "menu_open", Args: map[string]any{"name": "File"}})
			hoverAt(m, 3, 3) // the second row of File
			if got := m.menu.Selected(); got != 1 {
				t.Fatalf("hover on a row highlighted %d, want 1", got)
			}
			hoverAt(m, c.x, c.y)
			if got := m.menu.Selected(); got != -1 {
				t.Errorf("the highlight stays on row %d at %s", got, c.name)
			}
			if m.menu.Title() != "File" {
				t.Errorf("the dropdown is %q, want it to stay open", m.menu.Title())
			}
		})
	}
}

// TestHoverThroughTheRootHighlightsTheFirstRowOfALabel drives the pointer over the
// bar labels through the root model. A label that is not the open one switches to
// its dropdown and highlights the first selectable row, as Fresh's open_menu does.
// The open label, which is the bar and not a row, highlights nothing.
func TestHoverThroughTheRootHighlightsTheFirstRowOfALabel(t *testing.T) {
	m := menuModel(t, false)
	m.dispatch(Command{Action: "menu_open", Args: map[string]any{"name": "File"}})
	hoverAt(m, 9, 0) // the Edit label
	if m.menu.Title() != "Edit" {
		t.Fatalf("hover on Edit opened %q", m.menu.Title())
	}
	sel := m.menu.Selected()
	rows := m.menu.Rows()
	if sel < 0 || rows[sel].Separator || rows[sel].Disabled {
		t.Fatalf("hover on Edit highlights %d, want the first selectable row", sel)
	}
	for i := range sel {
		if !rows[i].Separator && !rows[i].Disabled {
			t.Fatalf("row %d of Edit is selectable and before the highlight %d", i, sel)
		}
	}
	hoverAt(m, 9, 0) // the same label again
	if m.menu.Title() != "Edit" || m.menu.Selected() != -1 {
		t.Errorf("second hover on Edit: title %q, highlight %d", m.menu.Title(), m.menu.Selected())
	}
}

func TestHoverOverASubmenuRowOpensIt(t *testing.T) {
	m := menuModel(t, false)
	m.height = 60
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 60})
	m.dispatch(Command{Action: "menu_open", Args: map[string]any{"name": "Edit"}})
	rows := m.menu.Rows()
	row := -1
	for i, r := range rows {
		if r.Label == "Copy with Formatting" {
			row = i
		}
	}
	if row < 0 {
		t.Fatal("no Copy with Formatting row")
	}
	hoverAt(m, 10, 2+row) // the Edit dropdown hangs from column 7
	if !m.menu.SubOpen() || m.menu.Selected() != row {
		t.Fatalf("hover on the submenu row: open %v, highlight %d", m.menu.SubOpen(), m.menu.Selected())
	}
}

func TestCopyWithFormattingRowRunsCopyWithTheTheme(t *testing.T) {
	ed := newFakeEditor("hello")
	m := New(Deps{Editor: ed, Explorer: newFakeTree(3), Keys: menuKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if err := m.Run("copy_with_theme:nord"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	last := len(ed.actions) - 1
	if ed.actions[last] != "copy_with_theme" || ed.args[last]["theme"] != "nord" {
		t.Fatalf("the editor got %q %v", ed.actions[last], ed.args[last])
	}
}

// TestViewMenuMarksFollowTheRealSettings toggles each View setting through the
// menu action and checks the mark the row draws after reopening the dropdown.
func TestViewMenuMarksFollowTheRealSettings(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "hello\n", 80, 50)
	mark := func(title, label string) bool {
		t.Helper()
		m.menu = m.menu.Close()
		m.dispatch(Command{Action: "menu_open", Args: map[string]any{"name": title}})
		for _, r := range m.menu.Rows() {
			if r.Label == label {
				if !r.Checkbox {
					t.Fatalf("%s > %s has no checkbox", title, label)
				}
				return r.Checked
			}
		}
		t.Fatalf("no row %s > %s", title, label)
		return false
	}
	tests := []struct{ title, label, action string }{
		{"View", "Line Numbers", "toggle_line_numbers"},
		{"View", "Line Wrap", "toggle_line_wrap"},
		{"View", "Vertical Scrollbar", "toggle_vertical_scrollbar"},
		{"View", "Horizontal Scrollbar", "toggle_horizontal_scrollbar"},
		{"View", "Mouse Support", "toggle_mouse_capture"},
		{"LSP", "Toggle Mouse Hover", "toggle_mouse_hover"},
		{"LSP", "Toggle Inlay Hints", "toggle_inlay_hints"},
	}
	for _, tt := range tests {
		before := mark(tt.title, tt.label)
		runAction(t, m, tt.action)
		if after := mark(tt.title, tt.label); after == before {
			t.Errorf("%s: the mark stays %v after %s", tt.label, after, tt.action)
		}
		runAction(t, m, tt.action)
		if again := mark(tt.title, tt.label); again != before {
			t.Errorf("%s: the mark is %v after toggling twice, want %v", tt.label, again, before)
		}
	}
}

// findMenuCell returns the first cell of the 80 by 40 frame whose hit in the open menu
// has the given kind and index. An index of -1 matches any index.
func findMenuCell(t *testing.T, m *Model, kind menubar.TargetKind, index int) (int, int) {
	t.Helper()
	for y := range 40 {
		for x := range 80 {
			if got := m.menu.Hit(x, y); got.Kind == kind && (index < 0 || got.Index == index) {
				return x, y
			}
		}
	}
	t.Fatalf("no cell of kind %v with index %d", kind, index)
	return 0, 0
}

// TestHoverThroughTheRootOnTheSubmenuAreas drives the pointer over a submenu and the
// areas around it. A submenu row highlights its child. Every other area clears the
// child's highlight, and the parent row, the submenu and the menu stay as Fresh keeps
// them, except that a row of another kind closes the submenu.
func TestHoverThroughTheRootOnTheSubmenuAreas(t *testing.T) {
	m := menuModel(t, false)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.dispatch(Command{Action: "menu_open", Args: map[string]any{"name": "View"}})
	terminal := -1
	for i, r := range m.menu.Rows() {
		if r.Label == "Terminal" {
			terminal = i
		}
	}
	if terminal < 0 {
		t.Fatal("no Terminal row in View")
	}
	x, y := findMenuCell(t, m, menubar.TargetItem, terminal)
	hoverAt(m, x, y)
	if !m.menu.SubOpen() || m.menu.Selected() != terminal {
		t.Fatalf("hover on Terminal: submenu open %v, highlight %d", m.menu.SubOpen(), m.menu.Selected())
	}
	x, y = findMenuCell(t, m, menubar.TargetSubItem, 1)
	hoverAt(m, x, y)
	if m.menu.SubSelected() != 1 {
		t.Fatalf("hover on submenu row 1 highlighted %d", m.menu.SubSelected())
	}
	x, y = findMenuCell(t, m, menubar.TargetSubBorder, -1)
	hoverAt(m, x, y)
	if !m.menu.SubOpen() || m.menu.SubSelected() != -1 || m.menu.Selected() != terminal {
		t.Errorf("on the submenu border: open %v, child %d, parent %d", m.menu.SubOpen(), m.menu.SubSelected(), m.menu.Selected())
	}
	hoverAt(m, 70, 0) // the bar, outside the open label
	if !m.menu.SubOpen() || m.menu.SubSelected() != -1 || m.menu.Selected() != -1 {
		t.Errorf("on the bar: open %v, child %d, parent %d", m.menu.SubOpen(), m.menu.SubSelected(), m.menu.Selected())
	}
	x, y = findMenuCell(t, m, menubar.TargetItem, 0)
	hoverAt(m, x, y)
	if m.menu.SubOpen() || m.menu.Selected() != 0 {
		t.Errorf("hover on another row: the submenu stays open %v, highlight %d", m.menu.SubOpen(), m.menu.Selected())
	}
}
