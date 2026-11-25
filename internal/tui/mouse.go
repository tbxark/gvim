// Mouse routing: clicks, drags and the wheel, translated into editor, explorer
// and chrome actions.

package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/sidebar"
)

// handleClick routes a left press to the part of the frame under it, and a
// middle press on a tab closes the tab. A press on the explorer goes to the
// panel, which answers with its own messages. Presses that have no effect yet
// are dropped here.
func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	mo := tea.Mouse(msg)
	// A left press may be a double or a triple click; the dock's word/line
	// selection and the editor's word/line selection both read the count once.
	count := 0
	if mo.Button == tea.MouseLeft {
		count = m.clicks.PressWindow(m.now(), mo.X, mo.Y, m.doubleClickWindow())
	}
	// The dock's rows belong to the shell: the dock decides whether the press is
	// forwarded, opens a link, scrolls the scrollback or only takes the focus.
	if cmd, taken := m.dockMouse(msg, mo.X, mo.Y, count); taken {
		return cmd
	}
	t := m.layout().Hit(mo.X, mo.Y)
	if mo.Button == tea.MouseMiddle {
		m.mx.extend, m.mx.alt, m.mx.ctrl, m.mx.super = false, false, false, false
		if t.Kind == frame.HitTab || t.Kind == frame.HitTabClose {
			return m.requestClose(t.Index)
		}
		if t.Kind == frame.HitEditorText {
			return m.middleClickPane(t.Pane, mo.X, mo.Y)
		}
		return nil
	}
	if mo.Button == tea.MouseRight {
		m.mx.extend, m.mx.alt, m.mx.ctrl, m.mx.super = false, false, false, false
		// Ctrl+Right-click inspects the theme colors at the clicked cell; a plain
		// right click opens the context menu.
		if mo.Mod&tea.ModCtrl != 0 {
			m.inspectThemeAt(mo.X, mo.Y)
			return nil
		}
		return m.rightClick(msg)
	}
	if mo.Button != tea.MouseLeft {
		return nil
	}
	m.noteMods(mo)
	m.drag = drag{}
	switch t.Kind {
	case frame.HitTab:
		m.focusEditor()
		m.activateTab(t.Index)
		m.drag = drag{active: true, tab: &tabDrag{index: t.Index, startX: mo.X, startY: mo.Y}}
	case frame.HitTabClose:
		return m.requestClose(t.Index)
	case frame.HitTabNew:
		// As Fresh's "+", the button opens a small menu of what a new tab can be.
		m.showContextMenu(mo, plusMenuItems())
	case frame.HitTabScrollBefore:
		m.scrollTabs(-1)
	case frame.HitTabScrollAfter:
		m.scrollTabs(1)
	case frame.HitWelcomeRow:
		m.focusEditor()
		return m.runWelcome(t.Index)
	case frame.HitExplorerRow, frame.HitExplorerBody:
		// The press is resolved against the tree as it is scrolled now. The focus
		// comes after it and does not move the view, so a wheel scroll is kept.
		cmd := m.explorerMouse(msg)
		m.takeExplorerFocus()
		return cmd
	case frame.HitExplorerTitleClose, frame.HitExplorerTitleReload, frame.HitExplorerGrip:
		return m.explorerMouse(msg)
	case frame.HitSidebarHeader:
		// Every section header (explorer, Contents, Diagnostics, plugin) is the shared
		// divider/header: a press starts the height drag, a release without motion folds
		// the section. Focus follows the pressed section.
		return m.pressSidebarHeader(t.Section, mo.Y)
	case frame.HitSidebarClose:
		if sec, ok := m.sidebarSection(t.Section); ok {
			switch sec.Kind {
			case sidebar.KindDiagnostics:
				m.closeDiagPanel()
				return nil
			case sidebar.KindPlugin:
				m.closePluginSection(t.Section)
				return nil
			}
		}
		m.closeContents()
		return nil
	case frame.HitSidebarRow:
		if sec, ok := m.sidebarSection(t.Section); ok {
			switch sec.Kind {
			case sidebar.KindDiagnostics:
				return m.pressDiagRow(t.Index)
			case sidebar.KindPlugin:
				return m.pressPluginSection(t.Section, mo)
			}
		}
		return m.pressContentsRow(t.Index, mo.X)
	case frame.HitSidebarBody:
		if sec, ok := m.sidebarSection(t.Section); ok {
			switch sec.Kind {
			case sidebar.KindDiagnostics:
				m.takeDiagFocus()
				return nil
			case sidebar.KindPlugin:
				return m.pressPluginSection(t.Section, mo)
			}
		}
		m.takeContentsFocus()
		return nil
	case frame.HitStatusPalette:
		return m.openPalette(">")
	case frame.HitStatusSegment:
		switch t.Index {
		case frame.StatusSegmentMessages:
			// Fresh's {messages} opens the status log; gvim's is the warnings list.
			return m.dispatch(Command{Action: "show_warnings"})
		case frame.StatusSegmentTerminalRestart:
			return m.dispatch(Command{Action: "restart_terminal"})
		}
		return m.statusSegmentClick(t.Index)
	case frame.HitStatusPlugin:
		if entry, ok := m.statusExtraEntryAt(t.Index); ok && entry.source == statusExtraEnv {
			return m.envStatusClick()
		}
		return m.plugStatusClick(t.Index)
	case frame.HitEditorText, frame.HitGutter, frame.HitScrollbarTrack:
		if _, ok := m.hexAtPane(t.Pane); ok {
			m.focusPane(t.Pane)
			m.focusEditor()
			if t.Kind == frame.HitScrollbarTrack {
				m.hexScrollTo(t.Pane, mo.Y)
			}
			return nil
		}
		return m.pressPane(t.Pane, mo.X, mo.Y, count)
	case frame.HitSplitBorder:
		m.pressBorder(t.Index, count)
	case frame.HitNone:
		l := m.layout()
		if mo.Y == l.StatusY {
			for _, r := range l.Status {
				if !r.Covers(mo.X) {
					continue
				}
				txt := strings.TrimSpace(r.Text)
				if r.Style == frame.StyleError || strings.HasPrefix(txt, "[E:") || strings.HasPrefix(txt, "[W:") || strings.HasPrefix(txt, "[I:") {
					return m.toggleDiagnosticsPanel()
				}
			}
		}
	}
	return nil
}

