// Package editorpane paints the text area of the editor: the visible buffer
// lines with their gutter, soft-wrapped rows, selection, syntax colors,
// change marks, the vertical and horizontal scrollbars, and the display aids
// (carets, rulers, indent guides, visible whitespace, the current line,
// occurrences, fold markers and diagnostic underlines). It also maps a cell of
// the area back to a buffer position for the mouse.
//
// The package is pure. A Pane is a value built for one frame. Render returns a
// grid, and Caret, Hit, PosAt, ScrollTopFor, RevealTop and BodyRows answer from
// the same geometry as Render. Nothing is read or written outside the arguments,
// and there is no tea import. Coordinates are pane-local: column 0 is the pane's
// left edge (the first gutter column), and row 0 is the first body row. The root
// model adds the explorer columns and the body top before it calls in.
//
// Soft wrap follows the legacy rule (internal/tui/wrap.go, derived from the
// legacy screens). With Wrap on, a buffer line wider than the wrap width takes
// several rows, and the wrap width is the text area less two columns (or Opts.WrapColumn
// when that is smaller), with the rows after the first indented as the line is when
// Opts.WrapIndent is set. With Wrap
// off, each line is one row, and the window moves by display columns (Left).
// The caret and the mouse both work in display rows, but the editor moves and
// scrolls by buffer lines.
package editorpane

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/syntax"
)

// Document is the read-only view the pane paints. ui.Document has the same
// method set, so the root passes its active document through unchanged.
//
// A Document may also implement CaretLister and Folder. The pane checks for
// them with a type assertion, so a document without them paints one caret and
// no folds.
type Document interface {
	Modified() bool
	LineCount() int
	Line(i int) string
	// SavedLines is the text at open or the last save, split into lines. The
	// pane diffs it against the current lines for the change marks.
	SavedLines() []string
	Cursor() buffer.Pos
	// Selection returns the anchor and the head. ok is false when nothing is
	// selected.
	Selection() (anchor, head buffer.Pos, ok bool)
	// LineSpans returns the syntax spans of line i, or nil when there is no
	// lexer. The caller caches the lexing per document version. The pane asks
	// only for the lines it paints.
	LineSpans(i int) []syntax.Span
}

// Pane is the editor text area for one frame.
type Pane struct {
	Doc Document // nil paints blank body rows
	Top int      // first visible buffer line; clamped to the document
	// TopRow is how many display rows of line Top are scrolled off above the body. It
	// lets a wrapped line taller than the body be scrolled through. It is zero for a
	// view that starts at the line, and the pane ignores it when Wrap is off. For a line
	// that is tall (see longwrap.go), which is not laid out whole, it is the rune column of
	// a row, any column of it, since the rows above are not counted.
	TopRow int
	Styles Styles
	// HideGutter removes the line-number gutter and its change bar, so the text
	// starts at the first column. The zero value shows the gutter.
	HideGutter bool
	// Highlights gives the search highlights of line i. The root sets it while a
	// find bar is open. Nil paints none.
	Highlights func(line int) []Highlight
	// Left is the first visible display column when Wrap is off. The pane moves
	// it so the caret stays on the window.
	Left int
	// KeepLeft keeps Left as it is, even when the caret is off the window. The root
	// sets it while the view is panned by the wheel.
	KeepLeft bool
	// Wrap breaks a line wider than the text area over several rows. Off, each
	// line is one row, cut at the right edge, and the body shows a horizontal
	// scrollbar on its last row when a line is wider than the text area.
	Wrap bool
	// Rulers are the columns that carry a ruler, 1-based as Fresh counts them:
	// column c is the text cell c-1.
	Rulers []int
	// IndentGuides draws a guide at each indent level in the leading blanks.
	IndentGuides bool
	// VisibleWhitespace shows a space as a middle dot and a tab as an arrow.
	VisibleWhitespace bool
	// CurrentLine tints the text row of the caret line.
	CurrentLine bool
	// Occurrences paints the whole-word matches of the word at the caret. While
	// text is selected on one line, it paints the other matches of that text instead.
	Occurrences bool
	// Diagnostics gives the diagnostic spans of line i. Nil paints none.
	Diagnostics func(line int) []DiagnosticSpan
	// TabSize is the columns per tab stop, for tab expansion, indent guides, visible
	// whitespace and indentation folds. The zero value is defaultTabSize.
	TabSize int
	// HideTabArrows draws a tab as blanks under VisibleWhitespace, without the arrow.
	HideTabArrows bool
	// HideFoldMarkers draws no fold arrows. The zero value shows them.
	HideFoldMarkers bool
	// Icons picks the glyphs of the fold arrows. The zero value is the plain set.
	Icons icons.Mode
	// HideVerticalScrollbar leaves the scrollbar column blank: the thumb and the track
	// are not drawn, and the change marks still are. The column is kept, so the text
	// keeps its width.
	HideVerticalScrollbar bool
	// HideHorizontalScrollbar draws no horizontal scrollbar, so the text takes the row
	// the bar would take. The first column can still move to keep the caret on screen.
	HideHorizontalScrollbar bool
	// CaretShape is how the caret cell is drawn. The zero value is CaretBlock.
	CaretShape CaretShape
	// Opts are the editor.* display options: relative numbers, hanging indent,
	// guides, whitespace marks and bracket highlights. The zero value is the plain pane.
	Opts Options

	// The overlay hooks (see overlays.go) give the decorations of one line. The root sets
	// them in paneGeometry, the one place that builds the pane for the painter and the
	// hit test alike. Nil paints none.
	Overlays    func(line int) []StyledSpan       // styled runs, rune columns
	RowStyle    func(line int) (grid.Style, bool) // the look of whole rows
	EOL         func(line int) []VirtualText      // text after the last cell
	Replaces    func(line int) []Replacement      // text over single cells
	Gutter      func(line int) (GutterMark, bool) // replaces the change bar
	ScrollMarks func(line int) (ScrollMark, bool) // colors the last column

	// Fillers puts blank rows between the lines (see fillers.go). A pane with fillers
	// never wraps, and never shows folds. Nil has none.
	Fillers *Fillers
	// TrackMarks mark places of the whole text on the scrollbar track, which the visible
	// rows' own marks (ScrollMarks) cannot: the compare view marks each difference there.
	// A mark is drawn on the track cells, never over the thumb.
	TrackMarks []TrackMark
}

