package tui

import (
	"cmp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// fakeEditor implements EditorState. It records every action it is asked to run.
type fakeEditor struct {
	doc        *fakeDoc
	tabs       []TabInfo
	status     string
	rows, cols int
	actions    []editor.Action
	args       []map[string]any
	quitAction editor.Action // the action that reports quit
	extends    []buffer.Pos  // the drag ends passed to ExtendSelection
	root       string        // the workspace root that SetRoot gave
}

func newFakeEditor(lines ...string) *fakeEditor {
	return &fakeEditor{
		doc:  &fakeDoc{lines: lines},
		tabs: []TabInfo{{Name: "a.txt", Active: true}},
	}
}

func (f *fakeEditor) SetViewport(rows, cols int) { f.rows, f.cols = rows, cols }

func (f *fakeEditor) Do(a editor.Action, args map[string]any) editor.Result {
	f.actions = append(f.actions, a)
	f.args = append(f.args, args)
	return editor.Result{Quit: a == f.quitAction}
}

func (f *fakeEditor) Active() Document { return f.doc }
func (f *fakeEditor) Tabs() []TabInfo  { return f.tabs }
func (f *fakeEditor) Status() string   { return f.status }

// SetRoot records the workspace root that the root gives the editor.
func (f *fakeEditor) SetRoot(root string) { f.root = root }

// SetCursor moves the fake caret. ExtendSelection records the drag ends, and the
// scroll methods move the fake viewport, clamped the way the editor clamps it.
func (f *fakeEditor) SetCursor(p buffer.Pos, _ bool) { f.doc.cursor = p }
func (f *fakeEditor) ExtendSelection(p buffer.Pos, u editor.Unit) {
	f.extends = append(f.extends, p)
	f.doc.cursor = p
}
func (f *fakeEditor) ScrollBy(rows int) { f.ScrollTo(f.doc.top + rows) }
func (f *fakeEditor) ScrollTo(top int) {
	f.doc.top = min(max(top, 0), max(len(f.doc.lines)-1, 0))
}

// fakeDoc implements Document over a slice of lines.
type fakeDoc struct {
	lines  []string
	cursor buffer.Pos
	top    int
	eol    string // the line ending; empty means "LF"
}

func (d *fakeDoc) Name() string                { return "a.txt" }
func (d *fakeDoc) Modified() bool              { return false }
func (d *fakeDoc) LineCount() int              { return max(len(d.lines), 1) }
func (d *fakeDoc) Line(i int) string           { return d.lines[i] }
func (d *fakeDoc) SavedLines() []string        { return d.lines }
func (d *fakeDoc) Top() int                    { return d.top }
func (d *fakeDoc) Encoding() string            { return "ASCII" }
func (d *fakeDoc) LineEnding() string          { return cmp.Or(d.eol, "LF") }
func (d *fakeDoc) Cursor() buffer.Pos          { return d.cursor }
func (d *fakeDoc) LineSpans(int) []syntax.Span { return nil }
func (d *fakeDoc) Selection() (a, h buffer.Pos, ok bool) {
	return buffer.Pos{}, buffer.Pos{}, false
}

// fakeTree implements ExplorerTree over a fixed list of rows. Move and ScrollBy
// clamp the way the real tree does, and Toggle and Reload do nothing.
type fakeTree struct {
	rows     []explorer.Row
	selected int
	offset   int
	moves    []int
	scrolls  []int
	ignored  bool // the gitignored-entries setting
}

func newFakeTree(n int) *fakeTree {
	t := &fakeTree{}
	for i := range n {
		name := "f" + strconv.Itoa(i)
		t.rows = append(t.rows, explorer.Row{Name: name, Path: "/ws/" + name})
	}
	return t
}

func (t *fakeTree) Root() string          { return "/ws" }
func (t *fakeTree) Rows() []explorer.Row  { return t.rows }
func (t *fakeTree) ShowHidden() bool      { return false }
func (t *fakeTree) SetShowHidden(bool)    {}
func (t *fakeTree) ShowIgnored() bool     { return t.ignored }
func (t *fakeTree) SetShowIgnored(v bool) { t.ignored = v }
func (t *fakeTree) Toggle(string) error   { return nil }
func (t *fakeTree) Reveal(string) error   { return nil }
func (t *fakeTree) Reload() error         { return nil }
func (t *fakeTree) ScrollOffset() int     { return t.offset }
func (t *fakeTree) Selected() (explorer.Row, bool) {
	if len(t.rows) == 0 {
		return explorer.Row{}, false
	}
	return t.rows[t.selected], true
}

func (t *fakeTree) Move(delta int) {
	t.moves = append(t.moves, delta)
	t.selected = min(max(t.selected+delta, 0), len(t.rows)-1)
}

func (t *fakeTree) ScrollBy(n int) {
	t.scrolls = append(t.scrolls, n)
	t.offset = min(max(t.offset+n, 0), max(len(t.rows)-1, 0))
}

// fakeKeymap resolves chords from a table keyed by "context|name|mods".
type fakeKeymap map[string]Command

func (k fakeKeymap) Resolve(key KeyPress, ctx KeyContext) (Command, bool) {
	c, ok := k[string(ctx)+"|"+key.Name+"|"+strings.Join(key.Mods, "+")]
	return c, ok
}

// testKeys binds the chords the tests press.
func testKeys() fakeKeymap {
	return fakeKeymap{
		"normal|b|ctrl":        {Action: "toggle_file_explorer"},
		"file_explorer|b|ctrl": {Action: "toggle_file_explorer"},
		"normal|e|ctrl":        {Action: "focus_file_explorer"},
		"file_explorer|e|ctrl": {Action: "focus_editor"},
		"file_explorer|Down|":  {Action: "file_explorer_down"},
		"file_explorer|Up|":    {Action: "file_explorer_up"},
		"normal|s|ctrl":        {Action: "save"},
		"normal|q|ctrl":        {Action: "quit"},
		"normal|c|ctrl":        {Action: "copy"},
		"file_explorer|c|ctrl": {Action: "copy"},
		"normal|Enter|":        {Action: "insert_newline"},
		"normal|x|ctrl":        {Action: "close_tab", Args: map[string]any{"force": true}},
		"normal|n|ctrl":        {Action: "new"},
		"normal|w|ctrl":        {Action: "close_tab"},
	}
}

// testClock is a fixed instant for the click tests.
func testClock() time.Time {
	return time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
}

// ms is a duration of n milliseconds.
func ms(n int) time.Duration {
	return time.Duration(n) * time.Millisecond
}

// newTestModel returns a model over ed and tree, sized to w by h, and quits on
// the quit action.
func newTestModel(t *testing.T, ed *fakeEditor, tree ExplorerTree, w, h int) *Model {
	t.Helper()
	ed.quitAction = "quit"
	m := New(Deps{Editor: ed, Explorer: tree, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// press sends one key press and returns the command it started, if any.
func press(m *Model, k tea.KeyPressMsg) tea.Cmd {
	_, cmd := m.Update(k)
	return cmd
}

func ctrl(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

func typed(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func special(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func click(x, y int, b tea.MouseButton) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: b})
}

// TestStartupNoticeShowsInTheStatusRow checks that a notice given in Deps, such as
// a config warning, is on the status row from the first frame.
func TestStartupNoticeShowsInTheStatusRow(t *testing.T) {
	t.Parallel()
	const notice = "config: bad key"
	m := New(Deps{
		Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{},
		Clock: testClock, Notice: notice,
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	g := m.paint()
	y := m.layout().StatusY
	var row strings.Builder
	for x := range g.Width() {
		row.WriteString(g.At(x, y).Text)
	}
	if !strings.Contains(row.String(), notice) {
		t.Errorf("status row %q does not show the startup notice %q", row.String(), notice)
	}
}

func TestNewStartsWithExplorerHiddenAndEditorFocused(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	if m.explorerOpen || m.explorerFocus {
		t.Fatalf("explorer open %v focused %v at start", m.explorerOpen, m.explorerFocus)
	}
}

func TestToggleExplorerCyclesOpenFocusClose(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	full := ed.cols

	press(m, ctrl('b'))
	if !m.explorerOpen || !m.explorerFocus {
		t.Fatalf("first toggle: open %v focused %v", m.explorerOpen, m.explorerFocus)
	}
	if ed.cols >= full {
		t.Fatalf("editor columns did not shrink for the panel: %d, was %d", ed.cols, full)
	}

	// With the explorer focused, Ctrl+B hides it and focus goes to the editor.
	press(m, ctrl('b'))
	if m.explorerOpen || m.explorerFocus {
		t.Fatalf("second toggle with the explorer focused: open %v focused %v, want hidden", m.explorerOpen, m.explorerFocus)
	}
	if ed.cols != full {
		t.Fatalf("editor columns after hiding = %d, want %d", ed.cols, full)
	}

	// Hidden, Ctrl+B shows the panel again and focuses it.
	press(m, ctrl('b'))
	if !m.explorerOpen || !m.explorerFocus {
		t.Fatalf("third toggle: open %v focused %v, want shown and focused", m.explorerOpen, m.explorerFocus)
	}
	if len(ed.actions) != 0 {
		t.Fatalf("toggles reached the editor: %v", ed.actions)
	}
}

func TestToggleWithoutExplorerDoesNothing(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 80, 24)
	press(m, ctrl('b'))
	press(m, ctrl('e'))
	if m.explorerOpen || m.explorerFocus {
		t.Fatalf("a model without a tree opened the explorer: open %v focused %v", m.explorerOpen, m.explorerFocus)
	}
}

func TestExplorerKeysMoveTheTreeAndNotTheText(t *testing.T) {
	ed := newFakeEditor("hello")
	tree := newFakeTree(30)
	m := newTestModel(t, ed, tree, 80, 24)
	press(m, ctrl('e')) // focus_file_explorer

	press(m, special(tea.KeyDown))
	press(m, special(tea.KeyUp))
	if got := tree.moves; len(got) != 2 || got[0] != 1 || got[1] != -1 {
		t.Fatalf("tree moves = %v, want [1 -1]", got)
	}

	press(m, typed("z")) // explorer has focus, so the text is not inserted
	if len(ed.actions) != 0 {
		t.Fatalf("editor ran %v while the explorer had focus", ed.actions)
	}
}

func TestTypedTextInsertsWhenEditorHasFocus(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, nil, 80, 24)
	press(m, typed("a"))
	if len(ed.actions) != 1 || ed.actions[0] != "insert_char" {
		t.Fatalf("actions = %v", ed.actions)
	}
	if got := ed.args[0]["text"]; got != "a" {
		t.Fatalf("insert_char text = %v", got)
	}
}

func TestBoundKeyRunsItsActionWithArgs(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, nil, 80, 24)
	press(m, ctrl('x'))
	if len(ed.actions) != 1 || ed.actions[0] != "close_tab" {
		t.Fatalf("actions = %v", ed.actions)
	}
	if ed.args[0]["force"] != true {
		t.Fatalf("args = %v", ed.args[0])
	}
}

func TestBoundKeyBeatsTypedText(t *testing.T) {
	// Enter has a binding and no text, so the binding alone runs.
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, nil, 80, 24)
	press(m, special(tea.KeyEnter))
	if len(ed.actions) != 1 || ed.actions[0] != "insert_newline" {
		t.Fatalf("actions = %v", ed.actions)
	}
}

func TestQuitActionReturnsTeaQuit(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 80, 24)
	cmd := press(m, ctrl('q'))
	if cmd == nil {
		t.Fatal("quit returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit command produced %T", cmd())
	}
	if cmd := press(m, ctrl('s')); cmd != nil {
		t.Fatalf("save returned a command: %v", cmd)
	}
}

func TestClipboardActionsAreSkippedInExplorer(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	press(m, ctrl('e'))
	press(m, ctrl('c'))
	if len(ed.actions) != 0 {
		t.Fatalf("copy reached the editor from the explorer: %v", ed.actions)
	}
	press(m, ctrl('e')) // focus the editor again
	press(m, ctrl('c'))
	if len(ed.actions) != 1 || ed.actions[0] != "copy" {
		t.Fatalf("copy in the editor: actions = %v", ed.actions)
	}
}

func TestWindowSizeSetsTheEditorViewport(t *testing.T) {
	ed := newFakeEditor("hello")
	newTestModel(t, ed, nil, 80, 24)
	// Body rows 3..22 are 20 rows. The gutter for one line is six cells.
	if ed.rows != 20 || ed.cols != 74 {
		t.Fatalf("viewport = %dx%d, want 20x74", ed.rows, ed.cols)
	}
}

func TestWheelOverExplorerScrollsTheTree(t *testing.T) {
	tree := newFakeTree(40)
	m := newTestModel(t, newFakeEditor("hello"), tree, 80, 24)
	press(m, ctrl('b'))
	l := m.layout()

	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 2, Y: l.BodyTop + 1, Button: tea.MouseWheelDown}))
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 2, Y: l.BodyTop + 1, Button: tea.MouseWheelUp}))
	if len(tree.scrolls) != 2 || tree.scrolls[0] != wheelRows || tree.scrolls[1] != -wheelRows {
		t.Fatalf("scrolls = %v, want [%d %d]", tree.scrolls, wheelRows, -wheelRows)
	}
}

