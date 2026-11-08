package tui

// OpenCompare shows the tabs left and right side by side as the compare view, with the
// focus on the left one. It reports false when either tab does not exist. The --diff
// flag calls it at startup, once the two files are open in tabs 0 and 1. It then says how many
// differences there are and goes to the first one, as Compare Files does, so the startup notice
// is replaced.
func (m *Model) OpenCompare(left, right int) bool {
	if !m.openCompare(left, right) {
		return false
	}
	// The files were opened for the comparison, so closing it closes them.
	m.splits.owned = []string{m.tabPath(left), m.tabPath(right)}
	m.compareSummary(true)
	return true
}

// Comparing reports whether the compare view is open.
func (m *Model) Comparing() bool {
	return m.splits.comparing
}
