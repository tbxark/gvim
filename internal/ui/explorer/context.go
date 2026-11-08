package explorer

// ContextTarget is what a right press in the panel landed on.
type ContextTarget struct {
	// Paths are the entries the menu acts on, in display order: the picked rows when
	// the press was on one of several picked rows, otherwise the row under the pointer.
	// It is empty for the blank area of the panel.
	Paths []string
	// Root is set when the only target is the tree root, or the press was on the blank
	// area. Fresh gives these the narrow menu of creation and paste.
	Root bool
}

// SelectAt prepares the panel for a context menu opened by a right press at (x, y)
// and says what it landed on. A press on a row selects that row without opening it
// or toggling a folder, and drops the picks unless the row is one of several picked
// rows, so the menu then acts on all of them. A press on the blank area keeps the
// selection and reports the root. A press outside the rows reports ok false.
func (p Panel) SelectAt(g Geometry, x, y int) (ContextTarget, bool) {
	if p.tree == nil || g.Width < 2 {
		return ContextTarget{}, false
	}
	t := p.hitAt(g, x, y)
	switch t.zone {
	case zoneBody:
		return ContextTarget{Root: true}, true
	case zoneRow:
	default:
		return ContextTarget{}, false
	}
	rows := p.tree.Rows()
	idx := p.tree.ScrollOffset() + t.row
	path := rows[idx].Path
	s := p.state()
	if len(s.picked) > 1 && s.picked[path] {
		return ContextTarget{Paths: p.targets()}, true
	}
	s.picked, s.anchor = nil, ""
	p.selectIndex(rows, idx)
	return ContextTarget{Paths: []string{path}, Root: path == p.tree.Root()}, true
}
