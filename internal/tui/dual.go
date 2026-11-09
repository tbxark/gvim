package tui

import (
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/split"
)

// The Markdown dual view: one document shown twice side by side, the source in
// the left pane and the composed page in the right one, with the focus on the
// source. It is gvim's native form of Fresh's dual mode (markdown_compose.ts): a
// split of two panes over the same buffer, the right one composed. The document's
// view settings stay the default for single panes; the dual view adds a
// derived per-pane override (see panePageView), so closing it gives the
// document's own mode back untouched. See docs/markdown.md.

// The Markdown view modes a document can be shown in, as Fresh names them.
const (
	markdownModeSource   = "source"
	markdownModeRendered = "rendered"
	markdownModeDual     = "dual"
)

// dualView is the state of an open dual view: the tab shown in both panes, and
// the two leaves of the split (source on the left, preview on the right). It is
// valid only while the split tree still holds exactly those two leaves over the
// active tab; any other change clears it (dualValid/syncDual).
type dualView struct {
	tab     int
	source  *splitNode
	preview *splitNode
}

// dualRole is which side of the dual view a pane is.
type dualRole uint8

const (
	dualNone dualRole = iota
	dualSource
	dualPreview
)

// validDual returns the open dual view, or nil when there is none or it no
// longer matches the split tree: a closed pane, another split, a tab change or a
// compare view all end it without touching the document.
func (m *Model) validDual() *dualView {
	d := m.splits.dual
	if d == nil || m.splits.comparing || d.source == nil || d.preview == nil {
		return nil
	}
	leaves := m.splits.Root.Leaves()
	if len(leaves) != 2 || leaves[0] != d.source || leaves[1] != d.preview {
		return nil
	}
	if d.tab != m.activeTab() || d.source.Tab != d.tab || d.preview.Tab != d.tab {
		return nil
	}
	return d
}

// clearDual forgets an open dual view. The document is not touched: the panes go
// back to the document's own view settings.
func (m *Model) clearDual() {
	if d := m.splits.dual; d != nil {
		// A restored dual split carries its modes on the leaves for persistence;
		// once the dual view ends the panes go back to the document's settings.
		if d.source != nil {
			d.source.ViewMode = ""
		}
		if d.preview != nil {
			d.preview.ViewMode = ""
		}
	}
	m.splits.dual = nil
}

// markdownMode derives the view mode of the active document: dual while a valid
// dual view shows it, rendered when its page view is on, source otherwise.
func (m *Model) markdownMode() string {
	if m.validDual() != nil {
		return markdownModeDual
	}
	if doc := m.ed.Active(); doc != nil && m.pageViewFor(doc).On {
		return markdownModeRendered
	}
	return markdownModeSource
}

// dualRoleOf returns which side of the dual view pane p is. The layout's panes
// are in the leaves' reading order, so the source pane is the first and the
// preview the second. A pane built without a rectangle (the focused-pane
// shortcut in the caret and body-row paths) takes the role of the focused leaf.
func (m *Model) dualRoleOf(p splitPane) dualRole {
	d := m.validDual()
	if d == nil {
		return dualNone
	}
	if p.W > 0 {
		for i, lp := range m.layout().Panes {
			if lp != p {
				continue
			}
			switch i {
			case 0:
				return dualSource
			case 1:
				return dualPreview
			}
			return dualNone
		}
	}
	switch m.splits.Focus {
	case d.source:
		return dualSource
	case d.preview:
		return dualPreview
	}
	return dualNone
}

// paneViewMode returns the saved per-pane Markdown view override: the leaves of
// the layout are in the geometry panes' reading order, and a pane built without a
// rectangle takes the focused leaf's mode.
func (m *Model) paneViewMode(p splitPane) string {
	if m.splits.Root == nil {
		return ""
	}
	leaves := m.splits.Root.Leaves()
	if p.W > 0 {
		for i, lp := range m.layout().Panes {
			if lp == p && i < len(leaves) {
				return leaves[i].ViewMode
			}
		}
	}
	if p.Focused && m.splits.Focus != nil {
		return m.splits.Focus.ViewMode
	}
	return ""
}

// panePageView returns the page view of pane p: the document's, unless the pane
// is a side of the dual view, where the preview is composed and the source is
// plain whatever the document's settings say.
func (m *Model) panePageView(p splitPane, doc Document) editorpane.PageView {
	pv := m.pageViewFor(doc)
	switch m.dualRoleOf(p) {
	case dualPreview:
		pv.On = true
		pv.Markdown = m.markdownDoc(doc)
		return pv
	case dualSource:
		return editorpane.PageView{}
	}
	// A restored normal split may carry an explicit per-pane mode (a future or
	// hand-written session); the document default stays for an empty value.
	if m.markdownDoc(doc) {
		switch m.paneViewMode(p) {
		case "rendered":
			pv.On = true
			pv.Markdown = true
			return pv
		case "source":
			return editorpane.PageView{}
		}
	}
	return pv
}

// requireMarkdown reports whether the active document is a Markdown document,
// with the notice that says so when it is not.
func (m *Model) requireMarkdown() bool {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return false
	}
	if !m.markdownDoc(doc) {
		m.notice = "Markdown: the active buffer is not Markdown"
		return false
	}
	return true
}