// explorerMouse gives a mouse message to the explorer panel, which runs its
// press, drag, release and wheel handling. The panel ignores what is not its
// own, so the root sends every mouse message here. A hidden panel gets zero
// geometry and ignores the message. closeExplorer resets the panel, so a drag
// that was in progress does not survive a hide.
func (m *Model) explorerMouse(msg tea.Msg) tea.Cmd {
	if m.tree == nil {
		return nil
	}
	g := expanel.Geometry{}
	if m.explorerOpen {
		g = m.explorerGeometry(m.layout())
	}
	next, cmd := m.explorerPanel.Update(msg, g)
	m.explorerPanel = next
	return cmd
}

// pressEditor is a press in the editor body, already resolved to hit in pane p by
// pressPane. A text or gutter cell places the caret, a count of 2 selects the word
// and a count of 3 the line. A press on the scrollbar track moves the viewport to
// the press, and a press on the thumb grabs it for the drag.
func (m *Model) pressEditor(p editorpane.Pane, hit editorpane.Hit, count int) tea.Cmd {
	m.lastPress = count
	m.focusEditor()
	switch hit.Kind {
	case editorpane.HitScrollbarThumb:
		m.drag = drag{active: true, thumb: true}
	case editorpane.HitScrollbarTrack:
		m.ed.ScrollTo(p.ScrollTopFor(m.layout().FocusedPane().W, m.bodyRows(), hit.Row))
		m.drag = drag{active: true, thumb: true}
	case editorpane.HitHScrollbar:
		m.drag = drag{active: true, hbar: true}
		m.panBar(hit.Col + m.layout().FocusedPane().X)
	case editorpane.HitText, editorpane.HitGutter:
		if hit.Fold && m.toggleFoldAt(hit.Pos.Line) {
			return nil
		}
		if hit.Kind == editorpane.HitText && count == 1 {
			if m.mx.alt && !m.mx.ctrl && !m.mx.super {
				if mc, ok := m.ed.(multiCursorEditor); ok && hit.Below == 0 {
					mc.ToggleCursorAt(hit.Pos)
					m.drag = drag{}
					return nil
				}
			}
			if m.mx.super && hit.Below == 0 {
				if doc := m.ed.Active(); doc != nil {
					if u, ok := urlAtRuneCol(doc.Line(hit.Pos.Line), hit.Pos.Col); ok {
						m.drag = drag{}
						m.confirmOpenURL(u)
						return nil
					}
				}
				m.collapseCarets()
				m.ed.SetCursor(hit.Pos, false)
				m.drag = drag{}
				return m.dispatch(Command{Action: "lsp_goto_definition"})
			}
			if m.mx.ctrl && !m.mx.alt && hit.Below == 0 {
				if doc := m.ed.Active(); doc != nil {
					if u, ok := urlAtRuneCol(doc.Line(hit.Pos.Line), hit.Pos.Col); ok {
						m.drag = drag{}
						m.confirmOpenURL(u)
						return nil
					}
				}
			}
			if !m.mx.extend && !m.mx.alt && !m.mx.super && hit.Below == 0 && m.toggleMarkdownCheckboxAt(hit.Pos) {
				m.drag = drag{}
				return nil
			}
		}
		if count == 1 && !m.mx.extend {
			if t, ok := m.lineTargetAt(m.ed.Active(), hit.Pos.Line); ok {
				m.drag = drag{}
				return m.runLineTarget(t, m.ed.Active())
			}
		}
		if hit.Below > 0 && count == 1 && !m.mx.extend && m.placeVirtualLine(hit) {
			return nil
		}
		d := m.placeCaret(hit.Pos, count, hit.Virtual)
		if hit.Kind == editorpane.HitGutter && count == 1 && !m.mx.extend {
			d.gutterLine = true
			d.gutterStartLine = hit.Pos.Line
		}
		m.drag = d
	}
	return nil
}

