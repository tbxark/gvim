package menubar

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// labelPorts is a fakePorts that also knows the shortcuts of a keymap.
type labelPorts struct {
	*fakePorts
	labels map[string]string
}

func (p labelPorts) KeyLabel(action string) (string, bool) { return p.labels[action], true }

// rowByLabel returns the row of the open dropdown called label.
func rowByLabel(t *testing.T, m Model, label string) Row {
	t.Helper()
	for _, r := range m.Rows() {
		if r.Label == label {
			return r
		}
	}
	t.Fatalf("no row %q in %s", label, m.Title())
	return Row{}
}

// selectRow moves the highlight to the row called label with the Down key.
func selectRow(t *testing.T, m Model, label string) Model {
	t.Helper()
	for range len(m.Rows()) {
		if m.Rows()[m.Selected()].Label == label {
			return m
		}
		m, _ = send(m, keyMsg("down"))
	}
	t.Fatalf("cannot reach row %q", label)
	return m
}

func TestRowsShowTheShortcutsOfTheActiveKeymap(t *testing.T) {
	ports := labelPorts{allOn(), map[string]string{"redo": "⌃Y", "search": "⌃F", "select_all": "⌃A"}}
	m := newBar(ports, 80, 30).openTitle("Edit")
	tests := []struct{ label, keys string }{
		{"Redo", "⌃Y"},       // not the table's ⌃R
		{"Select All", "⌃A"}, // the table has none
		{"Delete Line", ""},  // the table's ⌃⌥K is not bound here
		{"Find...", "⌃F"},    // bound
		{"Find Next", ""},    // the table's F3 is not bound here
		{"Undo", ""},         // unbound in this keymap
		{"Copy with Formatting", " >  "},
	}
	for _, tc := range tests {
		if got := rowByLabel(t, m, tc.label).Keys; got != tc.keys {
			t.Errorf("%s shows %q, want %q", tc.label, got, tc.keys)
		}
	}
}

// TestRowsShowNoShortcutWithoutAKeymap: with no keymap to ask, no row shows a shortcut. The
// table's keys are not drawn, so the menus never show a key the active keymap does not bind.
func TestRowsShowNoShortcutWithoutAKeymap(t *testing.T) {
	m := newBar(allOn(), 80, 30).openTitle("Edit")
	if got := rowByLabel(t, m, "Redo").Keys; got != "" {
		t.Errorf("Redo shows %q without a keymap, want none", got)
	}
}

func TestCheckboxRowsShowTheStateOfTheSetting(t *testing.T) {
	ports := allOn()
	ports.on["line_wrap"] = true
	m := newBar(ports, 80, 30).openTitle("View")
	wrap, numbers := rowByLabel(t, m, "Line Wrap"), rowByLabel(t, m, "Line Numbers")
	if !wrap.Checkbox || !wrap.Checked || !numbers.Checkbox || !numbers.Checked {
		t.Fatalf("Line Wrap %+v, Line Numbers %+v, want both checked boxes", wrap, numbers)
	}
	if r := rowByLabel(t, m, "Set Page Width..."); r.Checkbox {
		t.Errorf("a plain row is a checkbox: %+v", r)
	}
	if r := rowByLabel(t, m, "Mouse Support"); !r.Checkbox || !r.Checked {
		t.Errorf("Mouse Support = %+v, want a checked box", r)
	}
	ports.on["line_wrap"] = false
	m = newBar(ports, 80, 30).openTitle("View")
	if r := rowByLabel(t, m, "Line Wrap"); !r.Checkbox || r.Checked {
		t.Errorf("Line Wrap = %+v, want an unchecked box", r)
	}
	// The glyph is drawn before the label of the row, and the label of a plain row
	// starts where the glyph of a checkbox row does.
	rows := paintRows(m)
	var wrapRow, plainRow string
	for _, r := range rows {
		switch {
		case strings.Contains(r, "Line Wrap"):
			wrapRow = r
		case strings.Contains(r, "Set Page Width"):
			plainRow = r
		}
	}
	if !strings.Contains(wrapRow, "│ ☐ Line Wrap") {
		t.Errorf("checkbox row = %q", wrapRow)
	}
	if !strings.Contains(plainRow, "│ Set Page Width") {
		t.Errorf("plain row = %q", plainRow)
	}
}

func TestSubmenuOpensAndRunsItsRow(t *testing.T) {
	p := allOn()
	m := selectRow(t, newBar(p, 80, 30).openTitle("View"), "Keybinding Style")
	m, cmd := send(m, keyMsg("enter"))
	if cmd != nil || !m.IsOpen() || !m.SubOpen() {
		t.Fatalf("Enter on a submenu row: open %v, submenu %v, cmd %v", m.IsOpen(), m.SubOpen(), cmd != nil)
	}
	var labels []string
	for _, r := range m.SubRows() {
		labels = append(labels, r.Label)
	}
	if want := []string{"Default", "Emacs", "VSCode", "macOS GUI (⌘)"}; !slices.Equal(labels, want) {
		t.Fatalf("submenu rows = %v, want %v", labels, want)
	}
	if r := m.SubRows()[0]; !r.Checkbox || !r.Checked {
		t.Errorf("the active keymap row is %+v, want a checked box", r)
	}
	if r := m.SubRows()[1]; r.Checked {
		t.Errorf("Emacs row is checked: %+v", r)
	}
	m, _ = send(m, keyMsg("down"))
	m, _ = send(m, keyMsg("enter"))
	if m.IsOpen() {
		t.Fatal("running a submenu row left the menu open")
	}
	if want := []string{"switch_keybinding_map:emacs"}; !slices.Equal(p.ran, want) {
		t.Fatalf("ran %v, want %v", p.ran, want)
	}
}

