package tui

import (
	"image/color"
	"slices"

	"github.com/tbxark/gvim/buffer"
	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/diff"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/pluginapi"
)

// openCompare shows the tabs left and right side by side as a compare view, with the
// focus on the left one. It reports false when either tab does not exist.
func (m *Model) openCompare(left, right int) bool {
	n := len(m.ed.Tabs())
	if left < 0 || right < 0 || left >= n || right >= n {
		return false
	}
	if name := m.largeTabIn(left, right); name != "" {
		m.notice = name + " is too large to compare"
		return false
	}
	a, b := &split.Node{Tab: left}, &split.Node{Tab: right}
	m.splits.owned = nil
	m.splits.Root = &split.Node{Dir: split.SideBySide, A: a, B: b}
	m.splits.Focus = nil
	m.splits.comparing = true
	m.focusLeaf(a)
	return true
}

// compareStep moves both sides of the compare view to the next or previous difference, as
// Fresh does: the caret of each side goes to the line where the difference starts on that
// side (or, on a side with no lines in it, the line before the gap, right above its filler
// rows), and the focused side shows the difference in the middle of its pane. The other side
// follows by the scroll sync. The search wraps around at both ends, and the notice says which
// difference it is. The differences come from compareHunks.
func (m *Model) compareStep(forward bool) {
	if !m.splits.comparing {
		m.notice = uiText("gvim.notice.compare.no_view", noCompareNotice)
		return
	}
	if name := m.largeCompared(); name != "" {
		m.notice = name + " is too large to compare"
		return
	}
	hunks, ok := m.compareHunks()
	if !ok {
		m.notice = uiText("gvim.notice.compare.other_tab_no_document", "Compare: the other tab has no document")
		return
	}
	if len(hunks) == 0 {
		m.notice = m.sameTextNotice()
		return
	}
	nodes := [2]*split.Node{m.splits.Root.A, m.splits.Root.B}
	docs := [2]Document{m.leafDoc(nodes[0]), m.leafDoc(nodes[1])}
	side := 0
	if m.splits.Focus == nodes[1] {
		side = 1
	}
	at := docs[side].Cursor()
	last := docs[side].LineCount() - 1
	i, wrapped := pickHunk(hunks, side, at.Line, at.Col, last, forward)
	h := hunks[i]
	for s := range docs {
		line := hunkJump(h, s, docs[s].LineCount()-1)
		if s == side {
			m.ed.SetCursor(buffer.Pos{Line: line}, false)
			continue
		}
		m.setTabCursor(nodes[s].Tab, line)
	}
	m.compareCenter(hunkJump(h, side, last))
	m.notice = uiTextVars("gvim.notice.compare.difference_position", "Difference %{index}/%{total}", map[string]any{"index": i + 1, "total": len(hunks)})
	if wrapped {
		m.notice += " " + uiText("gvim.notice.compare.wrapped", "[wrapped]")
	}
	m.syncCompare()
}

// sameTextNotice is the notice for a compare view whose sides have the same lines: they are
// identical, or they differ only in how their lines end.
func (m *Model) sameTextNotice() string {
	if e := m.compareEnds(); e.differ {
		return uiTextVars("gvim.notice.compare.line_endings", "Compare: the files differ only in their line endings (left %{left}, right %{right})", map[string]any{"left": e.name[0], "right": e.name[1]})
	}
	return uiText("gvim.notice.compare.identical", "Compare: the files are identical")
}

// noCompareNotice is the notice of a compare command with no compare view.
const noCompareNotice = "Compare: no comparison is open here"

// compareCenter scrolls the focused pane so that line is in the middle of its rows, counting
// the filler rows, and as far as the text allows. The other pane follows (syncCompare).
func (m *Model) compareCenter(line int) {
	st, ok := m.compareState()
	doc := m.ed.Active()
	if !ok || doc == nil {
		m.ed.ScrollTo(line)
		return
	}
	side := 0
	if m.splits.Focus == m.splits.Root.B {
		side = 1
	}
	fill := st.side[side].fill
	rows := m.focusedBodyRows()
	total := fill.Total(doc.LineCount())
	row := min(max(fill.TextRow(line)-rows/2, 0), max(total-rows, 0))
	top, in := fill.LineAt(doc.LineCount(), row)
	m.ed.ScrollTo(top)
	m.setTopRow(in)
}

