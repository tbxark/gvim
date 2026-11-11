package tui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/theme"
)

// multiFake is an EditorState with one document per tab. ActivateTab moves the
// active tab, and CloseTab removes a tab the way the editor does. DocAt gives the
// documents of the other tabs, as the app adapter does.
type multiFake struct {
	docs   []*fakeDoc
	active int
	rows   int
	cols   int
}

type fakeFolderDoc struct {
	*fakeDoc
	folds []fold.Range
}

func (d *fakeFolderDoc) Folds(tabWidth int) []fold.Range {
	return d.folds
}

func (d *fakeFolderDoc) Folded(line int) bool {
	return false
}

func TestStickyScrollPaintsHeader(t *testing.T) {
	m := New(Deps{})
	m.width = 80
	m.height = 24
	g := grid.New(80, 24)
	base := &fakeDoc{lines: []string{"func MyFunction() {", "    line 1", "    line 2", "    line 3", "}"}}
	doc := &fakeFolderDoc{
		fakeDoc: base,
		folds: []fold.Range{
			{Start: 0, End: 4},
		},
	}
	p := splitPane{
		X: 0, Y: 1, W: 80, H: 20, Top: 2,
	}
	m.paintStickyScroll(g, p, doc)
	row := g.Plain()[1]
	if !strings.Contains(row, "func MyFunction()") {
		t.Fatalf("row 1 = %q, want sticky header containing 'func MyFunction()'", row)
	}

	// When toggled off, sticky scroll paints nothing
	m.stickyScroll = false
	g2 := grid.New(80, 24)
	m.paintStickyScroll(g2, p, doc)
	row2 := g2.Plain()[1]
	if strings.Contains(row2, "func MyFunction()") {
		t.Fatalf("row 1 = %q, want empty when sticky scroll is disabled", row2)
	}
}

type fakePathDoc struct {
	*fakeDoc
	path string
}

func (d *fakePathDoc) FilePath() string {
	return d.path
}

func TestOverviewRulerMarksWithDiagnostics(t *testing.T) {
	m := New(Deps{})
	m.lsp.diags = map[string][]Diagnostic{
		"/work/a.go": {
			{Start: buffer.Pos{Line: 10, Col: 0}, End: buffer.Pos{Line: 10, Col: 5}, Severity: editorpane.SeverityError},
			{Start: buffer.Pos{Line: 25, Col: 0}, End: buffer.Pos{Line: 25, Col: 5}, Severity: editorpane.SeverityWarning},
		},
	}
	doc := &fakePathDoc{fakeDoc: &fakeDoc{lines: make([]string, 100)}, path: "/work/a.go"}
	pane := editorpane.Pane{Styles: m.panes}
	pane = m.overviewRulerMarks(pane, doc)
	if len(pane.TrackMarks) != 2 {
		t.Fatalf("track marks count = %d, want 2", len(pane.TrackMarks))
	}
	if pane.TrackMarks[0].From != 10 || pane.TrackMarks[1].From != 25 {
		t.Fatalf("track marks = %+v, want lines 10 and 25", pane.TrackMarks)
	}
}

// newMultiFake returns a fake with n tabs of lines lines each. Line j of tab i is
// "t<i> <j>", so a test can tell the tabs apart by their text.
func newMultiFake(n, lines int) *multiFake {
	f := &multiFake{}
	for i := range n {
		var text []string
		for j := range lines {
			text = append(text, "t"+strconv.Itoa(i)+" "+strconv.Itoa(j))
		}
		f.docs = append(f.docs, &fakeDoc{lines: text})
	}
	return f
}

// newDocsFake returns a fake with one tab per slice of lines.
func newDocsFake(docs ...[]string) *multiFake {
	f := &multiFake{}
	for _, lines := range docs {
		f.docs = append(f.docs, &fakeDoc{lines: lines})
	}
	return f
}

func (f *multiFake) doc() *fakeDoc { return f.docs[f.active] }

func (f *multiFake) SetViewport(rows, cols int) { f.rows, f.cols = rows, cols }

// Do runs close_tab and its alias close on the active tab, as the editor does. Any
// other action is unknown to the fake, as it is to the app adapter, so Run reports it
// as unsupported.
func (f *multiFake) Do(a editor.Action, args map[string]any) editor.Result {
	switch a {
	case "close_tab", "close":
		force, _ := args["force"].(bool)
		if err := f.CloseTab(f.active, force); err != nil {
			return editor.Result{Err: err}
		}
		return editor.Result{}
	}
	return editor.Result{Err: fmt.Errorf("%w: %s", editor.ErrUnknownAction, a)}
}