// TrackMark is a run of text rows [From, To] on the scrollbar track, in the rows of the
// whole text (filler rows included), colored with the foreground of Style.
type TrackMark struct {
	From, To int
	Style    grid.Style
}

// CaretShape is the look of the caret cell. The root shapes the terminal caret the
// same way.
type CaretShape int

// The caret shapes.
const (
	CaretBlock     CaretShape = iota // the cell is drawn in reverse video
	CaretBar                         // the cell shows a thin bar at its left edge
	CaretUnderline                   // the cell is underlined
)

// DiagnosticSpan is the run of rune columns [Start, End) of a line with a
// diagnostic of Severity. The severities are the LSP numbers: SeverityError to
// SeverityHint. A cell with two diagnostics takes the more severe one.
type DiagnosticSpan struct {
	Start, End int
	Severity   int
}

// The LSP severities of a diagnostic.
const (
	SeverityError   = 1
	SeverityWarning = 2
	SeverityInfo    = 3
	SeverityHint    = 4
)

// Highlight is the run of rune columns [Start, End) of a line painted with the
// search colors. Current marks the match the caret is on.
type Highlight struct {
	Start, End int
	Current    bool
}

// HitKind names what is under a pane cell.
type HitKind int

// The kinds a Hit can report.
const (
	HitNone           HitKind = iota // outside the body, the horizontal bar, or no document
	HitText                          // the text: a caret or selection position
	HitGutter                        // the line numbers: the start of the line
	HitScrollbarTrack                // the scrollbar, outside the thumb
	HitScrollbarThumb                // the scrollbar thumb
	HitHScrollbar                    // the horizontal scrollbar; Col is the pane column
)

// Hit is the result of a mouse hit test. Pos is set for text and gutter hits,
// to the document position the press maps to. Row is the body row of the cell,
// which the scrollbar uses with ScrollTopFor.
type Hit struct {
	Kind HitKind
	Pos  buffer.Pos
	Row  int
	Col  int // the pane column of a horizontal scrollbar hit
	// Fold is set for a gutter hit on the first row of a line that starts a fold
	// that a click may toggle: any collapsed fold, and an open one while the
	// fold markers show.
	Fold bool
}

// Gutter sizes. The number field is at least three digits wide, and it widens
// by one once the line count has three digits (from 100 lines), as the legacy
// gutter does (legacy at 200 lines: "  88 │" at 200 lines).
const (
	gutterMinDigits = 3
	gutterSeparator = 3 // " │ " after the number field
	tildeText       = "~"
	changeBar       = "│" // margin bar: new or grown line
	changeScroll    = "▌" // last column: line differs from the saved text
	thumbGlyph      = "┃" // scrollbar thumb: secondary color (spec section 3)
	trackGlyph      = "│" // scrollbar track: separator color (spec section 3)
	caretBarGlyph   = "▏" // the caret of CaretBar: a thin bar at the left edge of the cell
	thumbHGlyph     = "━" // horizontal scrollbar thumb
	trackHGlyph     = "─" // horizontal scrollbar track
)

// GutterWidth is the width of the line-number gutter for a document with lines
// lines. It matches the gutter the layout reserves in the root.
func GutterWidth(lines int) int {
	digits := len(strconv.Itoa(max(lines, 0)))
	return max(gutterMinDigits, digits+1) + gutterSeparator
}

// frame is the per-call state of one Render, Caret or Hit: the document, the
// window and the derived geometry. It is built once and passed by value.
type frame struct {
	doc    Document
	styles Styles
	w, h   int // h is the number of text rows; the horizontal bar is not one
	lines  int
	// display is the line count that sizes the gutter and the scrollbar thumb. It is
	// lines, except in a large document whose line index is still being built, where
	// it is the estimate of the final count when that is larger.
	display int
	// win is the document's window onto very long lines, or nil when it has none or
	// the document is not large. See large.go.
	win LineWindower
	// fill is the filler rows of the pane, or nil.
	fill *Fillers
	// off and cut describe the line being laid out when it is shown as a window:
	// off is the rune column where the window starts, and cut says the window ends
	// before the line does. Both are zero for a line shown whole.
	off    int
	cut    bool
	top    int
	topRow int // the display rows of line top that are scrolled off, with wrap on
	gutter int
	wrap   bool
	wrapW  int
	tab    int // the columns per tab stop
	// left is the first visible display column, always zero when wrap is on.
	left int
	// hbar is true when the last body row is the horizontal scrollbar, at row
	// barY. contentW is the widest visible line, which sizes the bar.
	hbar     bool
	barY     int
	contentW int
	overflow bool
	caret    buffer.Pos
	sels     []buffer.Selection // the primary selection and the extra ones
	extra    []buffer.Pos       // the positions of the extra carets
	bar      map[int]bool       // lines that get the margin bar
	scroll   map[int]bool       // lines that get the last-column change mark
	// highlights gives the search highlights of a line. Nil paints none.
	highlights func(line int) []Highlight
	// folds maps each fold start line to whether it is collapsed. hidden marks
	// the lines inside a collapsed fold. Both are nil without folds.
	folds  map[int]bool
	hidden []bool
	// occWord is the word whose occurrences are painted, or "".
	occWord string
	curLine bool
	rulers  []int
	diags   func(line int) []DiagnosticSpan
	// The hidden parts of the pane (see Pane), and the caret shape.
	noTabArrows bool
	noFolds     bool
	icons       icons.Mode
	noVBar      bool
	noHBar      bool
	caretShape  CaretShape
	keepLeft    bool
	opts        Options
	fx          *frameFX // the state of the options, set by initFX
	hooks       overlayHooks
	dx          *overlayFX // the looks of the overlays; nil when the pane has no overlay hook
	marks       []TrackMark
}