// setTabCursor puts the caret of the document of tab i at the start of line, through the
// plugin editor port. A tab that is not open, or an editor without the port, is left
// alone: the caret of the unfocused side is a courtesy, and the focused side does not
// depend on it.
func (m *Model) setTabCursor(tab, line int) {
	pe := m.plug.pe
	if pe == nil {
		return
	}
	if bufs := pe.ListBuffers(); tab >= 0 && tab < len(bufs) {
		_ = pe.SetCursor(bufs[tab].ID, pluginapi.Pos{Line: line}, false, false, false)
	}
}

// compareHunks returns the differences between the two panes of the compare view, as
// diff.Lines gives them. Each hunk gives where it starts on each side, and the hunk
// list does not depend on the focus. compareDecor paints the lines the hunks cover. The
// diff is kept while the text of both sides is the same, so a frame does not run it
// again. ok is false when there is no compare view, or when a side has no document.
func (m *Model) compareHunks() (hunks []diff.Hunk, ok bool) {
	st, ok := m.compareState()
	return st.hunks, ok
}

// compareState is compareHunks with what the painter needs from the hunks: the looks of the
// lines and the filler rows of each side. It is built once for a text of the two sides.
func (m *Model) compareState() (compareDiff, bool) {
	if !m.splits.comparing {
		return compareDiff{}, false
	}
	left, right := m.leafDoc(m.splits.Root.A), m.leafDoc(m.splits.Root.B)
	if left == nil || right == nil || isLargeDoc(left) || isLargeDoc(right) {
		// The diff takes all the lines of both sides, which a large document does not
		// hold.
		return compareDiff{}, false
	}
	// A document that cannot tell its version cannot be told apart from an edited one,
	// so its diff is not kept.
	lv, lok := left.(versionedDoc)
	rv, rok := right.(versionedDoc)
	if !lok || !rok {
		hunks := diff.Lines(textLines(left), textLines(right))
		return compareDiff{hunks: hunks, side: newCompareSides(hunks)}, true
	}
	key := compareKey{
		left: m.splits.Root.A.Tab, right: m.splits.Root.B.Tab,
		leftName: left.Name(), rightName: right.Name(),
		leftLines: left.LineCount(), rightLines: right.LineCount(),
		leftVersion: lv.Version(), rightVersion: rv.Version(),
	}
	if c := m.splits.diff; c.valid && c.key == key {
		return c, true
	}
	hunks := diff.Lines(textLines(left), textLines(right))
	m.splits.diff = compareDiff{valid: true, key: key, hunks: hunks, side: newCompareSides(hunks)}
	return m.splits.diff, true
}

// compareKey names the text of the two sides that a kept diff is of.
type compareKey struct {
	left, right               int // the tabs
	leftName, rightName       string
	leftLines, rightLines     int
	leftVersion, rightVersion uint64
}

// compareDiff is the diff of the compare view that compareState kept.
type compareDiff struct {
	valid bool
	key   compareKey
	hunks []diff.Hunk
	side  [2]compareSide
}

// compareLook is how the lines of a difference are painted: the theme's diff colors of
// the editor, as Fresh's compare plugin uses them.
type compareLook uint8

const (
	lookNone compareLook = iota
	lookRemoved
	lookAdded
	lookModified
)

// compareLines returns the look of each line that a hunk covers on side: 0 for the left
// document and 1 for the right one. In a change the lines that have a partner on the
// other side are modified, and the rest are removed on the left or added on the right.
func compareLines(hunks []diff.Hunk, side int) map[int]compareLook {
	looks := map[int]compareLook{}
	for _, h := range hunks {
		start, n, other := h.OldStart, h.OldLines, h.NewLines
		lone := lookRemoved
		if side == 1 {
			start, n, other = h.NewStart, h.NewLines, h.OldLines
			lone = lookAdded
		}
		for k := range n {
			if h.Kind == diff.Change && k < other {
				looks[start+k] = lookModified
			} else {
				looks[start+k] = lone
			}
		}
	}
	return looks
}

// The colors of the gutter marks of a compare view, as Fresh's compare plugin draws them.
var (
	compareAddedColor    = color.RGBA{R: 80, G: 200, B: 120, A: 255}
	compareModifiedColor = color.RGBA{R: 220, G: 160, B: 90, A: 255}
	compareRemovedColor  = color.RGBA{R: 220, G: 90, B: 90, A: 255}
)

