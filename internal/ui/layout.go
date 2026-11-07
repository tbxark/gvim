package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/editorpane"
	expanel "github.com/tbxark/gvim/internal/ui/explorer"
	"github.com/tbxark/gvim/internal/ui/icons"
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

	// The explorer panel's own rows. They do not move when the menu bar is hidden:
	// the title is row 1 and the tree body starts at row 2 (explorer.Panel).
	explorerTitleRow = 1
	explorerBodyTop  = 2

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
	hideMenuBar      bool       // the menu row is not drawn; the tab strip takes row 0
	hideStatus       bool       // a suggestion list or the file browser is open
	hideLineNumbers  bool       // the gutter is not drawn
	dockOpen         bool       // the terminal dock is shown (dock.go); it takes rows under the body
	hideTabBar       bool       // the tab strip row is not drawn; the body takes it
	showPromptLine   bool       // the prompt row is drawn under the status row
	hideVScroll      bool       // the vertical scrollbar is not drawn, so its column is text
	iconMode         icons.Mode // the glyphs of the explorer buttons and the tab buttons

	// The split tree and the state of each leaf in reading order (split.go). A nil
	// tree is one pane that shows lines. maximized gives the focused leaf the whole
	// body, and the other leaves keep their index with no area.
	split     *splitNode
	leaves    []leafInfo
	maximized bool
}

// statusView is the content of the status row. The status bar sub-model fills
// the segments that are blank here.
type statusView struct {
	position string // for example "Ln 1, Col 1"
	message  string
	eol      string // "LF" or "CRLF"
	encoding string // "ASCII" or "UTF-8"
	language string // "Text"
	lines    int    // line count; its digits set the message column
	large    string // the chip of a document in large file mode, with the index progress, or ""

	warnSlot bool // reserve the blank warning-counter slot
	warnings int  // warning count; shown as " [⚠ N] " when above zero
	readOnly bool // the document is read-only; shown as " [RO] "

	extra []statusItem // the items of plugins (plugin_status.go)
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
	// the menu bar when menuOn, and the tab strip is stripY: row 1 with the menu
	// bar, row 0 without it, and -1 when the tab bar is hidden. statusY is the
	// status row, drawn only when statusOn. Without it the body takes that row.
	bodyTop, bodyBottom int
	menuOn              bool
	stripY              int
	statusY             int
	statusOn            bool
	scrollbar           bool // content overflows, so the last column is the scrollbar

	// The prompt row sits under the status row, above the find bar. It is drawn only
	// when promptOn is set, and it is empty except for the prompts that use it.
	promptOn bool
	promptY  int

	// The terminal dock (dock.go) sits under the body, over the full width. Its rows
	// dockY..dockY+dockH-1 are the title row and then the shell's rows. The body ends
	// above it, so bodyBottom is already past the dock. dockOn is false while the dock
	// is hidden or does not fit.
	dockOn bool
	dockY  int
	dockH  int

	// Explorer (sections 4.2 and 4.3). The panel covers columns 0..ex-1 and rows
	// 1..exBottom. ex is zero when the panel is hidden.
	explorerOpen bool
	ex           int
	exBottom     int // bottom border row, the last body row
	exRows       int
	exReloadX    int // column of the refresh button, -1 when the button is not drawn
	exCloseX     int // column of the close button, -1 when the button is not drawn

	// Editor area (section 4.2). The panes fill editorX..w-1 and bodyTop..bodyBottom.
	// The gutter, textX, textW and scrollbar are those of the focused pane, which for
	// one pane is the whole area: the gutter sits at editorX and the text at textX.
	editorX int
	gutter  int
	textX   int
	textW   int

	// The menu labels (section 4.5, row 0, empty when the bar is hidden) and the tab
	// strip (section 4.6, row stripY). The strip is placed by placeStrip, which also
	// gives the scroll position it used.
	menuRuns []run
	strip    strip

	// The welcome screen (no tab open) replaces the editor pane. welcome holds its
	// placed lines, and welcomeOn is true while it shows.
	welcomeOn bool
	welcome   []welcomeLine

	// Status bar (section 4.5). Runs cover the whole row.
	status []run

	// Editor panes (split.go). panes holds them in reading order, and it is one pane
	// when nothing is split. focus is the index of the focused pane. borders holds the
	// lines between the panes.
	panes   []splitPane
	focus   int
	borders []splitBorder
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

	// The menu row is row 0 when it is shown, and the tab strip is the row under it.
	// The find bar takes rows under the status row, so the body and the status row
	// move up by its height.
	l.menuOn = !in.hideMenuBar
	stripRow := 0
	if l.menuOn {
		stripRow = 1
	}
	// A hidden tab strip gives its row to the body. stripY is -1 then, so no
	// hit or paint reads the strip row.
	l.stripY = stripRow
	l.bodyTop = stripRow + 1
	if in.hideTabBar {
		l.stripY = -1
		l.bodyTop = stripRow
	}
	l.statusY = h - 1 - in.findRows
	if in.showPromptLine {
		l.promptOn, l.promptY = true, l.statusY
		l.statusY--
	}
	l.statusOn = !in.hideStatus
	l.bodyBottom = l.statusY - 1
	if !l.statusOn {
		l.bodyBottom = l.statusY
	}
	if in.dockOpen {
		l.setDock()
	}
	bodyRows := l.bodyBottom - l.bodyTop + 1

	l.setExplorer(in)
	l.editorX = l.ex
	l.placePanes(in)
	if in.hideVScroll {
		l.hideScrollbars()
	}

	if l.menuOn {
		l.menuRuns = menuRuns(in.explorerFocus)
	}
	l.strip = placeStrip(l.editorX, w, in.tabs, in.tabFirst, in.iconMode)
	if in.welcome {
		l.welcomeOn = true
		l.welcome = welcomeLines(l.editorX, max(w-l.editorX, 0), l.bodyTop, bodyRows, in.recents)
	}
	if l.statusOn {
		l.status = statusRuns(w, in.status)
	}
	return l
}

