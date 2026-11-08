package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/theme"
)

// ToggleFoldAt makes realEditor the foldToggler the app adapter is.
func (r realEditor) ToggleFoldAt(line int) bool { return r.ed.ToggleFoldAt(line) }

// mouseAt sends a left press with the modifiers mod at the text cell (col, row) of the
// body, where row 0 is the first body row.
func mouseAt(m *Model, col, row int, mod tea.KeyMod) {
	l := m.layout()
	m.Update(tea.MouseClickMsg(tea.Mouse{X: l.TextX + col, Y: l.BodyTop + row, Button: tea.MouseLeft, Mod: mod}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: l.TextX + col, Y: l.BodyTop + row, Button: tea.MouseLeft}))
}

func selectionOf(ed *editor.Editor) (anchor, head buffer.Pos, ok bool) {
	d := ed.Active()
	if d.Sel == nil {
		return buffer.Pos{}, buffer.Pos{}, false
	}
	return d.Sel.Anchor, d.Sel.Head, true
}

func TestModifierClickExtendsTheSelection(t *testing.T) {
	tests := []struct {
		name string
		mod  tea.KeyMod
		want bool // the click extends the selection
	}{
		{"shift", tea.ModShift, true},
		{"ctrl", tea.ModCtrl, true},
		{"plain", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, ed := viewModel(t, DefaultViewOptions(), "hello world\nsecond line\nthird\n", 60, 12)
			mouseAt(m, 2, 0, 0)
			mouseAt(m, 4, 1, tt.mod)
			a, h, ok := selectionOf(ed)
			if ok != tt.want {
				t.Fatalf("selection = %v %v %v, want selection %v", a, h, ok, tt.want)
			}
			if tt.want && (a != (buffer.Pos{Line: 0, Col: 2}) || h != (buffer.Pos{Line: 1, Col: 4})) {
				t.Fatalf("selection = %v to %v, want 0:2 to 1:4", a, h)
			}
			if got := ed.Active().Cursor; !tt.want && got != (buffer.Pos{Line: 1, Col: 4}) {
				t.Fatalf("caret = %v, want 1:4", got)
			}
		})
	}
}

func TestShiftClickKeepsTheAnchorAndSecondaryCarets(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "hello world\nsecond line\nthird\n", 60, 12)
	mouseAt(m, 2, 0, 0)
	if r := ed.Do("add_cursor_below", nil); r.Err != nil {
		t.Fatal(r.Err)
	}
	mouseAt(m, 4, 2, tea.ModShift)
	if n := len(ed.Active().Cursors()); n != 1 {
		t.Fatalf("extra carets after a shift-click = %d, want 1 kept", n)
	}
	mouseAt(m, 1, 0, tea.ModShift)
	if a, _, ok := selectionOf(ed); !ok || a == (buffer.Pos{}) {
		t.Fatalf("a second shift-click lost the anchor: anchor %v, selection %v", a, ok)
	}
}

func TestPlainClickEndsMultiCaretEditing(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "hello world\nsecond line\nthird\n", 60, 12)
	mouseAt(m, 2, 0, 0)
	if r := ed.Do("add_cursor_below", nil); r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(ed.Active().Cursors()) == 0 {
		t.Fatal("setup: no extra caret")
	}
	mouseAt(m, 3, 2, 0)
	if cs := ed.Active().Cursors(); len(cs) != 0 {
		t.Fatalf("extra carets after a plain click = %v, want none", cs)
	}
	if got := ed.Active().Cursor; got != (buffer.Pos{Line: 2, Col: 3}) {
		t.Fatalf("caret = %v, want 2:3", got)
	}
}

func TestGutterClickTogglesTheFoldWithoutMovingTheCaret(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "func a() {\n\tx := 1\n\ty := 2\n}\nz\n", 60, 12)
	mouseAt(m, 3, 4, 0) // the caret goes to line 4
	l := m.layout()
	gx := l.Panes[0].X + 1
	m.Update(tea.MouseClickMsg(tea.Mouse{X: gx, Y: l.BodyTop, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: gx, Y: l.BodyTop, Button: tea.MouseLeft}))
	if !ed.Active().Folded(0) {
		t.Fatal("the gutter click did not collapse the fold on line 0")
	}
	if got := ed.Active().Cursor; got != (buffer.Pos{Line: 4, Col: 1}) {
		t.Fatalf("caret = %v after the fold click, want it untouched at 4:1", got)
	}
	// A click on a gutter cell of a plain line places the caret as before.
	m.Update(tea.MouseClickMsg(tea.Mouse{X: gx + 1, Y: l.BodyTop + 1, Button: tea.MouseLeft}))
	if ed.Active().Folded(0) != true || ed.Active().Cursor.Line != 3 {
		t.Fatalf("a gutter click on a plain line: folded %v, caret %v", ed.Active().Folded(0), ed.Active().Cursor)
	}
	// Another click on the fold line expands it again.
	m.Update(tea.MouseClickMsg(tea.Mouse{X: gx + 2, Y: l.BodyTop, Button: tea.MouseLeft}))
	if ed.Active().Folded(0) {
		t.Fatal("the second gutter click did not expand the fold")
	}
}