// newFrame builds the frame for a w by h pane. It reports false when there is
// nothing to paint: no document, or an empty pane.
func (p Pane) newFrame(w, h int) (frame, bool) {
	if p.Doc == nil || w <= 0 || h <= 0 {
		return frame{}, false
	}
	lines := max(p.Doc.LineCount(), 1)
	display := max(DisplayLines(p.Doc), lines)
	f := frame{
		doc:        p.Doc,
		styles:     p.Styles,
		w:          w,
		h:          h,
		lines:      lines,
		display:    display,
		gutter:     GutterWidth(display),
		wrap:       p.wrapOn(),
		tab:        p.tabSize(),
		caret:      p.Doc.Cursor(),
		highlights: p.Highlights,
		curLine:    p.CurrentLine,
		rulers:     p.Rulers,
		diags:      p.Diagnostics,

		noTabArrows: p.HideTabArrows,
		noFolds:     p.HideFoldMarkers,
		icons:       p.Icons,
		noVBar:      p.HideVerticalScrollbar,
		noHBar:      p.HideHorizontalScrollbar,
		caretShape:  p.CaretShape,
		keepLeft:    p.KeepLeft,
		hooks:       p.overlayHooks(),
		marks:       p.TrackMarks,
	}
	if f.hooks.any() {
		f.dx = &overlayFX{}
	}
	if p.HideGutter {
		f.gutter = 0
	}
	f.wrapW = (w - f.gutter) - 2 // the legacy wrap width: text area less two
	if windowed(p.Doc) {
		f.win, _ = p.Doc.(LineWindower)
	}
	if !windowed(p.Doc) {
		f.fill = p.Fillers
	}
	f.setFolds(p.Doc)
	f.top = f.visibleAtOrAbove(min(max(p.Top, 0), lines-1))
	f.setSelections(p.Doc)
	f.initFX(p)
	switch {
	case f.fill != nil && p.TopRow > 0 && f.top == p.Top:
		f.topRow = min(p.TopRow, f.rowsIn(f.top, f.layoutLine(f.top, false))-1)
	case f.wrap && p.TopRow > 0 && f.top == p.Top:
		if f.isTall(f.top) {
			// The view of a tall line starts at a rune column, any column of the row it names.
			f.topRow = min(p.TopRow, f.runeLen(f.top))
		} else {
			f.topRow = min(p.TopRow, len(f.layoutLine(f.top, false).spans)-1)
		}
	}
	f.placeHorizontal(p.Left, h)
	// The vertical overflow counts the text rows left after the horizontal bar.
	f.overflow = f.visibleCount()+f.fillRows() > f.h || (f.display > f.lines && f.display > f.h)
	switch {
	case isLarge(p.Doc):
		// The saved text of a large document is not held, so there is nothing to diff
		// the lines against. The margin bar marks the modified lines on screen, and the
		// scrollbar carries no change marks.
		f.bar = f.dirtyRows()
	case p.Doc.Modified():
		f.bar, f.scroll = f.changes()
	}
	return f, true
}

// ChangeMemo is implemented by a Document that keeps the change marks until its text or
// its saved text changes, as *editor.Document does. A repaint then does not diff all the
// lines again. The maps are shared between frames and are only read.
type ChangeMemo interface {
	MemoChanges(compute func() (bar, scroll map[int]bool)) (bar, scroll map[int]bool)
}

// LineMatcher is implemented by a Document that compares a line with a string without
// building the line, as *editor.Document's adapter does. The change marks use it to find
// the lines that still equal the saved ones, so an edit in a big document does not build
// every line again.
type LineMatcher interface {
	LineIs(i int, text string) bool
}

// EditedLiner is implemented by a Document that knows which lines edits touched since the
// saved text, as *editor.Document's adapter does. The change marks then compare only those
// lines, so typing in a big document does not look at all the others. The lines before lo
// and from hi on are the saved ones, counted from the start and from the end.
type EditedLiner interface {
	EditedLines() (lo, hi int, ok bool)
}

// SharedSaver is implemented by a Document that hands out its saved lines without a copy.
// The pane only reads them.
type SharedSaver interface {
	SavedLinesShared() []string
}

// changes returns the marks of the frame: the lines that get the margin bar, which are
// worked out for the lines that edits touched, and the lines that get the change mark in
// the last column, which are worked out for the rows on screen, because only they are drawn.
func (f frame) changes() (bar, scroll map[int]bool) {
	var saved []string
	if ss, ok := f.doc.(SharedSaver); ok {
		saved = ss.SavedLinesShared()
	} else {
		saved = f.doc.SavedLines()
	}
	compute := func() (map[int]bool, map[int]bool) { return f.barMarks(saved), nil }
	if m, ok := f.doc.(ChangeMemo); ok {
		bar, _ = m.MemoChanges(compute)
	} else {
		bar, _ = compute()
	}
	return bar, f.visibleScroll(saved)
}

// longMarkRunes is the length from which a line is not compared with its saved text for
// the change marks. Comparing takes time in proportion to the line, on every key typed in it,
// so an edited line that long counts as changed, and as grown when it is not shorter.
const longMarkRunes = 1 << 16

// lineLener is implemented by a Document that tells the rune length of a line without
// reading it, as *editor.Document's adapter does.
type lineLener interface {
	LineLen(i int) int
}

// longForMarks reports whether line i is too long to compare with its saved text.
func (f frame) longForMarks(i int) bool {
	if ll, ok := f.doc.(lineLener); ok && !isLarge(f.doc) {
		return ll.LineLen(i) > longMarkRunes
	}
	return false
}