func (f *multiFake) Active() Document {
	if len(f.docs) == 0 {
		return nil
	}
	return f.docs[f.active]
}

func (f *multiFake) Tabs() []TabInfo {
	tabs := make([]TabInfo, len(f.docs))
	for i := range f.docs {
		tabs[i] = TabInfo{Name: "t" + strconv.Itoa(i), Active: i == f.active}
	}
	return tabs
}

func (f *multiFake) Status() string { return "" }

func (f *multiFake) SetCursor(p buffer.Pos, _ bool) { f.doc().cursor = p }

func (f *multiFake) ExtendSelection(p buffer.Pos, _ editor.Unit) { f.doc().cursor = p }

func (f *multiFake) ScrollBy(rows int) { f.ScrollTo(f.doc().top + rows) }

func (f *multiFake) ScrollTo(top int) {
	d := f.doc()
	d.top = min(max(top, 0), max(len(d.lines)-1, 0))
}

func (f *multiFake) ActivateTab(i int) {
	if i >= 0 && i < len(f.docs) {
		f.active = i
	}
}

func (f *multiFake) CloseTab(i int, _ bool) error {
	if i < 0 || i >= len(f.docs) {
		return errors.New("no such tab")
	}
	f.docs = slices.Delete(f.docs, i, i+1)
	switch {
	case len(f.docs) == 0:
		f.active = 0
	case i < f.active:
		f.active--
	case i == f.active:
		f.active = min(f.active, len(f.docs)-1)
	}
	return nil
}

func (f *multiFake) CloseNeedsConfirm(int) (bool, error) { return false, nil }

func (f *multiFake) Quit(bool) []editor.Dirty { return nil }

func (f *multiFake) DocAt(i int) Document {
	if i < 0 || i >= len(f.docs) {
		return nil
	}
	return f.docs[i]
}

// newSplitModel returns a root model over f, sized w by h with the explorer hidden.
func newSplitModel(t *testing.T, f *multiFake, w, h int) *Model {
	t.Helper()
	m := New(Deps{Editor: f, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// panesOf returns the panes of the layout, and fails the test when there are fewer
// than n of them.
func panesOf(t *testing.T, m *Model, n int) []splitPane {
	t.Helper()
	ps := m.layout().Panes
	if len(ps) < n {
		t.Fatalf("%d panes, want at least %d", len(ps), n)
	}
	return ps
}

func TestSplitActionsShareTheTabInTheNewPane(t *testing.T) {
	tests := []struct {
		action  string
		stacked bool // the new pane is below the first one, not beside it
	}{
		{"split_horizontal", true},
		{"split_vertical", false},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			f := newMultiFake(2, 30)
			m := newSplitModel(t, f, 80, 24)
			f.SetCursor(buffer.Pos{Line: 6}, false)
			f.ScrollTo(5) // the caret on line 6 is inside the window, so no pane has to scroll
			runAction(t, m, tt.action)
			l := m.layout()
			if len(l.Panes) != 2 {
				t.Fatalf("%d panes, want 2", len(l.Panes))
			}
			first, second := l.Panes[0], l.Panes[1]
			if first.Tab != 0 || second.Tab != 0 {
				t.Errorf("tabs %d and %d, want both 0", first.Tab, second.Tab)
			}
			if first.Top != 5 || second.Top != 5 {
				t.Errorf("tops %d and %d, want both 5", first.Top, second.Top)
			}
			if !second.Focused || first.Focused || l.Focus != 1 {
				t.Errorf("focus on pane %d, want the new pane 1", l.Focus)
			}
			if stacked := second.Y > first.Y; stacked != tt.stacked {
				t.Errorf("stacked = %v, want %v: panes %+v and %+v", stacked, tt.stacked, first, second)
			}
		})
	}
}

func TestSplitRefusesWhenThePaneIsTooSmall(t *testing.T) {
	tests := []struct {
		name   string
		w, h   int
		action string
		panes  int
		notice string // the notice the split leaves, empty when it splits
	}{
		// At H=6 the body is one row, so a stacked split has no room.
		{"stacked in a one-row body", 40, 6, "split_horizontal", 1, "Not enough room to split"},
		{"side by side in the same body", 40, 6, "split_vertical", 2, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newSplitModel(t, newMultiFake(2, 30), tt.w, tt.h)
			runAction(t, m, tt.action)
			if got := len(m.layout().Panes); got != tt.panes {
				t.Errorf("%d panes, want %d", got, tt.panes)
			}
			if m.notice != tt.notice {
				t.Errorf("notice = %q, want %q", m.notice, tt.notice)
			}
		})
	}
}

