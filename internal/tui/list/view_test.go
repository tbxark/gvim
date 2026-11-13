package list

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// filterRows is a list with two groups, used by the filter tests.
func filterRows() []Row {
	return []Row{
		{Header: true, Left: "normal"},
		{Left: "save", Right: "ctrl+s"},
		{Left: "open", Right: "ctrl+o"},
		{Header: true, Left: "global"},
		{Left: "quit", Right: "ctrl+q"},
	}
}

// shownLefts returns the left text of each shown row, headers included.
func shownLefts(v *View) []string {
	var out []string
	for _, i := range v.shown {
		out = append(out, v.Rows[i].Left)
	}
	return out
}

func TestListFilterKeepsOnlyMatchingRowsAndTheirHeaders(t *testing.T) {
	tests := []struct {
		name   string
		typed  string
		want   []string
		noPick bool
	}{
		{"empty filter shows every row", "", []string{"normal", "save", "open", "global", "quit"}, false},
		{"match inside a group keeps its header", "sa", []string{"normal", "save"}, false},
		{"match in the other group drops the first header", "qu", []string{"global", "quit"}, false},
		{"match on the right text", "ctrl+o", []string{"normal", "open"}, false},
		{"no match leaves nothing to choose", "zz", nil, true},
		{"filter is case-insensitive", "SAVE", []string{"normal", "save"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New("T", filterRows(), style.Overlay{}, nil)
			for _, r := range tt.typed {
				v.update(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
			got := shownLefts(v)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("shown = %v, want %v", got, tt.want)
			}
			_, ok := v.selected()
			if ok == tt.noPick {
				t.Fatalf("a row selected = %v, want %v", ok, !tt.noPick)
			}
		})
	}
}

func TestListBackspaceWidensTheFilter(t *testing.T) {
	v := New("T", filterRows(), style.Overlay{}, nil)
	v.update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if len(v.shown) != 0 {
		t.Fatalf("filter z shows %v, want nothing", shownLefts(v))
	}
	v.update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(v.shown) != len(filterRows()) {
		t.Fatalf("after backspace shown = %v, want every row", shownLefts(v))
	}
}

func TestListArrowsSkipHeaders(t *testing.T) {
	v := New("T", filterRows(), style.Overlay{}, nil)
	if got, _ := v.selected(); got.Left != "save" {
		t.Fatalf("first highlight = %q, want save (the first item after its header)", got.Left)
	}
	tests := []struct {
		name string
		key  rune
		want string
	}{
		{"down moves to the next item", tea.KeyDown, "open"},
		{"down again crosses the global header", tea.KeyDown, "quit"},
		{"down at the end stays", tea.KeyDown, "quit"},
		{"up returns across the header", tea.KeyUp, "open"},
		{"up to the first item", tea.KeyUp, "save"},
		{"up at the top stays", tea.KeyUp, "save"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v.update(tea.KeyPressMsg{Code: tt.key})
			if got, _ := v.selected(); got.Left != tt.want {
				t.Fatalf("highlight = %q, want %q", got.Left, tt.want)
			}
		})
	}
}

func TestListEnterRunsTheHighlightedRow(t *testing.T) {
	var ran []string
	run := func(v *View, r Row) tea.Cmd {
		ran = append(ran, r.Left)
		v.active = false
		return nil
	}
	v := New("T", filterRows(), style.Overlay{}, run)
	v.update(tea.KeyPressMsg{Code: tea.KeyDown})
	v.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if strings.Join(ran, ",") != "open" {
		t.Fatalf("ran %v, want [open]", ran)
	}
	if v.active {
		t.Fatal("the run closed nothing; the view is still active")
	}
}

func TestListEnterWithoutRunDoesNothing(t *testing.T) {
	v := New("T", filterRows(), style.Overlay{}, nil)
	if cmd := v.update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("Enter with no run returned a command")
	}
	if !v.active {
		t.Fatal("Enter closed the view")
	}
}

