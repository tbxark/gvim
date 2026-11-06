package ui

import (
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/ui/editorpane"
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
	t, ok := m.ed.(foldToggler)
	return ok && t.ToggleFoldAt(line)
}

// dragScrollEvery is the period of the autoscroll of a text drag whose pointer is held
// outside the pane.
const dragScrollEvery = 50 * time.Millisecond

// dragScrollMsg is the autoscroll tick of a text drag. seq names the tick armed last,
// so a tick left over from an earlier drag does nothing.
type dragScrollMsg struct{ seq int }

// dragSelect grows the selection of a text drag to the latest pointer. While the
// pointer is on the edge row or beyond it, it arms a tick that grows the selection
// again, so the view keeps scrolling with the pointer held still. A tick that moved
// nothing, as at the end of the document, ends the chain; the next motion restarts it.
func (m *Model) dragSelect(fromTick bool) tea.Cmd {
	doc := m.ed.Active()
	fp := m.layout().focusedPane()
	if doc == nil || fp.w <= 0 || fp.h <= 0 {
		m.drag.tick = 0
		return nil
	}
	w, h := fp.w, fp.h
	p := m.paneFor(fp, doc)
	x := min(max(m.drag.last.X-fp.x, 0), w-1)
	py := m.drag.last.Y - fp.y
	before := doc.Cursor()
	m.ed.ExtendSelection(m.dragPos(p, doc, w, h, x, py), m.drag.unit)
	outside := py <= 0 || py >= max(p.BodyRows(w, h), 1)-1
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
	if i < 0 || i >= len(l.panes) || !l.panes[i].focused || doc == nil {
		return true
	}
	fp := l.panes[i]
	left, most := m.paneGeometry(fp, doc).HScroll(fp.w, fp.h)
	m.mx.pan = panView{
		on: true, left: min(max(left+dx, 0), most),
		tab: m.activeTab(), caret: doc.Cursor(),
	}
	return true
}
