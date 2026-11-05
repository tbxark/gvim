package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/editorpane"
)

// Geometry from the responsive spec (blueprint section 4). The painter and the
// hit map both read the layout computed here. No other file holds a column or
// row constant for these rows.
//
// The overlays (palette, find bar, prompts, dropdown) add their rows in the
// overlay phase. Their rules are not here yet.
const (
	gateMinWidth  = 8 // section 4.8: below this the frame shows only the gate message
	gateMinHeight = 3
	gateText      = "gvim: terminal too small, need 8x3"

	explorerMinWidth     = 16 // spec section 3: the drag minimum
	explorerMaxWidth     = 60 // spec section 3: the drag maximum
	explorerDefaultWidth = 32 // spec section 3: the automatic width
	explorerEditorMin    = 20 // the editor keeps this many columns when the frame allows
	explorerTitleNeed    = 21 // narrowest width at which the title has its "─" fill

	labelGap       = 3 // blank cells between menu labels
	noNameLabel    = "[No Name]"
	paletteHelp    = "Palette: ⌃P"
	warnBlankWidth = 7 // " [⚠ N] " for one digit, reserved while the counter is unknown
)

// barLabels is the menu bar, in order. "Explorer" joins it while the explorer
// has focus. The menu bar sub-model takes this list over with the generated
// menu table.
func barLabels(explorerFocus bool) []string {
	labels := []string{"File", "Edit", "View", "Selection", "Go", "LSP"}
	if explorerFocus {
		labels = append(labels, "Explorer")
	}
	return append(labels, "Help")
}

// layoutInput is the state the geometry depends on. It is plain data, so
// computeLayout is a pure function of the terminal size and this value.
type layoutInput struct {
	explorerOpen     bool
	explorerFocus    bool // "Explorer" joins the menu bar while focus is there
	explorerOverride int  // width set by a grip drag; 0 means automatic
	explorerRows     int  // visible tree rows, used by hit testing
	findRows         int  // rows the find bar takes at the bottom; zero when it is closed
	lines            int  // line count of the document, which sizes the gutter
	tabs             []TabInfo
	tabFirst         int  // the tab strip's scroll position, a tab index
	welcome          bool // no tab is open, so the welcome screen replaces the editor
	recents          []string
	status           statusView
}

// statusView is the content of the status row. The status bar sub-model fills
// the segments that are blank here.
type statusView struct {
	position string // for example "Ln 1, Col 1"
	message  string
	eol      string // "LF"
	encoding string // "ASCII" or "UTF-8"
	language string // "Text"
	lines    int    // line count; its digits set the message column
	warnSlot bool   // reserve the blank warning-counter slot
	warnings int    // warning count; shown as " [⚠ N] " when above zero
}

// run is a piece of text at a fixed column, together with what a click on it
// reaches. The painter draws runs and the hit test reads them, so a label and
// its click target cannot drift apart.
type run struct {
	x, w  int // first column and width in cells
	text  string
	kind  hitKind
	index int
}

// covers reports whether column x falls inside the run.
func (r run) covers(x int) bool { return x >= r.x && x < r.x+r.w }

// newRun places text at column x with the given hit target.
func newRun(x int, text string, kind hitKind, index int) run {
	return run{x: x, w: ansi.StringWidth(text), text: text, kind: kind, index: index}
}

