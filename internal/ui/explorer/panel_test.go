package explorer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	tree "github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/icons"
	"github.com/tbxark/gvim/internal/ui/style"
)

// fixture writes a project directory: a/ with x.txt, b.txt, c.txt and a hidden
// entry. It returns the project path.
func fixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "proj")
	must(t, os.MkdirAll(filepath.Join(dir, "a"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "a", "x.txt"), nil, 0o644))
	for _, name := range []string{"b.txt", "c.txt", ".hidden"} {
		must(t, os.WriteFile(filepath.Join(dir, name), nil, 0o644))
	}
	return dir
}

// manyFiles writes a project with n files, so the rows overflow a short body.
func manyFiles(t *testing.T, n int) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "many")
	must(t, os.MkdirAll(dir, 0o755))
	for i := range n {
		must(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("longname%02d", i)), nil, 0o644))
	}
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func openTree(t *testing.T, dir string) *tree.Tree {
	t.Helper()
	tr, err := tree.Open(dir)
	must(t, err)
	return tr
}

// clock is a fake time source that tests advance by hand.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

// rowIndex returns the row of the tree whose name is name.
func rowIndex(t *testing.T, tr *tree.Tree, name string) int {
	t.Helper()
	for i, r := range tr.Rows() {
		if r.Name == name {
			return i
		}
	}
	t.Fatalf("no row named %q", name)
	return -1
}

func run(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestHeaderFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		width           int
		text            string
		reload, closeAt int
	}{
		{5, "┌ Fil", -1, -1},
		{12, "┌ File Explo", -1, -1},
		{16, "┌ File Explorer ", -1, -1},
		{17, "┌ File Explorer ⟳", 16, -1},
		{18, "┌ File Explorer ⟳┐", 16, -1},
		{19, "┌ File Explorer ⟳─×", 16, 18},
		{20, "┌ File Explorer ⟳─×┐", 16, 18},
		{21, "┌ File Explorer ⟳─×─┐", 16, 18},
		{30, "┌ File Explorer " + strings.Repeat("─", 9) + "⟳─×─┐", 25, 27},
	}
	for _, tt := range tests {
		h := HeaderFor(tt.width, icons.Plain)
		if h.Text != tt.text {
			t.Errorf("width %d: text %q, want %q", tt.width, h.Text, tt.text)
		}
		if got := ansi.StringWidth(h.Text); got != tt.width {
			t.Errorf("width %d: text is %d cells", tt.width, got)
		}
		if h.Reload != tt.reload || h.Close != tt.closeAt {
			t.Errorf("width %d: buttons at %d and %d, want %d and %d", tt.width, h.Reload, h.Close, tt.reload, tt.closeAt)
		}
	}
}

func TestHeaderForNerdKeepsTheColumns(t *testing.T) {
	t.Parallel()
	refresh, closeBtn := icons.Nerd.Glyph(icons.Refresh), icons.Nerd.Glyph(icons.Close)
	for width := 0; width <= 60; width++ {
		plain, nerd := HeaderFor(width, icons.Plain), HeaderFor(width, icons.Nerd)
		if nerd.Reload != plain.Reload || nerd.Close != plain.Close {
			t.Errorf("width %d: nerd buttons at %d and %d, plain at %d and %d",
				width, nerd.Reload, nerd.Close, plain.Reload, plain.Close)
		}
		if got := ansi.StringWidth(nerd.Text); got != width {
			t.Errorf("width %d: nerd text is %d cells", width, got)
		}
		// The columns hold the glyphs, wherever they are.
		if nerd.Reload >= 0 && ansi.Cut(nerd.Text, nerd.Reload, nerd.Reload+1) != refresh {
			t.Errorf("width %d: column %d is not the refresh glyph in %q", width, nerd.Reload, nerd.Text)
		}
		if nerd.Close >= 0 && ansi.Cut(nerd.Text, nerd.Close, nerd.Close+1) != closeBtn {
			t.Errorf("width %d: column %d is not the close glyph in %q", width, nerd.Close, nerd.Text)
		}
	}
}