// compareSideOf returns which side of the compare view a pane with tab shows: 0 for the
// left one, 1 for the right one, and -1 for a pane that is not a side of it.
func (m *Model) compareSideOf(tab int) int {
	if !m.splits.comparing || m.splits.Root.Leaf() {
		return -1
	}
	switch tab {
	case m.splits.Root.A.Tab:
		return 0
	case m.splits.Root.B.Tab:
		return 1
	}
	return -1
}

// compareDecor gives the pane of a compare view the look of its differences: a background
// on every line that differs from the other side with the part that differs picked out in a
// stronger one, a "~", "-" or "+" in the gutter, a mark in the scrollbar column, and the
// filler rows that keep the two sides level. A pane that is not a side of the compare view
// is returned as it is.
func (m *Model) compareDecor(p split.Pane, pane editorpane.Pane) editorpane.Pane {
	side := m.compareSideOf(p.Tab)
	if side < 0 {
		return pane
	}
	st, ok := m.compareState()
	if !ok {
		return pane
	}
	if !p.Focused {
		pane.TopRow = [2]*split.Node{m.splits.Root.A, m.splits.Root.B}[side].TopRow
		// The sides pan together: this one shows the columns the focused one shows.
		if fp, doc := m.layout().FocusedPane(), m.ed.Active(); doc != nil && fp.Focused {
			left, _ := m.paneGeometry(fp, doc).HScroll(fp.W, fp.H)
			pane.Left, pane.KeepLeft = left, true
		}
	}
	// A line of the other side would push this one out of step when wrapped, so the
	// pane never wraps (see editorpane.Fillers).
	fill := *st.side[side].fill
	fillBG := "diff_add_bg" // a filler row stands for a line only the other side has
	if side == 1 {
		fillBG = "diff_remove_bg"
	}
	fill.Style = grid.Style{BG: style.Color(m.theme, "editor", fillBG)}
	pane.Fillers = &fill
	ends := m.compareEnds()
	if len(st.hunks) == 0 && !ends.differ {
		return pane
	}
	pane.TrackMarks = st.side[side].marks
	hunkLooks := st.side[side].looks
	// A line that no hunk covers and that the two sides end differently is modified, as
	// Fresh marks it, with a note at its end that says how this side ends it.
	endOnly := func(line int) bool {
		if _, in := hunkLooks[line]; in || !ends.differ || pane.Doc == nil {
			return false
		}
		n := pane.Doc.LineCount()
		pseudo := line == n-1 && n > 1 && pane.Doc.Line(line) == "" // the empty text after a final newline
		return line >= 0 && line < n && !pseudo
	}
	looks := func(line int) compareLook {
		if l, ok := hunkLooks[line]; ok {
			return l
		}
		if endOnly(line) {
			return lookModified
		}
		return lookNone
	}
	bg := map[compareLook]grid.Style{}
	for look, key := range map[compareLook]string{lookRemoved: "diff_remove_bg", lookAdded: "diff_add_bg", lookModified: "diff_modify_bg"} {
		if c := style.Color(m.theme, "editor", key); c != nil {
			bg[look] = grid.Style{BG: c}
		}
	}
	inner := pane.RowStyle
	pane.RowStyle = func(line int) (grid.Style, bool) {
		var st grid.Style
		found := false
		if inner != nil {
			st, found = inner(line)
		}
		if b, ok := bg[looks(line)]; ok {
			return editorpane.PatchStyle(st, b), true
		}
		return st, found
	}
	m.compareSpans(&pane, side, st.side[side].partner)
	if ends.differ {
		innerEOL := pane.EOL
		note := editorpane.VirtualText{Text: " " + ends.name[side] + " line ending", Style: grid.Style{FG: compareModifiedColor, Faint: true}}
		pane.EOL = func(line int) []editorpane.VirtualText {
			var out []editorpane.VirtualText
			if innerEOL != nil {
				out = innerEOL(line)
			}
			if endOnly(line) {
				out = append(out, note)
			}
			return out
		}
	}
	marks := map[compareLook]struct {
		text string
		fg   color.Color
	}{
		lookModified: {"~", compareModifiedColor},
		lookAdded:    {"+", compareAddedColor},
		lookRemoved:  {"-", compareRemovedColor},
	}
	gutter, scroll := pane.Gutter, pane.ScrollMarks
	pane.Gutter = func(line int) (editorpane.GutterMark, bool) {
		if mk, ok := marks[looks(line)]; ok {
			return editorpane.GutterMark{Text: mk.text, Style: grid.Style{FG: mk.fg}}, true
		}
		if gutter != nil {
			return gutter(line)
		}
		return editorpane.GutterMark{}, false
	}
	pane.ScrollMarks = func(line int) (editorpane.ScrollMark, bool) {
		if mk, ok := marks[looks(line)]; ok {
			return editorpane.ScrollMark{Style: grid.Style{FG: mk.fg}}, true
		}
		if scroll != nil {
			return scroll(line)
		}
		return editorpane.ScrollMark{}, false
	}
	return pane
}

