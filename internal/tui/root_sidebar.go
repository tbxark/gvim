package tui

// sidebarSection is a section of the sidebar that keyboard focus can enter: the
// file explorer, then the Contents section while it is open. A section added
// later goes into sidebarSections, and the cycle picks it up with no other
// change.
type sidebarSection int

const (
	sectionExplorer sidebarSection = iota
	sectionContents
)

// sidebarSections lists the sections in cycle order. The explorer is listed while
// there is a tree, even when its panel is hidden, because entering it shows it;
// the Contents section is listed while it is part of the column.
func (m *Model) sidebarSections() []sidebarSection {
	if m.tree == nil {
		return nil
	}
	secs := []sidebarSection{sectionExplorer}
	if m.contents.Open() {
		secs = append(secs, sectionContents)
	}
	return secs
}

// sectionFocused reports whether keyboard focus is in section s.
func (m *Model) sectionFocused(s sidebarSection) bool {
	switch s {
	case sectionExplorer:
		return m.explorerFocus
	case sectionContents:
		return m.contentsFocus
	}
	return false
}

// focusSection gives keyboard focus to section s, and shows the section first when
// it is hidden. Focusing a folded section unfolds it: the keyboard does not go to
// a section with no rows on screen.
func (m *Model) focusSection(s sidebarSection) {
	switch s {
	case sectionExplorer:
		m.explorerCollapsed = false
		m.focusExplorer()
	case sectionContents:
		m.focusContents()
	}
}

// cycleSidebar moves keyboard focus one step through the sidebar sections and then
// the editor, which is the last stop: dir 1 goes forward and -1 goes back. The menu
// closes, because the focus leaves it. A focus that is not in any section, such as
// the tab strip or the menu, counts as the editor.
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
		m.focusEditor()
		return
	}
	m.focusSection(secs[next])
}
