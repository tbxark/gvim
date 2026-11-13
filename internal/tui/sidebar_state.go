package tui

import (
	"strings"

	"github.com/tbxark/gvim/internal/tui/sidebar"
)

// maxSidebarSectionRows bounds one section's explicit body height in a session
// (the plugin cap; Contents has its own smaller sidebar.MaxRows cap).
const maxSidebarSectionRows = 1000

// clampSectionRows bounds one section height read from a session or a drag:
// Contents 1..sidebar.MaxRows, every other section 1..maxSidebarSectionRows.
func clampSectionRows(kind sidebar.Kind, n int) int {
	if n <= 0 {
		return 0
	}
	maxRows := maxSidebarSectionRows
	if kind == sidebar.KindContents {
		maxRows = sidebar.MaxRows
	}
	return min(max(n, 1), maxRows)
}

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
	// SectionRows carries every section's explicit height: the explorer, Contents and
	// Diagnostics by name and the plugin sections as "plugin:<id>". Values are clamped on
	// read, and at most the plugin-section cap is applied.
	if n := clampSectionRows(sidebar.KindExplorer, st.SectionRows["explorer"]); n > 0 {
		m.explorerRows = n
	}
	if n := clampSectionRows(sidebar.KindContents, st.SectionRows["contents"]); n > 0 {
		m.contents.SetRows(n)
	}
	if n := clampSectionRows(sidebar.KindDiagnostics, st.SectionRows["diagnostics"]); n > 0 {
		m.diag.rows = n
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
	m.contents.SetCollapsed(st.ContentsCollapsed)
	m.toc.restore = &contentsRestore{open: st.ContentsOpen, collapsed: st.ContentsCollapsed, scroll: st.ContentsScroll}
}

// pluginRowsOf is the requested rows of the plugin sections in a SectionRows map, by panel
// id, clamped and bounded by the session cap. It is nil when there are none.
func pluginRowsOf(sectionRows map[string]int) map[string]int {
	var out map[string]int
	for key, n := range sectionRows {
		id, ok := strings.CutPrefix(key, "plugin:")
		if !ok || len(out) >= maxSessionPluginSections {
			continue
		}
		if rows := clampSectionRows(sidebar.KindPlugin, n); rows > 0 {
			if out == nil {
				out = make(map[string]int)
			}
			out[id] = rows
		}
	}
	return out
}