// compareEnds says how the two sides of the compare view end their lines.
type compareEnds struct {
	differ bool      // one side ends its lines with CRLF and the other with LF
	name   [2]string // "LF" or "CRLF" for each side
}

// compareEnds reads the line endings of the two sides. The diff works on the text of the lines,
// which has no ending, so a difference in how they end is found here.
func (m *Model) compareEnds() compareEnds {
	if !m.splits.comparing || m.splits.Root.Leaf() {
		return compareEnds{}
	}
	l, r := m.leafDoc(m.splits.Root.A), m.leafDoc(m.splits.Root.B)
	if l == nil || r == nil {
		return compareEnds{}
	}
	e := compareEnds{name: [2]string{l.LineEnding(), r.LineEnding()}}
	e.differ = e.name[0] != e.name[1]
	return e
}

// compareSpans adds to the pane the stronger background of the part of each modified line that
// differs from its partner on the other side. A theme without the highlight colors has none.
func (m *Model) compareSpans(pane *editorpane.Pane, side int, partner map[int]int) {
	key := "diff_remove_highlight_bg"
	if side == 1 {
		key = "diff_add_highlight_bg"
	}
	c := style.Color(m.theme, "editor", key)
	doc, other := pane.Doc, m.leafDoc([2]*split.Node{m.splits.Root.A, m.splits.Root.B}[1-side])
	if c == nil || doc == nil || other == nil {
		return
	}
	inner := pane.Overlays
	pane.Overlays = func(line int) []editorpane.StyledSpan {
		var out []editorpane.StyledSpan
		if inner != nil {
			out = inner(line)
		}
		pl, ok := partner[line]
		if !ok || pl >= other.LineCount() {
			return out
		}
		mine, theirs := []rune(doc.Line(line)), []rune(other.Line(pl))
		var from, to int
		var differs bool
		if side == 0 {
			from, to, _, _, differs = changedSpan(mine, theirs)
		} else {
			_, _, from, to, differs = changedSpan(theirs, mine)
		}
		if !differs || to <= from {
			return out
		}
		return append(out, editorpane.StyledSpan{Start: from, End: to, Style: grid.Style{BG: c}, Priority: 1})
	}
}

// syncCompare keeps the two sides of the compare view level: the unfocused side shows the
// same display rows as the focused one, filler rows included. The root calls it after every
// update, so a scroll, a jump, an edit that moves the fillers or a change of focus all find
// the two sides on the same rows.
func (m *Model) syncCompare() {
	if !m.splits.comparing || m.splits.Root.Leaf() {
		return
	}
	st, ok := m.compareState()
	doc := m.ed.Active()
	if !ok || doc == nil {
		return
	}
	nodes := [2]*split.Node{m.splits.Root.A, m.splits.Root.B}
	side := 0
	switch m.splits.Focus {
	case nodes[1]:
		side = 1
	case nodes[0]:
	default:
		return
	}
	other := nodes[1-side]
	od := m.docAt(other.Tab)
	if od == nil {
		return
	}
	top := doc.Top()
	row := st.side[side].fill.BlockStart(top) + m.topRow(top)
	other.Top, other.TopRow = st.side[1-side].fill.LineAt(od.LineCount(), row)
}