func TestSubmenuKeysAndPointer(t *testing.T) {
	open := func() Model {
		m := selectRow(t, newBar(allOn(), 80, 30).openTitle("View"), "Terminal")
		m, _ = send(m, keyMsg("right"))
		if !m.SubOpen() {
			t.Fatal("Right on a submenu row did not open it")
		}
		return m
	}
	t.Run("left and esc close the submenu only", func(t *testing.T) {
		for _, k := range []string{"left", "esc"} {
			m, _ := send(open(), keyMsg(k))
			if m.SubOpen() || !m.IsOpen() || m.Title() != "View" {
				t.Errorf("%s: submenu %v, open %v, title %q", k, m.SubOpen(), m.IsOpen(), m.Title())
			}
		}
	})
	t.Run("the highlight skips separators", func(t *testing.T) {
		m := open()
		for range 5 {
			m, _ = send(m, keyMsg("down"))
		}
		rows := m.SubRows()
		if got := rows[m.SubSelected()].Label; got != "Toggle Keyboard Capture" {
			t.Errorf("highlight on %q, want the row after the separator", got)
		}
	})
	t.Run("a letter runs no row: the menus have no mnemonics", func(t *testing.T) {
		p := allOn()
		m := selectRow(t, newBar(p, 80, 30).openTitle("View"), "Terminal")
		m, _ = send(m, keyMsg("right"))
		for _, letter := range "abcdefghijklmnopqrstuvwxyz" {
			if m.Consumes(keyMsg(string(letter))) {
				t.Errorf("the open dropdown consumes the letter %q", letter)
			}
			m, _ = send(m, keyMsg(string(letter)))
		}
		if !m.IsOpen() || len(p.ran) != 0 {
			t.Errorf("open %v, ran %v; a letter must do nothing", m.IsOpen(), p.ran)
		}
	})
	t.Run("clicks", func(t *testing.T) {
		p := allOn()
		m := selectRow(t, newBar(p, 80, 30).openTitle("View"), "Terminal")
		geo := m.geometry(m.w, m.h)
		y := geo.box.top + 1 + m.Selected() - geo.box.first
		m, _ = send(m, press(geo.box.x+3, y))
		if !m.SubOpen() {
			t.Fatal("a click on a submenu row did not open it")
		}
		geo = m.geometry(m.w, m.h)
		if !geo.hasSub || geo.sub.x < geo.box.x+geo.box.w {
			t.Fatalf("submenu box %+v does not sit beside the dropdown %+v", geo.sub, geo.box)
		}
		if got := m.Hit(geo.sub.x+3, geo.sub.top+1); got.Kind != TargetSubItem || got.Index != 0 {
			t.Fatalf("Hit on the first submenu row = %+v", got)
		}
		m, _ = send(m, press(geo.sub.x+3, geo.sub.top+2))
		if m.IsOpen() || !slices.Equal(p.ran, []string{"close_terminal"}) {
			t.Errorf("open %v, ran %v", m.IsOpen(), p.ran)
		}
	})
}

func TestDropdownMovesLeftToFitTheFrame(t *testing.T) {
	for _, w := range []int{50, 40, 30} {
		for _, title := range Labels(false) {
			m := newBar(allOn(), w, 24).openTitle(title)
			box := m.geometry(w, 24).box
			if box.x < 0 || box.x+box.w > w {
				t.Errorf("width %d, %s: box x %d w %d is outside the frame", w, title, box.x, box.w)
			}
			if g := paintRows(m); !strings.Contains(strings.Join(g, "\n"), "╮") {
				t.Errorf("width %d, %s: the top right corner is not on screen", w, title)
			}
		}
	}
}

func TestLongDropdownScrollsAndKeepsTheStatusRow(t *testing.T) {
	const h = 24
	m := newBar(allOn(), 80, h).openTitle("View")
	rows := m.Rows()
	if len(rows) < h {
		t.Fatalf("setup: the View menu has only %d rows", len(rows))
	}
	for range 3 * len(rows) {
		m, _ = send(m, keyMsg("down"))
		g := grid.New(80, h)
		m.Paint(g)
		plain := g.Plain()
		if strings.TrimSpace(plain[h-1]) != "" {
			t.Fatalf("the dropdown drew on the status row: %q", plain[h-1])
		}
		box := m.geometry(80, h).box
		if sel := m.Selected(); sel < box.first || sel >= box.first+box.visible() {
			t.Fatalf("highlight %d is outside the window %d..%d", sel, box.first, box.first+box.visible())
		}
		if !strings.Contains(plain[box.top+1+m.Selected()-box.first], m.Rows()[m.Selected()].Label) {
			t.Fatalf("the highlighted row %q is not on screen", m.Rows()[m.Selected()].Label)
		}
		if got := m.Hit(box.x+3, box.top+1+m.Selected()-box.first); got.Kind != TargetItem || got.Index != m.Selected() {
			t.Fatalf("Hit on the highlighted row = %+v, want item %d", got, m.Selected())
		}
	}
}