func TestListClickChoosesItemButNotHeader(t *testing.T) {
	var ran []string
	run := func(_ *View, r Row) tea.Cmd {
		ran = append(ran, r.Left)
		return nil
	}
	v := New("T", filterRows(), style.Overlay{}, run)
	g := grid.New(80, 24)
	v.draw(g)
	// The list rows start two rows under the box top. The first row is the
	// normal header, then save, open, the global header and quit.
	top := v.Box.Y + 2
	v.update(tea.MouseClickMsg(tea.Mouse{X: v.Box.X + 3, Y: top, Button: tea.MouseLeft}))
	if len(ran) != 0 {
		t.Fatalf("a click on the header ran %v", ran)
	}
	v.update(tea.MouseClickMsg(tea.Mouse{X: v.Box.X + 3, Y: top + 2, Button: tea.MouseLeft}))
	if strings.Join(ran, ",") != "open" {
		t.Fatalf("a click on open ran %v", ran)
	}
}

func TestListDrawShowsTitleFilterAndRows(t *testing.T) {
	v := New("Keyboard Shortcuts", filterRows(), style.Overlay{}, nil)
	g := grid.New(60, 20)
	v.draw(g)
	rows := g.Plain()
	box, _ := box(60, 20)
	cells := []struct {
		name string
		y    int
		want string
	}{
		{"title", box.Y, "Keyboard Shortcuts"},
		{"filter line", box.Y + 1, "Filter:"},
		{"group header", box.Y + 2, "normal"},
		{"first item", box.Y + 3, "save"},
		{"item right text", box.Y + 3, "ctrl+s"},
		{"hint", box.Y + box.H - 1, "Esc close"},
	}
	for _, c := range cells {
		if !strings.Contains(rows[c.y], c.want) {
			t.Errorf("%s: row %d = %q, want it to contain %q", c.name, c.y, rows[c.y], c.want)
		}
	}
}

func TestListDrawHighlightsSelectedRow(t *testing.T) {
	selected := grid.Style{Bold: true}
	v := New("T", filterRows(), style.Overlay{Selected: selected}, nil)
	g := grid.New(60, 20)
	v.draw(g)
	box, _ := box(60, 20)
	saveY := box.Y + 3
	if !g.At(box.X+2, saveY).Style.Bold {
		t.Fatal("the highlighted row does not use the selected style")
	}
	if g.At(box.X+2, box.Y+4).Style.Bold {
		t.Fatal("a row that is not highlighted uses the selected style")
	}
}

func TestListScrollKeepsHighlightVisible(t *testing.T) {
	var rows []Row
	for i := range 40 {
		rows = append(rows, Row{Left: "item" + string(rune('a'+i%26)) + string(rune('a'+i/26))})
	}
	v := New("T", rows, style.Overlay{}, nil)
	g := grid.New(60, 12)
	v.draw(g)
	_, n := box(60, 12)
	for range 15 {
		v.update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	v.draw(g)
	if v.top == 0 {
		t.Fatalf("the window did not scroll to show row 15 (rows on screen = %d)", n)
	}
	if v.sel < v.top || v.sel >= v.top+n {
		t.Fatalf("highlight %d is outside the window [%d, %d)", v.sel, v.top, v.top+n)
	}
}

func TestListCursorFollowsFilterText(t *testing.T) {
	v := New("T", filterRows(), style.Overlay{}, nil)
	g := grid.New(60, 20)
	v.draw(g)
	box, _ := box(60, 20)
	x, y, ok := v.cursor(60, 20)
	if !ok || y != box.Y+1 || x != box.X+2+len("Filter: ") {
		t.Fatalf("empty filter cursor = (%d,%d,%v), want just after the label", x, y, ok)
	}
	v.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	v.update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	v.draw(g)
	x2, _, _ := v.cursor(60, 20)
	if x2 != x+2 {
		t.Fatalf("cursor after two letters = %d, want %d", x2, x+2)
	}
}

func TestListCtrlKeysDoNotTypeIntoFilter(t *testing.T) {
	v := New("T", filterRows(), style.Overlay{}, nil)
	v.update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if len(v.filter) != 0 {
		t.Fatalf("ctrl+a typed %q into the filter", string(v.filter))
	}
}

func TestListBoxFitsTheFrame(t *testing.T) {
	tests := []struct {
		name   string
		w, h   int
		wantOK bool
	}{
		{"normal terminal", 80, 24, true},
		{"too narrow", 10, 24, false},
		{"too short", 80, 4, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, rows := box(tt.w, tt.h)
			if (rows > 0) != tt.wantOK {
				t.Fatalf("rows = %d, want drawn = %v", rows, tt.wantOK)
			}
			if tt.wantOK && (r.X < 0 || r.Y < 0 || r.X+r.W > tt.w || r.Y+r.H > tt.h) {
				t.Fatalf("box %+v leaves the %dx%d frame", r, tt.w, tt.h)
			}
		})
	}
}

