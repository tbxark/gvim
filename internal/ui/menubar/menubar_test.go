package menubar

import (
	"errors"
	"image/color"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// The accent colors the menu tests pin, from the dark theme: the accent of the
// selected row and the text on it.
var (
	colorPrimary   = color.RGBA{0x2F, 0x81, 0xF7, 0xFF}
	colorOnPrimary = color.RGBA{0xEF, 0xF8, 0xFF, 0xFF}
)

// fakePorts implements Ports. Run records the actions it is given and fails
// with runErr when set. Holds reports true only for the keys in on.
type fakePorts struct {
	on     map[string]bool
	ran    []string
	runErr error
}

func (f *fakePorts) Run(action string) error {
	f.ran = append(f.ran, action)
	return f.runErr
}

func (f *fakePorts) Holds(name string) bool { return f.on[name] }

// allOn returns ports where every context key holds, so no row is disabled.
func allOn() *fakePorts {
	return &fakePorts{on: map[string]bool{
		"has_buffer": true, "has_text_buffer": true, "can_edit": true, "can_paste": true,
		"can_save": true, "can_save_all": true, "can_revert": true, "has_selection": true,
		"can_cut": true, "can_copy": true, "file_explorer": true, "file_explorer_focused": true,
		"file_explorer_show_hidden": true, "file_explorer_show_gitignored": true,
		"line_numbers": true, "vertical_scrollbar": true, "horizontal_scrollbar": true,
		"mouse_capture": true, "keymap_default": true, "session_mode": true,
		"formatter_available": true,
	}}
}

// newBar returns a closed menu bar on a w by h frame, with ports p.
func newBar(p Ports, w, h int) Model {
	return New(p, darkStyles).SetSize(w, h)
}

// darkStyles are the chrome styles of the dark theme, which the menu tests draw
// with, so the dropdown has its accent.
var darkStyles = mustDarkStyles()

func mustDarkStyles() style.Styles {
	th, err := theme.Load("dark")
	if err != nil {
		panic(err)
	}
	return style.New(th)
}

// press returns a left press at (x, y).
func press(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

// keyMsg returns a key press for a named key such as "down" or "esc".
func keyMsg(name string) tea.KeyPressMsg {
	switch name {
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "left":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft})
	case "right":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyRight})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	}
	return tea.KeyPressMsg(tea.Key{Text: name, Code: []rune(name)[0]})
}

// send runs one message through Update and returns the next model and command.
func send(m Model, msg tea.Msg) (Model, tea.Cmd) {
	return m.Update(msg)
}

// paintRows paints m on a fresh grid of the model's size and returns its rows.
func paintRows(m Model) []string {
	g := grid.New(m.w, m.h)
	m.Paint(g)
	return g.Plain()
}

func TestLabelsListTheBar(t *testing.T) {
	closed := Labels(false)
	want := []string{"File", "Edit", "View", "Selection", "Go", "LSP", "Help"}
	if !slices.Equal(closed, want) {
		t.Fatalf("Labels(false) = %v, want %v", closed, want)
	}
	focused := Labels(true)
	if len(focused) != 8 || focused[6] != "Explorer" || focused[7] != "Help" {
		t.Fatalf("Labels(true) = %v, want Explorer before Help", focused)
	}
}

func TestBarRowFixture(t *testing.T) {
	m := newBar(allOn(), 60, 10)
	rows := paintRows(m)
	const want = " File   Edit   View   Selection   Go   LSP   Help"
	if got := strings.TrimRight(rows[0], " "); got != want {
		t.Fatalf("bar row = %q, want %q", got, want)
	}
	focused := paintRows(newBar(allOn(), 80, 10).WithExplorerFocus(true))
	if !strings.Contains(focused[0], "LSP   Explorer   Help") {
		t.Fatalf("explorer focus bar row = %q", focused[0])
	}
}

func TestBarRowCutAtNarrowWidth(t *testing.T) {
	rows := paintRows(newBar(allOn(), 10, 4))
	if got := strings.TrimRight(rows[0], " "); got != " File   Ed" {
		t.Fatalf("narrow bar row = %q", got)
	}
}

func TestActivateOpensFileOnFirstRow(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	if !m.IsOpen() || m.Title() != "File" {
		t.Fatalf("Activate: open=%v title=%q", m.IsOpen(), m.Title())
	}
	if m.Selected() != 0 || m.Rows()[0].Label != "New File" {
		t.Fatalf("selected %d (%q), want New File", m.Selected(), m.Rows()[m.Selected()].Label)
	}
}

