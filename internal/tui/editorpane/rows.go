package editorpane

// RowStarts returns the rune column of the document line at which each display row
// of the line starts in a w by h pane, so the editor can move by display row. It
// returns nil when Wrap is off or the line fits on one row.
func (p Pane) RowStarts(w, h, line int) []int {
	f, ok := p.newFrame(w, h)
	if !ok || !f.wrap || line < 0 || line >= f.lines || f.isTall(line) {
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

// RowWindow returns, for a tall line, the rune columns at which the rows of the line start
// around column col: the row before the one that holds col, that row and the two rows after
// it, as many of them as the line has. tall is true then, and the line is wrapped. For a line
// that is not tall it returns nil, false, and RowStarts gives its rows. The editor moves by
// display row inside a tall line with these, as it does with the rows RowStarts lists.
func (p Pane) RowWindow(w, h, line, col int) (starts []int, tall bool) {
	f, ok := p.newFrame(w, h)
	if !ok || !f.wrap || line < 0 || line >= f.lines || !f.isTall(line) {
		return nil, false
	}
	start := f.tallRowStart(line, col)
	if prev, ok := f.tallPrevRow(line, start); ok {
		starts = append(starts, prev)
	}
	rows := f.tallRows(line, false, start, 3)
	for _, r := range rows {
		starts = append(starts, r.start)
	}
	return starts, true
}