// layout is the geometry of one frame in terminal cells. Rows and columns are
// numbered from zero at the top left. Fields named "bottom", "statusY" and "*Y"
// are inclusive rows. Fields named "*End" and "*W" are exclusive or sizes, as
// their comments say.
type layout struct {
	w, h  int
	gated bool // the minimum-size gate replaces the whole frame

	// Row bands (section 4.1). The editor body is bodyTop..bodyBottom. Row 0 is
	// the menu bar, row 1 the tab strip, and statusY the status row.
	bodyTop, bodyBottom int
	statusY             int
	scrollbar           bool // content overflows, so the last column is the scrollbar

	// Explorer (sections 4.2 and 4.3). The panel covers columns 0..ex-1 and rows
	// 1..exBottom. ex is zero when the panel is hidden.
	explorerOpen bool
	ex           int
	exBottom     int // bottom border row, the last body row
	exRows       int
	exTitle      string // header row, exactly ex cells
	exReloadX    int    // column of "⟳", -1 when the button is not drawn
	exCloseX     int    // column of "×", -1 when the button is not drawn

	// Editor text area (section 4.2). The gutter sits at editorX and the text at textX.
	editorX int
	gutter  int
	textX   int
	textW   int

	// Row 0 and row 1 (sections 4.5 and 4.6). The strip is placed by placeStrip,
	// which also gives the scroll position it used.
	menuRuns []run
	strip    strip

	// The welcome screen (no tab open) replaces the editor pane. welcome holds its
	// placed lines, and welcomeOn is true while it shows.
	welcomeOn bool
	welcome   []welcomeLine

	// Status bar (section 4.5). Runs cover the whole row.
	status []run
}

// computeLayout returns the geometry for a w by h terminal. It is pure: the
// same inputs always give the same layout, and painting and hit testing both
// use it.
func computeLayout(w, h int, in layoutInput) layout {
	l := layout{w: w, h: h}
	if w < gateMinWidth || h < gateMinHeight {
		l.gated = true
		return l
	}

	// The find bar takes rows under the status row, so the body and the status
	// row move up by its height.
	l.statusY = h - 1 - in.findRows
	l.bodyTop, l.bodyBottom = 2, l.statusY-1
	bodyRows := l.bodyBottom - l.bodyTop + 1
	l.scrollbar = bodyRows > 0 && in.lines > bodyRows

	l.setExplorer(in)
	l.editorX = l.ex
	l.gutter = gutterWidth(in.lines)
	l.textX = l.editorX + l.gutter
	l.textW = max(0, w-l.textX)

	l.menuRuns = menuRuns(in.explorerFocus)
	l.strip = placeStrip(l.editorX, w, in.tabs, in.tabFirst)
	if in.welcome {
		l.welcomeOn = true
		l.welcome = welcomeLines(l.editorX, max(w-l.editorX, 0), l.bodyTop, bodyRows, in.recents)
	}
	l.status = statusRuns(w, in.status)
	return l
}

// setExplorer fills the explorer fields. The panel spans rows 1..h-2 (section 4.1).
func (l *layout) setExplorer(in layoutInput) {
	l.exReloadX, l.exCloseX = -1, -1
	if !in.explorerOpen {
		return
	}
	l.explorerOpen = true
	l.ex = explorerCols(l.w, in.explorerOverride)
	l.exBottom = l.bodyBottom
	l.exRows = in.explorerRows
	l.exTitle = explorerTitle(l.ex)
	l.exReloadX = buttonColumn(l.exTitle, "⟳")
	l.exCloseX = buttonColumn(l.exTitle, "×")
}

// gutterWidth is the width of the line-number gutter. It is the pane's gutter,
// so the columns the layout reserves are the columns the pane paints.
func gutterWidth(lines int) int {
	return editorpane.GutterWidth(lines)
}

// buttonColumn is the first column of glyph in title, or -1 when it is absent.
func buttonColumn(title, glyph string) int {
	before, _, ok := strings.Cut(title, glyph)
	if !ok {
		return -1
	}
	return ansi.StringWidth(before)
}

// explorerCols is the explorer width E (spec section 3). The automatic width is
// 32 columns, and a grip drag sets the override. Both are clamped to [16, 60],
// and the upper bound drops to W-20 on a narrow frame so the editor keeps room.
// The width never exceeds the frame.
func explorerCols(w, override int) int {
	e := explorerDefaultWidth
	if override != 0 {
		e = override
	}
	hi := min(explorerMaxWidth, max(w-explorerEditorMin, explorerMinWidth))
	return min(max(e, explorerMinWidth), hi, w)
}

