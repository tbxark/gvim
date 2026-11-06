package editorpane

// RowStarts returns the rune column of the document line at which each display row
// of the line starts in a w by h pane, so the editor can move by display row. It
// returns nil when Wrap is off or the line fits on one row.
func (p Pane) RowStarts(w, h, line int) []int {
	f, ok := p.newFrame(w, h)
	if !ok || !f.wrap || line < 0 || line >= f.lines {
		return nil
	}
	ln := f.layoutLine(line, false)
	if len(ln.spans) < 2 {
		return nil
	}
	raw := []rune(f.doc.Line(line))
	out := make([]int, len(ln.spans))
	col, expanded, cells := 0, 0, 0
	for k, sp := range ln.spans {
		for col < len(raw) && expanded < sp.from {
			n := cellAdvance(cells, raw[col], f.tab)
			cells += n
			if raw[col] == '\t' || controlRune(raw[col]) {
				expanded += n
			} else {
				expanded++
			}
			col++
		}
		out[k] = col
	}
	return out
}
