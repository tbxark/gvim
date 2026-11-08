package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/outline"
)

// The Contents sidebar section: the Markdown outline under the File Explorer.
// This is the root side of it: the section state (sidebar.Contents), when it is
// on screen, the scan bookkeeping, the keyboard and the mouse. The widget and
// its geometry live in internal/tui/sidebar; the column's place in the frame in
// internal/tui/frame.
//
// Fresh ships the same section as the bundled plugin markdown_toc.ts. gvim has
// it in the TUI because a terminal outline is part of the sidebar's presentation,
// and the plugin would need a widget runtime gvim does not have; the config
// keys are the plugin's (plugins.markdown_toc), so the documented knobs are the
// same. See docs/sidebar.md.

// tocDebounce is how long the section waits after an edit before it scans
// again: edits arrive per keystroke, and the scan waits for a pause. It is
// Fresh's RESCAN_DEBOUNCE_MS.
const tocDebounce = 300 * time.Millisecond

// tocRescanMsg fires the debounced rescan. seq names the edit that armed it, so
// a tick that a later edit superseded does not scan early.
type tocRescanMsg struct{ seq int }

// tocState is the Contents section's bookkeeping in the root: whether the
// feature is enabled, the buffer the rows describe, and the text version they
// were scanned from, so an edit is noticed once.
type tocState struct {
	enabled bool
	// wanted records that the section was opened by a command (Markdown: Show
	// Outline), so it stays available across buffer switches even with
	// auto_open off. It is cleared by Close Outline.
	wanted bool
	// closed records that the reader closed the section for the buffer they
	// were looking at. The next buffer activation opens it again when
	// auto_open is on, as Fresh remounts its plugin section there.
	closed bool
	// ident names the active buffer the state belongs to.
	ident string
	// version and versionOK are the text version the rows were scanned from.
	version   uint64
	versionOK bool
	scanned   bool
	// seq names the newest debounce armed, and armed says one is in flight, so
	// a stream of messages about the same edit does not keep pushing the scan
	// out.
	seq   int
	armed bool
}

// dividerState is a press on the Contents header: a drag resizes the section,
// a release that never moved folds it, and the x is its own target.
type dividerState struct {
	active    bool
	startY    int
	startRows int
	moved     bool
}

// markdownExtensions are the file extensions the section treats as Markdown,
// as the bundled plugin did.
var markdownExtensions = map[string]bool{
	".md": true, ".markdown": true, ".mdown": true, ".mkd": true, ".mdx": true,
}

// isMarkdownName reports whether a buffer name is a Markdown file.
func isMarkdownName(name string) bool {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return markdownExtensions[strings.ToLower(name[i:])]
	}
	return false
}

// initContents starts the Contents section from deps. A nil MarkdownTOC enables
// it with the defaults.
func (m *Model) initContents(deps Deps) {
	cfg := DefaultMarkdownTOC()
	if deps.MarkdownTOC != nil {
		cfg = *deps.MarkdownTOC
	}
	m.toc.enabled = cfg.Enabled
	m.contents = sidebar.NewContents(cfg.Settings)
	m.contents.FoldHook = m.tocFoldHook
}

// tocFoldHook folds the buffer section under a heading when fold_buffer is on.
// gvim's folds are indentation folds, so a plain Markdown heading usually opens
// none; docs/sidebar.md records the difference from Fresh's per-heading ranges.
func (m *Model) tocFoldHook(heading outline.Heading, expanded bool) {
	if !m.contents.FoldBuffer() {
		return
	}
	m.toggleFoldAt(heading.Line)
}

// contentsSync notices what changed since the last message: a new active
// buffer, an edit of the one the rows describe, or a move of the caret, and
// keeps the section's rows and selection current. It runs after every message
// and does only comparisons until something changed.
func (m *Model) contentsSync() tea.Cmd {
	doc := m.ed.Active()
	if m.tree == nil || doc == nil {
		if m.contents.Open() {
			m.contents.SetOpen(false)
		}
		m.toc.ident = ""
		return nil
	}
	ident := fmt.Sprintf("%d\x00%s", m.activeTab(), doc.Name())
	if ident != m.toc.ident {
		m.toc.ident = ident
		m.toc.closed = false
		m.toc.scanned = false
		m.contents.Reset()
		markdown := isMarkdownName(doc.Name())
		switch {
		case !markdown || !m.toc.enabled || m.toc.closed:
			m.contents.SetOpen(false)
		case m.toc.wanted || m.contents.AutoOpen():
			m.openContents(false)
		default:
			m.contents.SetOpen(false)
		}
	}
	if !m.contents.Open() {
		return nil
	}
	if !isMarkdownName(doc.Name()) {
		m.contents.SetOpen(false)
		return nil
	}
	m.setContentsViewRows()
	m.contents.Follow(doc.Cursor().Line, doc.Top(), m.editorFocused())
	// An edit of the scanned buffer waits for a pause, then rescans. One
	// debounce is enough: the tick fires for the newest edit, and arming on
	// every message would push the scan out for as long as messages arrive.
	if ver, ok := docVersion(doc); ok && m.toc.versionOK && ver != m.toc.version {
		if !m.toc.armed {
			m.toc.armed = true
			return m.armTOCRescan()
		}
	}
	return nil
}

