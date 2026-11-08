package editorpane

import (
	"github.com/tbxark/gvim/internal/fold"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/icons"
)

// Folder is implemented by a Document that has indentation folds, as
// *editor.Document does. The pane marks each fold start line in the gutter and
// hides the body of a collapsed fold. A Document without it has no folds.
type Folder interface {
	// Folds returns the fold ranges, ordered by start line. tabWidth is the
	// width of a tab for the indentation.
	Folds(tabWidth int) []fold.Range
	// Folded reports whether the fold that starts at line is collapsed.
	Folded(line int) bool
}

// setFolds fills the fold markers and the hidden lines of the frame from doc.
// A document that is not a Folder, that has no folds, or that is in large file mode,
// leaves both nil.
func (f *frame) setFolds(doc Document) {
	fl, ok := doc.(Folder)
	if !ok || isLarge(doc) || f.fill != nil {
		// The folds of a large document would be found by reading all of it. A pane with
		// fillers shows every line, so that the rows of two panes keep matching.
		return
	}
	ranges := fl.Folds(f.tab)
	if len(ranges) == 0 {
		return
	}
	f.folds = make(map[int]bool, len(ranges))
	collapsed := make(map[int]bool)
	for _, r := range ranges {
		c := fl.Folded(r.Start)
		f.folds[r.Start] = c
		if c {
			collapsed[r.Start] = true
		}
	}
	f.hidden = fold.Visible(f.lines, ranges, collapsed)
}

// foldable reports whether a click may toggle the fold that starts at line i: it is
// collapsed, which stays possible with the markers hidden, or its marker shows.
func (f frame) foldable(i int) bool {
	collapsed, ok := f.folds[i]
	return ok && (collapsed || !f.noFolds)
}

// visibleCount is the number of lines that are not hidden by a fold.
func (f frame) visibleCount() int {
	n := f.lines
	for _, h := range f.hidden {
		if h {
			n--
		}
	}
	return n
}

// next returns the first line after i that is not hidden by a fold, or f.lines
// when there is none.
func (f frame) next(i int) int {
	for j := i + 1; j < f.lines; j++ {
		if f.hidden == nil || !f.hidden[j] {
			return j
		}
	}
	return f.lines
}

// visibleAtOrAbove returns line i, or the start of the fold that hides it.
func (f frame) visibleAtOrAbove(i int) int {
	for i > 0 && f.hidden != nil && f.hidden[i] {
		i--
	}
	return i
}

// prevLine returns the last line before i that is not hidden by a fold, or -1
// when there is none.
func (f frame) prevLine(i int) int {
	for j := i - 1; j >= 0; j-- {
		if f.hidden == nil || !f.hidden[j] {
			return j
		}
	}
	return -1
}

// paintFoldMarker writes the fold marker of line i on row y, when line i starts a
// fold. The marker is open for an expanded fold and closed for a collapsed one. It
// takes the last gutter column. With the gutter hidden it takes column end, the cell
// after the row's text, so no text cell is overwritten. When the text fills the row
// there is no such cell before the scrollbar column, and no marker is drawn.
func (f frame) paintFoldMarker(g *grid.Grid, i, y, end int) {
	collapsed, ok := f.folds[i]
	if !ok || f.noFolds {
		return
	}
	mark := f.icons.Glyph(icons.FoldOpen)
	if collapsed {
		mark = f.icons.Glyph(icons.FoldClosed)
	}
	if f.gutter > 0 {
		g.Set(f.gutter-1, y, mark, f.styles.Gutter)
		return
	}
	if end < f.w-1 {
		g.Set(end, y, mark, f.styles.Gutter)
	}
}