func TestDropdownFixture(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	rows := paintRows(m)
	if !strings.HasPrefix(rows[1], "╭") {
		t.Fatalf("top border row = %q", rows[1])
	}
	if !strings.HasPrefix(rows[2], "│ New File") || !strings.Contains(rows[2], "⌃N") {
		t.Fatalf("first row = %q", rows[2])
	}
	if !strings.HasPrefix(rows[4], "│") || strings.Contains(rows[4], "New File") {
		t.Fatalf("row 4 should be the separator after Open File: %q", rows[4])
	}
	if !strings.Contains(rows[4], "──") {
		t.Fatalf("separator row has no rule: %q", rows[4])
	}
}

func TestDropdownSelectedRowIsPrimary(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	g := grid.New(60, 30)
	m.Paint(g)
	if c := g.At(3, 2).Style; c.BG != colorPrimary || c.FG != colorOnPrimary {
		t.Fatalf("highlighted first row style = %+v, want primary bg and onPrimary fg", c)
	}
	if c := g.At(3, 3).Style; c.BG == colorPrimary {
		t.Fatal("row below the highlight has the primary background")
	}
}

func TestDisabledRowsAreDimmedAndSkipped(t *testing.T) {
	p := &fakePorts{on: map[string]bool{}} // no context key holds
	m := newBar(p, 60, 30).Activate()
	// "Save" needs can_save, so it is disabled and the keyboard skips it.
	var save int
	for i, r := range m.Rows() {
		if r.Label == "Save" {
			save = i
			if !r.Disabled {
				t.Fatal("Save is enabled without can_save")
			}
		}
	}
	g := grid.New(60, 30)
	m.Paint(g)
	if !g.At(3, 2+save).Style.Faint {
		t.Fatal("disabled row is not faint")
	}
	for range 3 {
		m, _ = send(m, keyMsg("down"))
	}
	if m.Selected() == save {
		t.Fatal("keyboard highlighted a disabled row")
	}
}

func TestKeyboardNavigationSkipsSeparators(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	// Open File has: New File, Open File..., separator, Save ...
	m, _ = send(m, keyMsg("down"))
	if m.Selected() != 1 {
		t.Fatalf("Down selected %d, want 1", m.Selected())
	}
	m, _ = send(m, keyMsg("down"))
	if m.Selected() != 3 {
		t.Fatalf("Down over the separator selected %d, want 3", m.Selected())
	}
	m, _ = send(m, keyMsg("up"))
	m, _ = send(m, keyMsg("up"))
	m, _ = send(m, keyMsg("up"))
	if m.Selected() != 0 {
		t.Fatalf("Up at the top selected %d, want 0", m.Selected())
	}
}

func TestLeftRightWrapAroundTheBar(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	m, _ = send(m, keyMsg("left"))
	if m.Title() != "Help" {
		t.Fatalf("Left from File opened %q, want Help (wrap)", m.Title())
	}
	m, _ = send(m, keyMsg("right"))
	if m.Title() != "File" {
		t.Fatalf("Right from Help opened %q, want File (wrap)", m.Title())
	}
	m, _ = send(m, keyMsg("right"))
	if m.Title() != "Edit" || m.Selected() < 0 {
		t.Fatalf("Right opened %q with selection %d", m.Title(), m.Selected())
	}
}

func TestEscapeClosesAndKeysFallThrough(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	if !m.Consumes(keyMsg("esc")) {
		t.Fatal("Esc is not consumed by an open dropdown")
	}
	m, _ = send(m, keyMsg("esc"))
	if m.IsOpen() {
		t.Fatal("Esc left the dropdown open")
	}
	m = m.Activate()
	if m.Consumes(tea.KeyPressMsg(tea.Key{Code: 'q', Mod: tea.ModCtrl})) {
		t.Fatal("Ctrl+Q is consumed; it must reach the keymap")
	}
}

func TestEnterRunsActionAndCloses(t *testing.T) {
	p := allOn()
	m := newBar(p, 60, 30).Activate()
	m, cmd := send(m, keyMsg("enter"))
	if m.IsOpen() {
		t.Fatal("Enter left the dropdown open")
	}
	if cmd != nil {
		t.Fatalf("a successful run returned a command")
	}
	if !slices.Equal(p.ran, []string{"new"}) {
		t.Fatalf("ran %v, want [new]", p.ran)
	}
}

