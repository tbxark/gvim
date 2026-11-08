package tui

// shiftTab is the index of tab t after tab i closes: a tab after i moves down one index,
// and tab i itself has no index any more, which is -1.
func shiftTab(t, i int) int {
	switch {
	case t == i:
		return -1
	case t > i:
		return t - 1
	}
	return t
}

// noteTab records a change of the active tab. The tab that was active before the change
// becomes the previous tab, so switch_to_previous_tab goes back to it. It runs before and
// after each split action, and after each update, so every switch is recorded once.
//
// The editor keeps no tab history, so the history is kept here, and it is only the last
// switch. Fresh keeps a list of focused tabs, which differs only after the previous tab
// closes: Fresh then goes to the one before it.
func (m *Model) noteTab() {
	cur := m.activeTab()
	if cur == m.splits.seenTab {
		return
	}
	if m.splits.seenTab >= 0 && cur >= 0 {
		m.splits.prevTab = m.splits.seenTab
	}
	m.splits.seenTab = cur
}

// windowStep moves the focus delta panes, as next_pane does, when the tree is split. With
// no split there is one pane, and the window is the tab strip, so the step moves the active
// tab delta places and wraps at both ends.
func (m *Model) windowStep(delta int) {
	if !m.splits.Root.Leaf() {
		m.moveFocus(delta)
		return
	}
	n, i := len(m.ed.Tabs()), m.activeTab()
	if n < 2 || i < 0 {
		return
	}
	m.activateTab(((i+delta)%n + n) % n)
}

// switchPreviousTab makes the tab that was active before the last switch the active one.
// A second press switches back. With no earlier switch to go back to, it says so.
func (m *Model) switchPreviousTab() {
	prev := m.splits.prevTab
	if prev < 0 || prev >= len(m.ed.Tabs()) || prev == m.activeTab() {
		m.notice = "No previous tab"
		return
	}
	m.activateTab(prev)
}
