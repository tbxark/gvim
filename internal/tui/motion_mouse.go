package tui

import (
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/editorpane"
)

// wheelColumns is how many columns one sideways wheel notch pans the view, as
// Fresh's PAN_COLUMNS does.
const wheelColumns = 8

// mouseExtra is the state of the mouse extras: modifier clicks and the pan of the
// view by the sideways wheel.
type mouseExtra struct {
	// extend is set while a Shift or Ctrl press is handled. The press grows the
	// selection from its anchor instead of placing the caret.
	extend bool
	pan    panView
	// dragSeq numbers the autoscroll ticks of text drags.
	dragSeq int
}

// panView is a horizontal position the wheel chose for the focused pane. It holds
// while the caret and the tab stay as they were; a caret move takes the view back to
// the caret, as a keystroke does.
type panView struct {
	on    bool
	left  int
	tab   int
	caret buffer.Pos
}

// foldToggler is the optional editor port that toggles the fold starting at a line
// without moving the caret. The app adapter implements it.
type foldToggler interface {
	ToggleFoldAt(line int) bool
}

// scrollMargined is the optional editor port that reports the scroll-off margin in
// rows. The app adapter implements it.
type scrollMargined interface {
	ScrollMargin() int
}

// scrollMarginFor is the scroll-off margin to keep when msg moved the caret. A mouse
// message places the caret where the pointer is, so the caret may sit in the margin,
// as with the editor's own rule.
func (m *Model) scrollMarginFor(msg tea.Msg) int {
	switch msg.(type) {
	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg:
		return 0
	}
	if s, ok := m.ed.(scrollMargined); ok {
		return s.ScrollMargin()
	}
	return 0
}

// noteMods records the modifiers of a left press. Shift and Ctrl both extend the
// selection, since some terminals swallow Shift+click.
func (m *Model) noteMods(mo tea.Mouse) {
	m.mx.extend = mo.Mod&(tea.ModShift|tea.ModCtrl) != 0
}

// collapseCarets ends multi-caret editing before a plain click places the caret,
// so the next keystroke does not edit the old carets, wherever they are.
func (m *Model) collapseCarets() {
	if cl, ok := m.ed.Active().(editorpane.CaretLister); ok && len(cl.Cursors()) > 0 {
		m.ed.Do("remove_secondary_cursors", nil)
	}
}

// toggleFoldAt toggles the fold that starts at line. It reports whether the editor
// did, and a false answer leaves the click to place the caret.
func (m *Model) toggleFoldAt(line int) bool {
	if doc := m.ed.Active(); doc != nil && m.togglePluginFold(doc, line) {
		return true
	}
	t, ok := m.ed.(foldToggler)
	return ok && t.ToggleFoldAt(line)
}

// dragScrollEvery is the period of the autoscroll of a text drag whose pointer is held
// outside the pane. dragMaxStep is the most lines (or columns) a tick moves the head.
// A tick moves it 1 + the distance of the pointer from the edge, up to that cap: about
// 14 lines a second at the edge row, 28 one row beyond, and at most 71 farther out.
const (
	dragScrollEvery = 70 * time.Millisecond
	dragMaxStep     = 5
)

// dragScrollMsg is the autoscroll tick of a text drag. seq names the tick armed last,
// so a tick left over from an earlier drag does nothing.
type dragScrollMsg struct{ seq int }

// dragSelect grows the selection of a text drag to the latest pointer. While the
// pointer is on the edge row or column or beyond it, it arms a tick that grows the
// selection again, so the view keeps scrolling with the pointer held still. A tick
// that moved nothing, as at the end of the document, ends the chain; the next motion
// restarts it. A motion goes to the cell under the pointer, and a tick moves the head
// from where it is by a step that grows with the distance (see dragStep). Beyond the
// left or right edge the head also moves along the line while Wrap is off.
func (m *Model) dragSelect(fromTick bool) tea.Cmd {
	doc := m.ed.Active()
	fp := m.layout().FocusedPane()
	if doc == nil || fp.W <= 0 || fp.H <= 0 {
		m.drag.tick = 0
		return nil
	}
	w, h := fp.W, fp.H
	p := m.paneFor(fp, doc)
	x := min(max(m.drag.last.X-fp.X, 0), w-1)
	py := m.drag.last.Y - fp.Y
	before := doc.Cursor()
	pos := m.dragPos(p, doc, w, h, x, py)
	last := max(p.BodyRows(w, h), 1) - 1
	vs, vd := pastEdge(py, 0, last)
	var hs, hd int
	if p.PansSideways() {
		hs, hd = pastEdge(m.drag.last.X-fp.X, fp.Gutter, w-2)
	}
	if fromTick {
		pos = dragStepPos(doc, pos, before, vs*dragStep(vd), hs*dragStep(hd), vs != 0, hs != 0)
	}
	m.ed.ExtendSelection(pos, m.drag.unit)
	outside := vs != 0 || hs != 0
	stalled := fromTick && m.ed.Active().Cursor() == before
	if !outside || stalled {
		m.drag.tick = 0
		return nil
	}
	if m.drag.tick != 0 && !fromTick {
		return nil // a tick is already in flight
	}
	m.mx.dragSeq++
	m.drag.tick = m.mx.dragSeq
	seq := m.drag.tick
	return tea.Tick(dragScrollEvery, func(time.Time) tea.Msg { return dragScrollMsg{seq: seq} })
}