// hideScrollbars clears the scrollbar of every pane. The column stays in the pane, so
// the text keeps its width.
func (l *layout) hideScrollbars() {
	for i := range l.panes {
		l.panes[i].scrollbar = false
	}
	l.scrollbar = false
}

// The terminal dock's rows. The dock is a title row and at least three rows of shell,
// and the editor keeps at least dockMinBody rows beside it.
const (
	dockMinRows = 4
	dockMinBody = 3
)

// dockHeight is the rows the dock takes from the bottom of a body of bodyRows rows: a
// third of the body and at least dockMinRows. It is zero when the body left over
// would have fewer than dockMinBody rows.
func dockHeight(bodyRows int) int {
	h := max(bodyRows/3, dockMinRows)
	if bodyRows-h < dockMinBody {
		return 0
	}
	return h
}

// setDock takes the dock's rows from the bottom of the body. It does nothing when the
// dock does not fit. The rows come off bodyBottom before the explorer and the
// welcome screen are placed, so they shrink to the rows left.
func (l *layout) setDock() {
	h := dockHeight(l.bodyBottom - l.bodyTop + 1)
	if h == 0 {
		return
	}
	l.dockOn = true
	l.dockH = h
	l.dockY = l.bodyBottom - h + 1
	l.bodyBottom -= h
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
	header := expanel.HeaderFor(l.ex, in.iconMode)
	l.exReloadX, l.exCloseX = header.Reload, header.Close
}

// gutterWidth is the width of the line-number gutter. It is the pane's gutter,
// so the columns the layout reserves are the columns the pane paints.
func gutterWidth(lines int) int {
	return editorpane.GutterWidth(lines)
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

// statusMessageMin is the room, in cells, that plugin items on the left leave to the
// status message at least.
const statusMessageMin = 24

// statusRuns lays out the status row (section 4.5). The left group is cut first
// to the room the right group leaves. The right group is dropped from its end
// until the left group keeps at least min(floor(2W/5), 40) cells.
func statusRuns(w int, s statusView) []run {
	type segment struct {
		text string
		kind hitKind
		id   int // the run index of a hitStatusSegment (one of the statusSegment constants) or of a hitStatusPlugin
	}
	// A plugin item is clickable when it has an on_click function; its run index is its
	// place in s.extra.
	pluginSeg := func(i int) segment {
		it := s.extra[i]
		if it.clickable {
			return segment{" " + it.text + " ", hitStatusPlugin, i}
		}
		return segment{" " + it.text + " ", hitNone, 0}
	}
	left := []segment{
		{" Local ", hitNone, 0},
		{" " + padCells(s.position, positionWidth(s.lines)) + " ", hitNone, 0},
	}
	for i, it := range s.extra {
		if !it.right {
			left = append(left, pluginSeg(i))
		}
	}
	left = append(left, segment{" " + s.message + " ", hitNone, 0})
	var right []segment
	if s.large != "" {
		right = append(right, segment{" " + s.large + " ", hitNone, 0})
	}
	if s.readOnly {
		right = append(right, segment{" [RO] ", hitNone, 0})
	}
	right = append(right,
		segment{" " + s.eol + " ", hitStatusSegment, statusSegmentEOL},
		segment{" " + s.encoding + " ", hitStatusSegment, statusSegmentEncoding},
		segment{" " + s.language + " ", hitStatusSegment, statusSegmentLanguage},
	)
	for i, it := range s.extra {
		if it.right {
			right = append(right, pluginSeg(i))
		}
	}
	switch {
	case s.warnings > 0:
		right = append(right, segment{" [⚠ " + strconv.Itoa(s.warnings) + "] ", hitNone, 0})
	case s.warnSlot:
		// The blank keeps the columns the counter takes once it is known.
		right = append(right, segment{strings.Repeat(" ", warnBlankWidth), hitNone, 0})
	}
	right = append(right, segment{" " + paletteHelp + " ", hitStatusPalette, 0})

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
	// left is " Local ", the position, the plugin items, the message. The items share what
	// the others leave, with the first cells of the message kept for it: a plugin cannot
	// push a notice out of the row.
	itemRoom := room - ansi.StringWidth(left[0].text) - ansi.StringWidth(left[1].text) -
		min(ansi.StringWidth(left[len(left)-1].text), statusMessageMin)
	for i, seg := range left {
		if room <= 0 {
			break
		}
		text := seg.text
		if i >= 2 && i < len(left)-1 {
			if itemRoom <= 0 {
				continue
			}
			text = truncateCells(text, itemRoom)
			itemRoom -= ansi.StringWidth(text)
		}
		tw := ansi.StringWidth(text)
		if tw > room {
			runs = append(runs, newRun(x, truncateCells(text, room), seg.kind, seg.id))
			break
		}
		runs = append(runs, newRun(x, text, seg.kind, seg.id))
		x += tw
		room -= tw
	}

	x = w - rightWidth()
	for i, seg := range right {
		index := i
		if seg.kind == hitStatusSegment || seg.kind == hitStatusPlugin {
			index = seg.id
		}
		runs = append(runs, newRun(x, seg.text, seg.kind, index))
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
