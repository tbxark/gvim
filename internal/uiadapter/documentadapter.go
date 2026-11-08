package uiadapter

import (
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/internal/highlight"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/syntax"
)

// documentAdapter is the read-only tui.Document view of one *editor.Document.
type documentAdapter struct {
	doc   *editor.Document
	spans *highlight.Cache
}

// Encoding reports the saved text's encoding label, computed by the editor when
// the text was opened or saved.
func (d documentAdapter) Encoding() string { return d.doc.Encoding() }

// LineEnding is the ending of the saved text, as the editor reports it.
func (d documentAdapter) LineEnding() string { return d.doc.LineEnding() }

// Name is the file name, or "[No Name]" for an unnamed buffer.
func (d documentAdapter) Name() string {
	return d.doc.Name()
}

// Modified reports unsaved edits.
func (d documentAdapter) Modified() bool {
	return d.doc.Modified()
}

// LineCount is the number of lines in the buffer.
func (d documentAdapter) LineCount() int {
	return d.doc.Buffer.Lines()
}

// Line returns line i of the buffer.
func (d documentAdapter) Line(i int) string {
	return d.doc.Buffer.Line(i)
}

// SavedLines is the text at open or the last save, split into lines. The pane
// diffs it against the buffer for the change marks.
func (d documentAdapter) SavedLines() []string {
	if d.doc.Large() {
		// The saved text of a large document is not held; the pane marks the changed
		// lines through LineDirty.
		return nil
	}
	return d.doc.SavedLines()
}

// SavedLinesShared is SavedLines without the copy. The pane only reads it.
func (d documentAdapter) SavedLinesShared() []string {
	if d.doc.Large() {
		return nil
	}
	return d.doc.SavedLinesShared()
}

// EditedLines is the range of lines that may differ from the saved text. See
// editor.Document.EditedLines.
func (d documentAdapter) EditedLines() (lo, hi int, ok bool) { return d.doc.EditedLines() }

// LineIs reports whether line i is text, without building the line. The pane compares
// the lines with the saved lines this way for the change marks.
func (d documentAdapter) LineIs(i int, text string) bool {
	return d.doc.Buffer.LineEquals(i, text)
}

// MemoValue keeps a value the pane worked out from the text until the text changes.
func (d documentAdapter) MemoValue(key any, compute func() any) any {
	return d.doc.MemoValue(key, compute)
}

// MemoChanges keeps the change marks of the pane until the text or the saved text changes.
func (d documentAdapter) MemoChanges(compute func() (bar, scroll map[int]bool)) (bar, scroll map[int]bool) {
	return d.doc.MemoChanges(compute)
}

// HasPath reports whether the document has a file on disk. Save and revert need
// it, and an unnamed buffer has none.
func (d documentAdapter) HasPath() bool {
	return d.doc.Path != ""
}

// FilePath is the path the document is saved to, or the empty string for an
// untitled buffer. The Save As prompt is prefilled with it.
func (d documentAdapter) FilePath() string {
	return d.doc.Path
}

// Top is the first visible line of the viewport.
func (d documentAdapter) Top() int {
	return d.doc.Top
}

// Cursor is the caret position.
func (d documentAdapter) Cursor() buffer.Pos {
	return d.doc.Cursor
}

// Selection returns the selection ends, and ok is false when nothing is selected.
func (d documentAdapter) Selection() (anchor, head buffer.Pos, ok bool) {
	if d.doc.Sel == nil {
		return buffer.Pos{}, buffer.Pos{}, false
	}
	return d.doc.Sel.Anchor, d.doc.Sel.Head, true
}

// LineSpans returns the syntax spans of line i, or nil when no lexer matched
// the document or i is out of range. The spans are lexed once per edit.
func (d documentAdapter) LineSpans(i int) []syntax.Span {
	return d.spans.LineSpans(d.doc, i)
}

// LineSpansIn returns the syntax spans of text, runes [col, ...) of line i, which is a piece
// of a very long line. See highlight.Cache.Segment.
func (d documentAdapter) LineSpansIn(i, col int, text string) []syntax.Span {
	return d.spans.Segment(d.doc, i, col, text)
}

// Cursors returns the extra carets of the document, which the pane paints in the
// secondary caret style.
func (d documentAdapter) Cursors() []editor.Caret { return d.doc.Cursors() }

// VirtualColumns is how many display columns the caret sits past the end of its
// line (virtual space), which the pane draws the caret at.
func (d documentAdapter) VirtualColumns() int { return d.doc.VirtualColumns() }

// Folds returns the indentation fold ranges, for a tab of tabWidth columns.
func (d documentAdapter) Folds(tabWidth int) []fold.Range { return d.doc.Folds(tabWidth) }

// Folded reports whether the fold that starts at line is collapsed.
func (d documentAdapter) Folded(line int) bool { return d.doc.Folded(line) }

// ViewSettings is the document's display settings, read by the view commands and the pane.
func (d documentAdapter) ViewSettings() editor.ViewSettings { return d.doc.ViewSettings() }

// Version is the version of the document's text, which changes with every edit.
func (d documentAdapter) Version() uint64 { return d.doc.Buffer.Version() }

// Language is the document's language name, shown in the status row.
func (d documentAdapter) Language() string { return d.doc.Language() }

// BufferID is the id the plugins know the document by (the root's bufferIdentified port),
// which is what the decorations of the document are filed under.
func (d documentAdapter) BufferID() pluginapi.BufferID { return pluginapi.BufferID(d.doc.ID()) }

// Virtual reports whether the document is a virtual buffer. The root reads it through an
// optional interface (the root's isVirtualDoc port), so the language-server sync leaves the
// document out.
func (d documentAdapter) Virtual() bool { return d.doc.Virtual() }