func TestEnterOnDisabledRowDoesNothing(t *testing.T) {
	p := &fakePorts{on: map[string]bool{}}
	m := newBar(p, 60, 30).Activate()
	m, _ = send(m, keyMsg("down")) // Open File... is enabled
	m, _ = send(m, keyMsg("down"))
	m, _ = send(m, keyMsg("down"))
	if m.Selected() < 0 {
		t.Fatal("no row highlighted")
	}
	m, _ = send(m, keyMsg("enter"))
	for _, a := range p.ran {
		if a == "save" {
			t.Fatal("a disabled Save ran")
		}
	}
	if m.IsOpen() {
		t.Fatal("Enter did not close")
	}
}

func TestSubmenuRowReportsUnsupported(t *testing.T) {
	p := allOn()
	m := newBar(p, 60, 30).Activate().move(1) // Edit
	// Edit: Undo, Redo, separator, Cut, Copy, Copy with Formatting (submenu).
	for range 4 {
		m, _ = send(m, keyMsg("down"))
	}
	if m.Rows()[m.Selected()].Label != "Copy with Formatting" {
		t.Fatalf("selected %q, want the submenu row", m.Rows()[m.Selected()].Label)
	}
	_, cmd := send(m, keyMsg("enter"))
	if cmd == nil {
		t.Fatal("submenu row returned no failure command")
	}
	msg, ok := cmd().(Failed)
	if !ok || msg.Label != "Copy with Formatting" || !errors.Is(msg.Err, ErrUnsupported) {
		t.Fatalf("failure = %#v", cmd())
	}
	if len(p.ran) != 0 {
		t.Fatalf("submenu row called Run with %v", p.ran)
	}
}

func TestRunErrorIsReported(t *testing.T) {
	boom := errors.New("boom")
	p := allOn()
	p.runErr = boom
	m := newBar(p, 60, 30).Activate()
	_, cmd := send(m, keyMsg("enter"))
	if cmd == nil {
		t.Fatal("failed run returned no command")
	}
	if got, ok := cmd().(Failed); !ok || !errors.Is(got.Err, boom) || got.Label != "New File" {
		t.Fatalf("failure = %#v", cmd())
	}
}

func TestNilPortsDisableEveryRun(t *testing.T) {
	m := newBar(nil, 60, 30).Activate()
	_, cmd := send(m, keyMsg("enter"))
	if cmd == nil {
		t.Fatal("nil ports ran a row without a failure")
	}
	if got := cmd().(Failed); !errors.Is(got.Err, ErrUnsupported) {
		t.Fatalf("err = %v", got.Err)
	}
}

func TestClickLabelTogglesAndSwitches(t *testing.T) {
	m := newBar(allOn(), 60, 30)
	editX := labelRuns(false)[1].x
	m, _ = send(m, press(editX, 0))
	if m.Title() != "Edit" {
		t.Fatalf("click on Edit opened %q", m.Title())
	}
	m, _ = send(m, press(editX, 0))
	if m.IsOpen() {
		t.Fatal("second click on the open label did not close it")
	}
	m, _ = send(m, press(editX, 0))
	m, _ = send(m, press(labelRuns(false)[0].x, 0))
	if m.Title() != "File" {
		t.Fatalf("click on File while Edit was open opened %q", m.Title())
	}
}

func TestClickItemRunsAndCloses(t *testing.T) {
	p := allOn()
	m := newBar(p, 60, 30).Activate()
	m, _ = send(m, press(3, 3)) // Open File... on row 3
	if m.IsOpen() {
		t.Fatal("click on an item left the dropdown open")
	}
	if !slices.Equal(p.ran, []string{"open"}) {
		t.Fatalf("ran %v, want [open]", p.ran)
	}
}

func TestClickBorderAndSeparatorClose(t *testing.T) {
	p := allOn()
	m := newBar(p, 60, 30).Activate()
	m, _ = send(m, press(3, 4)) // separator row
	if m.IsOpen() || len(p.ran) != 0 {
		t.Fatalf("separator click: open=%v ran=%v", m.IsOpen(), p.ran)
	}
	m = m.Activate()
	m, _ = send(m, press(3, 1)) // top border
	if m.IsOpen() {
		t.Fatal("top border click left the dropdown open")
	}
}