// editedWindow returns the lines [lo, hi) of the text and [lo, savedHi) of the saved text
// that may differ: all of them, or the lines edits touched when the document says which, less
// the lines at both ends that are the same as the saved ones. A line that is too long to be
// compared is never left out.
func (f frame) editedWindow(saved []string) (lo, hi, savedHi int) {
	lo, hi, savedHi = 0, f.lines, len(saved)
	if el, ok := f.doc.(EditedLiner); ok {
		if elo, ehi, ok := el.EditedLines(); ok && ehi <= f.lines && elo <= ehi {
			// The lines below ehi are the saved ones counted from the end.
			if shi := len(saved) - (f.lines - ehi); shi >= elo && shi <= len(saved) {
				lo, hi, savedHi = elo, ehi, shi
			}
		}
	}
	if lm, ok := f.doc.(LineMatcher); ok {
		for lo < hi && lo < savedHi && !f.longForMarks(lo) && lm.LineIs(lo, saved[lo]) {
			lo++
		}
		for hi > lo && savedHi > lo && !f.longForMarks(hi-1) && lm.LineIs(hi-1, saved[savedHi-1]) {
			hi--
			savedHi--
		}
	}
	return lo, hi, savedHi
}

// barMarks diffs the saved lines against the current lines that may differ, and returns
// the lines that get the margin bar.
func (f frame) barMarks(saved []string) map[int]bool {
	lo, hi, savedHi := f.editedWindow(saved)
	if hi-lo == 1 && savedHi-lo == 1 && f.longForMarks(lo) {
		// One very long line: it is not read. It is marked unless it got shorter.
		if f.doc.(lineLener).LineLen(lo) >= len(saved[lo]) {
			return map[int]bool{lo: true}
		}
		return nil
	}
	cur := make([]curLine, hi-lo)
	for i := range cur {
		cur[i] = f.curLineAt(lo+i, saved)
	}
	// The lines outside the window are the saved ones, so they carry no mark: the window
	// gives the same marks as all lines.
	return shifted(barMarks(saved[lo:savedHi], cur), lo)
}

// curLineAt is line i of the document as barMarks compares it. A line too long to compare is not
// read: it is the saved line at its index when the two are the same, which one comparison that
// stops at the first difference tells, and otherwise its length alone. An edited long line thus
// costs no more per frame than its length, and a long line the window only covers costs no copy.
func (f frame) curLineAt(i int, saved []string) curLine {
	if !f.longForMarks(i) {
		return curLine{text: f.doc.Line(i)}
	}
	n := f.doc.(lineLener).LineLen(i)
	if lm, ok := f.doc.(LineMatcher); ok && i < len(saved) && n == len(saved[i]) && lm.LineIs(i, saved[i]) {
		return curLine{text: saved[i]}
	}
	return curLine{runes: n, long: true}
}

// visibleScroll returns the lines among the rows on screen, from the top line, that get the
// change mark in the last column: those whose text differs from the saved line at the same
// index. When the line count changed, the row just past the last line is marked too, if
// any line differs.
func (f frame) visibleScroll(saved []string) map[int]bool {
	lm, _ := f.doc.(LineMatcher)
	wlo, whi, _ := f.editedWindow(saved)
	differs := func(i int) bool {
		switch {
		case i >= len(saved):
			return true
		case f.longForMarks(i):
			return i >= wlo && i < whi
		case lm != nil:
			return !lm.LineIs(i, saved[i])
		}
		return f.doc.Line(i) != saved[i]
	}
	var out map[int]bool
	mark := func(i int) {
		if out == nil {
			out = make(map[int]bool)
		}
		out[i] = true
	}
	i, y := f.top, 0
	for ; i < f.lines && y < f.h; i, y = f.next(i), y+1 {
		if differs(i) {
			mark(i)
		}
	}
	if i >= f.lines && y < f.h && len(saved) != f.lines && out == nil {
		for j := min(wlo, f.lines); j < f.lines; j++ {
			if differs(j) {
				out = map[int]bool{}
				break
			}
		}
	}
	if out != nil && i >= f.lines && len(saved) != f.lines {
		out[f.lines] = true
	}
	return out
}

// shifted returns the marks of m moved down by n lines. It returns nil for no marks.
func shifted(m map[int]bool, n int) map[int]bool {
	if m == nil || n == 0 {
		return m
	}
	out := make(map[int]bool, len(m))
	for i := range m {
		out[i+n] = true
	}
	return out
}

// Render paints the pane into a new w by h grid. Body rows past the end of the
// document show "~" in the gutter. The scrollbar takes the last column when the
// document is taller than h, and the horizontal scrollbar takes the last body
// row when a line is wider than the text area with Wrap off. The caret cell is
// drawn in reverse video when it is in the body; Caret returns the same
// position for the terminal cursor.
func (p Pane) Render(w, h int) *grid.Grid {
	g := grid.New(w, h)
	for y := range g.Height() {
		g.Fill(0, g.Width(), y, p.Styles.Text)
	}
	f, ok := p.newFrame(w, h)
	if !ok {
		return g
	}
	rows := f.paintText(g)
	f.paintRulers(g)
	f.paintMarks(g, rows)
	f.paintHorizontalBar(g)
	f.paintSecondaryCarets(g)
	f.paintCaret(g)
	return g
}

// paintCaret draws the caret cell in the frame's caret shape, so the caret shows even
// when the terminal cursor is hidden. The caret never takes the scrollbar column.
func (f frame) paintCaret(g *grid.Grid) {
	x, y, ok := f.cellOf(f.caret)
	if !ok || x < f.gutter || x >= f.w-1 {
		return
	}
	c := g.At(x, y)
	st := c.Style
	text := c.Text
	if text == "" {
		text = " "
	}
	switch f.caretShape {
	case CaretBar:
		text = caretBarGlyph
	case CaretUnderline:
		st.Underline = true
	default:
		st.Reverse = true
	}
	g.Set(x, y, text, st)
}

// rowInfo ties a painted body row to the buffer line it shows. The marks are
// per line, so every row of a wrapped line takes them.
type rowInfo struct {
	y, line int
	filler  bool // a filler row: it belongs to no line, so it takes no mark of the line
}

