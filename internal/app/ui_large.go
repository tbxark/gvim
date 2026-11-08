package app

import (
	"context"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/search"
	"github.com/tbxark/gvim/internal/ui/editorpane"
)

// The adapters of large file mode. The root runs the line index of a large document,
// its search and its save in commands, and asks the editor for each step through
// these methods. They add nothing of their own: a document that is not large answers as
// a small document does.

// PlanIndex prepares the next step of the line index of a large document. See
// editor.Editor.PlanIndex.
func (a editorAdapter) PlanIndex() (editor.IndexJob, bool) { return a.ed.PlanIndex() }

// ApplyIndex records a step of the line index on the editor's goroutine.
func (a editorAdapter) ApplyIndex(r editor.IndexResult) bool { return a.ed.ApplyIndex(r) }

// LoadFullFunc returns the read that loads a file whole, for the answer "load" to the
// question that opening a large file in another encoding than UTF-8 asks.
func (a editorAdapter) LoadFullFunc(path string) func() (*editor.Document, error) {
	return a.ed.LoadFullFunc(path)
}

// WriteSaveContext runs the writes of a plan until ctx is cancelled. The root calls it
// from a command, so a quit can stop the write of a large file.
func (a editorAdapter) WriteSaveContext(ctx context.Context, p editor.SavePlan) []editor.SaveResult {
	return p.RunContext(ctx)
}

// SearchKey identifies the text of the active document for a scan of it: the document,
// its version and whether its line index is complete. ok is false unless the active
// document is large, because the matches of any other are listed at once.
func (a editorAdapter) SearchKey() (doc any, version uint64, indexComplete bool, ok bool) {
	d := a.ed.Active()
	if d == nil || !d.Large() {
		return nil, 0, false, false
	}
	return d, d.Buffer.Version(), d.Index().Complete, true
}

// StartScan starts a scan of the active large document for q from line startLine. The
// scan reads a snapshot, so the text may change while it runs.
func (a editorAdapter) StartScan(q search.Query, startLine int) (*search.Scan, error) {
	d := a.ed.Active()
	if d == nil {
		return nil, editor.ErrNoDocument
	}
	snap := d.Buffer.Snapshot()
	sc, err := search.NewScan(snap, q, startLine)
	if err != nil {
		snap.Release()
		return nil, err
	}
	return sc, nil
}

// FindNext finds the match to go to from the position from in the active large
// document, without a list of matches. See search.FindNext.
func (a editorAdapter) FindNext(q search.Query, from buffer.Pos, backward bool, at search.Cursor) (search.Match, bool, bool, search.Cursor, error) {
	d := a.ed.Active()
	if d == nil {
		return search.Match{}, false, true, search.Cursor{}, editor.ErrNoDocument
	}
	snap := d.Buffer.Snapshot()
	defer snap.Release()
	dir := search.Forward
	if backward {
		dir = search.Backward
	}
	return search.FindNextAt(snap, q, from, dir, 0, at)
}

// VisibleMatches returns the matches of q that touch lines first through last of the
// active large document, for the search highlights of a frame. A line too long to be
// painted whole is left out.
func (a editorAdapter) VisibleMatches(q search.Query, first, last int) ([]search.Match, error) {
	d := a.ed.Active()
	if d == nil {
		return nil, nil
	}
	b := d.Buffer
	rows := func(i int) string {
		if i < 0 || i >= b.Lines() || b.LineBytes(i) > editorpane.LongLineRunes {
			return ""
		}
		return b.Line(i)
	}
	return search.FindVisible(rows, first, last, q)
}

// Large reports whether the document is in large file mode.
func (d documentAdapter) Large() bool { return d.doc.Large() }

// Index reports how much of the line index of the document exists.
func (d documentAdapter) Index() buffer.IndexInfo { return d.doc.Index() }

// ApproxLineCount is the number of lines the document will have when its line index is
// complete, estimated while it is built. It sizes the gutter and the scrollbar thumb.
func (d documentAdapter) ApproxLineCount() int { return d.doc.Index().ApproxLines }

// LineDirty reports whether line i was changed since the file was opened or saved.
func (d documentAdapter) LineDirty(i int) bool { return d.doc.Buffer.LineDirty(i) }

// LineLen is the rune length of line i.
func (d documentAdapter) LineLen(i int) int { return d.doc.Buffer.LineLen(i) }

// HasLongLine reports whether a line of the document is longer than editorpane.WindowedLineRunes.
// The pane then shows the document without wrap, and the long lines through a
// window, as it does a large one.
func (d documentAdapter) HasLongLine() bool { return d.doc.HasLongLine(editorpane.WindowedLineRunes) }

// LineBytes is the stored length of line i in bytes, found without reading the line
// of a large document. It is an upper bound of the runes of the line, which is all the
// pane asks of it, so a document in memory answers with the runes, which it has counted.
func (d documentAdapter) LineBytes(i int) int {
	if !d.doc.Large() {
		return d.doc.Buffer.LineLen(i)
	}
	return d.doc.Buffer.LineBytes(i)
}

// LineSlice is the text of runes [col, col+n) of line i. A long line of a large
// document is shown through it, so the pane never builds the text of the whole line.
func (d documentAdapter) LineSlice(i, col, n int) string {
	return d.doc.Buffer.LineSlice(i, col, n)
}
