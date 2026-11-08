// Package frame is the geometry of the gvim frame and its hit map: the layout of
// the menu bar, the tab Strip, the explorer, the editor panes, the terminal dock
// and the status row, computed from plain data, and the table that maps a frame
// cell to the target a press reaches. Painting and hit testing read the same
// layout, so a label and its click target cannot drift apart.
//
// The package is a leaf: it imports the grid-independent primitives (icons,
// editorpane, explorer, space) and the split kit, never the root model.
package frame

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/editorpane"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/split"
)

// Geometry from the responsive spec (blueprint section 4). The painter and the
// hit map both read the Layout computed here. No other file holds a column or
// row constant for these rows.
//
// The overlays (palette, find bar, prompts, dropdown) add their rows in the
// overlay phase. Their rules are not here yet.
const (
	GateMinWidth  = 8 // section 4.8: below this the frame shows only the gate message
	GateMinHeight = 3
	GateText      = "gvim: terminal too small, need 8x3"

	explorerMinWidth     = 16 // spec section 3: the drag minimum
	explorerMaxWidth     = 60 // spec section 3: the drag maximum
	explorerDefaultWidth = 32 // spec section 3: the automatic width
	explorerEditorMin    = 20 // the editor keeps this many columns when the frame allows

	// MenuRows is the height of the menu band: the row of the labels and one
	// padding row of the menu background under them, so the labels do not sit
	// against the tab strip. The tab strip is the row under the band.
	MenuRows = 2

	LabelGap       = 3 // blank cells between menu labels
	NoNameLabel    = "[No Name]"
	PaletteHelp    = "Palette: Ctrl+P"
	warnBlankWidth = 7 // " [⚠ N] " for one digit, reserved while the counter is unknown

	// A status message too long for the status row wraps onto up to MaxNoticeRows rows
	// right above it, the notice band. The band is painted over the bottom rows of the
	// body, so it never moves the text, and it goes when the next key or click expires
	// the message. It leaves the top body row alone and is not drawn in a frame
	// narrower than noticeMinWidth.
	MaxNoticeRows  = 3
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

// Input is the state the geometry depends on. It is plain data, so
// ComputeLayout is a pure function of the terminal size and this value.
type Input struct {
	ExplorerOpen     bool
	ExplorerFocus    bool // "Explorer" joins the menu bar while focus is there
	ExplorerOverride int  // width set by a grip drag; 0 means automatic
	ExplorerRows     int  // visible tree rows, used by hit testing
	FindRows         int  // rows the find bar takes at the bottom; zero when it is closed
	Lines            int  // line count of the document, which sizes the gutter
	Tabs             []TabInfo
	TabFirst         int  // the tab Strip's scroll position, a tab index
	Welcome          bool // no tab is open, so the welcome screen replaces the editor
	Recents          []string
	DoorKeys         []string // the welcome doors' shortcuts in the active keymap; nil for the defaults
	Status           StatusView
	HideMenuBar      bool       // the menu band is not drawn; the tab Strip takes row 0
	HideStatus       bool       // a suggestion list or the file browser is open
	HideLineNumbers  bool       // the gutter is not drawn
	DockOpen         bool       // the terminal dock is shown (internal/tui/dock); it takes rows under the body
	HideTabBar       bool       // the tab Strip row is not drawn; the body takes it
	ShowPromptLine   bool       // reserve the prompt row under the status row (only while a prompt is open)
	HideVScroll      bool       // the vertical scrollbar is not drawn, so its column is text
	IconMode         icons.Mode // the glyphs of the explorer buttons and the tab buttons

	// The Split tree and the state of each leaf in reading order (Split.go). A nil
	// tree is one pane that shows lines. maximized gives the focused leaf the whole
	// body, and the other leaves keep their index with no area.
	Split     *split.Node
	Leaves    []split.LeafInfo
	Maximized bool
}

// StatusView is the content of the status row. The status bar sub-model fills
// the segments that are blank here.
type StatusView struct {
	Position string // for example "Ln 1, Col 1"
	Message  string
	EOL      string // "LF" or "CRLF"
	Encoding string // "ASCII" or "UTF-8"
	Language string // "Text"
	Lines    int    // line count; its digits set the message column
	Large    string // the chip of a document in large file mode, with the index progress, or ""

	LSP      string // the language-server pill ("LSP", "LSP (off)"), or "" for a language with no server
	WarnSlot bool   // reserve the blank warning-counter slot
	Warnings int    // warning count; shown as " [⚠ N] " when above zero
	ReadOnly bool   // the document is read-only; shown as " [RO] "

	Extra []StatusItem // the items of plugins (plugin_status.go)

	PaletteHint string // the palette hint of the right group; empty for PaletteHelp
}

// Run is a piece of text at a fixed column, together with what a click on it
// reaches. The painter draws runs and the hit test reads them, so a label and
// its click target cannot drift apart.
type Run struct {
	X, W  int // first column and width in cells
	Text  string
	Kind  HitKind
	Index int
}

// Covers reports whether column x falls inside the Run.
func (r Run) Covers(x int) bool { return x >= r.X && x < r.X+r.W }

// newRun places text at column x with the given hit target.
func newRun(x int, text string, kind HitKind, index int) Run {
	return Run{X: x, W: ansi.StringWidth(text), Text: text, Kind: kind, Index: index}
}

// Layout is the geometry of one frame in terminal cells. Rows and columns are
// numbered from zero at the top left. Fields named "bottom", "statusY" and "*Y"
// are inclusive rows. Fields named "*End" and "*W" are exclusive or sizes, as
// their comments say.
type Layout struct {
	W, H  int
	Gated bool // the minimum-size gate replaces the whole frame

	// Row bands (section 4.1). The editor body is BodyTop..BodyBottom. The menu
	// band is MenuTop..MenuBottom when the menu bar is shown, and both are -1 when
	// it is hidden; MenuLabelY is the row of the labels. The tab Strip is stripY:
	// row 2 under the menu band, row 0 without it, and -1 when the tab bar is
	// hidden. statusY is the status row, drawn only when statusOn. Without it the
	// body takes that row.
	BodyTop, BodyBottom int
	MenuOn              bool
	MenuTop, MenuBottom int
	MenuLabelY          int
	StripY              int
	StatusY             int
	StatusOn            bool
	Scrollbar           bool // content overflows, so the last column is the scrollbar

	// The notice band: a status message that does not fit the status row, wrapped onto
	// the rows right above it and painted over the body (see setNotice). notice is empty
	// when the message fits; noticeY is the first row of the band.
	NoticeX int // the band's first column, the editor's left edge
	NoticeY int
	Notice  []string

	// The prompt row sits under the status row, above the find bar. It is reserved
	// only while PromptOn is set, which the root does while a prompt overlay is
	// open; with no prompt open the status row is the last row of the frame.
	PromptOn bool
	PromptY  int

	// The terminal dock (internal/tui/dock) sits under the body, over the full width. Its rows
	// dockY..dockY+dockH-1 are the title row and then the shell's rows. The body ends
	// above it, so bodyBottom is already past the dock. DockOn is false while the dock
	// is hidden or does not fit.
	DockOn bool
	DockY  int
	DockH  int

	// Explorer (sections 4.2 and 4.3). The panel covers columns 0..ex-1 and rows
	// ExTitleY..exBottom: the title row, which is the tab Strip row when the menu
	// bar is shown, and then the tree body from ExBodyTop down. ex is zero when
	// the panel is hidden.
	ExplorerOpen bool
	Ex           int
	ExTitleY     int // the title row, aligned with the tab Strip
	ExBodyTop    int // the first row of the tree body
	ExBottom     int // bottom border row, the last body row
	ExRows       int
	ExReloadX    int // column of the refresh button, -1 when the button is not drawn
	ExCloseX     int // column of the close button, -1 when the button is not drawn

	// Editor area (section 4.2). The panes fill EditorX..w-1 and bodyTop..bodyBottom.
	// The gutter, textX, textW and scrollbar are those of the focused pane, which for
	// one pane is the whole area: the gutter sits at EditorX and the text at textX.
	EditorX int
	Gutter  int
	TextX   int
	TextW   int

	// The menu labels (section 4.5, row MenuLabelY, empty when the bar is hidden) and
	// the tab Strip (section 4.6, row stripY). The Strip is placed by placeStrip, which
	// also gives the scroll position it used.
	MenuRuns []Run
	Strip    Strip

	// The welcome screen (no tab open) replaces the editor pane. welcome holds its
	// placed lines, and WelcomeOn is true while it shows.
	WelcomeOn bool
	Welcome   []WelcomeLine

	// Status bar (section 4.5). Runs cover the whole row.
	Status []Run

	// Editor Panes (split.go). Panes holds them in reading order, and it is one pane
	// when nothing is split. focus is the index of the focused pane. borders holds the
	// lines between the Panes.
	Panes   []split.Pane
	Focus   int
	Borders []split.Border
}

// ComputeLayout returns the geometry for a w by h terminal. It is pure: the
// same inputs always give the same Layout, and painting and hit testing both
// use it.
func ComputeLayout(w, h int, in Input) Layout {
	l := Layout{W: w, H: h}
	if w < GateMinWidth || h < GateMinHeight {
		l.Gated = true
		return l
	}

	// The menu band is the top MenuRows rows when the bar is shown, and the tab
	// Strip is the row under it: row 2 with the band, row 0 without it. The find
	// bar takes rows under the status row, so the body and the status row move up
	// by its height.
	l.MenuOn = !in.HideMenuBar
	l.MenuTop, l.MenuBottom, l.MenuLabelY = -1, -1, -1
	menuRows := 0
	if l.MenuOn {
		l.MenuTop, l.MenuBottom = 0, MenuRows-1
		l.MenuLabelY = l.MenuTop
		menuRows = MenuRows
	}
	// A hidden tab Strip gives its row to the body. stripY is -1 then, so no
	// hit or paint reads the Strip row.
	l.StripY = menuRows
	l.BodyTop = menuRows + 1
	if in.HideTabBar {
		l.StripY = -1
		l.BodyTop = menuRows
	}
	// The explorer's title row is the tab Strip row. With the menu bar hidden the
	// panel keeps the rows it had before the band existed: the title on row 1 and
	// the tree body on row 2.
	l.ExTitleY = max(menuRows, 1)
	l.ExBodyTop = l.ExTitleY + 1
	l.StatusY = h - 1 - in.FindRows
	if in.ShowPromptLine {
		l.PromptOn, l.PromptY = true, l.StatusY
		l.StatusY--
	}
	l.StatusOn = !in.HideStatus
	if l.StatusOn {
		l.setNotice(in.Status, in.explorerWidth(w))
	}
	l.BodyBottom = l.StatusY - 1
	if !l.StatusOn {
		l.BodyBottom = l.StatusY
	}
	if in.DockOpen {
		l.setDock()
	}
	bodyRows := l.BodyBottom - l.BodyTop + 1

	l.setExplorer(in)
	l.EditorX = l.Ex
	l.placePanes(in)
	if in.HideVScroll {
		l.hideScrollbars()
	}

	if l.MenuOn {
		l.MenuRuns = MenuRuns(in.ExplorerFocus)
	}
	l.Strip = PlaceStrip(l.EditorX, w, in.Tabs, in.TabFirst, in.IconMode)
	if in.Welcome {
		l.WelcomeOn = true
		l.Welcome = WelcomeLines(l.EditorX, max(w-l.EditorX, 0), l.BodyTop, bodyRows, in.Recents, in.DoorKeys)
	}
	if l.StatusOn {
		st := in.Status
		if len(l.Notice) > 0 {
			// The band shows the message, so the status row does not repeat it.
			st.Message = ""
		}
		l.Status = StatusRuns(w, st)
	}
	return l
}

// setNotice wraps a status message that does not fit the status row onto the rows
// above it. The rows are the body's, drawn over, and the first body row stays. The band
// starts at column x0, the editor's left edge, so an open explorer keeps its border.
func (l *Layout) setNotice(s StatusView, x0 int) {
	room := min(l.StatusY-l.BodyTop-1, MaxNoticeRows)
	if room <= 0 {
		return
	}
	if rows := statusNotice(l.W, x0, s, room); len(rows) > 0 {
		l.Notice = rows
		l.NoticeX = x0
		l.NoticeY = l.StatusY - len(rows)
	}
}

// explorerWidth is the columns the explorer panel takes at the left of a w cells wide
// frame, or zero when it is closed.
func (in Input) explorerWidth(w int) int {
	if !in.ExplorerOpen {
		return 0
	}
	return ExplorerCols(w, in.ExplorerOverride)
}

// statusNotice is the message of s wrapped to the rows of the notice band, at most
// maxRows of them, or nil when the status row shows the whole message. The band runs
// from column x0 to the right edge. The full text is in Show Warnings.
func statusNotice(w, x0 int, s StatusView, maxRows int) []string {
	if s.Message == "" || w < noticeMinWidth || w-x0-2*space.PadX < noticeMinText {
		return nil
	}
	if _, cut := statusRunsCut(w, s); !cut {
		return nil
	}
	return noticeRows(s.Message, w-x0-2*space.PadX, maxRows)
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
func (l *Layout) hideScrollbars() {
	for i := range l.Panes {
		l.Panes[i].Scrollbar = false
	}
	l.Scrollbar = false
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
func (l *Layout) setDock() {
	h := dockHeight(l.BodyBottom - l.BodyTop + 1)
	if h == 0 {
		return
	}
	l.DockOn = true
	l.DockH = h
	l.DockY = l.BodyBottom - h + 1
	l.BodyBottom -= h
}

// setExplorer fills the explorer fields. The panel spans ExTitleY..bodyBottom
// (section 4.1).
func (l *Layout) setExplorer(in Input) {
	l.ExReloadX, l.ExCloseX = -1, -1
	if !in.ExplorerOpen {
		return
	}
	l.ExplorerOpen = true
	l.Ex = ExplorerCols(l.W, in.ExplorerOverride)
	l.ExBottom = l.BodyBottom
	l.ExRows = in.ExplorerRows
	header := expanel.HeaderFor(l.Ex, in.IconMode)
	l.ExReloadX, l.ExCloseX = header.Reload, header.Close
}

// GutterWidth is the width of the line-number gutter. It is the pane's gutter,
// so the columns the Layout reserves are the columns the pane paints.
func GutterWidth(lines int) int {
	return editorpane.GutterWidth(lines)
}

// ExplorerCols is the explorer width E (spec section 3). The automatic width is
// 32 columns, and a grip drag sets the override. Both are clamped to [16, 60],
// and the upper bound drops to W-20 on a narrow frame so the editor keeps room.
// The width never exceeds the frame.
func ExplorerCols(w, override int) int {
	e := explorerDefaultWidth
	if override != 0 {
		e = override
	}
	hi := min(explorerMaxWidth, max(w-explorerEditorMin, explorerMinWidth))
	return min(max(e, explorerMinWidth), hi, w)
}

// MenuRuns places the menu bar labels (section 4.5). A label is shown with
// three blanks between labels.
func MenuRuns(explorerFocus bool) []Run {
	var runs []Run
	x := 1
	for i, label := range barLabels(explorerFocus) {
		runs = append(runs, newRun(x, label, HitMenuLabel, i))
		x += ansi.StringWidth(label) + LabelGap
	}
	return runs
}

// statusMessageMin is the room, in cells, that plugin items on the left leave to the
// status message at least.
const statusMessageMin = 24

// statusItemKeep is the most cells of plugin items on the left that the right group of the
// status row gives way for.
const statusItemKeep = 36

// StatusRuns lays out the status row (section 4.5). The left group is cut first
// to the room the right group leaves. The right group is dropped from its end
// until the left group keeps at least min(floor(2W/5), 40) cells.
func StatusRuns(w int, s StatusView) []Run {
	runs, _ := statusRunsCut(w, s)
	return runs
}

// statusRunsCut is StatusRuns, and it also reports whether the status message was cut
// or dropped to fit the row.
func statusRunsCut(w int, s StatusView) ([]Run, bool) {
	type segment struct {
		text string
		kind HitKind
		id   int // the Run index of a hitStatusSegment (one of the statusSegment constants) or of a hitStatusPlugin
	}
	// A plugin item is clickable when it has an on_click function; its Run index is its
	// place in s.extra.
	pluginSeg := func(i int) segment {
		it := s.Extra[i]
		if it.Clickable {
			return segment{space.Pad(it.Text), HitStatusPlugin, i}
		}
		return segment{space.Pad(it.Text), HitNone, 0}
	}
	left := []segment{
		{space.Pad("Local"), HitNone, 0},
		{space.Pad(space.PadTo(s.Position, PositionWidth(s.Lines))), HitStatusSegment, StatusSegmentPosition},
	}
	for i, it := range s.Extra {
		if !it.Right {
			left = append(left, pluginSeg(i))
		}
	}
	left = append(left, segment{space.Pad(s.Message), HitNone, 0})
	var right []segment
	if s.Large != "" {
		right = append(right, segment{space.Pad(s.Large), HitNone, 0})
	}
	if s.ReadOnly {
		right = append(right, segment{space.Pad("[RO]"), HitNone, 0})
	}
	right = append(right,
		segment{space.Pad(s.EOL), HitStatusSegment, StatusSegmentEOL},
		segment{space.Pad(s.Encoding), HitStatusSegment, StatusSegmentEncoding},
		segment{space.Pad(s.Language), HitStatusSegment, StatusSegmentLanguage},
	)
	if s.LSP != "" {
		right = append(right, segment{space.Pad(s.LSP), HitStatusSegment, StatusSegmentLSP})
	}
	for i, it := range s.Extra {
		if it.Right {
			right = append(right, pluginSeg(i))
		}
	}
	switch {
	case s.Warnings > 0:
		right = append(right, segment{space.Pad("[⚠ " + strconv.Itoa(s.Warnings) + "]"), HitNone, 0})
	case s.WarnSlot:
		// The blank keeps the columns the counter takes once it is known.
		right = append(right, segment{strings.Repeat(" ", warnBlankWidth), HitNone, 0})
	}
	hint := PaletteHelp
	if s.PaletteHint != "" {
		hint = s.PaletteHint
	}
	right = append(right, segment{space.Pad(hint), HitStatusPalette, 0})

	// The label of a modal plugin is the only sign of its state, so it comes before the right
	// group (the palette hint, then the language and the rest go first) and before " Local ".
	itemW := 0
	for i, it := range s.Extra {
		if !it.Right {
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

	var runs []Run
	cut := s.Message != ""
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
			text = space.End(text, itemRoom)
			itemRoom -= ansi.StringWidth(text)
		}
		tw := ansi.StringWidth(text)
		if tw > room {
			runs = append(runs, newRun(x, space.End(text, room), seg.kind, seg.id))
			break
		}
		runs = append(runs, newRun(x, text, seg.kind, seg.id))
		if i == len(left)-1 {
			cut = strings.Contains(s.Message, "\n")
		}
		x += tw
		room -= tw
	}

	x = w - rightWidth()
	for i, seg := range right {
		index := i
		if seg.kind == HitStatusSegment || seg.kind == HitStatusPlugin {
			index = seg.id
		}
		runs = append(runs, newRun(x, seg.text, seg.kind, index))
		x += ansi.StringWidth(seg.text)
	}
	return runs, cut
}

// PositionWidth is the width of the cursor position item. The message column
// moves one cell per digit of the line count, so the item widens with it. One
// digit gives 13.
func PositionWidth(lines int) int {
	return 12 + len(strconv.Itoa(max(lines, 0)))
}