// pastEdge says whether v is past either edge of a span, and how far. side is +1 on or
// beyond the high edge hi, -1 on or before the low edge lo, and 0 between them; dist is
// the number of cells beyond the edge itself (0 on it).
func pastEdge(v, lo, hi int) (side, dist int) {
	switch {
	case v >= hi:
		return 1, v - hi
	case v <= lo:
		return -1, lo - v
	}
	return 0, 0
}

// dragStep is the number of lines (or columns) a tick moves the head when the pointer is
// d cells beyond the edge: one more than the distance, up to dragMaxStep.
func dragStep(d int) int { return min(1+d, dragMaxStep) }

// dragStepPos is the end of the selection after an autoscroll tick. pointer is the
// position of the pointer cell and head where the selection ends now. dy and dx are the
// signed steps in lines and columns, used where the pointer is past that axis' edge
// (vert, horiz). The head moves from where it was, so the speed does not depend on how
// the view follows the caret. On an axis where the pointer is within the edges, the
// position of the pointer cell is kept.
func dragStepPos(doc Document, pointer, head buffer.Pos, dy, dx int, vert, horiz bool) buffer.Pos {
	pos := pointer
	if vert {
		pos.Line = min(max(head.Line+dy, 0), doc.LineCount()-1)
		pos.Col = min(pointer.Col, utf8.RuneCountInString(doc.Line(pos.Line)))
	}
	if horiz {
		pos.Col = min(max(head.Col+dx, 0), utf8.RuneCountInString(doc.Line(pos.Line)))
	}
	return pos
}

// dragScroll handles an autoscroll tick: it grows the selection toward the pointer
// held outside the pane, unless the drag ended or a newer tick replaced this one.
func (m *Model) dragScroll(msg dragScrollMsg) tea.Cmd {
	if !m.drag.active || m.drag.thumb || m.drag.border != nil || m.drag.tick != msg.seq {
		return nil
	}
	return m.dragSelect(true)
}

// dragPos maps the pointer at pane-local (x, py) to the end of a text drag. Within
// the pane it is the cell under the pointer. Past the top or bottom edge, and on
// the edge row itself, it is that many lines beyond the edge row, so the selection
// keeps growing while the pointer is held there and the view follows the caret. The
// bottom edge is the last text row, so the horizontal scrollbar row counts as past it.
func (m *Model) dragPos(p editorpane.Pane, doc Document, w, h, x, py int) buffer.Pos {
	last := max(p.BodyRows(w, h), 1) - 1
	pos := p.PosAt(w, h, x, min(max(py, 0), last))
	over := 0
	switch {
	case py >= last:
		over = py - last + 1
	case py <= 0:
		over = py - 1
	}
	if over == 0 {
		return pos
	}
	line := min(max(pos.Line+over, 0), doc.LineCount()-1)
	return buffer.Pos{Line: line, Col: min(pos.Col, utf8.RuneCountInString(doc.Line(line)))}
}

// applyPan gives the focused pane the horizontal position the wheel chose, while it
// still holds.
func (m *Model) applyPan(pane editorpane.Pane, doc Document) editorpane.Pane {
	if pv := m.mx.pan; pv.on && pv.tab == m.activeTab() && pv.caret == doc.Cursor() {
		pane.Left, pane.KeepLeft = pv.left, true
	}
	return pane
}

// panBar pans the focused pane so that the horizontal thumb is under the frame column
// x, for a press or a drag on the horizontal scrollbar. The caret stays where it is.
func (m *Model) panBar(x int) {
	doc := m.ed.Active()
	fp := m.layout().FocusedPane()
	if doc == nil || fp.W <= 0 || fp.H <= 0 {
		return
	}
	m.mx.pan = panView{
		on: true, left: m.paneGeometry(fp, doc).HScrollFor(fp.W, fp.H, x-fp.X),
		tab: m.activeTab(), caret: doc.Cursor(),
	}
}

// panWheel pans the view for a sideways wheel notch, or for a notch up or down with
// Shift held. It reports whether the notch was one of those, even when the pane
// cannot pan. Only the focused pane pans, and only with Wrap off.
func (m *Model) panWheel(i int, mo tea.Mouse) bool {
	dx := 0
	switch mo.Button {
	case tea.MouseWheelLeft:
		dx = -wheelColumns
	case tea.MouseWheelRight:
		dx = wheelColumns
	case tea.MouseWheelUp:
		dx = -wheelColumns
	case tea.MouseWheelDown:
		dx = wheelColumns
	default:
		return false
	}
	if (mo.Button == tea.MouseWheelUp || mo.Button == tea.MouseWheelDown) && mo.Mod&tea.ModShift == 0 {
		return false
	}
	l := m.layout()
	doc := m.ed.Active()
	if i < 0 || i >= len(l.Panes) || !l.Panes[i].Focused || doc == nil {
		return true
	}
	fp := l.Panes[i]
	left, most := m.paneGeometry(fp, doc).HScroll(fp.W, fp.H)
	m.mx.pan = panView{
		on: true, left: min(max(left+dx, 0), most),
		tab: m.activeTab(), caret: doc.Cursor(),
	}
	return true
}
