package editor

import "slices"

// touchRecent moves d to the front of the editor's MRU tab list. Called when a tab is
// added or becomes active. A document that is not a tab is ignored by RecentTabIndices.
func (e *Editor) touchRecent(d *Document) {
	if d == nil {
		return
	}
	e.recent = slices.DeleteFunc(e.recent, func(x *Document) bool { return x == d })
	e.recent = append([]*Document{d}, e.recent...)
}

// forgetRecent drops d from the MRU list (a closed or replaced tab).
func (e *Editor) forgetRecent(d *Document) {
	if d == nil || len(e.recent) == 0 {
		return
	}
	e.recent = slices.DeleteFunc(e.recent, func(x *Document) bool { return x == d })
}

// RecentTabIndices returns the open tabs in most-recently-used order: the active tab
// first, then the tabs in the order they were last active. Tabs that were never active
// (or were adopted directly) follow in tab order. It is the order multi-buffer completion
// and dabbrev search the other open buffers, Fresh's MRU order.
func (e *Editor) RecentTabIndices() []int {
	e.adoptAll()
	out := make([]int, 0, len(e.docs))
	seen := make([]bool, len(e.docs))
	add := func(i int) {
		if i < 0 || i >= len(e.docs) || seen[i] {
			return
		}
		seen[i] = true
		out = append(out, i)
	}
	if e.active >= 0 && e.active < len(e.docs) {
		add(e.active)
	}
	for _, d := range e.recent {
		if d != nil {
			add(slices.Index(e.docs, d))
		}
	}
	for i := range e.docs {
		add(i)
	}
	return out
}
