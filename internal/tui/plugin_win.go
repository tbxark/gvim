package tui

import (
	"math"
	"slices"

	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/pluginapi"
)

// Windows for plugins. A window is a leaf of the split tree.
// Its id is splitNode.id, which stays with the pane through splits, closes and focus
// changes. Every change goes through trySplit, focusLeaf and closeFocused, so the tab
// bookkeeping (tabRemoved, reconcileTabs, the focused leaf taking its tab from the
// editor) stays in split.go.

// maxVisibleRows is the most rows Host.VisibleRows lists.
const maxVisibleRows = 500

func closedWindow(id pluginapi.WinID) error {
	return pluginapi.Errf(pluginapi.CodeClosed, "window %d is closed", id)
}

// windowLeaves returns the leaves in reading order after giving each a window id.
func (m *Model) windowLeaves() []*split.Node {
	leaves := m.splits.Root.Leaves()
	for _, n := range leaves {
		m.splits.IDOf(n)
	}
	return leaves
}

// splitByID returns the leaf with window id, or nil.
func (m *Model) splitByID(id uint64) *split.Node {
	for _, n := range m.windowLeaves() {
		if n.ID == id {
			return n
		}
	}
	return nil
}

// leafTab is the tab leaf n shows; the focused leaf shows the active tab.
func (m *Model) leafTab(n *split.Node) int {
	if n == m.splits.Focus {
		return m.activeTab()
	}
	return n.Tab
}

// windowInfos describes every window in reading order. The size is the text area in
// screen cells and the height counts rows, from the same pane the painter and the hit
// test use.
func (m *Model) windowInfos() []pluginapi.WindowInfo {
	leaves := m.windowLeaves()
	bufs := m.plug.host.ed().ListBuffers()
	l := m.layout()
	out := make([]pluginapi.WindowInfo, len(leaves))
	for i, n := range leaves {
		info := pluginapi.WindowInfo{ID: pluginapi.WinID(n.ID), Active: n == m.splits.Focus}
		if tab := m.leafTab(n); tab >= 0 && tab < len(bufs) {
			info.Buffer = bufs[tab].ID
		}
		if i < len(l.Panes) {
			p := l.Panes[i]
			info.TopLine = p.Top
			info.X, info.Y = p.TextX, p.Y
			info.Width, info.Height = max(p.TextW-1, 0), p.H
			if doc := m.paneDoc(p); doc != nil && p.W > 0 && p.H > 0 {
				info.Height, info.LeftCol = m.paneGeometry(p, doc).View(p.W, p.H)
			}
		}
		out[i] = info
	}
	return out
}

// Windows lists the windows in reading order.
func (h *pluginHost) Windows() []pluginapi.WindowInfo {
	out := h.model().windowInfos()
	for i := range out {
		out[i].ID = h.globalWindow(uint64(out[i].ID))
	}
	return out
}

// CurrentWindow is the window with the focus.
func (h *pluginHost) CurrentWindow() pluginapi.WinID {
	m := h.model()
	return h.globalWindow(m.splits.IDOf(m.splits.Focus))
}

// SetWindowBuffer shows buffer b in window w. The focused window changes the active tab;
// another window keeps its own place in the buffer. A compare view names its two sides, so
// it is refused.
func (h *pluginHost) SetWindowBuffer(w pluginapi.WinID, b pluginapi.BufferID) error {
	m := h.model()
	wl, err := m.pluginResolveWindow(w)
	if err != nil {
		return err
	}
	n := m.splitByID(wl)
	if n == nil {
		return closedWindow(w)
	}
	tab := h.tabIndex(b)
	if tab < 0 {
		return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", b)
	}
	if m.splits.comparing {
		return pluginapi.Errf(pluginapi.CodeConflict, "the windows of a compare view cannot change buffer")
	}
	if n == m.splits.Focus {
		return h.Activate(b)
	}
	n.Tab, n.Top = tab, 0
	if doc := m.docAt(tab); doc != nil {
		n.Top = doc.Top()
	}
	return nil
}