func TestSplitNeedsAnOpenTab(t *testing.T) {
	m := newSplitModel(t, newMultiFake(0, 0), 80, 24)
	runAction(t, m, "split_vertical")
	if n := len(m.layout().Panes); n != 1 {
		t.Errorf("%d panes with no tab, want 1", n)
	}
	if m.notice != noDocumentNotice {
		t.Errorf("notice = %q, want %q", m.notice, noDocumentNotice)
	}
}

func TestNextAndPrevSplitMoveAlongTheReadingOrder(t *testing.T) {
	f := newMultiFake(3, 30)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "split_vertical")   // left shows t0, right has the focus
	m.activateTab(1)                    // the right pane shows t1
	runAction(t, m, "split_horizontal") // reading order: left t0, right top t1, right bottom t1, focused
	tests := []struct {
		action string
		focus  int // the reading index of the focused pane
		tab    int // the active tab
	}{
		{"next_split", 0, 0},
		{"next_split", 1, 1},
		{"next_split", 2, 1},
		{"next_split", 0, 0}, // wraps at the end
		{"prev_split", 2, 1}, // wraps at the start
		{"prev_split", 1, 1},
		{"prev_split", 0, 0},
	}
	for _, tt := range tests {
		runAction(t, m, tt.action)
		if got := m.layout().Focus; got != tt.focus {
			t.Errorf("after %s: focus %d, want %d", tt.action, got, tt.focus)
		}
		if f.active != tt.tab {
			t.Errorf("after %s: active tab %d, want %d", tt.action, f.active, tt.tab)
		}
	}
}

func TestFocusChangeRestoresEachPanesTabAndTop(t *testing.T) {
	f := newMultiFake(2, 30)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "split_vertical") // the right pane has the focus and shows t0
	m.activateTab(1)                  // the right pane shows t1
	m.ed.SetCursor(buffer.Pos{Line: 2}, false)
	m.ed.ScrollTo(2) // its top is 2, and the caret on line 2 is in its window

	runAction(t, m, "next_split") // the left pane takes the focus, at its own top 0
	ps := panesOf(t, m, 2)
	if m.layout().Focus != 0 || f.active != 0 || f.docs[0].top != 0 {
		t.Fatalf("left focus: focus %d active %d top %d, want 0, 0, 0", m.layout().Focus, f.active, f.docs[0].top)
	}
	if ps[1].Tab != 1 || ps[1].Top != 2 {
		t.Errorf("right pane after the focus left it: tab %d top %d, want 1, 2", ps[1].Tab, ps[1].Top)
	}

	m.ed.ScrollTo(4)              // the left pane scrolls to 4
	runAction(t, m, "next_split") // the right pane takes the focus back at its top 2
	ps = panesOf(t, m, 2)
	if m.layout().Focus != 1 || f.active != 1 || f.docs[1].top != 2 {
		t.Fatalf("right focus: focus %d active %d top %d, want 1, 1, 2", m.layout().Focus, f.active, f.docs[1].top)
	}
	if ps[0].Tab != 0 || ps[0].Top != 4 {
		t.Errorf("left pane after the focus left it: tab %d top %d, want 0, 4", ps[0].Tab, ps[0].Top)
	}
}

func TestClickFocusesThePaneUnderThePointer(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "split_vertical")
	runAction(t, m, "next_split") // the left pane has the focus
	tests := []struct {
		x, y, focus int
	}{
		{60, 5, 1},  // a press in the right pane focuses it
		{40, 5, 1},  // the border takes no focus
		{10, 5, 0},  // a press in the left pane focuses it
		{40, 21, 0}, // the bottom cell of the border takes no focus
	}
	for _, tt := range tests {
		m.Update(click(tt.x, tt.y, tea.MouseLeft))
		if got := m.layout().Focus; got != tt.focus {
			t.Errorf("press at (%d, %d): focus %d, want %d", tt.x, tt.y, got, tt.focus)
		}
	}
}