// middleClickPane focuses pane i, places the caret at (x, y) if on text, and pastes
// into writable buffers.
func (m *Model) middleClickPane(i, x, y int) tea.Cmd {
	if _, ok := m.hexAtPane(i); ok {
		m.focusPane(i)
		m.focusEditor()
		return nil
	}
	p, hit, top, ok := m.paneHitIn(i, x, y)
	m.focusPane(i)
	m.focusEditor()
	if ok {
		m.ed.ScrollTo(top)
		if m.splits.comparing {
			m.setTopRow(p.TopRow)
		}
		if hit.Kind == editorpane.HitText {
			m.collapseCarets()
			m.ed.SetCursor(hit.Pos, false)
		}
	}
	m.drag = drag{}
	if m.Holds("read_only") {
		return nil
	}
	return m.dispatch(Command{Action: "paste"})
}

// placeCaret moves the caret to pos, or selects the word or the line under it
// for counts 2 and 3. It returns the drag that follows the press, which grows
// the selection by the same unit.
func (m *Model) placeCaret(pos buffer.Pos, count int, virtual int) drag {
	if count == 1 && m.mx.extend {
		m.ed.SetCursor(pos, true)
		return drag{active: true, unit: editor.UnitChar}
	}
	m.collapseCarets()
	switch count {
	case 2:
		m.ed.SetCursor(pos, false)
		m.ed.Do("select_word", nil)
		return drag{active: true, unit: editor.UnitWord}
	case 3:
		m.ed.SetCursor(buffer.Pos{Line: pos.Line}, false)
		m.ed.ExtendSelection(pos, editor.UnitLine)
		return drag{active: true, unit: editor.UnitLine}
	}
	m.ed.SetCursor(pos, false)
	m.parkVirtualCaret(virtual)
	return drag{active: true, unit: editor.UnitChar}
}

// placeVirtualLine parks the caret on the virtual line below the buffer end that a
// press asked for, when the editor has vertical virtual space on. It reports
// whether it took the press; false falls back to the clamped caret.
func (m *Model) placeVirtualLine(hit editorpane.Hit) bool {
	if m.pageViewActive() {
		return false
	}
	vl, ok := m.ed.(virtualLinesEditor)
	if !ok {
		return false
	}
	m.collapseCarets()
	m.ed.SetCursor(hit.Pos, false)
	if !vl.SetVirtualLines(hit.Below, hit.LineCol) {
		return false
	}
	m.drag = drag{active: true, unit: editor.UnitChar}
	return true
}