// setContentsViewRows gives the widget the body rows the layout gave the
// section, so Page and the window follow the frame.
func (m *Model) setContentsViewRows() {
	l := m.layout()
	for _, sec := range l.Sections {
		if sec.Kind == sidebar.KindContents {
			m.contents.SetViewRows(sec.Rows())
			return
		}
	}
	m.contents.SetViewRows(0)
}

// openContents puts the section in the column, shows the column if it is
// hidden, and scans the active buffer. focus gives the section the keyboard.
func (m *Model) openContents(focus bool) {
	if m.tree == nil {
		return
	}
	if !m.explorerOpen {
		m.explorerOpen = true
		m.syncViewport()
	}
	m.contents.SetCollapsed(false)
	m.contents.SetOpen(true)
	m.toc.closed = false
	m.rescanContents()
	if focus {
		m.takeContentsFocus()
	} else if !m.explorerFocus && !m.contentsFocus {
		m.contents.SetFocus(false)
	}
}

// closeContents hides the section for the buffer being read. The next buffer
// activation brings it back when auto_open is on.
func (m *Model) closeContents() {
	if !m.contents.Open() {
		return
	}
	m.toc.wanted = false
	m.toc.closed = true
	m.contents.SetOpen(false)
	if m.contentsFocus {
		m.focusEditor()
	}
	m.syncViewport()
}

// rescanContents reads the active buffer and rebuilds the tree.
func (m *Model) rescanContents() {
	doc := m.ed.Active()
	if doc == nil || !m.contents.Open() {
		return
	}
	m.contents.Scan(doc)
	m.toc.scanned = true
	m.toc.armed = false
	m.toc.version, m.toc.versionOK = docVersion(doc)
	m.setContentsViewRows()
	m.contents.Follow(doc.Cursor().Line, doc.Top(), m.editorFocused())
}

// armTOCRescan waits for a pause in the edits before the rescan.
func (m *Model) armTOCRescan() tea.Cmd {
	m.toc.seq++
	seq := m.toc.seq
	return tea.Tick(tocDebounce, func(time.Time) tea.Msg { return tocRescanMsg{seq: seq} })
}

// tocRescan is the debounce that fired: the newest one scans, an older one is
// dropped because a later edit armed its own.
func (m *Model) tocRescan(msg tocRescanMsg) tea.Cmd {
	if msg.seq != m.toc.seq || !m.contents.Open() {
		return nil
	}
	m.toc.armed = false
	m.rescanContents()
	return nil
}

// markdownTOCShown opens the Contents section for the active buffer and gives
// it the keyboard; the palette's Markdown: Show Outline runs it.
func (m *Model) markdownTOCShown() {
	switch {
	case m.tree == nil:
		m.notice = "The Contents section needs the file explorer column"
		return
	case !m.toc.enabled:
		m.notice = "The markdown_toc section is disabled in the config"
		return
	}
	doc := m.ed.Active()
	if doc == nil || !isMarkdownName(doc.Name()) {
		m.notice = "Not a Markdown buffer"
		return
	}
	m.toc.wanted = true
	m.toc.closed = false
	m.contents.Reset()
	m.openContents(true)
}

// markdownTOCClosed hides the Contents section; the palette's Markdown: Close
// Outline runs it.
func (m *Model) markdownTOCClosed() {
	m.closeContents()
}

// markdownTOCToggleFollow flips follow between the cursor and the pane top for
// the session, as Fresh's follow command does.
func (m *Model) markdownTOCToggleFollow() {
	next := sidebar.FollowCursor
	if m.contents.FollowMode() == sidebar.FollowCursor {
		next = sidebar.FollowScroll
	}
	m.contents.SetFollow(next)
	m.notice = "Contents follow: " + next.String()
	if doc := m.ed.Active(); doc != nil {
		m.contents.Follow(doc.Cursor().Line, doc.Top(), m.editorFocused())
	}
}

