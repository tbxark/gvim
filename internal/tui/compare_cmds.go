package tui

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tbxark/gvim/internal/tui/browser"
	"github.com/tbxark/gvim/pluginapi"
)

// comparePick is a Compare Files or Compare Active File With command that waits for the file
// browser to choose the file it still needs. left is the first file once it is chosen, and
// "" before.
type comparePick struct {
	left string
}

// compareCommand runs the compare commands that open, merge and close a compare view. The
// commands that move through an open one are in splitAction.
func (m *Model) compareCommand(action string) {
	switch action {
	case "compare_files":
		m.pickCompareFile(&comparePick{}, "Compare (left file)")
	case "compare_with_file":
		doc := m.ed.Active()
		path := ""
		if doc != nil {
			path = m.tabPath(m.activeTab())
		}
		if path == "" {
			m.notice = "Compare: the active buffer is not a file"
			return
		}
		m.pickCompareFile(&comparePick{left: path}, "Compare "+filepath.Base(path)+" with")
	case "compare_copy_change":
		m.compareMerge(true)
	case "compare_take_change":
		m.compareMerge(false)
	case "compare_close":
		m.compareClose()
	}
}

// pickCompareFile opens the file browser under title for the file that pick waits for.
func (m *Model) pickCompareFile(pick *comparePick, title string) {
	dir := ""
	if m.tree != nil {
		dir = m.tree.Root()
	}
	m.splits.pick = pick
	m.pushOverlay(browser.New(browser.Options{
		Dir: dir, Styles: m.styles, Now: m.now, Title: title,
	}))
}

// comparePicked takes the file the browser chose for the waiting compare command: the left
// file of Compare Files, which asks for the right one next, or the right file that starts
// the comparison.
func (m *Model) comparePicked(path string) {
	pick := m.splits.pick
	m.splits.pick = nil
	if pick.left == "" {
		m.pickCompareFile(&comparePick{left: path}, "Compare "+filepath.Base(path)+" with")
		return
	}
	m.compareFiles(pick.left, path)
}

// compareFiles shows the files left and right side by side as the compare view. A file that
// is not open yet is opened in a tab, and closing the view closes that tab again unless it
// was edited. Both files must be small: the diff holds all their lines.
func (m *Model) compareFiles(left, right string) {
	if left == "" || right == "" {
		return
	}
	if filepath.Clean(left) == filepath.Clean(right) {
		m.notice = "Compare: that is the same file twice"
		return
	}
	if pc, ok := m.ed.(pathClassifier); ok {
		for _, p := range []string{left, right} {
			if pc.IsLargePath(p) {
				m.notice = filepath.Base(p) + " is too large to compare"
				return
			}
		}
	}
	var owned []string
	for _, p := range []string{left, right} {
		if m.tabOfPath(p) >= 0 {
			continue
		}
		if err := m.openFileErr(p, false); err != nil {
			m.notice = "Compare: " + err.Error()
			return
		}
		owned = append(owned, p)
	}
	l, r := m.tabOfPath(left), m.tabOfPath(right)
	if l < 0 || r < 0 || l == r {
		m.notice = "Compare: that is the same file twice"
		return
	}
	if !m.openCompare(l, r) {
		return
	}
	m.splits.owned = owned
	m.focusEditor()
	m.compareSummary(true)
}

// tabOfPath returns the index of the tab that holds the file at path, or -1.
func (m *Model) tabOfPath(path string) int {
	path = filepath.Clean(path)
	for i, t := range m.ed.Tabs() {
		if t.Path != "" && filepath.Clean(t.Path) == path {
			return i
		}
	}
	return -1
}

// compareSummary tells how many differences the compare view has, and goes to the first one
// when jump is set.
func (m *Model) compareSummary(jump bool) {
	hunks, ok := m.compareHunks()
	switch {
	case !ok:
	case len(hunks) == 0:
		m.notice = m.sameTextNotice()
	default:
		m.notice = "Compare: " + strconv.Itoa(len(hunks)) + " difference(s)"
		if jump {
			m.compareStep(true)
		}
	}
}

