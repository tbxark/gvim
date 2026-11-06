package ui

// OpenCompare shows the tabs left and right side by side as the compare view, with the
// focus on the left one. It reports false when either tab does not exist. The --diff
// flag calls it at startup, once the two files are open in tabs 0 and 1.
func (m *Model) OpenCompare(left, right int) bool {
	return m.openCompare(left, right)
}

// Comparing reports whether the compare view is open.
func (m *Model) Comparing() bool {
	return m.splits.comparing
}
