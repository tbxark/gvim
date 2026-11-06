package ui

// pressBorder starts a drag on the line at index i of the layout's lines. The drag keeps
// the split that the line divides, so the motion changes that split's ratio. A press on
// a line takes no focus.
func (m *Model) pressBorder(i int) {
	lines := m.layout().borders
	if i < 0 || i >= len(lines) {
		return
	}
	line := lines[i]
	m.drag = drag{active: true, border: &line}
}

// dragBorder moves the split of the dragged line so that the line follows the pointer at
// (x, y). The first child takes the part of the split's area before the pointer, rounded
// to the nearest percent and kept within the ratio bounds. The split is looked up in the
// current layout, so a drag keeps working when the frame changes size. A split that is no
// longer in the layout is left alone.
func (m *Model) dragBorder(x, y int) {
	node := m.drag.border.node
	line, ok := m.layout().borderOf(node)
	if !ok {
		return
	}
	at, origin, extent := x, line.area.x, line.area.w
	if !line.vertical {
		at, origin, extent = y, line.area.y, line.area.h
	}
	if extent < 2 {
		return
	}
	node.ratio = ratioAt(at-origin, extent-1)
	m.syncViewport()
	m.revealCaretVisual(0)
}
