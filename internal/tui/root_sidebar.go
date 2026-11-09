package tui

import "github.com/tbxark/gvim/internal/tui/sidebar"

// sidebarSection is a section of the sidebar that keyboard focus can enter: the
// file explorer, the native Contents/Diagnostics sections while they are open, and
// each mounted plugin section. The cycle follows the column's rendered order.
type sidebarSection struct {
	kind  sidebarSectionKind
	panel *mountedPanel // the plugin section, nil for a native section
}

type sidebarSectionKind int

const (
	sectionExplorer sidebarSectionKind = iota
	sectionContents
	sectionDiagnostics
	sectionPlugin
)

// sidebarSections lists the focus stops in rendered order: the explorer first
// (even while its panel is hidden: entering it shows it, as Fresh's first cycle step
// does), then every open native section and mounted plugin section in the column's
// order. A closed Contents/Diagnostics section is not rendered and is not a stop.
func (m *Model) sidebarSections() []sidebarSection {
	if m.tree == nil {
		return nil
	}
	secs := []sidebarSection{{kind: sectionExplorer}}
	for _, sec := range m.layout().Sections {
		switch sec.Kind {
		case sidebar.KindContents:
			secs = append(secs, sidebarSection{kind: sectionContents})
		case sidebar.KindDiagnostics:
			secs = append(secs, sidebarSection{kind: sectionDiagnostics})
		case sidebar.KindPlugin:
			mp := m.pluginSectionAt(sec.Index)
			if mp != nil {
				secs = append(secs, sidebarSection{kind: sectionPlugin, panel: mp})
			}
		}
	}
	return secs
}

// sectionFocused reports whether keyboard focus is in section s.
func (m *Model) sectionFocused(s sidebarSection) bool {
	switch s.kind {
	case sectionExplorer:
		return m.explorerFocus
	case sectionContents:
		return m.contentsFocus
	case sectionDiagnostics:
		return m.diag.focus
	case sectionPlugin:
		return s.panel != nil && m.plug.panels.focus == s.panel.id && s.panel.panel != nil && s.panel.panel.Focused()
	}
	return false
}

// focusSection gives keyboard focus to section s, and shows the section first when
// it is hidden. A plugin section's previous panel loses the keyboard and its
// provider sees focus_lost/focus_gained through focusPluginSection. Focusing a
// folded section unfolds it: the keyboard does not go to a section with no rows on
// screen. A non-plugin stop blurs any focused plugin first, so the plugin key layer
// cannot swallow keys after the focus has left.
func (m *Model) focusSection(s sidebarSection) {
	if s.kind != sectionPlugin {
		m.blurPluginPanels()
	}
	switch s.kind {
	case sectionExplorer:
		m.explorerCollapsed = false
		m.focusExplorer()
	case sectionContents:
		m.focusContents()
	case sectionDiagnostics:
		m.focusDiagnostics()
	case sectionPlugin:
		if s.panel == nil || s.panel.panel == nil {
			return
		}
		if !m.explorerOpen {
			m.explorerOpen = true
			m.syncViewport()
		}
		if m.plug.panels.collapsed[s.panel.id] {
			m.togglePluginSection(s.panel)
		}
		m.focusPluginSection(s.panel)
	}
}

// cycleSidebar moves keyboard focus one step through the rendered sidebar sections
// and then the editor, which is the last stop: dir 1 goes forward and -1 goes back.
// The menu closes, because the focus leaves it. A focus that is not in any section,
// such as the tab strip or the menu, counts as the editor. Plugin sections are in
// the cycle like Fresh's focus_next_sidebar_section; the F7 chrome cycle is unchanged.
func (m *Model) cycleSidebar(dir int) {
	secs := m.sidebarSections()
	stops := len(secs) + 1 // the sections, then the editor
	pos := stops - 1       // the editor, unless a section has the focus
	for i, s := range secs {
		if m.sectionFocused(s) {
			pos = i
			break
		}
	}
	next := (pos + dir + stops) % stops
	m.menu = m.menu.Close()
	if next == len(secs) {
		m.blurPluginPanels()
		m.focusEditor()
		return
	}
	m.focusSection(secs[next])
}