// explorerTitle is the explorer header, exactly e cells wide (section 4.3). The
// corner, the "×" and the "⟳" drop out in that order as the panel narrows.
func explorerTitle(e int) string {
	const head = "┌ File Explorer " // 16 cells
	switch {
	case e <= 16:
		return ansi.Truncate(head, e, "")
	case e == 17:
		return head + "⟳"
	case e == 18:
		return head + "⟳┐"
	case e == 19:
		return head + "⟳─×"
	case e == 20:
		return head + "⟳─×┐"
	default:
		return head + strings.Repeat("─", e-explorerTitleNeed) + "⟳─×─┐"
	}
}

// menuRuns places the menu bar labels (section 4.5). A label is shown with
// three blanks between labels.
func menuRuns(explorerFocus bool) []run {
	var runs []run
	x := 1
	for i, label := range barLabels(explorerFocus) {
		runs = append(runs, newRun(x, label, hitMenuLabel, i))
		x += ansi.StringWidth(label) + labelGap
	}
	return runs
}

// statusRuns lays out the status row (section 4.5). The left group is cut first
// to the room the right group leaves. The right group is dropped from its end
// until the left group keeps at least min(floor(2W/5), 40) cells.
func statusRuns(w int, s statusView) []run {
	left := []string{
		" Trusted ",
		" Local ",
		" " + padCells(s.position, positionWidth(s.lines)) + " ",
		" " + s.message + " ",
	}
	type segment struct {
		text string
		kind hitKind
	}
	right := []segment{
		{" " + s.eol + " ", hitStatusSegment},
		{" " + s.encoding + " ", hitStatusSegment},
		{" " + s.language + " ", hitStatusSegment},
	}
	switch {
	case s.warnings > 0:
		right = append(right, segment{" [⚠ " + strconv.Itoa(s.warnings) + "] ", hitNone})
	case s.warnSlot:
		// The blank keeps the columns the counter takes once it is known.
		right = append(right, segment{strings.Repeat(" ", warnBlankWidth), hitNone})
	}
	right = append(right, segment{" " + paletteHelp + " ", hitStatusPalette})

	minLeft := min(2*w/5, 40)
	budget := w - (minLeft + 1)
	keep := 0
	if w >= 15 {
		keep = 1
	}
	rightWidth := func() int {
		total := 0
		for _, seg := range right {
			total += ansi.StringWidth(seg.text)
		}
		return total
	}
	for rightWidth() > budget && len(right) > keep {
		right = right[:len(right)-1]
	}

	var runs []run
	room := w - rightWidth()
	x := 0
	for _, text := range left {
		if room <= 0 {
			break
		}
		tw := ansi.StringWidth(text)
		if tw > room {
			runs = append(runs, newRun(x, truncateCells(text, room), hitNone, 0))
			break
		}
		runs = append(runs, newRun(x, text, hitNone, 0))
		x += tw
		room -= tw
	}

	x = w - rightWidth()
	for i, seg := range right {
		runs = append(runs, newRun(x, seg.text, seg.kind, i))
		x += ansi.StringWidth(seg.text)
	}
	return runs
}

// positionWidth is the width of the cursor position item. The message column
// moves one cell per digit of the line count, so the item widens with it. One
// digit gives 13.
func positionWidth(lines int) int {
	return 12 + len(strconv.Itoa(max(lines, 0)))
}

// padCells pads s with blanks on the right to n cells. Longer text is kept.
func padCells(s string, n int) string {
	if pad := n - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// truncateCells shortens s to at most w cells, ending with "…" when it is cut.
func truncateCells(s string, w int) string {
	if ansi.StringWidth(s) <= w {
		return s
	}
	// The cut keeps one cell for the ellipsis.
	return ansi.Truncate(s, max(w-1, 0), "") + "…"
}