func TestWheelOverEditorDoesNotScrollTheTree(t *testing.T) {
	tree := newFakeTree(40)
	m := newTestModel(t, newFakeEditor("hello"), tree, 80, 24)
	press(m, ctrl('b'))
	l := m.layout()
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: l.TextX + 1, Y: l.BodyTop, Button: tea.MouseWheelDown}))
	if len(tree.scrolls) != 0 {
		t.Fatalf("editor wheel scrolled the tree: %v", tree.scrolls)
	}
}

func TestClickOnCloseButtonHidesExplorerOnlyForLeftButton(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	press(m, ctrl('b'))
	l := m.layout()
	if l.ExCloseX < 0 {
		t.Fatal("the close button is not drawn at 80 columns")
	}

	m.Update(click(l.ExCloseX, l.ExTitleY, tea.MouseRight))
	if !m.explorerOpen {
		t.Fatal("a right press closed the explorer")
	}
	_, cmd := m.Update(click(l.ExCloseX, l.ExTitleY, tea.MouseLeft))
	drain(t, m, cmd)
	if m.explorerOpen || m.explorerFocus {
		t.Fatalf("left press on × left the panel open %v focused %v", m.explorerOpen, m.explorerFocus)
	}
}

func TestClickOnExplorerRowGivesFocusToExplorer(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(5), 80, 24)
	press(m, ctrl('b'))
	press(m, ctrl('e')) // focus the editor
	l := m.layout()
	m.Update(click(3, l.BodyTop+1, tea.MouseLeft))
	if !m.explorerFocus {
		t.Fatal("a press on a row did not focus the explorer")
	}
}