// compareClose closes the compare view. The file shown in the focused pane stays on the
// screen. A file that the view opened itself is closed with it, unless it has unsaved changes:
// that one stays open in its tab.
func (m *Model) compareClose() {
	if !m.splits.comparing {
		m.notice = noCompareNotice
		return
	}
	owned := m.splits.owned
	m.splits.owned = nil
	m.collapseSplits()
	t := m.tabPort()
	for _, p := range owned {
		if i := m.tabOfPath(p); i >= 0 && t != nil {
			_ = m.closeTab(t, i, false)
		}
	}
	m.syncViewport()
}

// compareMerge makes the lines of the difference under the caret the same on both sides, as
// one edit that one undo takes back. With toOther set the other side takes this side's lines
// (vimdiff's dp), and otherwise this side takes the other's (do).
func (m *Model) compareMerge(toOther bool) {
	if !m.splits.comparing {
		m.notice = noCompareNotice
		return
	}
	st, ok := m.compareState()
	pe := m.plug.pe
	if !ok || pe == nil {
		m.notice = "Compare: this editor cannot merge a change"
		return
	}
	nodes := [2]*splitNode{m.splits.Root.A, m.splits.Root.B}
	side := 0
	if m.splits.Focus == nodes[1] {
		side = 1
	}
	i := hunkAt(st.hunks, side, m.ed.Active().Cursor().Line)
	if i < 0 {
		m.notice = "Compare: the cursor is not on a difference"
		return
	}
	from, to := 1-side, side
	if toOther {
		from, to = side, 1-side
	}
	src, dst := m.leafDoc(nodes[from]), m.leafDoc(nodes[to])
	bufs := pe.ListBuffers()
	if src == nil || dst == nil || nodes[to].Tab >= len(bufs) {
		m.notice = "Compare: the other tab has no document"
		return
	}
	h := st.hunks[i]
	sStart, sN, dStart, dN := h.OldStart, h.OldLines, h.NewStart, h.NewLines
	if from == 1 {
		sStart, sN, dStart, dN = h.NewStart, h.NewLines, h.OldStart, h.OldLines
	}
	lines := make([]string, sN)
	for k := range lines {
		lines[k] = src.Line(sStart + k)
	}
	edit := replaceLines(dst.LineCount(), func(l int) int { return len(dst.Line(l)) }, dStart, dStart+dN, lines)
	if err := pe.Edit(bufs[nodes[to].Tab].ID, []pluginapi.Edit{edit}, pluginapi.EditOptions{}); err != nil {
		m.notice = "Compare: " + filepath.Base(dst.Name()) + " was not changed: " + err.Error()
		return
	}
	m.compareSummary(false)
}

// replaceLines returns the edit that replaces the lines [start, end) of a document of count
// lines with src, so that the document then has exactly those lines there. lineLen is the
// length in bytes of a line, for the columns that reach the end of the text. The edit
// takes care of the line break next to the range: lines in the middle of the text end in one,
// and the last line has none, so a range that reaches the end of the text takes the break
// before it instead.
func replaceLines(count int, lineLen func(int) int, start, end int, src []string) pluginapi.Edit {
	text := strings.Join(src, "\n")
	atEnd := end >= count
	switch {
	case !atEnd:
		if len(src) > 0 {
			text += "\n"
		}
		return pluginapi.Edit{Range: pluginapi.Range{From: pluginapi.Pos{Line: start}, To: pluginapi.Pos{Line: end}}, Text: text}
	case start > 0:
		// From the end of the line before the range to the end of the text.
		from := pluginapi.Pos{Line: start - 1, Col: lineLen(start - 1)}
		to := pluginapi.Pos{Line: count - 1, Col: lineLen(count - 1)}
		if len(src) > 0 {
			text = "\n" + text
		}
		return pluginapi.Edit{Range: pluginapi.Range{From: from, To: to}, Text: text}
	default:
		to := pluginapi.Pos{Line: count - 1, Col: lineLen(count - 1)}
		return pluginapi.Edit{Range: pluginapi.Range{To: to}, Text: text}
	}
}
