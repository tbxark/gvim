package ui

import (
	"slices"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/ui/editorpane"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// splitDir is how an inner node of the split tree divides its area.
type splitDir int

const (
	sideBySide splitDir = iota // split_vertical: the children are left and right
	stacked                    // split_horizontal: the children are top and bottom
)

// The glyphs of the lines between panes. The lines take the split colour (splitCell).
const (
	splitVLine = "│" // a column between two side-by-side panes
	splitHLine = "─" // a row between two stacked panes
)

// minSplitExtent is the smallest extent, in columns for a side-by-side split and in
// rows for a stacked one, that a pane needs to be split: one cell for each child and
// the line between them.
const minSplitExtent = 3

// splitNode is a node of the split tree. A leaf (a and b nil) is one editor pane. It
// shows the tab tab, and top is its viewport top while another pane has the focus.
// The focused leaf takes both from the editor, so its own fields are stale until the
// focus leaves it (keepFocused). An inner node divides its area between a, the top
// or left child, and b, the bottom or right child. ratio is the percentage a takes.
// A leaf has an id, the window id of the plugin API. It is given on first use (idOf) and
// stays with the window through splits, closes and focus changes; a split passes it to
// the child that keeps the old pane.
type splitNode struct {
	dir   splitDir
	a, b  *splitNode
	ratio int // the percentage of the area the first child takes; zero means splitEven
	tab   int
	top   int
	// topRow is how many display rows of line top are scrolled off, for a pane of a compare
	// view: it lets the pane start inside the filler rows after a line (see syncCompare).
	topRow int
	id     uint64 // the window id, or zero until idOf gives one
}

// splitEven is the share of the first child in an even split, and the share a zero ratio
// stands for.
const splitEven = 50

// share returns the percentage of the area the first child takes.
func (n *splitNode) share() int {
	if n.ratio == 0 {
		return splitEven
	}
	return n.ratio
}

// leaf reports whether n has no children.
func (n *splitNode) leaf() bool { return n.a == nil }

// leaves returns the leaves of the tree in reading order: the first child before the
// second, depth first.
func (n *splitNode) leaves() []*splitNode {
	if n.leaf() {
		return []*splitNode{n}
	}
	return append(n.a.leaves(), n.b.leaves()...)
}

// splitSet is the split state of the root model.
type splitSet struct {
	root      *splitNode   // the tree; one leaf when nothing is split
	focus     *splitNode   // the focused leaf, always a leaf of root
	tabs      int          // the tab count the leaves were last checked against
	comparing bool         // the root is a compare view: two leaves side by side
	diff      compareDiff  // the differences of the compare view that were last computed
	owned     []string     // the files the compare view opened itself, which closing it closes too
	pick      *comparePick // a compare command that waits for the file browser, or nil
	maximized bool         // the focused leaf fills the body and the other leaves have no area
	seenTab   int          // the active tab noteTab last saw, or -1
	prevTab   int          // the tab that was active before the last switch, or -1
	lastID    uint64       // the last window id idOf gave out; ids are never reused
}

// idOf returns the window id of leaf n, giving it the next one when it has none.
func (s *splitSet) idOf(n *splitNode) uint64 {
	if n.id == 0 {
		s.lastID++
		n.id = s.lastID
	}
	return n.id
}

// newSplitSet returns the state with one leaf, which has the focus.
func newSplitSet() splitSet {
	leaf := &splitNode{}
	return splitSet{root: leaf, focus: leaf, seenTab: -1, prevTab: -1}
}

// tabDocs is the optional view of the documents of every tab. The split panes that
// show a tab other than the active one read it. The app adapter implements it. Without
// it such a pane paints nothing, and a compare view has no difference to show.
type tabDocs interface {
	// DocAt returns the document of tab i, or nil when there is no tab i. It must
	// return an untyped nil, not a typed nil in an interface.
	DocAt(i int) Document
}

// removeLeaf returns the tree t without the leaf n. The parent of n is replaced by n's
// sibling, which keeps its own state. t must have more than one leaf.
func removeLeaf(t, n *splitNode) *splitNode {
	if t.leaf() {
		return t
	}
	switch n {
	case t.a:
		return t.b
	case t.b:
		return t.a
	}
	t.a = removeLeaf(t.a, n)
	t.b = removeLeaf(t.b, n)
	return t
}

// splitAction runs one of the split, pane, window, tab and compare actions. The actions
// that move the focus between panes, or open one, give the focus to the editor, as a
// click does. The tab the action leaves active is recorded, for switch_to_previous_tab.
func (m *Model) splitAction(action string) {
	m.noteTab()
	defer m.noteTab()
	switch action {
	case "split_horizontal":
		m.focusEditor()
		m.splitFocused(stacked)
	case "split_vertical":
		m.focusEditor()
		m.splitFocused(sideBySide)
	case "next_split", "next_pane":
		m.focusEditor()
		m.moveFocus(1)
	case "prev_split", "prev_pane":
		m.focusEditor()
		m.moveFocus(-1)
	case "next_window":
		m.focusEditor()
		m.windowStep(1)
	case "prev_window":
		m.focusEditor()
		m.windowStep(-1)
	case "close_split":
		m.focusEditor()
		m.closeFocused()
	case "increase_split_size":
		m.resizeFocused(splitStep)
	case "decrease_split_size":
		m.resizeFocused(-splitStep)
	case "toggle_maximize_split":
		m.toggleMaximize()
	case "switch_to_previous_tab":
		m.switchPreviousTab()
	case "compare_next_difference":
		m.compareStep(true)
	case "compare_prev_difference":
		m.compareStep(false)
	case "compare_switch_side":
		m.compareSwitch()
	case "compare_files", "compare_with_file", "compare_copy_change", "compare_take_change", "compare_close":
		m.compareCommand(action)
	}
}

// splitFocused splits the focused pane in direction dir. The new pane is the second
// child, shows the same tab at the same top, and takes the focus. A pane too small to
// hold two children is not split.
func (m *Model) splitFocused(dir splitDir) {
	if msg := m.trySplit(dir); msg != "" {
		m.notice = msg
	}
}

// trySplit is splitFocused that returns the reason a split did not happen, or "" when it
// did, instead of showing it.
func (m *Model) trySplit(dir splitDir) string {
	if !m.hasDocument() {
		return noDocumentNotice
	}
	// The room is measured on the panes as they are laid out, so a maximized pane is
	// restored first.
	m.splits.maximized = false
	fp := m.layout().focusedPane()
	extent := fp.w
	if dir == stacked {
		extent = fp.h
	}
	if extent < minSplitExtent {
		return "Not enough room to split"
	}
	m.keepFocused()
	f := m.splits.focus
	f.dir = dir
	// The first child is the pane as it was, so it keeps the window id.
	f.a = &splitNode{tab: f.tab, top: f.top, id: f.id}
	f.b = &splitNode{tab: f.tab, top: f.top}
	f.id = 0
	m.splits.focus = f.b
	m.splits.comparing = false
	m.syncViewport()
	m.revealCaretVisual(0)
	return ""
}

// moveFocus moves the focus delta panes along the reading order, and wraps at the
// ends.
func (m *Model) moveFocus(delta int) {
	leaves := m.splits.root.leaves()
	n := len(leaves)
	i := slices.Index(leaves, m.splits.focus)
	if n < 2 || i < 0 {
		return
	}
	m.focusLeaf(leaves[((i+delta)%n+n)%n])
}

// closeFocused removes the focused pane. Its sibling takes its place, and the focus
// goes to the pane now in the same reading position, or to the last one. The last
// pane is kept.
func (m *Model) closeFocused() {
	if m.splits.root.leaf() {
		m.notice = oneSplitNotice
		return
	}
	i := slices.Index(m.splits.root.leaves(), m.splits.focus)
	m.splits.root = removeLeaf(m.splits.root, m.splits.focus)
	m.splits.comparing = false
	m.splits.maximized = false
	rest := m.splits.root.leaves()
	// The closed pane's state goes with it, so there is nothing to keep.
	m.splits.focus = nil
	m.focusLeaf(rest[min(i, len(rest)-1)])
}

// focusPane gives the focus to the pane with index i in reading order.
func (m *Model) focusPane(i int) {
	if leaves := m.splits.root.leaves(); i >= 0 && i < len(leaves) {
		m.focusLeaf(leaves[i])
	}
}

// focusLeaf gives the focus to the leaf n. The leaf that loses the focus keeps the tab
// and the top it had. The leaf that takes the focus shows its own tab, scrolled to its
// own top.
func (m *Model) focusLeaf(n *splitNode) {
	if n == m.splits.focus {
		return
	}
	m.keepFocused()
	m.splits.focus = n
	m.activateTab(n.tab)
	if m.ed.Active() != nil {
		m.ed.ScrollTo(n.top)
		if m.splits.comparing {
			m.setTopRow(n.topRow)
		}
	}
	m.syncViewport()
	m.revealCaretVisual(0)
}

// keepFocused copies the active tab and the viewport top into the focused leaf, so the
// leaf can show them again when the focus returns to it.
func (m *Model) keepFocused() {
	f := m.splits.focus
	if f == nil {
		return
	}
	f.tab = m.activeTab()
	if doc := m.ed.Active(); doc != nil {
		f.top = doc.Top()
		if m.splits.comparing {
			f.topRow = m.topRow(f.top)
		}
	}
}

// splitInput gives the layout the split tree and the state of each leaf in reading
// order. The focused leaf shows the active tab at the editor's viewport top.
func (m *Model) splitInput() (*splitNode, []leafInfo) {
	leaves := m.splits.root.leaves()
	infos := make([]leafInfo, len(leaves))
	for i, n := range leaves {
		if n != m.splits.focus {
			infos[i] = leafInfo{tab: n.tab, top: n.top}
			if doc := m.docAt(n.tab); doc != nil {
				infos[i].lines = displayLines(doc)
			}
			continue
		}
		infos[i] = leafInfo{tab: m.activeTab(), focused: true}
		if doc := m.ed.Active(); doc != nil {
			infos[i].top = doc.Top()
			infos[i].lines = displayLines(doc)
		}
	}
	return m.splits.root, infos
}

// docAt returns the document of tab i. It is the active document for the active tab,
// and otherwise what the editor gives through tabDocs. It is nil when there is none.
func (m *Model) docAt(i int) Document {
	if i < 0 {
		return nil
	}
	if i == m.activeTab() {
		return m.ed.Active()
	}
	if t, ok := m.ed.(tabDocs); ok {
		return t.DocAt(i)
	}
	return nil
}

// leafDoc returns the document that leaf n shows.
func (m *Model) leafDoc(n *splitNode) Document {
	if n == m.splits.focus {
		return m.ed.Active()
	}
	return m.docAt(n.tab)
}

// closeTab closes tab i through the tab port. A tab that closes updates the leaves.
func (m *Model) closeTab(t tabEditor, i int, force bool) error {
	path := m.tabPath(i)
	if err := t.CloseTab(i, force); err != nil {
		return err
	}
	m.cancelSavesOf(path)
	m.tabRemoved(i)
	return nil
}

// editorDo runs an editor action. An action that closes the active tab, such as
// close_tab, updates the leaves as closeTab does.
func (m *Model) editorDo(a editor.Action, args map[string]any) editor.Result {
	before, active := len(m.ed.Tabs()), m.activeTab()
	path := m.tabPath(active)
	res := m.ed.Do(a, args)
	if len(m.ed.Tabs()) == before-1 {
		m.cancelSavesOf(path)
		m.tabRemoved(active)
	}
	if a == "clear_search" && m.findBar != nil {
		// The editor drops the find range only. The bar holds the search text, and the
		// highlights come from that text, so the bar forgets it too.
		m.findBar.Clear()
	}
	return res
}

// tabRemoved updates the leaves after tab i closed. A leaf that shows a later tab
// moves down one index. A leaf that showed tab i shows the tab now at its index, or
// the last tab. With no tab left the splits collapse.
func (m *Model) tabRemoved(i int) {
	n := len(m.ed.Tabs())
	// A compare view is its two tabs together: with one of them gone it ends, as Fresh's
	// closes the whole group, instead of leaving the other file shown twice, unmarked.
	endsCompare := m.splits.comparing && !m.splits.root.leaf() && (m.splits.root.a.tab == i || m.splits.root.b.tab == i)
	for _, leaf := range m.splits.root.leaves() {
		if leaf == m.splits.focus {
			continue
		}
		switch {
		case leaf.tab > i:
			leaf.tab--
		case leaf.tab == i:
			leaf.tab = min(i, n-1)
		}
	}
	m.splits.seenTab = shiftTab(m.splits.seenTab, i)
	m.splits.prevTab = shiftTab(m.splits.prevTab, i)
	m.splits.tabs = n
	if n == 0 || endsCompare {
		m.collapseSplits()
	}
}

// reconcileTabs runs after every update. A new tab is appended, so the leaves keep
// their indices. A tab that went away without tabRemoved cannot be placed, so the
// splits collapse to the focused pane rather than show the wrong tab.
func (m *Model) reconcileTabs() {
	n := len(m.ed.Tabs())
	if n < m.splits.tabs {
		m.collapseSplits()
		// The indices moved with no record, so the tab history cannot be placed.
		m.splits.seenTab, m.splits.prevTab = -1, -1
	}
	m.splits.tabs = n
	m.noteTab()
}

// collapseSplits closes every pane but the focused one.
func (m *Model) collapseSplits() {
	f := m.splits.focus
	f.a, f.b = nil, nil
	m.splits.root = f
	m.splits.comparing = false
	m.splits.owned = nil
	m.splits.maximized = false
}

// scrollPane moves the viewport of pane i by delta lines. The focused pane scrolls its
// document. Another pane keeps its own top, and the caret stays where it is.
func (m *Model) scrollPane(i, delta int) {
	leaves := m.splits.root.leaves()
	if i < 0 || i >= len(leaves) {
		return
	}
	n := leaves[i]
	if n == m.splits.focus || m.splits.comparing {
		// The two sides of a compare view scroll together, from the focused one.
		m.scrollFocused(delta)
		return
	}
	doc := m.docAt(n.tab)
	if doc == nil {
		return
	}
	n.top = min(max(n.top+delta, 0), max(doc.LineCount()-1, 0))
}

// paneDoc returns the document that pane p shows.
func (m *Model) paneDoc(p splitPane) Document {
	if p.focused {
		return m.ed.Active()
	}
	return m.docAt(p.tab)
}

// paneFor returns the editor pane that paints p with doc. The find bar's highlights
// go to the focused pane only, because they belong to the active document.
func (m *Model) paneFor(p splitPane, doc Document) editorpane.Pane {
	pane := m.paneGeometry(p, doc)
	if p.focused {
		pane.Highlights = paneHighlights(m.findLineHighlights())
	}
	return pane
}

// paneGeometry is the editor pane of paneFor without the search highlights. The
// geometry does not depend on them, and building them searches the document, so
// the caret, viewport and press paths use this one.
func (m *Model) paneGeometry(p splitPane, doc Document) editorpane.Pane {
	pane := editorpane.Pane{
		Doc: doc, Top: p.top, Styles: m.panes, TabSize: m.tabSize,
		Diagnostics: m.lspSpans(doc),
	}
	pane = m.viewPane(pane, doc)
	pane = m.plugDecor(pane, doc)
	pane = m.lspGutter(pane, doc)
	pane = m.compareDecor(p, pane)
	if p.focused {
		pane = m.applyPan(pane, doc)
		pane.TopRow = m.topRow(p.top)
	}
	return pane
}

// focusedBodyRows is the number of text rows of the focused pane, which is the
// viewport the editor scrolls within. It is the pane's height less a horizontal
// scrollbar row.
func (m *Model) focusedBodyRows() int {
	fp := m.layout().focusedPane()
	doc := m.ed.Active()
	if doc == nil {
		return fp.h
	}
	return m.paneGeometry(fp, doc).BodyRows(fp.w, fp.h)
}

// revealCaretVisual scrolls the focused pane so that the caret's display row is on
// screen. The editor counts buffer lines, and a wrapped line takes several rows, so
// its own reveal can leave the caret below the body. The top it moves to is the one
// the pane paints with (editorpane.RevealTop), so the caret, the hit map and the
// paint agree on the row. margin is the rows kept between the caret row and the edge
// of the body, and it is 0 for a caret that did not move by the keyboard.
func (m *Model) revealCaretVisual(margin int) {
	doc := m.ed.Active()
	if doc == nil {
		return
	}
	// A gated frame has no panes to measure, so there is nothing to reveal.
	l := m.layout()
	if l.gated {
		return
	}
	fp := l.focusedPane()
	// The notice band is painted over the bottom rows of the pane: the caret is kept
	// above it, so the line being edited is not hidden by the message.
	h := max(fp.h-max(fp.y+fp.h-l.bandTop(), 0), 1)
	top, row := m.paneGeometry(fp, doc).RevealView(fp.w, h, margin)
	if top != doc.Top() {
		m.ed.ScrollTo(top)
	}
	m.setTopRow(row)
}

// viewTop is the first visible line of the active document, or 0 with no document.
func (m *Model) viewTop() int {
	if doc := m.ed.Active(); doc != nil {
		return doc.Top()
	}
	return 0
}

// caretAt is the active tab and the caret of its document. Update compares it
// before and after a message, so a move of the caret or a change of tab is seen
// once.
type caretAt struct {
	tab int
	pos buffer.Pos
}

// caretNow returns the active tab and the caret of the editor. With no document the
// tab is -1.
func (m *Model) caretNow() caretAt {
	doc := m.ed.Active()
	if doc == nil {
		return caretAt{tab: -1}
	}
	return caretAt{tab: m.activeTab(), pos: doc.Cursor()}
}

// pressPane is a left press in editor pane i at frame cell (x, y). The press is
// resolved against the pane as it is shown now, before the focus moves, because
// focusing a pane reveals its caret and can scroll it. The pane then keeps the view it
// was pressed in, so the caret lands on the line under the pointer.
func (m *Model) pressPane(i, x, y, count int) {
	p, hit, top, ok := m.paneHitIn(i, x, y)
	m.focusPane(i)
	if ok {
		m.ed.ScrollTo(top)
		if m.splits.comparing {
			m.setTopRow(p.TopRow)
		}
	}
	m.pressEditor(p, hit, count)
}

// paneHitIn maps frame cell (x, y) to what editor pane i shows, against the pane's
// viewport now. It also returns the pane's top line. ok is false when there is no
// pane i, or it shows no document.
func (m *Model) paneHitIn(i, x, y int) (p editorpane.Pane, hit editorpane.Hit, top int, ok bool) {
	l := m.layout()
	if i < 0 || i >= len(l.panes) {
		return p, hit, 0, false
	}
	sp := l.panes[i]
	doc := m.paneDoc(sp)
	if doc == nil {
		return p, hit, 0, false
	}
	p = m.paneGeometry(sp, doc)
	return p, p.Hit(sp.w, sp.h, x-sp.x, y-sp.y), sp.top, true
}

// paintPanes paints every editor pane and the lines between them. A pane with no
// document is left blank.
func (m *Model) paintPanes(g *grid.Grid, l layout) {
	for _, p := range l.panes {
		doc := m.paneDoc(p)
		if doc == nil || p.w <= 0 || p.h <= 0 {
			continue
		}
		editorpane.Blit(g, p.x, p.y, m.paneFor(p, doc).Render(p.w, p.h))
	}
	drawBorders(g, l.borders, m.splitCell())
}

// splitCell is the style of the lines between panes: the split colour of the theme, on
// the editor background. A theme without the key draws them in the scrollbar track
// colour.
func (m *Model) splitCell() grid.Style {
	cell := style.Cell(m.styles.Scrollbar)
	if c := style.Color(m.theme, "ui", "split_separator_fg"); c != nil {
		cell.FG = c
	}
	return cell
}

// drawBorders paints the lines between panes in cell.
func drawBorders(g *grid.Grid, borders []splitBorder, cell grid.Style) {
	for _, b := range borders {
		if b.vertical {
			for y := b.y; y < b.y+b.h; y++ {
				g.Put(b.x, y, g.Width(), splitVLine, cell)
			}
			continue
		}
		for x := b.x; x < b.x+b.w; x++ {
			g.Put(x, b.y, g.Width(), splitHLine, cell)
		}
	}
}
