package tui

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/browser"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/menubar"
	"github.com/tbxark/gvim/theme"
)

// numberedLines returns n lines "line 1", "line 2", and so on.
func numberedLines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "line " + strconv.Itoa(i+1)
	}
	return out
}

func TestViewPaintsTheEditorText(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello", "world"), nil, 40, 15)
	l := m.layout()
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	// The row's first cell is the frame's border, so the pane is read from the
	// editor column on.
	row := func(y int) string { return ansi.Cut(rows[y], l.EditorX, l.W) }
	if got, want := row(l.BodyTop), "  1 │ hello"; !strings.HasPrefix(got, want) {
		t.Errorf("first body row = %q, want the first line after the gutter %q", got, want)
	}
	if got, want := row(l.BodyTop+1), "  2 │ world"; !strings.HasPrefix(got, want) {
		t.Errorf("second body row = %q, want %q", got, want)
	}
	if got := row(l.BodyTop + 2); !strings.HasPrefix(got, "~") {
		t.Errorf("row under the text = %q, want a tilde row past the end", got)
	}
}

func TestViewPaintsTheEditorPaneRightOfTheExplorer(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	press(m, ctrl('b'))
	l := m.layout()
	if l.EditorX == 0 {
		t.Fatal("the explorer takes no columns at 80 wide")
	}
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	if got := ansi.Cut(rows[l.BodyTop], l.EditorX, l.W); !strings.HasPrefix(got, "  1 │ hello") {
		t.Errorf("first body row from x0 %d = %q, want the text", l.EditorX, got)
	}
}

func TestViewCursorIsTheEditorCaret(t *testing.T) {
	ed := newFakeEditor("hello", "world")
	ed.doc.cursor = buffer.Pos{Line: 1, Col: 3}
	m := newTestModel(t, ed, nil, 40, 15)
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("no terminal cursor with the editor focused")
	}
	l := m.layout()
	if want := l.TextX + 3; v.Cursor.X != want || v.Cursor.Y != l.BodyTop+1 {
		t.Errorf("cursor at (%d, %d), want (%d, %d)", v.Cursor.X, v.Cursor.Y, want, l.BodyTop+1)
	}
}

func TestViewHidesTheCaretWhileTheExplorerHasFocus(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	press(m, ctrl('e'))
	if v := m.View(); v.Cursor != nil {
		t.Fatal("the editor caret is shown with the explorer focused")
	}
}

func TestClickPlacesTheCaret(t *testing.T) {
	ed := newFakeEditor("hello", "world")
	m := newTestModel(t, ed, nil, 40, 15)
	l := m.layout()
	m.Update(click(l.TextX+2, l.BodyTop+1, tea.MouseLeft))
	if want := (buffer.Pos{Line: 1, Col: 2}); ed.doc.cursor != want {
		t.Fatalf("caret = %+v, want %+v", ed.doc.cursor, want)
	}
}

func TestClickOnGutterPlacesTheCaretAtTheLineStart(t *testing.T) {
	ed := newFakeEditor("hello", "world")
	m := newTestModel(t, ed, nil, 40, 15)
	l := m.layout()
	m.Update(click(1, l.BodyTop+1, tea.MouseLeft))
	if want := (buffer.Pos{Line: 1, Col: 0}); ed.doc.cursor != want {
		t.Fatalf("caret = %+v, want %+v", ed.doc.cursor, want)
	}
}

func TestDoubleClickSelectsTheWord(t *testing.T) {
	now := testClock()
	ed := newFakeEditor("hello")
	m := New(Deps{Editor: ed, Keys: testKeys(), Clock: func() time.Time { return now }})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 15})
	l := m.layout()
	m.Update(click(l.TextX+1, l.BodyTop, tea.MouseLeft))
	now = now.Add(ms(100))
	m.Update(click(l.TextX+1, l.BodyTop, tea.MouseLeft))
	if len(ed.actions) != 1 || ed.actions[0] != "select_word" {
		t.Fatalf("actions after the double press = %v, want select_word", ed.actions)
	}
}

func TestDragExtendsTheSelectionToTheCell(t *testing.T) {
	ed := newFakeEditor("hello", "world")
	m := newTestModel(t, ed, nil, 40, 15)
	l := m.layout()
	m.Update(click(l.TextX, l.BodyTop, tea.MouseLeft))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.TextX + 3, Y: l.BodyTop + 1, Button: tea.MouseLeft}))
	if len(ed.extends) != 1 || ed.extends[0] != (buffer.Pos{Line: 1, Col: 3}) {
		t.Fatalf("drag ends = %+v, want one end at line 1 col 3", ed.extends)
	}
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: l.TextX + 3, Y: l.BodyTop + 1, Button: tea.MouseLeft}))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.TextX + 4, Y: l.BodyTop + 1, Button: tea.MouseLeft}))
	if len(ed.extends) != 1 {
		t.Fatal("a motion after the release still extended the selection")
	}
}