// setMarkdownPageView sets the active document's page view through the editor's
// own per-buffer action, so the view settings are the one place the mode lives.
func (m *Model) setMarkdownPageView(on bool) {
	m.editorDo(editor.Action("toggle_page_view"), map[string]any{"value": on})
}

// openMarkdownDual opens the dual view of the active Markdown document: the
// focused pane becomes a 50/50 side-by-side split of the same tab, source left
// and composed preview right, with the focus on the source, like Fresh. It needs
// a single-pane Markdown document that is not in large-file mode.
func (m *Model) openMarkdownDual() bool {
	if !m.requireMarkdown() {
		return false
	}
	doc := m.ed.Active()
	if isLargeDoc(doc) {
		m.notice = "Markdown: the buffer is too large for the dual view"
		return false
	}
	if m.validDual() != nil {
		m.notice = "Markdown: " + markdownModeDual + " view"
		return true
	}
	m.clearDual()
	if m.splits.comparing {
		m.notice = "Markdown: close the compare view first"
		return false
	}
	if !m.splits.Root.Leaf() {
		m.notice = "Markdown: the dual view needs a single pane"
		return false
	}
	// The preview is composed by the per-pane override, not by the document, so
	// the document returns to source when the dual view closes (Fresh disables
	// the buffer's compose as it opens the dual tab).
	m.setMarkdownPageView(false)
	m.focusEditor()
	if msg := m.trySplit(sideBySide); msg != "" {
		m.notice = msg
		return false
	}
	root := m.splits.Root
	source, preview := root.A, root.B
	m.splits.dual = &dualView{tab: m.activeTab(), source: source, preview: preview}
	m.focusLeaf(source)
	m.syncDual()
	m.notice = "Markdown: " + markdownModeDual + " view"
	return true
}

// closeMarkdownDual closes the preview pane of the dual view and returns to the
// single source pane. The document keeps its own view settings, so the pane
// shows what it showed before the dual view opened.
func (m *Model) closeMarkdownDual() bool {
	d := m.validDual()
	if d == nil {
		m.clearDual()
		return false
	}
	if d.source != nil {
		d.source.ViewMode = ""
	}
	m.splits.Root = split.RemoveLeaf(m.splits.Root, d.preview)
	m.splits.dual = nil
	m.splits.Focus = nil
	m.splits.maximized = false
	leaves := m.splits.Root.Leaves()
	if len(leaves) > 0 {
		m.focusLeaf(leaves[0])
	}
	return true
}

// leaveDual closes an open dual view, if any.
func (m *Model) leaveDual() {
	if m.validDual() != nil {
		m.closeMarkdownDual()
		return
	}
	m.clearDual()
}

// syncDual keeps the unfocused pane of the dual view on the focused document's
// top line (the compare view's line-anchored sync). The preview wraps at the
// page measure, so it starts at the line with no row offset; the source keeps
// its own wrapped row. The root calls it after every update.
func (m *Model) syncDual() {
	if m.splits.dual == nil {
		return
	}
	d := m.validDual()
	if d == nil {
		m.clearDual()
		return
	}
	doc := m.ed.Active()
	if doc == nil {
		m.clearDual()
		return
	}
	top := doc.Top()
	switch m.splits.Focus {
	case d.source:
		d.preview.Top, d.preview.TopRow = top, 0
	case d.preview:
		d.source.Top, d.source.TopRow = top, 0
	}
}

// markdownModeSource shows the active document as source: no compose and no
// dual view.
func (m *Model) markdownModeSource() {
	if !m.requireMarkdown() {
		return
	}
	m.leaveDual()
	m.setMarkdownPageView(false)
	m.notice = "Markdown: " + markdownModeSource + " view"
}

// markdownModeRendered shows the active document as a composed page.
func (m *Model) markdownModeRendered() {
	if !m.requireMarkdown() {
		return
	}
	m.leaveDual()
	m.setMarkdownPageView(true)
	m.notice = "Markdown: " + markdownModeRendered + " view"
}

// markdownModeDual opens the dual view of the active document.
func (m *Model) markdownModeDual() { m.openMarkdownDual() }

// markdownToggleDual opens the dual view, or closes it and returns to source.
func (m *Model) markdownToggleDual() {
	if !m.requireMarkdown() {
		return
	}
	if m.validDual() != nil {
		m.markdownModeSource()
		return
	}
	m.openMarkdownDual()
}

// markdownCycleMode cycles source -> rendered -> dual -> source, Fresh's order.
func (m *Model) markdownCycleMode() {
	if !m.requireMarkdown() {
		return
	}
	switch m.markdownMode() {
	case markdownModeSource:
		m.markdownModeRendered()
	case markdownModeRendered:
		m.openMarkdownDual()
	default:
		m.markdownModeSource()
	}
}

// markdownModeAction runs one of the Markdown mode palette commands.
func (m *Model) markdownModeAction(action string) {
	switch action {
	case "markdown_mode_source":
		m.markdownModeSource()
	case "markdown_mode_rendered":
		m.markdownModeRendered()
	case "markdown_mode_dual":
		m.markdownModeDual()
	case "markdown_toggle_dual":
		m.markdownToggleDual()
	case "markdown_cycle_mode":
		m.markdownCycleMode()
	}
}
