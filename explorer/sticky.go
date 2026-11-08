package explorer

// MaxStickyAncestors is the most ancestors the sticky band pins above the run:
// Fresh's MAX_STICKY_ANCESTORS (view/shell/file_explorer.rs, matching VS Code's
// default upper bound). The band always leaves at least one row for the run.
const MaxStickyAncestors = 7

// StickyPins returns the indices in rows of the ancestors pinned above a run that
// starts at offset: the Row.Parent chain of the run's first row, outermost first,
// at most MaxStickyAncestors and at most bodyRows-1 so the run keeps one row.
// The slice is empty when nothing is pinned.
func StickyPins(rows []Row, offset, bodyRows int) []int {
	// Nothing is scrolled away at the top of the tree, so nothing is pinned
	// there (fresh-ui sticky_ancestors: "None at the top of the tree").
	if offset <= 0 || bodyRows <= 1 || offset >= len(rows) {
		return nil
	}
	limit := min(MaxStickyAncestors, bodyRows-1)
	var pins []int
	for p := rows[offset].Parent; p >= 0 && p < len(rows) && len(pins) < limit; p = rows[p].Parent {
		pins = append(pins, p)
		if rows[p].Parent == p { // a corrupt chain cannot loop forever
			break
		}
	}
	for i, j := 0, len(pins)-1; i < j; i, j = i+1, j-1 {
		pins[i], pins[j] = pins[j], pins[i]
	}
	return pins
}

// MaxScroll returns the smallest scroll offset whose run, under its own pins,
// fills the body to the last row: Fresh's ceiling for a windowed list with
// sticky rows ("count - (rows - pinned)"). A body with no rows or a tree with no
// rows scrolls nowhere.
func MaxScroll(rows []Row, bodyRows int) int {
	if bodyRows <= 0 || len(rows) == 0 {
		return 0
	}
	off := 0
	for off < len(rows)-1 && !reachesEnd(rows, off, bodyRows) {
		off++
	}
	return off
}

// reachesEnd reports whether the run from offset, shortened by its pins, reaches
// the last row.
func reachesEnd(rows []Row, offset, bodyRows int) bool {
	pins := StickyPins(rows, offset, bodyRows)
	return offset+bodyRows-len(pins) >= len(rows)
}

// FollowScroll returns the scroll offset that brings row index into the body
// under its pins: the offset whose run holds the row, clamped to the ceiling.
// offset is where the view is now; the pins change with the offset, so the
// answer is refined once.
func FollowScroll(rows []Row, index, offset, bodyRows int) int {
	if bodyRows <= 0 || len(rows) == 0 {
		return 0
	}
	index = min(max(index, 0), len(rows)-1)
	ceiling := MaxScroll(rows, bodyRows)
	off := min(max(offset, 0), ceiling)
	// The pins change with the offset, which changes the room for the run: move
	// until the row is in the run, for at most one move per possible offset.
	for range len(rows) + 1 {
		run := bodyRows - len(StickyPins(rows, off, bodyRows))
		next := off
		switch {
		case index < off:
			next = index
		case index >= off+run:
			next = index - run + 1
		}
		next = min(max(next, 0), ceiling)
		if next == off {
			break
		}
		off = next
	}
	return off
}

// Viewport resolves the tree's raw scroll offset into the offset the body paints
// from and the rows pinned above it: the raw offset clamped to the ceiling, and
// that offset's pins. The run then shows bodyRows-len(pins) rows from the offset.
func Viewport(rows []Row, raw, bodyRows int) (offset int, pins []int) {
	if bodyRows <= 0 || len(rows) == 0 {
		return 0, nil
	}
	offset = min(max(raw, 0), MaxScroll(rows, bodyRows))
	return offset, StickyPins(rows, offset, bodyRows)
}