func TestSidewaysWheelPansWithoutPullingBackToTheCaret(t *testing.T) {
	long := strings.Repeat("0123456789", 20)
	m, ed := viewModel(t, DefaultViewOptions(), long+"\nshort\n", 40, 10)
	runAction(t, m, "toggle_line_wrap")
	l := m.layout()
	p := l.Panes[0]
	scroll := func(b tea.MouseButton, mod tea.KeyMod) {
		m.Update(tea.MouseWheelMsg(tea.Mouse{X: p.X + p.Gutter + 2, Y: l.BodyTop + 1, Button: b, Mod: mod}))
	}
	left := func() int {
		pane := m.paneGeometry(m.layout().Panes[0], m.ed.Active())
		v, _ := pane.HScroll(p.W, p.H)
		return v
	}
	if left() != 0 {
		t.Fatalf("setup: left = %d", left())
	}
	scroll(tea.MouseWheelRight, 0)
	if left() != wheelColumns {
		t.Fatalf("left = %d after one notch right, want %d", left(), wheelColumns)
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10}) // another update, caret still at 0:0
	if left() != wheelColumns {
		t.Fatalf("left = %d after an update, the caret pulled the view back", left())
	}
	scroll(tea.MouseWheelDown, tea.ModShift)
	if left() != 2*wheelColumns {
		t.Fatalf("left = %d after shift+wheel down, want %d", left(), 2*wheelColumns)
	}
	scroll(tea.MouseWheelLeft, 0)
	scroll(tea.MouseWheelLeft, 0)
	scroll(tea.MouseWheelLeft, 0)
	if left() != 0 {
		t.Fatalf("left = %d after panning back past the start, want 0", left())
	}
	for range 100 {
		scroll(tea.MouseWheelRight, 0)
	}
	if got, want := left(), len(long)-(p.W-1-p.Gutter); got != want {
		t.Fatalf("left = %d at the far end, want %d", got, want)
	}
	if got := ed.Active().Cursor; got != (buffer.Pos{}) {
		t.Fatalf("caret moved to %v, want it left alone", got)
	}
	// A caret move takes the view back to the caret.
	ed.SetCursor(buffer.Pos{Line: 1, Col: 2}, false)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	if left() != 0 {
		t.Fatalf("left = %d after the caret moved, want the view back at the caret", left())
	}
}

func TestWheelStillScrollsVerticallyWithoutShift(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), strings.Repeat("line\n", 60), 40, 10)
	l := m.layout()
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: l.TextX + 1, Y: l.BodyTop + 1, Button: tea.MouseWheelDown}))
	if got := ed.Active().Top; got != wheelRows {
		t.Fatalf("top = %d after one notch, want %d", got, wheelRows)
	}
}

func TestDragPastTheEdgeKeepsExtendingTheSelection(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), strings.Repeat("line\n", 60), 40, 10)
	l := m.layout()
	rows := l.BodyBottom - l.BodyTop + 1
	mouseDown := tea.Mouse{X: l.TextX + 1, Y: l.BodyTop + 1, Button: tea.MouseLeft}
	m.Update(tea.MouseClickMsg(mouseDown))
	for _, below := range []int{0, 1, 3, 6} {
		mo := mouseDown
		mo.Y = l.BodyBottom + below
		m.Update(tea.MouseMotionMsg(mo))
		_, head, ok := selectionOf(ed)
		if !ok {
			t.Fatalf("no selection with the pointer %d rows below the pane", below)
		}
		if lastShown := ed.Active().Top + rows - 1; head.Line < lastShown && below > 0 {
			t.Fatalf("head line %d with the pointer %d below the pane (rows %d, top %d)", head.Line, below, rows, ed.Active().Top)
		}
		if below == 6 && head.Line < rows+5 {
			t.Fatalf("head line = %d with the pointer 6 rows below, want it past the first screen", head.Line)
		}
	}
	// Dragging back above the pane extends upward again.
	_, before, _ := selectionOf(ed)
	mo := mouseDown
	mo.Y = l.BodyTop - 3
	m.Update(tea.MouseMotionMsg(mo))
	_, head, _ := selectionOf(ed)
	if head.Line >= before.Line {
		t.Fatalf("head line = %d with the pointer above the pane, want it above %d", head.Line, before.Line)
	}
}