// compareCommands are the palette rows of an open compare view: the commands that step
// through its differences, switch its side, merge a change and close it. Without a compare
// view there are none. Compare Files and Compare Active File With are always listed, by the
// palette package.
func (m *Model) compareCommands() []commands.Entry {
	if !m.splits.comparing {
		return nil
	}
	rows := []struct{ name, desc, action string }{
		{"Compare: Next Difference", "Jump to the next difference between the compared files", "compare_next_difference"},
		{"Compare: Previous Difference", "Jump to the previous difference between the compared files", "compare_prev_difference"},
		{"Compare: Switch Side", "Move the focus to the other file of the comparison", "compare_switch_side"},
		{"Compare: Copy Change to Other Side", "Make the other file's lines of the change under the cursor match this file", "compare_copy_change"},
		{"Compare: Take Change from Other Side", "Make this file's lines of the change under the cursor match the other file", "compare_take_change"},
		{"Compare: Close", "Close the side-by-side comparison", "compare_close"},
	}
	out := make([]commands.Entry, len(rows))
	for i, r := range rows {
		out[i] = commands.Entry{Name: r.name, Description: r.desc, Action: r.action, Source: commands.SourceBuiltin, Keybinding: m.shortcutOf(r.action)}
	}
	return out
}

// largeTabIn returns the name of the first of the tabs i and j whose document is in
// large file mode, or "" when neither is. A tab that does not exist is not large.
func (m *Model) largeTabIn(i, j int) string {
	tabs, ok := m.ed.(tabDocs)
	if !ok {
		return ""
	}
	for _, k := range []int{i, j} {
		if doc := tabs.DocAt(k); doc != nil && isLargeDoc(doc) {
			return doc.Name()
		}
	}
	return ""
}

// largeCompared returns the name of the document of the compare view that is in large
// file mode, or "" when there is none. A document can become large after the view
// opened, by a reload of its file.
func (m *Model) largeCompared() string {
	for _, n := range []*split.Node{m.splits.Root.A, m.splits.Root.B} {
		if doc := m.leafDoc(n); doc != nil && isLargeDoc(doc) {
			return doc.Name()
		}
	}
	return ""
}

// compareSwitch moves the focus to the other file of the compare view, as Fresh's Switch
// Side does. The two panes keep their rows, and the caret of the other file goes to the line
// that is on the same display row as the caret that leaves, or on the nearest row of the view
// when that caret is scrolled out of it, so the view does not jump to a caret nobody is
// looking at.
func (m *Model) compareSwitch() {
	if !m.splits.comparing {
		m.notice = uiText("gvim.notice.compare.no_view", noCompareNotice)
		return
	}
	st, ok := m.compareState()
	from := m.ed.Active()
	nodes := [2]*split.Node{m.splits.Root.A, m.splits.Root.B}
	side := 0
	if m.splits.Focus == nodes[1] {
		side = 1
	}
	var at buffer.Pos
	row, view := 0, nodes[1-side] // the view the other pane has now
	top, topRow := view.Top, view.TopRow
	if ok && from != nil {
		at = from.Cursor()
		first := st.side[side].fill.BlockStart(from.Top()) + m.topRow(from.Top())
		row = min(max(st.side[side].fill.TextRow(at.Line), first), first+max(m.focusedBodyRows()-1, 0))
	}
	m.focusEditor()
	m.moveFocus(1)
	if to := m.ed.Active(); ok && to != nil && to != from {
		line, _ := st.side[1-side].fill.LineAt(to.LineCount(), row)
		m.ed.SetCursor(buffer.Pos{Line: line, Col: min(at.Col, len([]rune(to.Line(line))))}, false)
		m.ed.ScrollTo(top)
		m.setTopRow(topRow)
	}
	m.syncCompare()
}

// pickHunk returns the index of the hunk after the caret at (line, col) on side when forward
// is set, and before it otherwise, and whether the search wrapped past an end. A hunk is
// where hunkJump puts the caret, at column 0 of its line, so the next hunk starts on a later
// line, and the previous one on an earlier line or on this line with the caret past its
// start. last is the last line index of the focused document. It returns -1 when hunks is
// empty.
func pickHunk(hunks []diff.Hunk, side, line, col, last int, forward bool) (index int, wrapped bool) {
	if len(hunks) == 0 {
		return -1, false
	}
	start := func(i int) int { return hunkJump(hunks[i], side, last) }
	if forward {
		for i := range hunks {
			if start(i) > line {
				return i, false
			}
		}
		return 0, true
	}
	for i := range slices.Backward(hunks) {
		if s := start(i); s < line || s == line && col > 0 {
			return i, false
		}
	}
	return len(hunks) - 1, true
}

// textLines returns the lines of doc, for diff.Lines.
func textLines(doc Document) []string {
	lines := make([]string, doc.LineCount())
	for i := range lines {
		lines[i] = doc.Line(i)
	}
	return lines
}