// paintText writes the gutter and the text of every body row and returns the
// row list in order.
func (f frame) paintText(g *grid.Grid) []rowInfo {
	rows := make([]rowInfo, 0, f.h)
	y, i := 0, f.top
	for ; i < f.lines && y < f.h; i = f.next(i) {
		anchor := 0
		if i == f.top && f.fill == nil {
			// The rows of the top line that are scrolled off, or the column its view starts at.
			anchor = f.topRow
		}
		cur := f.curLine && i == f.caret.Line
		above := f.fillAbove(i)
		rows, y = f.paintFillers(g, rows, i, 0, above, y)
		for _, rd := range f.lineRows(i, true, anchor, f.h-y) {
			if f.fill != nil && i == f.top && above+rd.k < f.topRow {
				continue // a pane with fillers counts the rows of the top block in topRow
			}
			ln := rd.ln
			if f.gutter > 0 {
				label := fmt.Sprintf("%*s │ ", f.gutter-gutterSeparator, "")
				if rd.first {
					label = fmt.Sprintf("%*d │ ", f.gutter-gutterSeparator, f.lineNumber(i))
				}
				g.Put(0, y, f.w-1, label, f.numberStyle(i))
			}
			cells := f.rowCells(rd)
			// skip is how far left of the window the line ended: its virtual text starts
			// that many columns left of the first text column.
			skip := 0
			if drop := f.dropFor(ln); drop > 0 {
				skip = droppedBeyond(cells, drop)
				cells = dropColumns(cells, drop)
			}
			end := f.paintCells(g, y, cells, cur)
			if rd.last {
				end = f.paintFoldPlaceholder(g, i, y, end, cur)
			}
			if cur && f.styles.CurrentLine.BG != nil {
				g.Fill(end, f.w-1, y, grid.Style{FG: f.styles.Text.FG, BG: f.styles.CurrentLine.BG})
			}
			f.paintRowTail(g, ln, y, end, cur)
			if rd.last {
				f.paintVirtualText(g, ln, i, y, end, skip, cur)
			}
			if rd.first {
				// The marker is drawn after the text, so it never covers a text cell.
				f.paintFoldMarker(g, i, y, end)
			}
			rows = append(rows, rowInfo{y: y, line: i})
			y++
		}
		// A pane with fillers does not wrap, so the text of a line is one row.
		rows, y = f.paintFillers(g, rows, i, above+1, f.fillAfter(i), y)
	}
	for ; y < f.h; y, i = y+1, i+1 {
		g.Put(0, y, f.w-1, tildeText, f.styles.Gutter)
		rows = append(rows, rowInfo{y: y, line: i})
	}
	return rows
}

// paintCells writes one display row of cells from the gutter edge and returns
// the column after the last cell written. Runs of the same attributes are
// written together, and a zero-width mark joins the run before it. The last
// column is never text. cur is true on the caret line.
func (f frame) paintCells(g *grid.Grid, y int, cells []cell, cur bool) int {
	x := f.gutter
	maxX := f.w - 1
	for start := 0; start < len(cells); {
		end := start + 1
		for end < len(cells) && (cells[end].sameAttr(cells[start]) || isZeroWidth(cells[end].r)) {
			end++
		}
		var b strings.Builder
		for _, c := range cells[start:end] {
			switch {
			case c.rep == 0:
				b.WriteRune(c.r)
			case c.rep > repHidden:
				b.WriteString(f.dx.repText(c.rep))
			}
		}
		x += g.Put(x, y, maxX, b.String(), f.style(cells[start], cur))
		start = end
	}
	return x
}

// style is the grid style of one cell. Its base is the syntax color. A search
// match takes its background, or its whole style when it is the current match.
// An occurrence takes its background, and the caret line takes its tint, both
// under a match. The selection background goes over all of these. A whitespace
// cell takes the whitespace color, and a diagnostic takes its severity color
// and an underline.
func (f frame) style(c cell, cur bool) grid.Style {
	st := f.styles.Text
	if int(c.kind) < len(f.styles.Syntax) {
		st = f.styles.Syntax[c.kind]
	}
	switch {
	case c.match == matchCurrent:
		st = f.styles.CurrentMatch
	case c.match == matchOther:
		st.BG = f.styles.Match.BG
	case c.match == matchOccurrence && f.styles.Occurrence.BG != nil:
		st.BG = f.styles.Occurrence.BG
		// The band lands where syntax colors live, so text that would vanish on it
		// is moved to a readable color.
		if fg := repairedFG(st.FG, st.BG); fg != nil {
			st.FG = fg
		}
	case cur && f.styles.CurrentLine.BG != nil:
		st.BG = f.styles.CurrentLine.BG
	}
	if c.ws {
		fg := f.styles.Whitespace.FG
		if c.sel && f.styles.Decor.WhitespaceSelected.FG != nil {
			fg = f.styles.Decor.WhitespaceSelected.FG
		}
		if fg != nil {
			st.FG = fg
		}
	}
	f.decorStyle(c, &st)
	if c.dec != 0 {
		st = PatchStyle(st, f.dx.look(c.dec))
	}
	if c.diag != 0 {
		if fg := f.diagnosticFG(c.diag); fg != nil {
			st.FG = fg
		}
		st.Underline = true
	}
	if c.sel {
		st.BG = f.styles.Selection.BG
		if st.BG == nil {
			st.Reverse = true
		}
	}
	return st
}

// paintMarks writes the margin bar, the change mark and the scrollbar on the
// rows. The change mark has the last column when the line changed. Without a
// change mark, the scrollbar takes the column when the document overflows. The
// thumb is a "┃" in the thumb style and the track a "│" in the track style.
func (f frame) paintMarks(g *grid.Grid, rows []rowInfo) {
	last := f.w - 1
	thumbStart, thumbLen := f.thumb()
	for _, r := range rows {
		if r.filler {
			// A filler row has the scrollbar only.
			f.paintBar(g, r.y, thumbStart, thumbLen)
			continue
		}
		if text, st, ok := f.gutterMark(r.line); ok {
			g.Set(0, r.y, text, st)
		} else if f.bar[r.line] && f.gutter > 0 {
			g.Set(0, r.y, changeBar, f.styles.Gutter)
		}
		st, marked := f.scrollMark(r.line)
		switch {
		case marked:
			g.Set(last, r.y, changeScroll, st)
		case f.scroll[r.line]:
			g.Set(last, r.y, changeScroll, f.styles.Gutter)
		default:
			f.paintBar(g, r.y, thumbStart, thumbLen)
		}
	}
}