func TestCaretFollowsTheFocusedPane(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "split_vertical") // the right pane has the focus
	if x, y, ok := m.caret(); !ok || x != 47 || y != 3 {
		t.Errorf("caret in the right pane = (%d, %d, %v), want (47, 3, true)", x, y, ok)
	}
	runAction(t, m, "next_split") // the left pane has the focus
	m.ed.SetCursor(buffer.Pos{Line: 3, Col: 2}, false)
	if x, y, ok := m.caret(); !ok || x != 9 || y != 6 {
		t.Errorf("caret in the left pane = (%d, %d, %v), want (9, 6, true)", x, y, ok)
	}
}

func TestFocusingAPaneRevealsTheCaret(t *testing.T) {
	m := newSplitModel(t, newMultiFake(1, 30), 80, 24)
	runAction(t, m, "split_vertical") // the right pane has the focus
	m.ed.SetCursor(buffer.Pos{Line: 25}, false)
	m.ed.ScrollTo(20) // the right pane shows lines 20 and on, so the caret is on row 5
	runAction(t, m, "next_split")
	// The left pane's own top is 0, which hides line 25. Focusing it scrolls the
	// pane so that the caret is on its last row.
	x, y, ok := m.caret()
	if !ok || x != 7 || y != 21 {
		t.Errorf("caret after focusing the left pane = (%d, %d, %v), want (7, 21, true)", x, y, ok)
	}
}

func TestSplittingRevealsTheCaretInTheNewPane(t *testing.T) {
	m := newSplitModel(t, newMultiFake(1, 30), 80, 24)
	m.ed.SetCursor(buffer.Pos{Line: 15}, false)
	// The new bottom pane has 10 rows, so line 15 is off its first rows.
	runAction(t, m, "split_horizontal")
	if x, y, ok := m.caret(); !ok || y < 13 || y > 21 {
		t.Errorf("caret in the new pane = (%d, %d, %v), want a row in 13..21", x, y, ok)
	}
}

func TestWheelScrollsOnlyThePaneUnderThePointer(t *testing.T) {
	f := newMultiFake(1, 30)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "split_vertical") // the right pane has the focus, both show t0

	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 10, Y: 5, Button: tea.MouseWheelDown})) // the unfocused left pane
	ps := panesOf(t, m, 2)
	if ps[0].Top != wheelRows || f.docs[0].top != 0 {
		t.Errorf("left wheel: pane top %d, document top %d, want %d and 0", ps[0].Top, f.docs[0].top, wheelRows)
	}

	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 60, Y: 5, Button: tea.MouseWheelDown})) // the focused right pane
	ps = panesOf(t, m, 2)
	if ps[1].Top != wheelRows || f.docs[0].top != wheelRows || ps[0].Top != wheelRows {
		t.Errorf("right wheel: tops %d and %d, document top %d, want %d", ps[0].Top, ps[1].Top, f.docs[0].top, wheelRows)
	}
}

func TestPaintsEachPaneAndTheBorder(t *testing.T) {
	f := newMultiFake(2, 3)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "split_vertical") // the right pane has the focus
	m.activateTab(1)                  // so the right pane shows t1 and the left one still shows t0

	g := m.paint()
	// The text starts after the six-cell gutter. The tab index is the second
	// character of the first line.
	if got := g.At(8, 3).Text; got != "0" {
		t.Errorf("left pane shows %q at (8, 3), want the t0 text", got)
	}
	if got := g.At(48, 3).Text; got != "1" {
		t.Errorf("right pane shows %q at (48, 3), want the t1 text", got)
	}
	if got := g.At(40, 3).Text; got != splitVLine {
		t.Errorf("border at (40, 3) = %q, want %q", got, splitVLine)
	}
	if got := g.At(40, 21).Text; got != splitVLine {
		t.Errorf("border at (40, 21) = %q, want %q", got, splitVLine)
	}
}