func TestDragOvershootCountsFromTheLastTextRowAboveTheHorizontalBar(t *testing.T) {
	tests := []struct {
		name string
		// rowFromBar is the pointer row relative to the bar row: -2 is the row above the
		// last text row, -1 the last text row, 0 the bar row.
		rowFromBar int
		// extra is how many lines past the last text row (or the pointed row, above it)
		// the head lands.
		extra int
	}{
		{"above the last text row", -2, 0},
		{"last text row is one beyond", -1, 1},
		{"bar row is two beyond", 0, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := strings.Repeat("x", 200) + "\n" + strings.Repeat("line\n", 60)
			m, ed := viewModel(t, DefaultViewOptions(), text, 40, 12)
			runAction(t, m, "toggle_line_wrap")
			l := m.layout()
			fp := l.Panes[0]
			body := m.paneGeometry(fp, m.ed.Active()).BodyRows(fp.W, fp.H)
			if body != fp.H-1 {
				t.Fatalf("setup: %d text rows in a %d row pane, want the bar to take one", body, fp.H)
			}
			down := tea.Mouse{X: l.TextX + 1, Y: l.BodyTop + 1, Button: tea.MouseLeft}
			m.Update(tea.MouseClickMsg(down))
			mo := down
			mo.Y = fp.Y + body + tt.rowFromBar
			m.Update(tea.MouseMotionMsg(mo))
			_, head, ok := selectionOf(ed)
			if !ok {
				t.Fatal("no selection after the drag")
			}
			want := min(body+tt.rowFromBar, body-1) + tt.extra
			if head.Line != want {
				t.Fatalf("head line = %d with the pointer on row %d, want %d", head.Line, body+tt.rowFromBar, want)
			}
		})
	}
}

// ScrollMargin makes realEditor the scrollMargined port the app adapter is.
func (r realEditor) ScrollMargin() int { return r.ed.ScrollMargin() }

func TestScrollOffsetCountsDisplayRowsOfWrappedLines(t *testing.T) {
	const margin = 2
	// The viewport top is a line boundary, and a line wraps onto two rows, so the
	// scroll margin lands on a row by line: the caret can keep one row more than the
	// margin wants when the line boundary falls between two rows.
	tests := []struct {
		name      string
		margin    int
		belowFrom int
		belowTo   int
	}{
		{"margin of two rows", margin, margin, margin + 1},
		{"no margin", 0, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each line wraps into two rows in the 40 column pane.
			text := strings.Repeat(strings.Repeat("word ", 12)+"\n", 30)
			path := filepath.Join(t.TempDir(), "doc.txt")
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			ed := editor.New(editor.Options{ScrollOffset: tt.margin})
			if err := ed.OpenFile(path); err != nil {
				t.Fatal(err)
			}
			th, err := theme.Load("dark")
			if err != nil {
				t.Fatal(err)
			}
			o := DefaultViewOptions()
			keys := testKeys()
			keys["normal|Down|"] = Command{Action: "move_down"}
			m := New(Deps{Editor: realEditor{ed}, Keys: keys, Theme: th, Clock: testClock, View: &o})
			m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
			for range 40 {
				press(m, special(tea.KeyDown))
			}
			fp := m.layout().FocusedPane()
			body := m.paneGeometry(fp, m.ed.Active()).BodyRows(fp.W, fp.H)
			_, y, ok := m.paneGeometry(fp, m.ed.Active()).Caret(fp.W, fp.H)
			if !ok {
				t.Fatalf("caret %v is off the body (top %d)", ed.Active().Cursor, ed.Active().Top)
			}
			if below := body - 1 - y; below < tt.belowFrom || below > tt.belowTo {
				t.Fatalf("%d rows below the caret row (row %d of %d), want %d..%d", below, y, body, tt.belowFrom, tt.belowTo)
			}
		})
	}
}