// editorFocused reports whether the editor pane, not the chrome, has the
// keyboard: the Contents selection follows the pane top when it does not.
func (m *Model) editorFocused() bool {
	return !m.explorerFocus && !m.contentsFocus && !m.tabFocus && !m.dockKeyboard() && len(m.overlays) == 0
}

// jumpContents moves the caret to the selected heading. It is Enter on a row
// and a press on one; the two differ in where the keyboard goes afterwards.
func (m *Model) jumpContents() {
	if line, ok := m.contents.SelectedLine(); ok {
		m.gotoLine(line+1, 1)
	}
}

// scrollContentsToSelection shows the selected heading in the pane, which is
// what browsing the outline does in scroll mode: the caret stays where it was.
func (m *Model) scrollContentsToSelection() {
	if m.contents.FollowMode() != sidebar.FollowScroll {
		return
	}
	if line, ok := m.contents.SelectedLine(); ok {
		m.ed.ScrollTo(line)
	}
}

// sidebarSpecs is the section stack of the column for the layout.
func (m *Model) sidebarSpecs() []sidebar.Spec {
	specs := []sidebar.Spec{{Kind: sidebar.KindExplorer, Collapsed: m.explorerCollapsed}}
	if m.contents.Open() {
		specs = append(specs, sidebar.Spec{
			Kind:      sidebar.KindContents,
			Rows:      m.contents.Rows(),
			Collapsed: m.contents.Collapsed(),
		})
	}
	return specs
}

// takeContentsFocus gives the Contents section the keyboard, showing the column
// and the section first when they are hidden. It does not unfold a folded
// section: a press on the header is the fold control itself, and the release
// decides.
func (m *Model) takeContentsFocus() {
	if m.tree == nil || !m.contents.Open() {
		return
	}
	if !m.explorerOpen {
		m.explorerOpen = true
		m.syncViewport()
	}
	m.tabFocus = false
	m.explorerFocus = false
	m.dock.SetFocus(false)
	m.contentsFocus = true
	m.contents.SetFocus(true)
	m.contents.SetOpen(true)
}

// focusContents gives the Contents section the keyboard, opening it for a
// Markdown buffer and unfolding it first: the keyboard does not go to a section
// with no rows on screen.
func (m *Model) focusContents() {
	if m.tree == nil {
		return
	}
	if !m.contents.Open() {
		doc := m.ed.Active()
		if doc == nil || !isMarkdownName(doc.Name()) || !m.toc.enabled {
			return
		}
		m.toc.wanted = true
		m.openContents(true)
		return
	}
	m.contents.SetCollapsed(false)
	m.takeContentsFocus()
}

// contentsKey handles a key while the Contents section has the keyboard. Its
// tree keys move the selection, fold and jump; the rest are the app-wide
// actions, which keep their Normal bindings, as they do while the explorer has
// the keyboard. Typed text is dropped: it never reaches the buffer.
func (m *Model) contentsKey(kp KeyPress, text string) tea.Cmd {
	if m.contents.Collapsed() {
		switch kp.Name {
		case "Enter", " ":
			m.contents.SetCollapsed(false)
			return nil
		case "Escape":
			m.focusEditor()
			return nil
		}
		return m.sidebarFallthroughKey(kp)
	}
	if len(kp.Mods) == 0 {
		switch kp.Name {
		case "Up":
			m.contents.Move(-1)
			m.scrollContentsToSelection()
			return nil
		case "Down":
			m.contents.Move(1)
			m.scrollContentsToSelection()
			return nil
		case "PageUp":
			m.contents.Page(-1)
			m.scrollContentsToSelection()
			return nil
		case "PageDown":
			m.contents.Page(1)
			m.scrollContentsToSelection()
			return nil
		case "Home":
			m.contents.First()
			m.scrollContentsToSelection()
			return nil
		case "End":
			m.contents.Last()
			m.scrollContentsToSelection()
			return nil
		case "Left":
			m.contents.Collapse()
			m.scrollContentsToSelection()
			return nil
		case "Right":
			m.contents.Expand()
			m.scrollContentsToSelection()
			return nil
		case "Enter":
			m.jumpContents()
			m.focusEditor()
			return nil
		case "Escape":
			m.focusEditor()
			return nil
		}
	}
	return m.sidebarFallthroughKey(kp)
}

// sidebarFallthroughKey resolves a key the sidebar does not handle itself: an
// app-wide action keeps its Normal binding, and anything else is dropped, so a
// letter never types into the buffer.
func (m *Model) sidebarFallthroughKey(kp KeyPress) tea.Cmd {
	if m.keys == nil {
		return nil
	}
	if cmd, handled := m.chordKey(kp, KeyContextNormal); handled {
		return cmd
	}
	if cmd, ok := m.keys.Resolve(kp, KeyContextNormal); ok && explorerFallthrough[cmd.Action] {
		return m.dispatch(cmd)
	}
	return nil
}

