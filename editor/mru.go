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

// replaceRecent swaps old for new in place in the MRU history, keeping old's rank. It is
// used when a shared buffer's document identity is replaced by an ownership handoff or a
// mirror adoption: without it the new document would not be in the history and
// RecentTabIndices would fall back to tab order for it, changing completion and dabbrev
// other-buffer order after a handoff. A fallback (old not tracked) leaves any existing
// rank of new alone, which is correct for a document that was already in the history.
func (e *Editor) replaceRecent(old, new *Document) {
	if new == nil || old == new {
		return
	}
	if slices.Index(e.recent, old) < 0 {
		return
	}
	e.recent = slices.DeleteFunc(e.recent, func(x *Document) bool { return x == new })
	pos := slices.Index(e.recent, old)
	if pos < 0 {
		return
	}
	e.recent[pos] = new
}

// SetRecentTabIndices replaces the MRU history with the named tabs, most recent first.
// Indices outside the open tabs and repeats are ignored. Tabs that are not named keep
// falling back to tab order after the named ones, as RecentTabIndices does. It is used
// by session restore, which knows the documents by path and maps them back to the new
// tab indices; an empty order leaves the history empty.
func (e *Editor) SetRecentTabIndices(order []int) {
	e.adoptAll()
	e.recent = e.recent[:0]
	seen := make([]bool, len(e.docs))
	for _, i := range order {
		if i < 0 || i >= len(e.docs) || seen[i] {
			continue
		}
		seen[i] = true
		e.recent = append(e.recent, e.docs[i])
	}
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
