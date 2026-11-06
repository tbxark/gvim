package ui

import (
	"errors"
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/dialog"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// tabFake is a fakeEditor whose tabs can be activated and closed. Each tab has a
// dirty flag, and the active document can be untitled or can collide with an
// existing file on Save As.
type tabFake struct {
	*fakeEditor
	names    []string
	dirty    []bool
	active   int
	closed   []int
	untitled bool // the active document has no path
	exists   bool // Save As finds the target already on disk
	quit     bool
}

func newTabFake(names ...string) *tabFake {
	f := &tabFake{fakeEditor: newFakeEditor("hello"), names: names, dirty: make([]bool, len(names))}
	f.sync()
	return f
}

// sync rebuilds the tab list the root reads from the tab state.
func (f *tabFake) sync() {
	f.tabs = nil
	for i, n := range f.names {
		f.tabs = append(f.tabs, TabInfo{Name: n, Modified: f.dirty[i], Active: i == f.active})
	}
}

func (f *tabFake) Tabs() []TabInfo { return f.tabs }

// pathDoc is the active document with a path, unless the fake is untitled.
type pathDoc struct {
	Document
	hasPath bool
}

func (d pathDoc) HasPath() bool { return d.hasPath }

func (f *tabFake) Active() Document {
	if len(f.names) == 0 {
		return nil // no tab open: the welcome screen shows
	}
	return pathDoc{Document: f.fakeEditor.Active(), hasPath: !f.untitled}
}

// Do records the action. Save and save_as fail the way the editor does for an
// untitled buffer and for an existing target.
func (f *tabFake) Do(a editor.Action, args map[string]any) editor.Result {
	f.actions = append(f.actions, a)
	f.args = append(f.args, args)
	switch a {
	case "save":
		if f.untitled {
			return editor.Result{Err: editor.ErrNoPath}
		}
		f.dirty[f.active] = false
	case "save_as":
		force, _ := args["force"].(bool)
		if f.exists && !force {
			return editor.Result{Err: editor.ErrExists}
		}
		f.untitled = false
		f.dirty[f.active] = false
	case "new":
		f.names = append(f.names, "Untitled-"+strconv.Itoa(len(f.names)+1))
		f.dirty = append(f.dirty, false)
		f.active = len(f.names) - 1
	}
	f.sync()
	return editor.Result{}
}

func (f *tabFake) ActivateTab(i int) {
	if i >= 0 && i < len(f.names) {
		f.active = i
	}
	f.sync()
}

func (f *tabFake) CloseNeedsConfirm(i int) (bool, error) {
	if i < 0 || i >= len(f.names) {
		return false, errors.New("no such tab")
	}
	return f.dirty[i], nil
}

func (f *tabFake) CloseTab(i int, force bool) error {
	if i < 0 || i >= len(f.names) {
		return errors.New("no such tab")
	}
	if f.dirty[i] && !force {
		return editor.ErrUnsaved
	}
	f.closed = append(f.closed, i)
	f.names = append(f.names[:i], f.names[i+1:]...)
	f.dirty = append(f.dirty[:i], f.dirty[i+1:]...)
	f.active = max(min(f.active, len(f.names)-1), 0)
	f.sync()
	return nil
}

func (f *tabFake) Quit(force bool) []editor.Dirty {
	var dirty []editor.Dirty
	for i, d := range f.dirty {
		if d && !force {
			dirty = append(dirty, editor.Dirty{Index: i, Name: f.names[i]})
		}
	}
	if len(dirty) > 0 {
		return dirty
	}
	f.quit = true
	return nil
}

// fakeDialogs records the dialogs the root opens. Each one is a dialogOverlay that
// closes on the first message it gets.
type fakeDialogs struct {
	opened []dialogCall
}

type dialogCall struct {
	kind string
	text string
	tag  any
}

func (d *fakeDialogs) Unsaved(name string, tag any, _ style.Overlay) Overlay {
	d.opened = append(d.opened, dialogCall{kind: "unsaved", text: name, tag: tag})
	return &dialogOverlay{}
}

func (d *fakeDialogs) SaveAs(path string, tag any, _ style.Overlay) Overlay {
	d.opened = append(d.opened, dialogCall{kind: "saveas", text: path, tag: tag})
	return &dialogOverlay{}
}

// last returns the most recent dialog, and fails the test when there is none.
func (d *fakeDialogs) last(t *testing.T) dialogCall {
	t.Helper()
	if len(d.opened) == 0 {
		t.Fatal("no dialog was opened")
	}
	return d.opened[len(d.opened)-1]
}

type dialogOverlay struct{}

func (*dialogOverlay) Update(tea.Msg) (Overlay, tea.Cmd) { return nil, nil }
func (*dialogOverlay) Draw(*grid.Grid)                   {}
func (*dialogOverlay) Active() bool                      { return true }

// shellModel is a model over tf with the dialog fake wired and an explorer
// rooted at /ws.
func shellModel(t *testing.T, tf *tabFake, d *fakeDialogs) *Model {
	t.Helper()
	tf.quitAction = "quit"
	m := New(Deps{
		Editor: tf, Explorer: newFakeTree(3), Dialogs: d, Keys: testKeys(),
		Theme: theme.Theme{}, Clock: testClock,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// runCmd runs cmd and returns the messages it produces, looking into batches.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// drain runs cmd and feeds its messages back into m.
func drain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for _, msg := range runCmd(cmd) {
		m.Update(msg)
	}
}

// answer sends a dialog answer for the most recent dialog and returns the
// command the root started.
func answer(m *Model, d *fakeDialogs, c dialog.Choice, text string) tea.Cmd {
	last := d.opened[len(d.opened)-1]
	_, cmd := m.Update(dialog.Result{Tag: last.tag, Choice: c, Text: text})
	return cmd
}

func TestCloseDirtyTabAsksAndDiscardCloses(t *testing.T) {
	tf := newTabFake("a.txt", "b.txt")
	tf.dirty[1] = true
	tf.ActivateTab(1)
	d := &fakeDialogs{}
	m := shellModel(t, tf, d)

	m.dispatch(Command{Action: "close_tab"})
	if len(tf.closed) != 0 {
		t.Fatalf("a dirty tab closed before the dialog answered: %v", tf.closed)
	}
	if got := d.last(t); got.kind != "unsaved" || got.text != "b.txt" {
		t.Fatalf("dialog = %+v, want unsaved b.txt", got)
	}
	answer(m, d, dialog.ChoiceDiscard, "")
	if len(tf.closed) != 1 || tf.closed[0] != 1 {
		t.Fatalf("closed = %v, want [1]", tf.closed)
	}
}

func TestCancelKeepsDirtyTab(t *testing.T) {
	tf := newTabFake("a.txt")
	tf.dirty[0] = true
	d := &fakeDialogs{}
	m := shellModel(t, tf, d)
	m.dispatch(Command{Action: "close_tab"})
	answer(m, d, dialog.ChoiceCancel, "")
	if len(tf.closed) != 0 {
		t.Fatalf("cancel closed the tab: %v", tf.closed)
	}
}

func TestCloseCleanTabDoesNotAsk(t *testing.T) {
	tf := newTabFake("a.txt", "b.txt")
	d := &fakeDialogs{}
	m := shellModel(t, tf, d)
	m.dispatch(Command{Action: "close_tab"})
	if len(d.opened) != 0 || len(tf.closed) != 1 {
		t.Fatalf("dialogs %v closed %v, want no dialog and one close", d.opened, tf.closed)
	}
}

func TestForcedCloseGoesToTheEditor(t *testing.T) {
	tf := newTabFake("a.txt")
	tf.dirty[0] = true
	d := &fakeDialogs{}
	m := shellModel(t, tf, d)
	m.dispatch(Command{Action: "close_tab", Args: map[string]any{"force": true}})
	if len(d.opened) != 0 || len(tf.actions) != 1 || tf.actions[0] != "close_tab" {
		t.Fatalf("forced close: dialogs %v actions %v", d.opened, tf.actions)
	}
}

func TestQuitAsksEachDirtyTabInOrderThenQuits(t *testing.T) {
	tf := newTabFake("a.txt", "b.txt")
	tf.dirty = []bool{true, true}
	d := &fakeDialogs{}
	m := shellModel(t, tf, d)

	cmd := m.dispatch(Command{Action: "quit"})
	if cmd != nil || tf.quit {
		t.Fatalf("quit with dirty tabs returned %v and quit=%v", cmd, tf.quit)
	}
	if got := d.last(t); got.text != "a.txt" {
		t.Fatalf("first dialog = %+v, want a.txt", got)
	}
	// Save writes a.txt, and the loop asks about b.txt next.
	drain(t, m, answer(m, d, dialog.ChoiceSave, ""))
	if got := d.last(t); got.text != "b.txt" {
		t.Fatalf("second dialog = %+v, want b.txt", got)
	}
	// Cancel stops the quit.
	drain(t, m, answer(m, d, dialog.ChoiceCancel, ""))
	if tf.quit {
		t.Fatal("cancel quit the editor")
	}
}

func TestQuitWithoutDirtyTabsQuitsAtOnce(t *testing.T) {
	tf := newTabFake("a.txt")
	m := shellModel(t, tf, &fakeDialogs{})
	msgs := runCmd(m.dispatch(Command{Action: "quit"}))
	if len(msgs) != 1 {
		t.Fatalf("quit produced %v, want tea.Quit", msgs)
	}
	if _, ok := msgs[0].(tea.QuitMsg); !ok {
		t.Fatalf("quit produced %T, want tea.QuitMsg", msgs[0])
	}
}

func TestDiscardOnLastDirtyTabQuits(t *testing.T) {
	tf := newTabFake("a.txt")
	tf.dirty[0] = true
	d := &fakeDialogs{}
	m := shellModel(t, tf, d)
	m.dispatch(Command{Action: "quit"})
	msgs := runCmd(answer(m, d, dialog.ChoiceDiscard, ""))
	if len(msgs) != 1 {
		t.Fatalf("discard produced %v, want tea.Quit", msgs)
	}
	if _, ok := msgs[0].(tea.QuitMsg); !ok {
		t.Fatalf("discard produced %T, want tea.QuitMsg", msgs[0])
	}
}

func TestSaveOnUntitledOpensSaveAsWithPrefill(t *testing.T) {
	tf := newTabFake("Untitled-1")
	tf.untitled = true
	tf.dirty[0] = true
	d := &fakeDialogs{}
	m := shellModel(t, tf, d)

	m.dispatch(Command{Action: "save"})
	if got := d.last(t); got.kind != "saveas" || got.text != "/ws/a.txt.txt" {
		t.Fatalf("dialog = %+v, want Save As prefilled under the root", got)
	}
	answer(m, d, dialog.ChoiceSave, "/ws/new.txt")
	last := tf.args[len(tf.args)-1]
	if tf.actions[len(tf.actions)-1] != "save_as" || last["path"] != "/ws/new.txt" {
		t.Fatalf("actions %v args %v, want save_as to /ws/new.txt", tf.actions, last)
	}
}

func TestSaveAsExistingFileReplacesOnlyWhenConfirmed(t *testing.T) {
	tf := newTabFake("a.txt")
	tf.dirty[0] = true
	tf.exists = true
	d := &fakeDialogs{}
	m := shellModel(t, tf, d)

	m.dispatch(Command{Action: "save_as"})
	answer(m, d, dialog.ChoiceSave, "/ws/b.txt")
	if m.notice == "" || tf.dirty[0] != true {
		t.Fatalf("an existing file was written without the confirm: notice %q dirty %v", m.notice, tf.dirty[0])
	}
	m.dispatch(Command{Action: "save_as"})
	answer(m, d, dialog.ChoiceReplace, "/ws/b.txt")
	last := tf.args[len(tf.args)-1]
	if force, _ := last["force"].(bool); !force || last["path"] != "/ws/b.txt" {
		t.Fatalf("replace args = %v, want a forced save to /ws/b.txt", last)
	}
	if tf.dirty[0] {
		t.Fatal("the confirmed replace did not save the tab")
	}
}

func TestNilDialogsNoticesInsteadOfOpening(t *testing.T) {
	tf := newTabFake("a.txt")
	tf.dirty[0] = true
	m := New(Deps{Editor: tf, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.dispatch(Command{Action: "close_tab"})
	if len(tf.closed) != 0 || m.notice == "" {
		t.Fatalf("closed %v notice %q, want no close and a notice", tf.closed, m.notice)
	}
}

// tabX returns the first column of tab i in the strip, or -1.
func tabX(m *Model, i int) int {
	for _, r := range m.layout().strip.runs {
		if r.kind == hitTab && r.index == i {
			return r.x
		}
	}
	return -1
}

func TestTabClickActivatesAndMiddleClickCloses(t *testing.T) {
	tf := newTabFake("a.txt", "b.txt")
	m := shellModel(t, tf, &fakeDialogs{})

	m.Update(click(tabX(m, 1), 1, tea.MouseLeft))
	if tf.active != 1 {
		t.Fatalf("active tab = %d after a click on tab 1", tf.active)
	}
	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: tabX(m, 0), Y: 1, Button: tea.MouseMiddle}))
	drain(t, m, cmd)
	if len(tf.closed) != 1 || tf.closed[0] != 0 {
		t.Fatalf("middle click closed %v, want [0]", tf.closed)
	}
}

func TestClickOnTabCloseButtonClosesCleanTab(t *testing.T) {
	tf := newTabFake("a.txt", "b.txt")
	m := shellModel(t, tf, &fakeDialogs{})
	x := -1
	for _, r := range m.layout().strip.runs {
		if r.kind == hitTabClose && r.index == 1 {
			x = r.x
		}
	}
	if x < 0 {
		t.Fatal("the × of tab 1 is not drawn")
	}
	m.Update(click(x, 1, tea.MouseLeft))
	if len(tf.closed) != 1 || tf.closed[0] != 1 {
		t.Fatalf("× closed %v, want [1]", tf.closed)
	}
}

func TestF6CyclesMenuTabsExplorerEditor(t *testing.T) {
	tf := newTabFake("a.txt")
	m := shellModel(t, tf, &fakeDialogs{})
	press(m, ctrl('b'))
	if m.stage() != stageExplorer {
		t.Fatalf("after Ctrl+B the stage is %v, want the explorer", m.stage())
	}
	want := []focusStage{stageEditor, stageMenu, stageTabs, stageExplorer}
	for _, w := range want {
		press(m, special(tea.KeyF6))
		if m.stage() != w {
			t.Fatalf("F6 stage = %v, want %v", m.stage(), w)
		}
	}
}

func TestStartupShowsExplorerWhenAskedFor(t *testing.T) {
	tf := newTabFake("a.txt")
	m := New(Deps{Editor: tf, Explorer: newFakeTree(3), ShowExplorer: true, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	if !m.explorerOpen {
		t.Fatal("ShowExplorer did not open the explorer")
	}
	m = New(Deps{Editor: tf, Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	if m.explorerOpen {
		t.Fatal("the explorer opened without ShowExplorer")
	}
}

func TestExplorerDividerDragResizesWithinBounds(t *testing.T) {
	tf := newTabFake("a.txt")
	m := shellModel(t, tf, &fakeDialogs{})
	press(m, ctrl('b'))
	l := m.layout()
	grip := l.ex - 1
	_, cmd := m.Update(click(grip, 5, tea.MouseLeft))
	if cmd != nil {
		t.Fatalf("pressing the grip started %v", cmd)
	}
	_, cmd = m.Update(tea.MouseMotionMsg(tea.Mouse{X: 50, Y: 5, Button: tea.MouseLeft}))
	drain(t, m, cmd)
	if got := m.layout().ex; got != 51 {
		t.Fatalf("explorer width after the drag = %d, want 51", got)
	}
	_, cmd = m.Update(tea.MouseMotionMsg(tea.Mouse{X: 90, Y: 5, Button: tea.MouseLeft}))
	drain(t, m, cmd)
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 90, Y: 5, Button: tea.MouseLeft}))
	if got := m.layout().ex; got != 60 {
		t.Fatalf("explorer width past the maximum = %d, want 60", got)
	}
}

func TestEightyByTwentyFourLayoutDefaultsToThirtyTwoColumns(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{explorerOpen: true, lines: 10})
	if l.ex != 32 || l.editorX != 32 || l.bodyTop != 2 || l.bodyBottom != 22 || l.statusY != 23 {
		t.Fatalf("80x24: ex %d editorX %d body %d-%d status %d",
			l.ex, l.editorX, l.bodyTop, l.bodyBottom, l.statusY)
	}
}