func TestClickInEditorGivesFocusToEditor(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(5), 80, 24)
	press(m, ctrl('b'))
	l := m.layout()
	m.Update(click(l.TextX, l.BodyTop, tea.MouseLeft))
	if m.explorerFocus {
		t.Fatal("a press in the editor left the explorer focused")
	}
}

func TestDoubleClickCountsThroughTheClock(t *testing.T) {
	now := testClock()
	ed := newFakeEditor("hello")
	m := New(Deps{Editor: ed, Keys: testKeys(), Clock: func() time.Time { return now }})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	l := m.layout()
	x, y := l.TextX, l.BodyTop

	m.Update(click(x, y, tea.MouseLeft))
	now = now.Add(ms(100))
	m.Update(click(x, y, tea.MouseLeft))
	if m.lastPress != 2 {
		t.Fatalf("second press count = %d, want 2", m.lastPress)
	}
	now = now.Add(ms(900))
	m.Update(click(x, y, tea.MouseLeft))
	if m.lastPress != 1 {
		t.Fatalf("press after the window count = %d, want 1", m.lastPress)
	}
}

func TestSelectionFollowsTheTreeCursor(t *testing.T) {
	tree := newFakeTree(60)
	m := newTestModel(t, newFakeEditor("hello"), tree, 80, 24)
	press(m, ctrl('e')) // focus_file_explorer shows and focuses the panel
	body := m.bodyRows()
	for range body + 4 {
		press(m, special(tea.KeyDown))
	}
	off := tree.ScrollOffset()
	if sel := tree.selected; sel < off || sel >= off+body {
		t.Fatalf("selected row %d is outside the body %d..%d", sel, off, off+body-1)
	}
}