// TestTripleClickSelectsTheLine: three real left presses in the same text cell within the
// double-click window take placeCaret's count==3 branch, selecting the whole line. A drag
// then extends by the same line unit, and a motion after release no longer extends.
func TestTripleClickSelectsTheLine(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "hello\nworld\nthird\nfourth\n", 60, 15)
	l := m.layout()
	x, y := l.TextX+2, l.BodyTop+1 // the world line
	m.Update(click(x, y, tea.MouseLeft))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	m.Update(click(x, y, tea.MouseLeft))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	m.Update(click(x, y, tea.MouseLeft)) // third press stays held for the drag

	anchor, head, ok := selectionOf(ed)
	if !ok {
		t.Fatalf("triple-click selected no line; lastPress=%d", m.lastPress)
	}
	if anchor != (buffer.Pos{Line: 1, Col: 0}) || head != (buffer.Pos{Line: 2, Col: 0}) {
		t.Fatalf("triple-click selection = %v to %v, want 1:0 to 2:0", anchor, head)
	}

	// The drag that follows a triple press grows by whole lines.
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: x + 2, Y: l.BodyTop + 2, Button: tea.MouseLeft}))
	anchor, head, ok = selectionOf(ed)
	if !ok || anchor != (buffer.Pos{Line: 1, Col: 0}) || head != (buffer.Pos{Line: 3, Col: 0}) {
		t.Fatalf("line drag selection = %v to %v, ok %v, want 1:0 to 3:0", anchor, head, ok)
	}
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: x + 2, Y: l.BodyTop + 2, Button: tea.MouseLeft}))
	_, before, _ := selectionOf(ed)
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: x, Y: l.BodyTop + 3, Button: tea.MouseLeft}))
	_, after, _ := selectionOf(ed)
	if after != before {
		t.Fatalf("motion after release extended the line selection: %v to %v", before, after)
	}
}

// TestExpandLineSelectionKeyAtEveryCaret: the real Ctrl+L key path (keymap ->
// expand_line_selection) grows every caret's line selection, matching the editor action.
func TestExpandLineSelectionKeyAtEveryCaret(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "a\nb\nc\nd\n", 60, 15)
	if r := ed.Do("add_cursor_below", nil); r.Err != nil {
		t.Fatal(r.Err)
	}
	m.Update(ctrl('l'))
	d := ed.Active()
	if d.Sel == nil || d.Sel.Anchor != (buffer.Pos{Line: 1}) || d.Sel.Head != (buffer.Pos{Line: 2}) {
		t.Fatalf("primary selection after Ctrl+L = %+v, want 1:0 to 2:0", d.Sel)
	}
	cs := d.Cursors()
	if len(cs) != 1 || cs[0].Sel == nil || cs[0].Sel.Anchor != (buffer.Pos{Line: 0}) || cs[0].Sel.Head != (buffer.Pos{Line: 1}) {
		t.Fatalf("extra caret after Ctrl+L = %+v, want line 0 selection", cs)
	}
}

func TestWheelOverEditorScrollsTheViewportNotTheCaret(t *testing.T) {
	ed := newFakeEditor(numberedLines(40)...)
	ed.doc.cursor = buffer.Pos{Line: 0, Col: 0}
	m := newTestModel(t, ed, nil, 40, 15)
	l := m.layout()
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: l.TextX + 1, Y: l.BodyTop, Button: tea.MouseWheelDown}))
	if ed.doc.top != wheelRows {
		t.Fatalf("viewport top = %d, want %d", ed.doc.top, wheelRows)
	}
	if ed.doc.cursor != (buffer.Pos{}) {
		t.Fatalf("the wheel moved the caret to %+v", ed.doc.cursor)
	}
}

func TestScrollbarTrackPressMovesTheViewport(t *testing.T) {
	ed := newFakeEditor(numberedLines(100)...)
	m := newTestModel(t, ed, nil, 40, 15)
	l := m.layout()
	if !l.Scrollbar {
		t.Fatal("a 100-line document has no scrollbar at 15 rows")
	}
	row := 6
	// The scrollbar is the editor column's last cell: the frame border is outside it.
	m.Update(click(l.EditorX+l.EditorW-1, l.BodyTop+row, tea.MouseLeft))
	p := m.pane(m.ed.Active())
	if want := p.ScrollTopFor(l.FocusedPane().W, m.bodyRows(), row); ed.doc.top != want || want == 0 {
		t.Fatalf("viewport top = %d, want %d (non-zero)", ed.doc.top, want)
	}
}