func TestListLongFilterShowsItsEnd(t *testing.T) {
	v := New("T", filterRows(), style.Overlay{}, nil)
	g := grid.New(60, 20)
	v.draw(g)
	for _, r := range strings.Repeat("x", 80) + "END" {
		v.update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	v.draw(g)
	box, _ := box(60, 20)
	if !strings.Contains(g.Plain()[box.Y+1], "END") {
		t.Fatalf("filter row = %q, want the typed end of the filter", g.Plain()[box.Y+1])
	}
	x, y, ok := v.cursor(60, 20)
	if !ok || y != box.Y+1 || x >= box.X+box.W-1 {
		t.Fatalf("cursor = (%d,%d,%v), want a caret inside the filter row", x, y, ok)
	}
}

func TestListDrawSurvivesANarrowFrame(t *testing.T) {
	rows := []Row{{Left: "a-very-long-action-name", Right: "ctrl+alt+shift+f12  file_explorer"}}
	v := New("T", rows, style.Overlay{}, nil)
	for _, w := range []int{4, 12, 16, 20} {
		g := grid.New(w, 12)
		v.draw(g) // must not panic
		x, _, ok := v.cursor(w, 12)
		if !ok {
			continue
		}
		box, _ := box(w, 12)
		if x < box.X || x >= box.X+box.W {
			t.Errorf("width %d: caret column %d is outside the box %+v", w, x, box)
		}
	}
}

func TestListLongValueKeepsItsLabelInsideTheBox(t *testing.T) {
	t.Parallel()
	long := "/private/var/folders/tmp/" + strings.Repeat("segment/", 10) + "config.json"
	rows := []Row{{Left: "config_file", Right: long}}
	v := New("Settings", rows, style.Overlay{}, nil)
	g := grid.New(100, 20)
	v.draw(g)
	box := v.Box
	y := box.Y + 2
	text := g.Lines()[y]
	if !strings.Contains(text, "config_file") {
		t.Fatalf("the label is hidden by a long value: row %d = %q", y, text)
	}
	if !strings.Contains(text, "config.json") {
		t.Fatalf("the end of the value is not shown: row %d = %q", y, text)
	}
	for x := range box.X {
		if c := g.At(x, y); c.Text != "" && c.Text != " " {
			t.Fatalf("the value spills out of the box at column %d: %q", x, c.Text)
		}
	}
}

func TestLongListsKeepTheirMaximumHeight(t *testing.T) {
	box, rows := boxFor(100, 60, 500)
	if box.H != MaxHeight || rows != MaxHeight-3 {
		t.Errorf("box %d rows %d, want %d and %d", box.H, rows, MaxHeight, MaxHeight-3)
	}
}

// box returns the box of a list in a w by h frame, and the number of item rows it holds.
func box(w, h int) (space.Rect, int) {
	return boxFor(w, h, MaxHeight)
}

// boxFor is box for a list of n rows: a box that is not taller than its rows
// need, so two rows do not sit in a box of 28.
func boxFor(w, h, n int) (space.Rect, int) { return boxSized(w, h, n, 0) }
