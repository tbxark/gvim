package commands

// Palette is the list navigation of the command palette: the highlighted match,
// the first match in view, and the clamping that keeps both in range. It is the
// only place those rules live; the TUI reads them from here and does not keep
// its own copy.
//
// Palette does not hold the matches. The caller passes the number of matches
// and of visible rows to SetSize, then drives the list with Select and ScrollBy
// and reads it back with Selected and Window. The zero value has no matches and
// no visible rows.
//
// The TUI calls, in order:
//
//   - SetSize(len(matches), rows) whenever the query changes the match list and
//     whenever the box height changes; rows is clamp(H-4, 0, 10).
//   - Select(0) after the query changes, so the highlight returns to the top.
//   - Select(i) for Up and Down (with i-1 and i+1), and for a mouse hover on
//     row i of the window (with Window's first plus the row).
//   - ScrollBy(±3) for each wheel notch. The highlight is left alone.
//   - Window() to paint the list, and to map a click on row r to the match
//     index first+r.
type Palette struct {
	count    int // number of matches
	visible  int // rows the box shows
	selected int // index of the highlighted match
	offset   int // index of the first match in view
}

// SetSize records the number of matches and of visible rows, then clamps the
// selection and the offset to them. Negative values count as zero. With no
// matches both are 0 afterwards.
func (p *Palette) SetSize(matches, visible int) {
	p.count = max(matches, 0)
	p.visible = max(visible, 0)
	p.Select(p.selected)
}

// Select highlights match i, clamped to the list, and moves the view as little
// as needed to keep the highlighted match in it. A negative i selects the first
// match and an i past the end selects the last. The view does not move when the
// match is already in view, so a wheel scroll is not undone by a selection
// move that stays inside the window.
func (p *Palette) Select(i int) {
	p.selected = min(max(i, 0), max(p.count-1, 0))
	rows := max(p.visible, 1)
	switch {
	case p.selected < p.offset:
		p.offset = p.selected
	case p.selected >= p.offset+rows:
		p.offset = p.selected - rows + 1
	}
	p.clampOffset()
}

// ScrollBy moves the view by n matches; a negative n moves up. The offset is
// clamped so the view stays within the list. The highlighted match does not
// change, so it can scroll out of view, as the wheel does in the legacy palette.
func (p *Palette) ScrollBy(n int) {
	p.offset += n
	p.clampOffset()
}

// Selected returns the index of the highlighted match.
func (p *Palette) Selected() int { return p.selected }

// Window returns the visible matches as a run: first is the index of the first
// match in view and count is how many are in view, so the indexes are
// first through first+count-1. count is at most the visible rows and never
// runs past the last match.
func (p *Palette) Window() (first, count int) {
	first = p.offset
	return first, max(min(p.visible, p.count-first), 0)
}

// clampOffset keeps the offset in [0, count-rows], where rows is at least one
// so that a zero-height box still has a last match to scroll to.
func (p *Palette) clampOffset() {
	last := max(p.count-max(p.visible, 1), 0)
	p.offset = min(max(p.offset, 0), last)
}