// parkVirtualCaret puts the caret n display columns past the end of its line when
// a press asked for it and the editor has virtual space on. The click on the line
// end has already put the caret there.
func (m *Model) parkVirtualCaret(n int) {
	if n <= 0 {
		return
	}
	if m.pageViewActive() {
		return
	}
	if vs, ok := m.ed.(VirtualSpaceEditor); ok && vs.VirtualSpaceEnabled() {
		vs.SetVirtualColumns(n)
	}
}

// handleDrag moves the grab of a text selection or of the scrollbar thumb. The
// pointer may leave the pane: the thumb follows the row, and the text end
// follows the nearest cell. It returns the command that keeps a text drag
// scrolling while the pointer is held outside the pane.
func (m *Model) handleDrag(mo tea.Mouse) tea.Cmd {
	if m.divider.active && mo.Button == tea.MouseLeft {
		m.dragDivider(mo.Y)
		return nil
	}
	if !m.drag.active || mo.Button != tea.MouseLeft {
		return nil
	}
	if m.drag.border != nil {
		m.dragBorder(mo.X, mo.Y)
		return nil
	}
	if m.drag.tab != nil {
		m.dragTab(mo)
		return nil
	}
	doc := m.ed.Active()
	if doc == nil {
		return nil
	}
	fp := m.layout().FocusedPane()
	w, h := fp.W, fp.H
	if h <= 0 || w <= 0 {
		return nil
	}
	if m.drag.hbar {
		m.panBar(mo.X)
		return nil
	}
	if m.drag.thumb {
		// The track is the text rows, as the press maps it: the horizontal bar under
		// them is not part of it.
		rows := m.bodyRows()
		if rows <= 0 {
			return nil
		}
		y := min(max(mo.Y-fp.Y, 0), rows-1)
		m.ed.ScrollTo(m.paneFor(fp, doc).ScrollTopFor(w, rows, y))
		return nil
	}
	m.drag.last = mo
	return m.dragSelect(false)
}

// handleWheel scrolls the panel's tree over the explorer, and the editor
// viewport over the editor body, by wheelRows per notch. The caret stays where
// it is in both cases, and a wheel over an unfocused split pane scrolls that pane.
func (m *Model) handleWheel(msg tea.MouseWheelMsg) tea.Cmd {
	m.cancelMouseHover()
	mo := tea.Mouse(msg)
	l := m.layout()
	if cmd, taken := m.dockMouse(msg, mo.X, mo.Y, 1); taken {
		return cmd
	}
	if mo.Y == l.StripY && mo.X >= l.EditorX && mo.X < l.EditorX+l.EditorW {
		// The tab row scrolls the strip one tab per notch.
		switch mo.Button {
		case tea.MouseWheelUp:
			m.scrollTabs(-1)
		case tea.MouseWheelDown:
			m.scrollTabs(1)
		}
		return nil
	}
	t := l.Hit(mo.X, mo.Y)
	switch t.Kind {
	case frame.HitExplorerRow, frame.HitExplorerBody, frame.HitExplorerTitleClose, frame.HitExplorerTitleReload, frame.HitExplorerGrip:
		return m.explorerMouse(msg)
	case frame.HitSidebarRow, frame.HitSidebarBody, frame.HitSidebarHeader:
		if sec, ok := m.sidebarSection(t.Section); ok {
			switch sec.Kind {
			case sidebar.KindDiagnostics:
				m.wheelDiagPanel(mo.Button == tea.MouseWheelUp)
				return nil
			case sidebar.KindPlugin:
				if t.Kind != frame.HitSidebarHeader {
					return m.wheelPluginSection(t.Section, mo)
				}
				return nil
			}
		}
		m.wheelContents(mo.Button == tea.MouseWheelUp)
		return nil
	case frame.HitEditorText, frame.HitGutter, frame.HitScrollbarTrack:
		if m.panWheel(t.Pane, mo) {
			return nil
		}
		delta := 0
		rows := m.wheelRows()
		switch mo.Button {
		case tea.MouseWheelUp:
			delta = -rows
		case tea.MouseWheelDown:
			delta = rows
		}
		if delta != 0 {
			if leaves := m.layout().Panes; t.Pane >= 0 && t.Pane < len(leaves) {
				if m.hexPaneWheel(t.Pane, delta) {
					return nil
				}
			}
			m.scrollPane(t.Pane, delta)
		}
	}
	return nil
}