func TestDrawRows(t *testing.T) {
	t.Parallel()
	tr := openTree(t, fixture(t))
	p := New(tr, style.Styles{}, nil)
	g := grid.New(14, 8)
	p.Draw(g, Geometry{Width: 14, Bottom: 7, Focused: true})
	lines := g.Plain()

	if lines[1] != HeaderFor(14, icons.Plain).Text {
		t.Errorf("title row %q, want %q", lines[1], HeaderFor(14, icons.Plain).Text)
	}
	// Column 1 is the padding cell, and the marker lives there beside the expand icon.
	want := []string{
		"│▌▼ proj     │", // the root is selected, so the marker shows
		"│   ▶ a      │",
		"│     b.txt  │",
		"│     c.txt  │",
		"│            │", // the body is blank below the last row
	}
	for i, w := range want {
		if got := lines[2+i]; got != w {
			t.Errorf("row %d = %q, want %q", 2+i, got, w)
		}
	}
	if got, w := lines[7], "└────────────┘"; got != w {
		t.Errorf("bottom border %q, want %q", got, w)
	}
}

func TestDrawNoMarkerWhenUnfocused(t *testing.T) {
	t.Parallel()
	tr := openTree(t, fixture(t))
	g := grid.New(12, 8)
	New(tr, style.Styles{}, nil).Draw(g, Geometry{Width: 12, Bottom: 7})
	if got := g.Plain()[2]; strings.Contains(got, "▌") {
		t.Errorf("unfocused panel drew the marker: %q", got)
	}
}

func TestDrawScrollbar(t *testing.T) {
	t.Parallel()
	tr := openTree(t, manyFiles(t, 10))
	styles := style.Styles{
		Scrollbar:      lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		ScrollbarThumb: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	}
	g := grid.New(12, 6)
	New(tr, styles, nil).Draw(g, Geometry{Width: 12, Bottom: 6, Focused: true})

	// Four body rows for eleven tree rows: the thumb is one row, at the top.
	track := style.Cell(styles.Scrollbar).FG
	thumb := style.Cell(styles.ScrollbarThumb).FG
	if got := g.At(10, 2).Style.BG; got != thumb {
		t.Errorf("top scrollbar cell BG %v, want thumb %v", got, thumb)
	}
	for y := 3; y <= 5; y++ {
		if got := g.At(10, y).Style.BG; got != track {
			t.Errorf("row %d scrollbar cell BG %v, want track %v", y, got, track)
		}
	}
	// The bar takes the column left of the border, and names stop a padding cell
	// before it and end with an ellipsis.
	if got := g.Plain()[3]; got != "│     lo…  │" {
		t.Errorf("row 3 %q, want the name cut before the scrollbar", got)
	}
	if got := g.At(11, 3).Text; got != "│" {
		t.Errorf("border cell %q, want the right border", got)
	}
}

func TestPressFileSingleThenDouble(t *testing.T) {
	t.Parallel()
	dir := fixture(t)
	tr := openTree(t, dir)
	clk := &clock{now: time.Unix(1000, 0)}
	p := New(tr, style.Styles{}, clk.Now)
	g := Geometry{Width: 12, Bottom: 7, Focused: true}
	bPath := filepath.Join(dir, "b.txt")

	var cmd tea.Cmd
	p, cmd = p.Update(click(5, 4), g) // b.txt is body row 2
	if got, want := run(cmd), (OpenFileMsg{Path: bPath, Preview: true}); got != want {
		t.Errorf("single click sent %+v, want %+v", got, want)
	}
	clk.now = clk.now.Add(100 * time.Millisecond)
	p, cmd = p.Update(click(5, 4), g)
	if got, want := run(cmd), (OpenFileMsg{Path: bPath}); got != want {
		t.Errorf("double click sent %+v, want %+v", got, want)
	}
	clk.now = clk.now.Add(100 * time.Millisecond)
	_, cmd = p.Update(click(5, 4), g)
	if got := run(cmd); got != (FocusMsg{}) {
		t.Errorf("triple press sent %+v, want FocusMsg", got)
	}
	if sel, _ := tr.Selected(); sel.Path != bPath {
		t.Errorf("selection %q, want b.txt", sel.Path)
	}
}

func TestPressDirectoryTogglesOnce(t *testing.T) {
	t.Parallel()
	tr := openTree(t, fixture(t))
	clk := &clock{now: time.Unix(1000, 0)}
	p := New(tr, style.Styles{}, clk.Now)
	g := Geometry{Width: 12, Bottom: 7}

	var cmd tea.Cmd
	p, cmd = p.Update(click(5, 3), g) // a/ at row 3
	if got := run(cmd); got != (FocusMsg{}) {
		t.Errorf("directory press sent %+v, want FocusMsg", got)
	}
	if !expanded(tr, "a") {
		t.Fatal("single press did not expand a/")
	}
	clk.now = clk.now.Add(100 * time.Millisecond)
	p, _ = p.Update(click(5, 3), g) // second press of a double click
	if !expanded(tr, "a") {
		t.Error("double press toggled the directory again")
	}
	clk.now = clk.now.Add(time.Second)
	_, _ = p.Update(click(5, 3), g)
	if expanded(tr, "a") {
		t.Error("a later single press did not collapse a/")
	}
}