func TestOverlayRunsLastInTheStackAndGetsTheInputFirst(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	o := &fakeOverlay{open: true, text: "PALETTE"}
	m.pushOverlay(o)

	press(m, ctrl('b')) // a bound key: the overlay gets it instead of the keymap
	if m.explorerOpen {
		t.Fatal("the keymap ran while an overlay was open")
	}
	if len(o.keys) != 1 {
		t.Fatalf("overlay got %d messages, want 1", len(o.keys))
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "PALETTE") {
		t.Error("the overlay is not drawn over the frame")
	}
	if m.View().Cursor != nil {
		t.Error("the editor caret is shown under an overlay")
	}
}

func TestOverlayGetsMouseInput(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 40, 15)
	o := &fakeOverlay{open: true}
	m.pushOverlay(o)
	m.Update(click(10, 5, tea.MouseLeft))
	if len(o.keys) != 1 {
		t.Fatalf("overlay got %d mouse messages, want 1", len(o.keys))
	}
	if m.drag.active {
		t.Error("a click under an overlay started an editor drag")
	}
}

func TestEscapePopsTheTopOverlayAndIsNotForwarded(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 40, 15)
	o := &fakeOverlay{open: true}
	m.pushOverlay(o)
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("stack has %d overlays after Esc, want 0", len(m.overlays))
	}
	if len(o.keys) != 0 {
		t.Fatalf("Esc was forwarded to the overlay: %v", o.keys)
	}
}

func TestOverlayReturningNilCloses(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 40, 15)
	m.pushOverlay(&fakeOverlay{open: true, closeNow: true})
	press(m, typed("a"))
	if len(m.overlays) != 0 {
		t.Fatalf("stack has %d overlays after the overlay closed, want 0", len(m.overlays))
	}
}

func TestInactiveOverlayIsPopped(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 40, 15)
	m.pushOverlay(&fakeOverlay{open: true, deactivate: true})
	press(m, typed("a"))
	if len(m.overlays) != 0 {
		t.Fatal("an overlay that reports Active false stayed on the stack")
	}
}

func TestOverlayReplacesItselfOnTheStack(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 40, 15)
	first := &fakeOverlay{open: true}
	second := &fakeOverlay{open: true, text: "SECOND"}
	first.next = second
	m.pushOverlay(first)
	press(m, typed("a"))
	if len(m.overlays) != 1 || m.overlays[0] != second {
		t.Fatalf("stack = %v, want the replacement on top", m.overlays)
	}
}

func TestInputReachesTheEditorOnlyWhenTheStackIsEmpty(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, nil, 40, 15)
	m.pushOverlay(&fakeOverlay{open: true})
	press(m, typed("a"))
	if len(ed.actions) != 0 {
		t.Fatalf("the editor ran %v under an overlay", ed.actions)
	}
	press(m, special(tea.KeyEscape))
	press(m, typed("a"))
	if len(ed.actions) != 1 || ed.actions[0] != "insert_char" {
		t.Fatalf("after the overlay closed, actions = %v", ed.actions)
	}
}

// fakeOverlay is an Overlay that records its input. Update returns nil when
// closeNow is set, and sets open false when deactivate is set. It returns next
// in its place when next is set, and itself otherwise.
type fakeOverlay struct {
	open       bool
	closeNow   bool
	deactivate bool
	next       Overlay
	text       string
	keys       []tea.Msg
}

func (o *fakeOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	o.keys = append(o.keys, msg)
	switch {
	case o.closeNow:
		return nil, nil
	case o.deactivate:
		o.open = false
	case o.next != nil:
		return o.next, nil
	}
	return o, nil
}

func (o *fakeOverlay) Draw(g *grid.Grid) {
	if o.text != "" {
		g.Put(0, 0, g.Width(), o.text, grid.Style{})
	}
}

func (o *fakeOverlay) Active() bool { return o.open }

// withMenuKeys binds F10 to the menu bar and Ctrl+O to the browser on m.
func withMenuKeys(m *Model) *Model {
	k := testKeys()
	k["normal|F10|"] = Command{Action: "menu_activate"}
	k["normal|o|ctrl"] = Command{Action: "open"}
	m.keys = k
	return m
}

// unknownEditor reports find_selection_next as an action the editor does not
// know, the way the app adapter does for the actions that are not built yet.
type unknownEditor struct{ *fakeEditor }