// FocusWindow gives the keyboard to window w in the editor.
func (h *pluginHost) FocusWindow(w pluginapi.WinID) error {
	m := h.model()
	wl, err := m.pluginResolveWindow(w)
	if err != nil {
		return err
	}
	n := m.splitByID(wl)
	if n == nil {
		return closedWindow(w)
	}
	m.focusEditor()
	m.focusLeaf(n)
	return nil
}

// CloseWindow closes window w. Its buffer stays open. The last window cannot be closed.
func (h *pluginHost) CloseWindow(w pluginapi.WinID) error {
	m := h.model()
	wl, err := m.pluginResolveWindow(w)
	if err != nil {
		return err
	}
	if m.splitByID(wl) == nil {
		return closedWindow(w)
	}
	return m.closeSplitByID(wl)
}

// closeSplitByID removes the leaf with window id. Closing the focused one is the
// close_split action: the focus goes to its neighbour. Closing another one leaves the focus
// where it is.
func (m *Model) closeSplitByID(id uint64) error {
	n := m.splitByID(id)
	if n == nil {
		return closedWindow(pluginapi.WinID(id))
	}
	if m.splits.Root.Leaf() {
		return pluginapi.Errf(pluginapi.CodeConflict, "the last window cannot be closed")
	}
	if n == m.splits.Focus {
		m.closeFocused()
		return nil
	}
	m.splits.Root = split.RemoveLeaf(m.splits.Root, n)
	m.splits.comparing = false
	m.splits.maximized = false
	m.syncViewport()
	return nil
}

// SplitWindow splits window w and returns the new window, which shows o.Buffer (the
// window's own buffer when zero). The focus stays where it was unless o.Focus.
func (h *pluginHost) SplitWindow(w pluginapi.WinID, o pluginapi.SplitOptions) (pluginapi.WinID, error) {
	m := h.model()
	wl, err := m.pluginResolveWindow(w)
	if err != nil {
		return 0, err
	}
	src := m.splitByID(wl)
	if src == nil {
		return 0, closedWindow(w)
	}
	var dir split.Dir
	switch o.Dir {
	case "right":
		dir = split.SideBySide
	case "below":
		dir = split.Stacked
	default:
		return 0, pluginapi.Errf(pluginapi.CodeInvalid, "split dir must be \"right\" or \"below\", got %q", o.Dir)
	}
	if o.Ratio != 0 && !(o.Ratio > 0 && o.Ratio < 1) {
		return 0, pluginapi.Errf(pluginapi.CodeInvalid, "split ratio must be between 0 and 1, got %v", o.Ratio)
	}
	tab := -1
	if o.Buffer != 0 {
		if tab = h.tabIndex(o.Buffer); tab < 0 {
			return 0, pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", o.Buffer)
		}
	}
	id, err := m.splitNew(src, dir, o, tab)
	if err != nil {
		return 0, err
	}
	return h.globalWindow(id), nil
}

// splitNew splits leaf src in direction dir and returns the id of the new leaf. tab, when
// not negative, is the tab the new leaf shows. The leaf that had the focus has it again at
// the end, unless o.Focus.
func (m *Model) splitNew(src *split.Node, dir split.Dir, o pluginapi.SplitOptions, tab int) (uint64, error) {
	prev := m.splits.Focus
	prevID := m.splits.IDOf(prev)
	if src != prev {
		m.focusLeaf(src)
	}
	f := m.splits.Focus // the leaf the split turns into an inner node
	if msg := m.trySplit(dir); msg != "" {
		if src != prev {
			m.focusLeaf(prev)
		}
		code := pluginapi.CodeLimit
		if msg == noDocumentNotice {
			code = pluginapi.CodeClosed
		}
		return 0, pluginapi.Errf(code, "%s", msg)
	}
	created := f.B
	if tab >= 0 {
		m.activateTab(tab)
	}
	share := split.Even
	if o.Ratio > 0 {
		share = min(max(int(math.Round(o.Ratio*100)), 1), 99)
	}
	if o.Before {
		f.A, f.B = f.B, f.A
		f.Ratio = share
	} else {
		f.Ratio = 100 - share
	}
	id := m.splits.IDOf(created)
	if o.Focus {
		m.focusEditor()
	} else if old := m.splitByID(prevID); old != nil {
		m.focusLeaf(old)
	}
	m.syncViewport()
	return id, nil
}