func TestViewPaintsTheFrameAt80x24(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello", "world"), newFakeTree(4), 80, 24)
	press(m, ctrl('b'))
	v := m.View()
	if !v.AltScreen {
		t.Error("view is not in the alternate screen")
	}
	if v.MouseMode != tea.MouseModeAllMotion {
		t.Errorf("mouse mode = %v, want all motion", v.MouseMode)
	}
	rows := strings.Split(v.Content, "\n")
	if len(rows) != 24 {
		t.Fatalf("view has %d rows, want 24", len(rows))
	}
	for y, row := range rows {
		if w := ansi.StringWidth(row); w != 80 {
			t.Errorf("row %d is %d cells wide, want 80: %q", y, w, ansi.Strip(row))
		}
	}
	plain := ansi.Strip(v.Content)
	for _, want := range []string{"File", "Ln 1, Col 1", "Palette: Ctrl+P", "File Explorer"} {
		if !strings.Contains(plain, want) {
			t.Errorf("frame lacks %q", want)
		}
	}
}

func TestViewWithoutExplorerPaintsEveryRow(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 40, 15)
	rows := strings.Split(m.View().Content, "\n")
	if len(rows) != 15 {
		t.Fatalf("view has %d rows, want 15", len(rows))
	}
	if strings.Contains(ansi.Strip(m.View().Content), "File Explorer") {
		t.Error("explorer title painted with no explorer")
	}
}

func TestViewUnderTheGateShowsOnlyTheMessage(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 7, 5)
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	if rows[0] != "gvim: t" {
		t.Fatalf("gate row = %q", rows[0])
	}
	for y := 1; y < len(rows); y++ {
		if strings.TrimSpace(rows[y]) != "" {
			t.Errorf("row %d under the gate is not blank: %q", y, rows[y])
		}
	}
}

func TestViewBeforeTheFirstSizeIsEmpty(t *testing.T) {
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys()})
	v := m.View()
	if v.Content != "" || !v.AltScreen {
		t.Fatalf("view before a size = %q alt %v", v.Content, v.AltScreen)
	}
}

func TestInitStartsNoCommands(t *testing.T) {
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys()})
	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init returned a command")
	}
}