func (u unknownEditor) Do(a editor.Action, args map[string]any) editor.Result {
	if a == "find_selection_next" {
		return editor.Result{Err: editor.ErrUnknownAction}
	}
	return u.fakeEditor.Do(a, args)
}

// fileEditorFake is a fakeEditor that also opens files. It records the calls.
type fileEditorFake struct {
	*fakeEditor
	opened, previews []string
	line, col        int
}

func (f *fileEditorFake) OpenFile(path string) error {
	f.opened = append(f.opened, path)
	return nil
}

func (f *fileEditorFake) OpenPreview(path string) error {
	f.previews = append(f.previews, path)
	return nil
}

func (f *fileEditorFake) GotoLine(line, col int) error {
	f.line, f.col = line, col
	return nil
}

// pathedDoc is a fake document that has a file on disk.
type pathedDoc struct{ *fakeDoc }

func (pathedDoc) HasPath() bool { return true }

func TestF10OpensTheFileMenuAndEscapeCloses(t *testing.T) {
	m := withMenuKeys(newTestModel(t, newFakeEditor("hello"), nil, 60, 20))
	press(m, special(tea.KeyF10))
	if m.menu.Title() != "File" {
		t.Fatalf("menu title = %q after F10, want File", m.menu.Title())
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "New File") {
		t.Error("the File dropdown is not painted")
	}
	press(m, special(tea.KeyEscape))
	if m.menu.IsOpen() {
		t.Error("Esc left the menu open")
	}
}

func TestClickEditLabelThenEnterRunsUndo(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, nil, 60, 20)
	edit := menuLabelRuns(m)[1]
	m.Update(click(edit.X, 0, tea.MouseLeft))
	if m.menu.Title() != "Edit" {
		t.Fatalf("menu title = %q after the Edit click, want Edit", m.menu.Title())
	}
	press(m, special(tea.KeyEnter))
	if !slices.Contains(ed.actions, editor.Action("undo")) {
		t.Fatalf("editor actions = %v, want undo", ed.actions)
	}
	if m.menu.IsOpen() {
		t.Error("the dropdown stayed open after a row ran")
	}
}

func TestRunReportsAnActionTheEditorDoesNotKnow(t *testing.T) {
	m := New(Deps{Editor: unknownEditor{newFakeEditor("x")}, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	// search is handled by the root now, so an action no layer runs stands in for it.
	if err := m.Run("find_selection_next"); !errors.Is(err, menubar.ErrUnsupported) {
		t.Errorf("Run(find_selection_next) = %v, want ErrUnsupported", err)
	}
	if err := m.Run("move_right"); err != nil {
		t.Errorf("Run(move_right) = %v, want nil", err)
	}
}

func TestQuitRowQuitsWhenTheMenuCloses(t *testing.T) {
	ed := newFakeEditor("hello")
	m := withMenuKeys(newTestModel(t, ed, nil, 60, 20))
	press(m, special(tea.KeyF10))
	if err := m.Run("quit"); err != nil {
		t.Fatalf("Run(quit) = %v", err)
	}
	cmd := press(m, special(tea.KeyEscape))
	if cmd == nil {
		t.Fatal("the quit row started no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("the command is not a quit: %T", cmd())
	}
	if m.quitting {
		t.Error("quitting stayed set after the quit was sent")
	}
}

func TestFailedRowShowsANoticeUntilTheNextPress(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 100, 20)
	m.Update(menubar.Failed{Label: "Find", Err: menubar.ErrUnsupported})
	if !strings.Contains(ansi.Strip(m.View().Content), "Not supported in this port: Find") {
		t.Error("the unsupported notice is not in the status row")
	}
	press(m, typed("a"))
	if strings.Contains(ansi.Strip(m.View().Content), "Not supported in this port") {
		t.Error("the notice stayed after a key press")
	}
	if got := failedNotice(menubar.Failed{Label: "Save", Err: errors.New("disk full")}); got != "Save: disk full" {
		t.Errorf("failedNotice = %q", got)
	}
}

func TestExplorerMessagesDriveTheRoot(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	m.Update(expanel.FocusMsg{})
	if !m.explorerOpen || !m.explorerFocus {
		t.Fatal("FocusMsg did not open and focus the panel")
	}
	m.Update(expanel.CloseMsg{})
	if m.explorerOpen || m.explorerFocus {
		t.Fatal("CloseMsg did not hide the panel")
	}
	m.Update(expanel.ResizeMsg{Width: 30})
	if m.explorerWidth.Mode != explorer.WidthColumns || m.explorerWidth.Value != 30 {
		t.Errorf("explorer width = %+v after ResizeMsg, want 30 columns", m.explorerWidth)
	}
	m.Update(expanel.ErrorMsg{Err: errors.New("boom")})
	if m.notice != "boom" {
		t.Errorf("notice = %q after ErrorMsg, want boom", m.notice)
	}
	m.Update(expanel.OpenFileMsg{Path: "/a", Preview: true})
	if m.notice == "" {
		t.Error("opening a file with an editor that cannot open files gave no notice")
	}
}

func TestOpenFileMessagesUseTheFileEditor(t *testing.T) {
	fe := &fileEditorFake{fakeEditor: newFakeEditor("hello")}
	m := newTestModel(t, fe.fakeEditor, newFakeTree(3), 80, 24)
	m.ed = fe
	press(m, ctrl('b'))
	m.Update(expanel.OpenFileMsg{Path: "/a", Preview: false})
	if !slices.Equal(fe.opened, []string{"/a"}) {
		t.Fatalf("opened = %v, want [/a]", fe.opened)
	}
	if m.explorerFocus {
		t.Error("a permanent open left the explorer focused")
	}
	m.Update(expanel.OpenFileMsg{Path: "/p", Preview: true})
	if !slices.Equal(fe.previews, []string{"/p"}) {
		t.Errorf("previews = %v, want [/p]", fe.previews)
	}
	m.Update(browser.Open{Path: "/b", Line: 3, Col: 2})
	if !slices.Equal(fe.opened, []string{"/a", "/b"}) || fe.line != 3 || fe.col != 2 {
		t.Errorf("browser open: opened %v, caret %d:%d", fe.opened, fe.line, fe.col)
	}
}

func TestReloadMessageRereadsTheTree(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	m.Update(expanel.ReloadMsg{})
	if m.notice != "" {
		t.Errorf("a reload that succeeded left notice %q", m.notice)
	}
}

func TestCtrlOOpensTheBrowserAndEscapeCloses(t *testing.T) {
	m := withMenuKeys(newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24))
	press(m, ctrl('o'))
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d after Ctrl+O, want 1", len(m.overlays))
	}
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %d after Esc, want 0", len(m.overlays))
	}
}

