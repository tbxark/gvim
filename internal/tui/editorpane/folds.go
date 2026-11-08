package editorpane

import (
	"slices"

	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
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
	if isLarge(doc) || f.fill != nil {
		// The folds of a large document would be found by reading all of it. A pane with
		// fillers shows every line, so that the rows of two panes keep matching.
		return
	}
	var ranges []fold.Range
	collapsed := map[int]bool{}
	if fl, ok := doc.(Folder); ok {
		ranges = fl.Folds(f.tab)
		for _, r := range ranges {
			if fl.Folded(r.Start) {
				collapsed[r.Start] = true
			}
		}
	}
	// Plugin folding ranges merge with the document's (ids tie: the document's win).
	if f.pluginFolds != nil {
		extra, extraCollapsed, placeholder := f.pluginFolds()
		ranges = append(ranges, extra...)
		for start, c := range extraCollapsed {
			if _, ok := collapsed[start]; !ok {
				collapsed[start] = c
			}
		}
		f.foldPlaceholder = placeholder
	}
	if len(ranges) == 0 {
		return
	}
	slices.SortStableFunc(ranges, func(a, b fold.Range) int {
		if a.Start != b.Start {
			return a.Start - b.Start
		}
		return a.End - b.End
	})
	f.folds = make(map[int]bool, len(ranges))
	for _, r := range ranges {
		f.folds[r.Start] = collapsed[r.Start]
	}
	if len(collapsed) > 0 {
		f.hidden = fold.Visible(f.lines, ranges, collapsed)
	}
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
	if f.pageOn {
		// The page view hides the gutter and the fold markers; the collapsed fold
		// placeholder still shows in the text.
		return
	}
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