// paintBar writes the scrollbar cell of body row y: the thumb or the track. The column stays
// blank when the document does not overflow or the scrollbar is hidden.
func (f frame) paintBar(g *grid.Grid, y, thumbStart, thumbLen int) {
	switch {
	case f.overflow && f.noVBar:
		// The column stays blank: the scrollbar is hidden.
	case f.overflow && y >= thumbStart && y < thumbStart+thumbLen:
		g.Set(f.w-1, y, thumbGlyph, f.styles.Thumb)
	case f.overflow:
		if st, ok := f.trackMark(y); ok {
			g.Set(f.w-1, y, changeScroll, st)
			return
		}
		g.Set(f.w-1, y, trackGlyph, f.styles.Scrollbar)
	}
}

// Caret returns the terminal cursor position of the document caret, in
// pane-local cells. ok is false when the caret line is above the window or
// below the body.
func (p Pane) Caret(w, h int) (x, y int, ok bool) {
	f, ok := p.newFrame(w, h)
	if !ok {
		return 0, 0, false
	}
	return f.cellOf(f.caret)
}

// BodyRows is the number of text rows of a w by h pane, which is h less the row the
// horizontal scrollbar takes. With Wrap on there is no bar, so it is h. The root
// sizes the editor viewport from it.
func (p Pane) BodyRows(w, h int) int {
	if p.wrapOn() {
		return max(h, 0)
	}
	f, ok := p.newFrame(w, h)
	if !ok {
		return max(h, 0)
	}
	return f.h
}

// RevealTop returns the top line that puts the caret's display row inside a w by h
// pane, counting the wrapped rows of each line. A caret already on screen keeps
// the pane's top. A caret above the window takes its own line. A caret below takes
// the fewest lines that fit above it, so the scroll is as short as it can be.
func (p Pane) RevealTop(w, h int) int {
	return p.RevealTopMargin(w, h, 0)
}

// RevealTopMargin is RevealTop that also keeps margin display rows between the caret
// row and the top or bottom of the body, as the scroll-off setting asks. The margin
// is at most half the body, and it gives way at the start and the end of the
// document, where there is no text to show beyond the caret. The view moves by whole
// lines, so a wrapped line at the edge can leave a little more than margin rows.
func (p Pane) RevealTopMargin(w, h, margin int) int {
	top, _ := p.RevealView(w, h, margin)
	return top
}

// RevealView is RevealTopMargin that also returns the display rows of the top line to
// scroll off (Pane.TopRow). The view moves by rows inside a wrapped line only when the
// line is taller than the body, so that the caret's row can be shown; every other view
// starts at the first row of its top line.
func (p Pane) RevealView(w, h, margin int) (top, row int) {
	f, ok := p.newFrame(w, h)
	if !ok {
		return p.Top, 0
	}
	top, row = f.revealView()
	if margin <= 0 || f.caret.Line < 0 || f.caret.Line >= f.lines || (f.hidden != nil && f.hidden[f.caret.Line]) {
		return top, row
	}
	return f.withMargin(top, row, min(margin, (f.h-1)/2))
}

// ScrollView returns the view that is delta steps below the pane's (above for a negative
// delta), for a wheel notch. A step is a line, except inside a wrapped line taller than the
// body, where it is a display row, so that such a line can be read to its end. Without
// wrap it is always a line. The view stops with the last line, or the last row of a tall
// last line, at the top.
func (p Pane) ScrollView(w, h, delta int) (top, row int) {
	f, ok := p.newFrame(w, h)
	if !ok {
		return p.Top, 0
	}
	if f.fill != nil {
		return f.scrollFilled(delta)
	}
	if !f.wrap {
		return min(max(f.top+delta, 0), f.lines-1), 0
	}
	top, row = f.top, f.topRow
	rows := f.rowCount
	for ; delta > 0; delta-- {
		if f.isTall(top) {
			// A tall line is scrolled by rows to its last one. The row is a rune column.
			if next, ok := f.tallNextRow(top, row); ok {
				row = next
				continue
			}
		} else if n := rows(top); n > f.h && row+1 < n {
			row++
			continue
		}
		next := f.next(top)
		if next >= f.lines {
			break
		}
		top, row = next, 0
	}
	for ; delta < 0; delta++ {
		if f.isTall(top) {
			if prev, ok := f.tallPrevRow(top, row); ok {
				row = prev
				continue
			}
		} else if row > 0 {
			row--
			continue
		}
		prev := f.prevLine(top)
		if prev < 0 {
			break
		}
		top, row = prev, 0
		if f.isTall(top) {
			row = f.tallRowStart(top, f.runeLen(top))
		} else if n := rows(top); n > f.h {
			row = n - 1
		}
	}
	return top, row
}

// tabSize is the tab width of the pane: TabSize, or the default when it is zero.
func (p Pane) tabSize() int {
	if p.TabSize > 0 {
		return p.TabSize
	}
	return defaultTabSize
}

// caretRowIn is the display row of the caret within its own line.
func (f frame) caretRowIn() int {
	if f.fx.caretRowOK {
		return f.fx.caretRow
	}
	var k int
	if f.isTall(f.caret.Line) {
		// The rows from the line start to the caret's row, counted as far as the body is
		// tall, which is as far as any use of it goes.
		k, _ = f.tallRowsBetween(f.caret.Line, 0, f.caret.Col, 4*f.h+16)
	} else {
		ln := f.layoutLine(f.caret.Line, false)
		k, _ = caretInSpans(ln.geom, ln.spans, expandedIndex(ln.raw, f.caret.Col-ln.off, f.tab))
		k += f.fillAbove(f.caret.Line)
	}
	f.fx.caretRow, f.fx.caretRowOK = k, true
	return k
}

