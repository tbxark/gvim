package editorpane

// Large file mode. A document whose text stays on disk is painted from the lines it
// is asked for, so the pane must not ask for more lines than it shows, and must not
// read a line of a megabyte when it shows a few hundred columns of it. This file
// holds the optional views of such a document and the helpers that keep the frame's
// work bounded.

// LargeDoc is implemented by a Document that may be in large file mode, as
// *editor.Document is. The pane checks for it with a type assertion. It is declared
// here, and not imported from ui, because the pane does not import ui.
type LargeDoc interface {
	Large() bool
}

// LineCounter is implemented by a large Document. ApproxLineCount is the number of
// lines the document will have once its line index is complete, estimated while the
// index is still being built. It sizes the gutter and the scrollbar thumb, so they
// do not change with every batch of the index. The loops and the clamps use
// LineCount, which is exact.
type LineCounter interface {
	ApproxLineCount() int
}

// DirtyLiner is implemented by a large Document. LineDirty reports whether line i
// was changed since the file was opened or saved. The pane asks only for the lines
// it paints, where a small document is diffed against its saved text as a whole.
type DirtyLiner interface {
	LineDirty(i int) bool
}

// LineWindower is implemented by a large Document that can show part of a line
// without reading all of it. LineBytes is the stored length of line i in bytes, an
// upper bound of its length in runes, found without reading the line. LineSlice is
// the text of runes [col, col+n) of line i.
type LineWindower interface {
	LineBytes(i int) int
	LineSlice(i, col, n int) string
}

const (
	// LongLineRunes is the length from which a line of a large document is painted
	// through a window. A line that is not longer than this is painted whole.
	LongLineRunes = 16384
	// longWindow is the number of runes of a long line that are laid out. It is far
	// more than a screen is wide, so the window covers the visible columns and the
	// margin around the caret.
	longWindow = 4096
	// scanBytes is how many bytes of other lines the bracket and indent scans of one
	// frame may read in a large document.
	scanBytes = 1 << 20
	// headRunes is how much of a line the indent scans read.
	headRunes = 256
)

// isLarge reports whether doc is in large file mode.
func isLarge(doc Document) bool {
	ld, ok := doc.(LargeDoc)
	return ok && ld.Large()
}

// DisplayLines is the line count that sizes the gutter and the scrollbar thumb of
// doc: its line count, or in a large document whose line index is incomplete, the
// larger estimate of its final line count. It never falls below LineCount, so the
// gutter is wide enough for every line number that exists.
func DisplayLines(doc Document) int {
	n := doc.LineCount()
	if lc, ok := doc.(LineCounter); ok && isLarge(doc) {
		n = max(n, lc.ApproxLineCount())
	}
	return n
}

// wrapOn reports whether the pane wraps lines. A large document is never wrapped: a
// wrapped line of a megabyte would be laid out in full to find its rows.
func (p Pane) wrapOn() bool {
	return p.Wrap && !isLarge(p.Doc)
}

// dirtyRows marks the visible lines that were changed since the file was opened or
// saved, for the margin bar. It asks about the lines of the window only.
func (f frame) dirtyRows() map[int]bool {
	dl, ok := f.doc.(DirtyLiner)
	if !ok || !f.doc.Modified() {
		return nil
	}
	var bar map[int]bool
	for i, n := f.top, 0; i < f.lines && n < f.h+1; i, n = f.next(i), n+1 {
		if dl.LineDirty(i) {
			if bar == nil {
				bar = make(map[int]bool)
			}
			bar[i] = true
		}
	}
	return bar
}

// window is the part of a long line that is laid out: the text of the runes from
// column off on.
type window struct {
	text string
	off  int
}

// isLong reports whether line i is painted through a window: the document is large,
// wrap is off, and the line is longer than LongLineRunes.
func (f frame) isLong(i int) bool {
	return f.win != nil && !f.wrap && f.win.LineBytes(i) > LongLineRunes
}

// window returns the window of long line i. It starts at the first visible column,
// as far as the line reaches. A column is a rune on such a line, whatever the runes
// are: tab stops and wide characters are measured from the start of the window. The
// result is kept for the frame, so the line is decoded once for all the calls that
// lay it out.
func (f frame) window(i int) window {
	if w, ok := f.fx.wins[i]; ok {
		return w
	}
	off := min(f.left, max(f.win.LineBytes(i)-longWindow, 0))
	w := window{text: f.win.LineSlice(i, off, longWindow), off: off}
	if f.fx.wins == nil {
		f.fx.wins = make(map[int]window)
	}
	f.fx.wins[i] = w
	return w
}

// lineRaw returns the runes of line i that are laid out and the column of the first
// one: the whole line, or the window of a long line.
func (f frame) lineRaw(i int) (raw []rune, off int) {
	if f.isLong(i) {
		w := f.window(i)
		return []rune(w.text), w.off
	}
	return []rune(f.doc.Line(i)), 0
}

// dropFor is how many display columns of the laid out line ln are to the left of
// the visible columns. For a line shown whole that is the first visible column. A
// window starts at the first visible column, or at the end of a line shorter than
// that, so it drops the rest.
func (f frame) dropFor(ln line) int {
	if ln.long {
		return max(f.left-ln.off, 0)
	}
	return f.left
}

// budget is a count of bytes that the scans of one frame may still read.
type budget struct {
	left int
	// out is set when a line was refused for want of bytes, so the scan should stop
	// and not go on to a shorter line.
	out bool
}

// newBudget returns the byte budget of a scan, or nil when the scan is unbounded
// because the document is not large.
func (f frame) newBudget() *budget {
	if f.win == nil {
		return nil
	}
	return &budget{left: scanBytes}
}

// scanLine returns line i for a scan of the lines around the window. In a large
// document it charges the length of the line to b and gives up (ok is false) on a
// long line or when b is spent, so the scan ends where it is. In a document that is
// not large it returns the line.
func (f frame) scanLine(b *budget, i int) (text string, ok bool) {
	if b == nil || f.win == nil {
		return f.doc.Line(i), true
	}
	n := f.win.LineBytes(i)
	if n > b.left {
		b.out = true
		return "", false
	}
	if n > LongLineRunes {
		return "", false
	}
	b.left -= n
	return f.doc.Line(i), true
}

// head returns the start of line i, enough to measure its indent. A large document
// does not read the rest of the line.
func (f frame) head(i int) string {
	if f.win == nil {
		return f.doc.Line(i)
	}
	return f.win.LineSlice(i, 0, headRunes)
}

// nearRunes returns the runes of line i around column col, and the column of the
// first one. A line that is not long is returned whole. Of a long line only the
// runes near col are read, so the caret line is not decoded in full.
func (f frame) nearRunes(i, col int) (runes []rune, off int) {
	if f.win == nil || f.win.LineBytes(i) <= LongLineRunes {
		return []rune(f.doc.Line(i)), 0
	}
	off = max(col-longWindow/2, 0)
	return []rune(f.win.LineSlice(i, off, longWindow)), off
}