func TestSwitchProjectRunsTheBrowserInFolderMode(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	if err := m.Run("switch_project"); err != nil {
		t.Fatalf("Run(switch_project) = %v", err)
	}
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the browser", len(m.overlays))
	}
}

func TestBrowserChoiceSetsANotice(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), nil, 80, 24)
	m.Update(browser.Choose{Path: "/p"})
	if !strings.Contains(m.notice, "Switch Project") {
		t.Errorf("notice = %q after a folder choice", m.notice)
	}
}

func TestBrowserOverlayPopsOnceClosed(t *testing.T) {
	b := browser.New(browser.Options{Dir: t.TempDir()})
	if !b.Active() {
		t.Fatal("a new browser is not active")
	}
	next, _ := b.Update(special(tea.KeyEscape))
	if next != nil {
		t.Error("a closed browser was kept on the stack")
	}
	if b.Active() {
		t.Error("the browser is still active after Esc")
	}
}

func TestHoldsEvaluatesTheMenuContextKeys(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	off := map[string]bool{
		"can_save": false, "can_save_all": false, "has_selection": false,
		"file_explorer": false, "file_explorer_focused": false,
		"file_explorer_show_hidden": false, "not_a_key": false,
	}
	for key, want := range off {
		if got := m.Holds(key); got != want {
			t.Errorf("Holds(%q) = %v, want %v", key, got, want)
		}
	}
	if !m.Holds("has_buffer") || !m.Holds("can_edit") {
		t.Error("the buffer keys do not hold for an open document")
	}
	ed.tabs[0].Modified = true
	if !m.Holds("can_save_all") {
		t.Error("can_save_all does not hold with a modified tab")
	}
	press(m, ctrl('b'))
	if !m.Holds("file_explorer") || !m.Holds("file_explorer_focused") {
		t.Error("the explorer keys do not hold with the panel focused")
	}
}

func TestHasPathNeedsAFileOnDisk(t *testing.T) {
	if hasPath(&fakeDoc{}) {
		t.Error("an unnamed document reports a path")
	}
	if !hasPath(pathedDoc{&fakeDoc{}}) {
		t.Error("a document with a file does not report a path")
	}
}

func TestMenuRunCanStartAnExplorerCommand(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	press(m, ctrl('b'))
	if err := m.Run("file_explorer_refresh"); err != nil {
		t.Fatalf("Run(file_explorer_refresh) = %v", err)
	}
	if m.pending == nil {
		t.Error("the refresh command was not kept for the menu")
	}
}