// revealView is the view RevealView returns for the frame, before the margin.
func (f frame) revealView() (top, row int) {
	c := f.caret.Line
	if c < 0 || c >= f.lines || (f.hidden != nil && f.hidden[c]) {
		return f.top, f.topRow
	}
	if f.isTall(c) {
		return f.revealTall()
	}
	if c < f.top {
		// A caret above the window takes its own line. Only the rows of a line taller
		// than the body are scrolled, so that the caret's row is the last one shown.
		return c, max(f.caretRowIn()-(f.h-1), 0)
	}
	if _, _, ok := f.cellOf(f.caret); ok {
		return f.top, f.topRow
	}
	k := f.caretRowIn()
	if c == f.top {
		// The caret is on the top line, above or below the rows that show.
		if k < f.topRow {
			return c, k
		}
		return c, k - (f.h - 1)
	}
	// The caret is below the body. Count the rows from the caret's line up to the
	// caret's own row, then add the lines above it while they fit.
	rows, top := k+1, c
	for prev := f.prevLine(top); prev >= f.top; prev = f.prevLine(top) {
		n := f.rowCount(prev)
		if rows+n > f.h {
			break
		}
		rows += n
		top = prev
	}
	if top == c {
		return c, max(rows-f.h, 0)
	}
	return top, 0
}

// withMargin moves the view (top, row), which shows the caret row, so that margin rows
// of text sit above the caret row while the lines above allow it, and as many below it
// as the document has, up to margin.
func (f frame) withMargin(top, row, margin int) (int, int) {
	if f.isTall(top) {
		return f.marginTall(top, row, margin)
	}
	for f.caretRow(top)-row < margin {
		if row > 0 {
			row = max(row-(margin-(f.caretRow(top)-row)), 0)
			continue
		}
		prev := f.prevLine(top)
		if prev < 0 {
			break
		}
		top = prev
	}
	need := 1 + f.rowsBelowCaret(margin)
	for top < f.caret.Line && f.caretRow(top)-row+need > f.h {
		top, row = f.next(top), 0
	}
	if top == f.caret.Line && f.caretRow(top)-row+need > f.h {
		// The caret line is the top line, and it is taller than the body: its rows
		// scroll off until the caret has its margin below, as far as the caret's row.
		row = min(f.caretRow(top)+need-f.h, f.caretRow(top))
	}
	return top, row
}

// caretRow is the display row of the caret counted from line top, past the body if
// the caret is below it.
func (f frame) caretRow(top int) int {
	y := 0
	for i := top; i < f.lines; i = f.next(i) {
		if i == f.caret.Line {
			return y + f.caretRowIn()
		}
		y += f.rowCount(i)
	}
	return y
}

// rowsBelowCaret is the number of display rows after the caret row, up to limit.
func (f frame) rowsBelowCaret(limit int) int {
	var n int
	if f.isTall(f.caret.Line) {
		n = len(f.tallRows(f.caret.Line, false, f.caret.Col, limit+1)) - 1
		if n >= limit {
			return limit
		}
	} else {
		n = f.rowCount(f.caret.Line) - 1 - f.caretRowIn()
	}
	for i := f.next(f.caret.Line); i < f.lines && n < limit; i = f.next(i) {
		n += f.rowCount(i)
	}
	return min(n, limit)
}

// cellOf finds the cell of a document position by walking the rows from the
// top line. ok is false when the position is above the window, below the body,
// or on a line hidden by a fold.
func (f frame) cellOf(p buffer.Pos) (x, y int, ok bool) {
	y = -f.topRow
	if f.isTall(f.top) {
		y = 0 // the view of a tall line starts at a rune column, not at a row
	}
	for i := f.top; i < f.lines && y < f.h; i = f.next(i) {
		if f.isTall(i) {
			anchor := 0
			if i == f.top {
				anchor = f.topRow
			}
			if i == p.Line {
				return f.tallCellOf(i, anchor, p, y)
			}
			y += len(f.lineRows(i, false, anchor, f.h-y))
			continue
		}
		ln := f.layoutLine(i, false)
		if i == p.Line {
			raw, _ := f.lineRaw(i)
			rel := p.Col - ln.off
			if ln.long {
				rel = min(max(rel, 0), len(raw))
			}
			k, cells := caretInSpans(ln.geom, ln.spans, expandedIndex(raw, rel, f.tab))
			if ln.long {
				// A caret outside the window of a long line is on no cell of it, and its
				// column goes on past the window's edge as if the cells did.
				cells += p.Col - ln.off - rel
			}
			y += f.fillAbove(i) + k
			if k > 0 {
				cells += ln.indent
			}
			return f.gutter + cells - f.dropFor(ln), y, y >= 0 && y < f.h
		}
		y += f.rowsIn(i, ln)
	}
	return 0, 0, false
}

// Hit maps a pane-local cell to what it shows. A cell outside the body, a cell
// on the horizontal scrollbar row, or a cell in a pane with no document, is
// HitNone. The last column is the scrollbar when the document overflows, and
// the text otherwise.
func (p Pane) Hit(w, h, x, y int) Hit {
	f, ok := p.newFrame(w, h)
	if ok && f.hbar && y == f.barY && x >= f.gutter && x < w {
		return Hit{Kind: HitHScrollbar, Col: x}
	}
	if !ok || x < 0 || y < 0 || x >= w || y >= h || y >= f.h {
		return Hit{Kind: HitNone}
	}
	switch {
	case x < f.gutter:
		r := f.rowAt(y)
		if r.filler != notFiller {
			return Hit{Kind: HitNone, Row: y}
		}
		return Hit{Kind: HitGutter, Pos: buffer.Pos{Line: r.line}, Row: y, Fold: r.valid && r.first && f.foldable(r.line)}
	case f.overflow && !f.noVBar && x == w-1:
		start, length := f.thumb()
		if y >= start && y < start+length {
			return Hit{Kind: HitScrollbarThumb, Row: y}
		}
		return Hit{Kind: HitScrollbarTrack, Row: y}
	}
	return Hit{Kind: HitText, Pos: f.posAt(x, y), Row: y}
}