func TestDragHeldOutsideThePaneKeepsScrolling(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), strings.Repeat("line\n", 60), 40, 10)
	l := m.layout()
	down := tea.Mouse{X: l.TextX + 1, Y: l.BodyTop + 1, Button: tea.MouseLeft}
	m.Update(tea.MouseClickMsg(down))
	out := down
	out.Y = l.BodyBottom + 2
	_, cmd := m.Update(tea.MouseMotionMsg(out))
	if cmd == nil {
		t.Fatal("a motion below the pane armed no autoscroll tick")
	}
	seq := m.drag.tick
	if seq == 0 {
		t.Fatal("the drag records no tick in flight")
	}
	_, head, _ := selectionOf(ed)

	// A second motion while the tick is in flight arms no second chain.
	if _, cmd := m.Update(tea.MouseMotionMsg(out)); cmd != nil || m.drag.tick != seq {
		t.Fatalf("a motion with a tick in flight returned cmd %v, tick %d, want none and %d", cmd != nil, m.drag.tick, seq)
	}
	_, head2, _ := selectionOf(ed)
	if head2.Line < head.Line {
		t.Fatalf("head moved back from %v to %v", head, head2)
	}
	head, top := head2, ed.Active().Top

	// The pointer has not moved, and each tick extends the selection further.
	for range 3 {
		_, cmd := m.Update(dragScrollMsg{seq: m.drag.tick})
		if cmd == nil {
			t.Fatal("a tick outside the pane armed no next tick")
		}
		_, next, _ := selectionOf(ed)
		if next.Line <= head.Line || ed.Active().Top <= top {
			t.Fatalf("tick left head %v top %d, want both to grow past %v and %d", next, ed.Active().Top, head, top)
		}
		head, top = next, ed.Active().Top
	}

	// A stale tick does nothing.
	if _, cmd := m.Update(dragScrollMsg{seq: m.drag.tick - 1}); cmd != nil {
		t.Fatal("a stale tick armed a next tick")
	}
	if _, now, _ := selectionOf(ed); now != head {
		t.Fatalf("a stale tick moved the head to %v from %v", now, head)
	}

	// The chain ends once the head cannot move, at the end of the document.
	var ended bool
	for range 200 {
		if _, cmd := m.Update(dragScrollMsg{seq: m.drag.tick}); cmd == nil {
			ended = true
			break
		}
	}
	if !ended {
		t.Fatal("the autoscroll never stopped at the end of the document")
	}
	if _, end, _ := selectionOf(ed); end.Line < 59 {
		t.Fatalf("head stopped at line %d, want the end of the document", end.Line)
	}
}

func TestDragTickIsIgnoredAfterReleaseOrReturn(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), strings.Repeat("line\n", 60), 40, 10)
	l := m.layout()
	down := tea.Mouse{X: l.TextX + 1, Y: l.BodyTop + 1, Button: tea.MouseLeft}
	m.Update(tea.MouseClickMsg(down))
	out := down
	out.Y = l.BodyBottom + 2
	m.Update(tea.MouseMotionMsg(out))
	seq := m.drag.tick

	// The pointer returns inside the pane: the chain ends.
	if _, cmd := m.Update(tea.MouseMotionMsg(down)); cmd != nil || m.drag.tick != 0 {
		t.Fatalf("a motion back inside returned cmd %v, tick %d, want none and 0", cmd != nil, m.drag.tick)
	}
	_, head, _ := selectionOf(ed)
	if _, cmd := m.Update(dragScrollMsg{seq: seq}); cmd != nil {
		t.Fatal("a tick after the pointer came back armed a next tick")
	}
	if _, now, _ := selectionOf(ed); now != head {
		t.Fatalf("a tick after the pointer came back moved the head to %v from %v", now, head)
	}

	// A release ends the gesture, whatever tick is in flight.
	m.Update(tea.MouseMotionMsg(out))
	seq = m.drag.tick
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: out.X, Y: out.Y, Button: tea.MouseLeft}))
	_, head, _ = selectionOf(ed)
	if _, cmd := m.Update(dragScrollMsg{seq: seq}); cmd != nil {
		t.Fatal("a tick after the release armed a next tick")
	}
	if _, now, _ := selectionOf(ed); now != head {
		t.Fatalf("a tick after the release moved the head to %v from %v", now, head)
	}
}