func TestCloseFocusedSplit(t *testing.T) {
	tests := []struct {
		name   string
		setup  []string // actions run before close_split
		panes  int
		focus  int
		notice bool // the close leaves a notice
	}{
		{"the first of two is closed", []string{"split_vertical", "next_split"}, 1, 0, false},
		{"the middle of three is closed", []string{"split_vertical", "split_horizontal", "prev_split"}, 2, 1, false},
		{"the last of three is closed", []string{"split_vertical", "split_horizontal"}, 2, 1, false},
		{"the only pane is kept", nil, 1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMultiFake(2, 30)
			m := newSplitModel(t, f, 80, 24)
			for _, a := range tt.setup {
				runAction(t, m, a)
			}
			runAction(t, m, "close_split")
			l := m.layout()
			if len(l.Panes) != tt.panes || l.Focus != tt.focus {
				t.Fatalf("%d panes with focus %d, want %d with focus %d", len(l.Panes), l.Focus, tt.panes, tt.focus)
			}
			if f.active != l.Panes[l.Focus].Tab {
				t.Errorf("active tab %d does not match the focused pane's tab %d", f.active, l.Panes[l.Focus].Tab)
			}
			if (m.notice != "") != tt.notice {
				t.Errorf("notice = %q, want a notice: %v", m.notice, tt.notice)
			}
		})
	}
}

func TestTabCloseMovesTheLeavesWithTheirTabs(t *testing.T) {
	tests := []struct {
		name      string
		leafTab   int    // the tab the unfocused pane shows before the close
		closeTab  int    // the tab that closes
		wantTab   int    // the tab index the pane shows after the close
		wantFirst string // the first line of the document it shows
	}{
		{"a later tab moves down", 2, 0, 1, "t2 0"},
		{"an earlier tab does not move", 1, 2, 1, "t1 0"},
		{"the shown tab closes and the next one takes its index", 1, 1, 1, "t2 0"},
		{"the shown last tab closes", 2, 2, 1, "t1 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMultiFake(3, 3)
			m := newSplitModel(t, f, 80, 24)
			runAction(t, m, "split_vertical") // the right pane has the focus, the left shows t0
			runAction(t, m, "next_split")     // the left pane has the focus
			m.activateTab(tt.leafTab)         // the left pane shows the tab under test
			runAction(t, m, "next_split")     // the right pane takes the focus, the left keeps its tab

			m.requestClose(tt.closeTab)
			left := panesOf(t, m, 1)[0]
			if left.Tab != tt.wantTab {
				t.Fatalf("left pane shows tab %d, want %d", left.Tab, tt.wantTab)
			}
			doc := m.docAt(left.Tab)
			if doc == nil || doc.Line(0) != tt.wantFirst {
				t.Errorf("left pane document starts %q, want %q", docLineZero(doc), tt.wantFirst)
			}
		})
	}
}

// docLineZero returns the first line of doc, or "" when doc is nil.
func docLineZero(doc Document) string {
	if doc == nil {
		return ""
	}
	return doc.Line(0)
}

func TestUnexpectedTabLossCollapsesTheSplits(t *testing.T) {
	f := newMultiFake(2, 3)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "split_vertical")
	// A close that does not go through the root cannot say which leaf moves.
	if err := f.CloseTab(0, true); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if n := len(m.layout().Panes); n != 1 {
		t.Errorf("%d panes after an unseen close, want 1", n)
	}
	if !m.splits.Root.Leaf() || m.splits.Focus != m.splits.Root {
		t.Error("the tree is not the focused leaf after the collapse")
	}
}

func TestClosingEveryTabCollapsesTheSplits(t *testing.T) {
	f := newMultiFake(2, 3)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "split_vertical")
	m.requestClose(0)
	m.requestClose(0)
	if n := len(f.docs); n != 0 {
		t.Fatalf("%d tabs left, want 0", n)
	}
	if n := len(m.layout().Panes); n != 1 || !m.splits.Root.Leaf() {
		t.Errorf("%d panes and leaf %v with no tab, want one pane", n, m.splits.Root.Leaf())
	}
}

func TestSplitAndCompareActionsAreHandled(t *testing.T) {
	actions := []string{
		"split_horizontal", "split_vertical", "next_split", "prev_split", "close_split",
		"compare_next_difference", "compare_prev_difference", "compare_switch_side",
	}
	for _, a := range actions {
		m := newSplitModel(t, newMultiFake(2, 3), 80, 24)
		if err := m.Run(a); err != nil {
			t.Errorf("Run(%q) = %v, want the action handled", a, err)
		}
	}
}