// PosAt maps a pane-local cell to a document position. The row and the column
// are clamped into the document, so a drag past the edge gives the nearest
// position. A column left of the text maps to the start of the row, and a
// column past the end of a line maps to its last character.
func (p Pane) PosAt(w, h, x, y int) buffer.Pos {
	f, ok := p.newFrame(w, h)
	if !ok {
		return buffer.Pos{}
	}
	return f.posAt(x, y)
}

// ScrollTopFor returns the first visible line that centers the scrollbar thumb
// on body row row. It is the target of a track press, and of a thumb drag when
// the caller passes the row where the thumb's centre should go.
func (p Pane) ScrollTopFor(w, h, row int) int {
	if p.Doc == nil || h <= 0 {
		return 0
	}
	if f, ok := p.newFrame(w, h); ok && f.wrapped() {
		// With wrap a line is several rows, so the last page starts at the line whose
		// rows to the end just fill the body, not at lines-h.
		maxTop := f.lastPageTop()
		_, length := thumbOfMax(f.display, f.h, 0, maxTop)
		if span := f.h - length; maxTop > 0 && span > 0 {
			pos := min(max(row-length/2, 0), span)
			return min((pos*maxTop+span/2)/span, maxTop)
		}
		return 0
	}
	lines := max(p.Doc.LineCount(), 1)
	display := max(DisplayLines(p.Doc), lines)
	rows := p.Fillers.Total(lines) // the text rows: the lines, and the fillers between them
	if p.Fillers != nil {
		display = rows
	}
	_, length := thumbOf(display, h, 0)
	if h == length {
		return 0
	}
	pos := min(max(row-length/2, 0), h-length)
	// The thumb's start runs over 0..h-length as the top runs over 0..display-h, so
	// the last body row maps to the last page.
	span := h - length
	top := (pos*(display-h) + span/2) / span
	if p.Fillers != nil {
		// The top is a row; the pane's top is the line whose block holds it.
		line, _ := p.Fillers.LineAt(lines, min(top, max(rows-h, 0)))
		return line
	}
	return min(top, max(lines-h, 0))
}

// Thumb returns the first body row and the length in rows of the scrollbar
// thumb for the current top line in a pane h rows tall. It is meaningful when
// the document is taller than the pane.
func (p Pane) Thumb(h int) (start, length int) {
	if p.Doc == nil || h <= 0 {
		return 0, 0
	}
	lines := max(p.Doc.LineCount(), 1)
	display := max(DisplayLines(p.Doc), lines)
	top := min(max(p.Top, 0), lines-1)
	if p.Fillers != nil {
		return thumbOf(p.Fillers.Total(lines), h, p.Fillers.BlockStart(top)+max(p.TopRow, 0))
	}
	return thumbOf(display, h, top)
}

// thumb is the scrollbar thumb of the frame: its first body row and length.
func (f frame) thumb() (start, length int) {
	if f.fill != nil {
		return thumbOf(f.fill.Total(f.lines), f.h, f.fill.BlockStart(f.top)+f.topRow)
	}
	if f.wrapped() {
		return thumbOfMax(f.display, f.h, f.top, f.lastPageTop())
	}
	return thumbOf(f.display, f.h, f.top)
}

// wrapped reports whether the scrollbar maps the wrapped rows of the lines: wrap is on
// and the document is whole in memory, with no fillers.
func (f frame) wrapped() bool {
	return f.wrap && f.fill == nil && f.display == f.lines
}

// lastPageTop is the first line of the last page in a wrapping frame: the line from
// which the display rows of all the lines to the end fit the body. The scrollbar thumb
// rests on the bottom of the track there, and the end of the document is on screen.
func (f frame) lastPageTop() int {
	top, rows := f.lines-1, 0
	for i := f.visibleAtOrAbove(f.lines - 1); i >= 0; i = f.prevLine(i) {
		rows += f.rowCount(i)
		if rows > f.h {
			break
		}
		top = i
	}
	return top
}

// thumbOf sizes the thumb for lines document lines in a body h rows tall, with
// the first visible line top. The thumb is one row per lines/h of the document.
// Its start runs over 0..h-length as the top runs over 0..lines-h, so it reaches
// the bottom of the track when the last page is shown.
func thumbOf(lines, h, top int) (start, length int) {
	return thumbOfMax(lines, h, top, lines-h)
}

// thumbOfMax is thumbOf for a view whose last page starts at line maxTop.
func thumbOfMax(lines, h, top, maxTop int) (start, length int) {
	length = min(h, max(1, (h*h+lines/2)/lines))
	if maxTop > 0 {
		start = min(max(top, 0), maxTop) * (h - length) / maxTop
	}
	return start, length
}

// posAt maps the cell at (x, row) to a document position. See PosAt.
func (f frame) posAt(x, row int) buffer.Pos {
	r := f.rowAt(row)
	if r.filler != notFiller {
		return f.fillerPos(r)
	}
	cells := r.startCells()
	if x >= f.gutter {
		cells += x - f.gutter + f.dropFor(r.ln)
		if !r.first {
			// The hanging indent of a continuation row is not text: a press in it is the row's start.
			cells = max(cells-r.ln.indent, r.startCells())
		}
		if !r.last && cells >= r.endCells() {
			// A click past the end of a row that is not the last one lands on its
			// last character, not on the first character of the next row.
			cells = r.endCells() - 1
		}
	}
	return buffer.Pos{Line: r.line, Col: r.ln.off + runeIndexAt(r.ln.raw, cells, f.tab)}
}

// Blit copies src into dst with its top-left cell at (x0, y0). Cells outside dst
// are dropped. The root uses it to place the pane in the frame.
func Blit(dst *grid.Grid, x0, y0 int, src *grid.Grid) {
	for y := range src.Height() {
		for x := range src.Width() {
			c := src.At(x, y)
			dst.Set(x0+x, y0+y, c.Text, c.Style)
		}
	}
}