func TestPressOutsideClosesAndIsNotConsumed(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	click := press(40, 20)
	if m.Consumes(click) {
		t.Fatal("a press outside the dropdown is consumed")
	}
	m, _ = send(m, click)
	if m.IsOpen() {
		t.Fatal("a press outside did not close the dropdown")
	}
}

func TestConsumesLabelAndItemPresses(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	if !m.Consumes(press(labelRuns(false)[2].x, 0)) {
		t.Fatal("a press on a label is not consumed")
	}
	if !m.Consumes(press(3, 2)) {
		t.Fatal("a press on a row is not consumed")
	}
	if m.Consumes(tea.MouseClickMsg(tea.Mouse{X: 3, Y: 2, Button: tea.MouseRight})) {
		t.Fatal("a right press is consumed")
	}
}

func TestHoverSwitchesWhileOpen(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	hover := tea.MouseMotionMsg(tea.Mouse{X: labelRuns(false)[3].x, Y: 0})
	m, _ = send(m, hover)
	if m.Title() != "Selection" {
		t.Fatalf("hover over Selection opened %q", m.Title())
	}
	closed := newBar(allOn(), 60, 30)
	closed, _ = send(closed, hover)
	if closed.IsOpen() {
		t.Fatal("hover opened a dropdown on a closed bar")
	}
}

func TestHitRegions(t *testing.T) {
	m := newBar(allOn(), 60, 30).Activate()
	cases := []struct {
		name string
		x, y int
		want Target
	}{
		{"File label", 1, 0, Target{Kind: TargetLabel, Index: 0}},
		{"Edit label", labelRuns(false)[1].x, 0, Target{Kind: TargetLabel, Index: 1}},
		{"first row", 3, 2, Target{Kind: TargetItem, Index: 0}},
		{"second row", 3, 3, Target{Kind: TargetItem, Index: 1}},
		{"separator", 3, 4, Target{Kind: TargetBorder}},
		{"top border", 3, 1, Target{Kind: TargetBorder}},
		{"bottom border", 3, 2 + len(m.Rows()), Target{Kind: TargetBorder}},
		{"right of box", 40, 2, Target{Kind: TargetNone}},
		{"editor text", 30, 10, Target{Kind: TargetNone}},
		{"off the frame", 60, 0, Target{Kind: TargetNone}},
	}
	for _, c := range cases {
		if got := m.Hit(c.x, c.y); got != c.want {
			t.Errorf("%s: Hit(%d,%d) = %+v, want %+v", c.name, c.x, c.y, got, c.want)
		}
	}
}

func TestHitWithClosedBarOnlyLabels(t *testing.T) {
	m := newBar(allOn(), 60, 30)
	if got := m.Hit(3, 2); got.Kind != TargetNone {
		t.Fatalf("closed bar: Hit(3,2) = %+v", got)
	}
	if got := m.Hit(1, 0); got.Kind != TargetLabel {
		t.Fatalf("closed bar: Hit(1,0) = %+v", got)
	}
}

func TestDropdownIsClippedToTheFrame(t *testing.T) {
	m := newBar(allOn(), 20, 6).Activate()
	rows := paintRows(m)
	if len(rows) != 6 {
		t.Fatalf("rows = %d", len(rows))
	}
	if !strings.HasPrefix(rows[5], "╰") {
		t.Fatalf("bottom border is not on the last row inside the frame: %q", rows[5])
	}
	if strings.Contains(rows[5], "⌃Q") {
		t.Fatalf("a row was drawn over the bottom border: %q", rows[5])
	}
}

func TestTinyFrameDoesNotPanic(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {3, 3}, {4, 2}} {
		m := newBar(allOn(), size[0], size[1]).Activate()
		g := grid.New(size[0], size[1])
		m.Paint(g)
		_ = m.Hit(0, 0)
	}
}

func TestWithExplorerFocusClosesMissingLabel(t *testing.T) {
	m := newBar(allOn(), 80, 20).WithExplorerFocus(true).openTitle("Explorer")
	if !m.IsOpen() {
		t.Fatal("Explorer did not open")
	}
	m = m.WithExplorerFocus(false)
	if m.IsOpen() {
		t.Fatal("dropdown of the Explorer label stayed open after focus left")
	}
}