func expanded(tr *tree.Tree, name string) bool {
	for _, r := range tr.Rows() {
		if r.Name == name {
			return r.Expanded
		}
	}
	return false
}

func TestPressTitleButtonsAndBody(t *testing.T) {
	t.Parallel()
	tr := openTree(t, fixture(t))
	p := New(tr, style.Styles{}, nil)
	g := Geometry{Width: 30, Bottom: 8}
	tests := []struct {
		name string
		x, y int
		want tea.Msg
	}{
		{"reload", 25, 1, ReloadMsg{}},
		{"close", 27, 1, CloseMsg{}},
		{"title text", 3, 1, FocusMsg{}},
		{"left border", 0, 4, FocusMsg{}},
		{"bottom border", 5, 8, FocusMsg{}},
	}
	for _, tt := range tests {
		_, cmd := p.Update(click(tt.x, tt.y), g)
		if got := run(cmd); got != tt.want {
			t.Errorf("%s: sent %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestPressTitleButtonsInNerdMode(t *testing.T) {
	t.Parallel()
	tr := openTree(t, fixture(t))
	p := New(tr, style.Styles{}, nil)
	g := Geometry{Width: 30, Bottom: 8, Icons: icons.Nerd}
	gr := grid.New(30, 9)
	p.Draw(gr, g)
	for _, tt := range []struct {
		name  string
		x     int
		glyph icons.Icon
		want  tea.Msg
	}{
		{"reload", 25, icons.Refresh, ReloadMsg{}},
		{"close", 27, icons.Close, CloseMsg{}},
	} {
		if got := gr.At(tt.x, 1).Text; got != icons.Nerd.Glyph(tt.glyph) {
			t.Errorf("%s: cell %d holds %q, want the nerd glyph", tt.name, tt.x, got)
		}
		_, cmd := p.Update(click(tt.x, 1), g)
		if got := run(cmd); got != tt.want {
			t.Errorf("%s: sent %+v, want %+v", tt.name, got, tt.want)
		}
		// The cells beside a button do not press it.
		for _, x := range []int{tt.x - 1, tt.x + 1} {
			_, cmd := p.Update(click(x, 1), g)
			if got := run(cmd); got != (FocusMsg{}) {
				t.Errorf("%s: column %d sent %+v, want FocusMsg", tt.name, x, got)
			}
		}
	}
}

func TestPressEmptyBodyFocuses(t *testing.T) {
	t.Parallel()
	tr := openTree(t, fixture(t)) // four rows, so row 2+4 is empty in a taller body
	g := Geometry{Width: 12, Bottom: 9}
	_, cmd := New(tr, style.Styles{}, nil).Update(click(5, 7), g)
	if got := run(cmd); got != (FocusMsg{}) {
		t.Errorf("empty body sent %+v, want FocusMsg", got)
	}
}

func TestGripDragResizes(t *testing.T) {
	t.Parallel()
	tr := openTree(t, fixture(t))
	p := New(tr, style.Styles{}, nil)
	g := Geometry{Width: 29, Bottom: 8}

	p, cmd := p.Update(click(28, 4), g) // the grip is the right border
	if cmd != nil {
		t.Errorf("grip press sent %+v, want nothing", run(cmd))
	}
	motion := tea.MouseMotionMsg(tea.Mouse{X: 37, Y: 4, Button: tea.MouseLeft})
	p, cmd = p.Update(motion, g)
	if got := run(cmd); got != (ResizeMsg{Width: 38}) {
		t.Errorf("drag sent %+v, want width 38", got)
	}
	p, _ = p.Update(tea.MouseReleaseMsg(tea.Mouse{X: 37, Y: 4, Button: tea.MouseLeft}), g)
	if _, cmd := p.Update(motion, g); cmd != nil {
		t.Errorf("motion after release sent %+v", run(cmd))
	}
}

func TestWheelScrollsTree(t *testing.T) {
	t.Parallel()
	tr := openTree(t, manyFiles(t, 10))
	p := New(tr, style.Styles{}, nil)
	g := Geometry{Width: 12, Bottom: 6}

	wheel := func(b tea.MouseButton, x, y int) {
		_, _ = p.Update(tea.MouseWheelMsg(tea.Mouse{X: x, Y: y, Button: b}), g)
	}
	wheel(tea.MouseWheelDown, 5, 3)
	if got := tr.ScrollOffset(); got != 3 {
		t.Errorf("after one notch down offset %d, want 3", got)
	}
	wheel(tea.MouseWheelUp, 5, 3)
	wheel(tea.MouseWheelUp, 5, 3)
	if got := tr.ScrollOffset(); got != 0 {
		t.Errorf("after two notches up offset %d, want 0", got)
	}
	wheel(tea.MouseWheelDown, 20, 3) // past the panel's right edge: ignored
	if got := tr.ScrollOffset(); got != 0 {
		t.Errorf("wheel outside the panel moved the tree to %d", got)
	}
}

func TestScrollbarTrackPressCentersThumb(t *testing.T) {
	t.Parallel()
	tr := openTree(t, manyFiles(t, 10))
	p := New(tr, style.Styles{}, nil)
	g := Geometry{Width: 12, Bottom: 6}
	// Body row 3 of four, for eleven rows: the offset centers the thumb there.
	_, _ = p.Update(click(10, 5), g)
	if got, want := tr.ScrollOffset(), 6; got != want {
		t.Errorf("track press gave offset %d, want %d", got, want)
	}
}

func TestActions(t *testing.T) {
	t.Parallel()
	dir := fixture(t)
	tr := openTree(t, dir)
	p := New(tr, style.Styles{}, nil)
	g := Geometry{Width: 12, Bottom: 7, Focused: true}

	p, _, ok := p.Action("file_explorer_down", g)
	if !ok {
		t.Fatal("down not handled")
	}
	if sel, _ := tr.Selected(); sel.Name != "a" {
		t.Errorf("after down selected %q, want a", sel.Name)
	}

	p, cmd, ok := p.Action("file_explorer_expand", g)
	if !ok || cmd != nil || !expanded(tr, "a") {
		t.Errorf("expand on a/: ok=%v cmd=%v expanded=%v", ok, cmd, expanded(tr, "a"))
	}
	p, _, _ = p.Action("file_explorer_down", g) // x.txt
	if sel, _ := tr.Selected(); sel.Name != "x.txt" {
		t.Fatalf("after down selected %q, want x.txt", sel.Name)
	}
	p, _, _ = p.Action("file_explorer_collapse", g) // a file moves to its parent
	if sel, _ := tr.Selected(); sel.Name != "a" {
		t.Errorf("collapse on x.txt selected %q, want a", sel.Name)
	}

	p, _, _ = p.Action("file_explorer_collapse", g) // a/ is open: it closes
	if expanded(tr, "a") {
		t.Error("collapse on an open directory left it open")
	}

	_, cmd, _ = p.Action("file_explorer_refresh", g)
	if got := run(cmd); got != (ReloadMsg{}) {
		t.Errorf("refresh sent %+v", got)
	}

	if _, _, ok := p.Action("file_explorer_nonsense", g); ok {
		t.Error("unknown action reported handled")
	}
}

func TestActionOpenFile(t *testing.T) {
	t.Parallel()
	dir := fixture(t)
	tr := openTree(t, dir)
	p := New(tr, style.Styles{}, nil)
	g := Geometry{Width: 12, Bottom: 7, Focused: true}
	must(t, tr.Select(filepath.Join(dir, "c.txt")))
	_, cmd, ok := p.Action("file_explorer_open", g)
	if !ok {
		t.Fatal("open not handled")
	}
	if got := run(cmd); got != (OpenFileMsg{Path: filepath.Join(dir, "c.txt")}) {
		t.Errorf("open sent %+v", got)
	}
}

func TestActionPagesAndNilTree(t *testing.T) {
	t.Parallel()
	tr := openTree(t, manyFiles(t, 10))
	p := New(tr, style.Styles{}, nil)
	g := Geometry{Width: 12, Bottom: 6}
	p, _, _ = p.Action("file_explorer_page_down", g)
	if sel, _ := tr.Selected(); sel.Path == tr.Root() {
		t.Errorf("page down left the selection on the root")
	}
	_, _, _ = p.Action("file_explorer_page_up", g)
	if sel, _ := tr.Selected(); sel.Depth != 0 {
		t.Errorf("page up did not return to the root, selected %q", sel.Name)
	}

	var zero Panel
	if _, _, ok := zero.Action("file_explorer_down", g); ok {
		t.Error("zero panel handled an action")
	}
	if _, cmd := zero.Update(click(1, 2), g); cmd != nil {
		t.Error("zero panel sent a message")
	}
	zero.Draw(grid.New(12, 6), g) // must not panic
}

func TestFollowKeepsSelectionVisible(t *testing.T) {
	t.Parallel()
	tr := openTree(t, manyFiles(t, 10))
	p := New(tr, style.Styles{}, nil)
	g := Geometry{Width: 12, Bottom: 4} // two body rows
	for range 5 {
		p, _, _ = p.Action("file_explorer_down", g)
	}
	off := tr.ScrollOffset()
	sel, _ := tr.Selected()
	idx := rowIndex(t, tr, sel.Name)
	if idx < off || idx >= off+2 {
		t.Errorf("selected row %d outside the window [%d, %d)", idx, off, off+2)
	}
}

// failingTree makes every toggle fail, to test the error message.
type failingTree struct{ Tree }

func (failingTree) Toggle(string) error { return errors.New("boom") }

func TestToggleErrorIsReported(t *testing.T) {
	t.Parallel()
	tr := openTree(t, fixture(t))
	p := New(failingTree{tr}, style.Styles{}, nil)
	g := Geometry{Width: 12, Bottom: 7}
	_, cmd := p.Update(click(5, 3), g) // a/ toggle fails
	if got, ok := run(cmd).(ErrorMsg); !ok || got.Err == nil {
		t.Errorf("failed toggle sent %+v, want ErrorMsg", run(cmd))
	}
}

func TestFileToggleErrorViaAction(t *testing.T) {
	t.Parallel()
	tr := openTree(t, fixture(t))
	p := New(failingTree{tr}, style.Styles{}, nil)
	g := Geometry{Width: 12, Bottom: 7}
	must(t, tr.Select(filepath.Join(tr.Root(), "a")))
	_, cmd, ok := p.Action("file_explorer_open", g)
	if !ok {
		t.Fatal("open on a directory not handled")
	}
	if _, isErr := run(cmd).(ErrorMsg); !isErr {
		t.Errorf("open on a directory with failing toggle sent %+v", run(cmd))
	}
}

func TestDrawSkipsTinyPanel(t *testing.T) {
	t.Parallel()
	tr := openTree(t, fixture(t))
	g := grid.New(4, 4)
	New(tr, style.Styles{}, nil).Draw(g, Geometry{Width: 1, Bottom: 3})
	if g.Plain()[0] != "    " {
		t.Errorf("a one-column panel painted the grid: %q", g.Plain()[0])
	}
}

func TestThumbOf(t *testing.T) {
	t.Parallel()
	start, length := thumbOf(11, 4, 0)
	if start != 0 || length != 1 {
		t.Errorf("thumbOf(11,4,0) = %d,%d", start, length)
	}
	start, length = thumbOf(4, 4, 0)
	if start != 0 || length != 4 {
		t.Errorf("thumbOf(4,4,0) = %d,%d, want the whole body", start, length)
	}
	start, _ = thumbOf(11, 4, 10)
	if start > 3 {
		t.Errorf("thumb start %d past the body", start)
	}
}

func TestDrawFolderGlyphsCollapsedAndExpanded(t *testing.T) {
	t.Parallel()
	dir := fixture(t)
	tr := openTree(t, dir)
	p := New(tr, style.Styles{}, nil)
	g := grid.New(12, 8)
	p.Draw(g, Geometry{Width: 12, Bottom: 7})
	lines := g.Plain()
	if !strings.Contains(lines[3], "▶ a") {
		t.Fatalf("collapsed folder row %q, want ▶ glyph", lines[3])
	}
	must(t, tr.Toggle(filepath.Join(dir, "a")))
	g = grid.New(12, 8)
	p.Draw(g, Geometry{Width: 12, Bottom: 7})
	if lines := g.Plain(); !strings.Contains(lines[3], "▼ a") || strings.Contains(lines[3], "▶") {
		t.Fatalf("expanded folder row %q, want ▼ glyph", lines[3])
	}
}

func TestDrawFolderGlyphsInNerdMode(t *testing.T) {
	t.Parallel()
	dir := fixture(t)
	tr := openTree(t, dir)
	p := New(tr, style.Styles{}, nil)
	draw := func() *grid.Grid {
		g := grid.New(14, 8)
		p.Draw(g, Geometry{Width: 14, Bottom: 7, Icons: icons.Nerd})
		return g
	}
	if got := draw().At(4, 3).Text; got != icons.Nerd.Glyph(icons.DirCollapsed) {
		t.Fatalf("collapsed folder icon %q, want the nerd glyph", got)
	}
	must(t, tr.Toggle(filepath.Join(dir, "a")))
	if got := draw().At(4, 3).Text; got != icons.Nerd.Glyph(icons.DirExpanded) {
		t.Fatalf("expanded folder icon %q, want the nerd glyph", got)
	}
}

func TestDrawFileIconsInNerdMode(t *testing.T) {
	t.Parallel()
	dir := fixture(t)
	tr := openTree(t, dir)
	p := New(tr, style.Styles{}, nil)
	g := grid.New(24, 8)
	p.Draw(g, Geometry{Width: 24, Bottom: 7, Icons: icons.Nerd})
	at := func(x, y int) string { return g.At(x, y).Text }
	// Row 3 is the folder a/ and row 4 is b.txt, both one level down: the expand
	// mark at column 4, the type icon at 6 and the name from 8.
	if at(4, 3) != icons.Nerd.Glyph(icons.DirCollapsed) || at(6, 3) != icons.Nerd.Glyph(icons.Folder) || at(8, 3) != "a" {
		t.Errorf("folder row %q", g.Plain()[3])
	}
	if at(4, 4) != " " || at(6, 4) != icons.Nerd.FileIcon("b.txt", false, false) || at(8, 4) != "b" {
		t.Errorf("file row %q", g.Plain()[4])
	}
	// The plain set keeps the name at column 6 and draws no type icon.
	plain := grid.New(24, 8)
	p.Draw(plain, Geometry{Width: 24, Bottom: 7})
	if got := plain.At(6, 4).Text; got != "b" {
		t.Errorf("plain file row %q, want the name at column 6", plain.Plain()[4])
	}
}

func TestDrawGitMarksInNerdMode(t *testing.T) {
	t.Parallel()
	dir := fixture(t)
	tr := openTree(t, dir)
	p := New(tr, style.Styles{}, nil)
	marks := map[string]rune{filepath.Join(dir, "b.txt"): 'M'}
	for _, tt := range []struct {
		mode icons.Mode
		want string
	}{{icons.Plain, "M"}, {icons.Nerd, icons.Nerd.GitMark('M')}} {
		g := grid.New(24, 8)
		p.Draw(g, Geometry{Width: 24, Bottom: 7, Marks: marks, Icons: tt.mode})
		if got := g.At(21, 4).Text; got != tt.want {
			t.Errorf("%v: git mark cell %q, want %q (%q)", tt.mode, got, tt.want, g.Plain()[4])
		}
	}
}

// TestDrawGitMarkOnTheRow checks that a row whose absolute path has a git status
// letter shows it in the last column before the padding cell, and that the name keeps
// the cells before the letter. A row without a mark shows a blank there.
func TestDrawGitMarkOnTheRow(t *testing.T) {
	t.Parallel()
	dir := fixture(t)
	tr := openTree(t, dir)
	marks := map[string]rune{filepath.Join(dir, "b.txt"): 'M'}
	g := grid.New(16, 8)
	New(tr, style.Styles{}, nil).Draw(g, Geometry{Width: 16, Bottom: 7, Marks: marks})

	if got := g.At(13, 4).Text; got != "M" {
		t.Errorf("mark cell of b.txt = %q, want %q", got, "M")
	}
	if got := g.Plain()[4]; !strings.Contains(got, "b.txt") {
		t.Errorf("row of b.txt = %q, want the name shown", got)
	}
	if got := g.At(13, 5).Text; got != " " {
		t.Errorf("unmarked row c.txt has %q in the mark column, want blank", got)
	}
	if got := g.At(15, 4).Text; got != "│" {
		t.Errorf("border after the mark = %q, want the border", got)
	}
}

func TestDrawMarkerIsTheAccent(t *testing.T) {
	t.Parallel()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	st := style.New(th)
	tr := openTree(t, fixture(t))
	g := grid.New(12, 8)
	New(tr, st, nil).Draw(g, Geometry{Width: 12, Bottom: 7, Focused: true})
	// The root row is selected, so its marker is the first cell after the border.
	want := style.Cell(st.Accent).BG
	if c := g.At(1, 2); c.Text != "▌" || c.Style.FG != want {
		t.Fatalf("marker cell %q has fg %v, want the accent %v", c.Text, c.Style.FG, want)
	}
}
