package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/editorpane"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/space"
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
	paletteHelp    = "Palette: Ctrl+P"
	warnBlankWidth = 7 // " [⚠ N] " for one digit, reserved while the counter is unknown

	// A status message too long for the status row wraps onto up to maxNoticeRows rows
	// right above it, the notice band. The band is painted over the bottom rows of the
	// body, so it never moves the text, and it goes when the next key or click expires
	// the message. It leaves the top body row alone and is not drawn in a frame
	// narrower than noticeMinWidth.
	maxNoticeRows  = 3
	noticeMinWidth = 20
	noticeMinText  = 8 // the least text a band row keeps when an explorer takes the left
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
	doorKeys         []string // the welcome doors' shortcuts in the active keymap; nil for the defaults
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

	lsp      string // the language-server pill ("LSP", "LSP (off)"), or "" for a language with no server
	warnSlot bool   // reserve the blank warning-counter slot
	warnings int    // warning count; shown as " [⚠ N] " when above zero
	readOnly bool   // the document is read-only; shown as " [RO] "

	extra []statusItem // the items of plugins (plugin_status.go)

	paletteHint string // the palette hint of the right group; empty for paletteHelp
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

	// The notice band: a status message that does not fit the status row, wrapped onto
	// the rows right above it and painted over the body (see setNotice). notice is empty
	// when the message fits; noticeY is the first row of the band.
	noticeX int // the band's first column, the editor's left edge
	noticeY int
	notice  []string

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
	if l.statusOn {
		l.setNotice(in.status, in.explorerWidth(w))
	}
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
		l.welcome = welcomeLines(l.editorX, max(w-l.editorX, 0), l.bodyTop, bodyRows, in.recents, in.doorKeys)
	}
	if l.statusOn {
		st := in.status
		if len(l.notice) > 0 {
			// The band shows the message, so the status row does not repeat it.
			st.message = ""
		}
		l.status = statusRuns(w, st)
	}
	return l
}

// setNotice wraps a status message that does not fit the status row onto the rows
// above it. The rows are the body's, drawn over, and the first body row stays. The band
// starts at column x0, the editor's left edge, so an open explorer keeps its border.
func (l *layout) setNotice(s statusView, x0 int) {
	room := min(l.statusY-l.bodyTop-1, maxNoticeRows)
	if room <= 0 {
		return
	}
	if rows := statusNotice(l.w, x0, s, room); len(rows) > 0 {
		l.notice = rows
		l.noticeX = x0
		l.noticeY = l.statusY - len(rows)
	}
}

// explorerWidth is the columns the explorer panel takes at the left of a w cells wide
// frame, or zero when it is closed.
func (in layoutInput) explorerWidth(w int) int {
	if !in.explorerOpen {
		return 0
	}
	return explorerCols(w, in.explorerOverride)
}

// statusNotice is the message of s wrapped to the rows of the notice band, at most
// maxRows of them, or nil when the status row shows the whole message. The band runs
// from column x0 to the right edge. The full text is in Show Warnings.
func statusNotice(w, x0 int, s statusView, maxRows int) []string {
	if s.message == "" || w < noticeMinWidth || w-x0-2*space.PadX < noticeMinText {
		return nil
	}
	if _, cut := statusRunsCut(w, s); !cut {
		return nil
	}
	return noticeRows(s.message, w-x0-2*space.PadX, maxRows)
}

// noticeRows wraps text onto at most maxRows rows of width cells. A text that needs more
// rows loses its middle first (space.Middle), so its start and its end both stay: the end
// of a message is usually the reason. The budget shrinks until the wrapped rows fit.
func noticeRows(text string, width, maxRows int) []string {
	budget := width * maxRows
	for {
		rows := wrapText(space.Middle(text, budget), width)
		if len(rows) <= maxRows || budget <= width {
			return rows
		}
		budget -= max(width/4, 1)
	}
}