// ScrollWindow scrolls window w so that line is its first line, or in the middle of its
// rows with center. The caret stays where it is. A line outside the buffer is clamped.
func (h *pluginHost) ScrollWindow(w pluginapi.WinID, line int, center bool) error {
	m := h.model()
	wl, err := m.pluginResolveWindow(w)
	if err != nil {
		return err
	}
	n := m.splitByID(wl)
	if n == nil {
		return closedWindow(w)
	}
	doc := m.leafDoc(n)
	if doc == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "window %d shows no buffer", w)
	}
	top := line
	if center {
		top -= max(m.windowRows(n)-1, 0) / 2
	}
	top = min(max(top, 0), max(doc.LineCount()-1, 0))
	if n == m.splits.Focus {
		m.ed.ScrollTo(top)
		return nil
	}
	n.Top = top
	return nil
}

// windowRows is the number of text rows of leaf n.
func (m *Model) windowRows(n *split.Node) int {
	i := slices.Index(m.splits.Root.Leaves(), n)
	l := m.layout()
	if i < 0 || i >= len(l.Panes) {
		return 0
	}
	p := l.Panes[i]
	doc := m.paneDoc(p)
	if doc == nil || p.W <= 0 || p.H <= 0 {
		return p.H
	}
	rows, _ := m.paneGeometry(p, doc).View(p.W, p.H)
	return rows
}

// Equalize gives every window the same size along the direction of its splits: the share
// of each split follows the number of windows on its two sides.
func (h *pluginHost) Equalize() error {
	m := h.model()
	var walk func(n *split.Node)
	walk = func(n *split.Node) {
		if n.Leaf() {
			return
		}
		a, b := windowsAlong(n.A, n.Dir), windowsAlong(n.B, n.Dir)
		n.Ratio = (100*a + (a+b)/2) / (a + b)
		walk(n.A)
		walk(n.B)
	}
	walk(m.splits.Root)
	m.splits.maximized = false
	m.syncViewport()
	return nil
}

// windowsAlong counts the windows of the subtree n that sit in a row along dir: the
// sum over a split of the same direction, the larger side over a split across it.
func windowsAlong(n *split.Node, dir split.Dir) int {
	if n.Leaf() {
		return 1
	}
	a, b := windowsAlong(n.A, dir), windowsAlong(n.B, dir)
	if n.Dir == dir {
		return a + b
	}
	return max(a, b)
}

// VisibleRows lists the rows window w paints, from the pane geometry the painter uses.
func (h *pluginHost) VisibleRows(w pluginapi.WinID) ([]pluginapi.VisibleRow, error) {
	m := h.model()
	wl, err := m.pluginResolveWindow(w)
	if err != nil {
		return nil, err
	}
	n := m.splitByID(wl)
	if n == nil {
		return nil, closedWindow(w)
	}
	i := slices.Index(m.splits.Root.Leaves(), n)
	l := m.layout()
	if i < 0 || i >= len(l.Panes) {
		return nil, nil
	}
	p := l.Panes[i]
	doc := m.paneDoc(p)
	if doc == nil || p.W <= 0 || p.H <= 0 {
		return nil, nil
	}
	spans := m.paneGeometry(p, doc).VisibleRows(p.W, p.H, maxVisibleRows)
	rows := make([]pluginapi.VisibleRow, len(spans))
	for k, s := range spans {
		rows[k] = pluginapi.VisibleRow{Line: s.Line, ColFrom: s.From, ColTo: s.To, Wrapped: s.Wrapped}
	}
	return rows, nil
}
