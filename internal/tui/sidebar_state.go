package tui

// restoreSidebarState applies a restored session's sidebar UI state. It runs at the
// end of New, after the config defaults, the explorer root and the Contents
// section exist: the width form and side, the explorer fold and scroll, and the
// Diagnostics section land at once; the Contents section's open flag waits for the
// first Markdown buffer (toc.restore) and its scroll/selected position for that
// buffer's first scan (toc.pending), because the section has no rows before a scan.
func (m *Model) restoreSidebarState(st SidebarState) {
	if m.tree == nil {
		return
	}
	m.stashPluginSidebarRestore(st)
	m.explorerCollapsed = st.ExplorerCollapsed
	m.explorerWidth = st.Width
	m.explorerSide = st.Side
	// The selection goes back first: the scroll offset is only meaningful with the
	// row the reader had selected (a fresh tree selects the first row, and the
	// startup follow would scroll back to it).
	if st.ExplorerSelected != "" {
		if selector, ok := m.tree.(interface{ Select(string) error }); ok {
			_ = selector.Select(st.ExplorerSelected)
		}
	}
	if st.ExplorerScroll > 0 {
		m.tree.ScrollBy(st.ExplorerScroll)
	}
	m.restoredScroll = true
	m.diag.open = st.DiagnosticsOpen
	m.diag.collapsed = st.DiagnosticsCollapsed
	if st.DiagnosticsRows > 0 {
		m.diag.rows = st.DiagnosticsRows
	}
	m.contents.SetCollapsed(st.ContentsCollapsed)
	if st.ContentsRows > 0 {
		m.contents.SetRows(st.ContentsRows)
	}
	m.toc.restore = &contentsRestore{open: st.ContentsOpen, collapsed: st.ContentsCollapsed, scroll: st.ContentsScroll}
}