// wrapText breaks s into rows of at most width cells, at its spaces. A word longer than
// the room left on a row fills that row first and goes on in the next, so a long path does
// not leave the row before it nearly empty.
func wrapText(s string, width int) []string {
	var rows []string
	if width <= 0 {
		return nil
	}
	cur := ""
	for _, word := range strings.Fields(s) {
		for word != "" {
			sep := ""
			if cur != "" {
				sep = " "
			}
			room := width - ansi.StringWidth(cur+sep)
			switch ww := ansi.StringWidth(word); {
			case ww <= room:
				cur += sep + word
				word = ""
			case ww <= width:
				// The word fits a row of its own: start the next row with it.
				if cur != "" {
					rows = append(rows, cur)
				}
				cur = ""
			case room > 0:
				// Longer than any row: the room left takes its start.
				head := ansi.Truncate(word, room, "")
				cur += sep + head
				word = ansi.TruncateLeft(word, ansi.StringWidth(head), "")
				rows = append(rows, cur)
				cur = ""
			default:
				rows = append(rows, cur)
				cur = ""
			}
		}
	}
	if cur != "" {
		rows = append(rows, cur)
	}
	return rows
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

// statusItemKeep is the most cells of plugin items on the left that the right group of the
// status row gives way for.
const statusItemKeep = 36

// statusRuns lays out the status row (section 4.5). The left group is cut first
// to the room the right group leaves. The right group is dropped from its end
// until the left group keeps at least min(floor(2W/5), 40) cells.
func statusRuns(w int, s statusView) []run {
	runs, _ := statusRunsCut(w, s)
	return runs
}

// statusRunsCut is statusRuns, and it also reports whether the status message was cut
// or dropped to fit the row.
func statusRunsCut(w int, s statusView) ([]run, bool) {
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
			return segment{space.Pad(it.text), hitStatusPlugin, i}
		}
		return segment{space.Pad(it.text), hitNone, 0}
	}
	left := []segment{
		{space.Pad("Local"), hitNone, 0},
		{space.Pad(padCells(s.position, positionWidth(s.lines))), hitStatusSegment, statusSegmentPosition},
	}
	for i, it := range s.extra {
		if !it.right {
			left = append(left, pluginSeg(i))
		}
	}
	left = append(left, segment{space.Pad(s.message), hitNone, 0})
	var right []segment
	if s.large != "" {
		right = append(right, segment{space.Pad(s.large), hitNone, 0})
	}
	if s.readOnly {
		right = append(right, segment{space.Pad("[RO]"), hitNone, 0})
	}
	right = append(right,
		segment{space.Pad(s.eol), hitStatusSegment, statusSegmentEOL},
		segment{space.Pad(s.encoding), hitStatusSegment, statusSegmentEncoding},
		segment{space.Pad(s.language), hitStatusSegment, statusSegmentLanguage},
	)
	if s.lsp != "" {
		right = append(right, segment{space.Pad(s.lsp), hitStatusSegment, statusSegmentLSP})
	}
	for i, it := range s.extra {
		if it.right {
			right = append(right, pluginSeg(i))
		}
	}
	switch {
	case s.warnings > 0:
		right = append(right, segment{space.Pad("[⚠ " + strconv.Itoa(s.warnings) + "]"), hitNone, 0})
	case s.warnSlot:
		// The blank keeps the columns the counter takes once it is known.
		right = append(right, segment{strings.Repeat(" ", warnBlankWidth), hitNone, 0})
	}
	hint := paletteHelp
	if s.paletteHint != "" {
		hint = s.paletteHint
	}
	right = append(right, segment{space.Pad(hint), hitStatusPalette, 0})

	// The label of a modal plugin is the only sign of its state, so it comes before the right
	// group (the palette hint, then the language and the rest go first) and before " Local ".
	itemW := 0
	for i, it := range s.extra {
		if !it.right {
			itemW += ansi.StringWidth(pluginSeg(i).text)
		}
	}
	itemW = min(itemW, statusItemKeep)
	posW := ansi.StringWidth(left[1].text)
	msgW := min(ansi.StringWidth(left[len(left)-1].text), statusMessageMin)
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
	full := right
	for pass := 0; ; pass++ {
		minLeft := min(2*w/5, 40)
		if itemW > 0 {
			base := ansi.StringWidth(left[0].text) + posW + itemW + msgW
			minLeft = max(minLeft, base)
		}
		budget := w - (minLeft + 1)
		right = full
		for rightWidth() > budget && len(right) > keep {
			right = right[:len(right)-1]
		}
		if itemW == 0 || pass > 0 || w-rightWidth() >= ansi.StringWidth(left[0].text)+posW+itemW+msgW {
			break
		}
		left[0].text = "" // still no room for the label: " Local " goes
	}

	var runs []run
	cut := s.message != ""
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
		if text == "" {
			continue
		}
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
		if i == len(left)-1 {
			cut = strings.Contains(s.message, "\n")
		}
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
	return runs, cut
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