// pressSidebarHeader is a left press on a section's header row. The explorer's
// header folds the tree when a section is stacked under it and just takes the
// keyboard when it is alone; the Contents header starts the divider drag, and a
// release that never moved folds the section.
func (m *Model) pressSidebarHeader(section int, y int) tea.Cmd {
	if section == 0 {
		m.takeExplorerFocus()
		if len(m.layout().Sections) > 1 {
			m.explorerCollapsed = !m.explorerCollapsed
		}
		return nil
	}
	m.takeContentsFocus()
	m.divider = dividerState{active: true, startY: y, startRows: m.contents.Rows()}
	return nil
}

// pressContentsRow is a left press on a row of the Contents body: a press on
// the fold glyph folds or unfolds the heading, a press on the row selects it
// and jumps to it, keeping the keyboard in the sidebar as a click previews.
func (m *Model) pressContentsRow(row, x int) tea.Cmd {
	m.takeContentsFocus()
	r, ok := m.contents.RowAt(row)
	if !ok {
		return nil
	}
	if r.HasChildren && x == r.GlyphX {
		m.contents.ToggleFoldPos(m.contents.Scroll() + row)
		return nil
	}
	m.contents.SelectPos(m.contents.Scroll() + row)
	m.jumpContents()
	return nil
}

// wheelContents scrolls the Contents body by a wheel notch.
func (m *Model) wheelContents(up bool) {
	delta := wheelRows
	if up {
		delta = -wheelRows
	}
	m.contents.Wheel(delta)
}

// dragDivider moves the boundary between the sections: the pointer row sets
// the Contents section's rows, and the explorer keeps at least one row.
func (m *Model) dragDivider(y int) {
	delta := m.divider.startY - y
	rows := m.divider.startRows + delta
	if total := m.columnBodyRows(); total > 0 {
		rows = min(rows, total-1)
	}
	rows = max(rows, 1)
	if rows != m.contents.Rows() {
		m.divider.moved = true
		m.contents.SetRows(rows)
	}
	if delta != 0 {
		m.divider.moved = true
	}
}

// releaseDivider ends a press on the Contents header: a drag keeps the new
// rows, a press that never moved folds or unfolds the section.
func (m *Model) releaseDivider() {
	if !m.divider.active {
		return
	}
	if !m.divider.moved {
		m.contents.ToggleCollapsed()
	}
	m.divider = dividerState{}
}

// columnBodyRows is the body rows the column shares between its sections: the
// column's rows less one header per section and its bottom border.
func (m *Model) columnBodyRows() int {
	l := m.layout()
	if len(l.Sections) == 0 {
		return 0
	}
	return max(l.ExBottom-l.ExTitleY+1-len(l.Sections)-1, 0)
}

// paintSidebar paints the explorer column. With one section and no Contents it
// is the explorer panel exactly as it always was; with the Contents section
// open the column is a stack of header rows and bodies that share their border
// rows, and the column's bottom border closes it.
func (m *Model) paintSidebar(g *grid.Grid, l layout) {
	if len(l.Sections) == 0 {
		return
	}
	if len(l.Sections) == 1 && !m.contents.Open() {
		m.explorerPanel.Draw(g, m.explorerGeometry(l))
		return
	}
	for _, sec := range l.Sections {
		spec := sidebar.HeaderSpec{
			Kind:      sec.Kind,
			First:     sec.Index == 0,
			Several:   len(l.Sections) > 1,
			Collapsed: sec.Collapsed,
		}
		sidebar.PaintHeader(g, l.Ex, sec.HeaderY, sidebar.HeaderFor(l.Ex, m.view.icons, spec), m.styles)
		if sec.Collapsed {
			continue
		}
		switch sec.Kind {
		case sidebar.KindExplorer:
			m.explorerPanel.DrawBody(g, m.explorerGeometry(l))
		case sidebar.KindContents:
			m.contents.Draw(g, l.Ex, sec, m.view.icons, m.styles)
		}
	}
	last := l.Sections[len(l.Sections)-1]
	ground := last.HeaderY + 1
	if last.Rows() > 0 {
		ground = last.BodyBottom + 1
	}
	sidebar.PaintGround(g, l.Ex, ground, l.ExBottom-1, m.styles)
	sidebar.PaintBottomBorder(g, l.Ex, l.ExBottom, m.styles)
}
