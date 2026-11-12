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

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/sidebar"
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

	// MenuRows is the height of the menu band: 1 row of labels, so the bar
	// stays compact and directly abuts the tabs and the tree.
	MenuRows = 1

	LabelGap       = 3 // blank cells between menu labels
	NoNameLabel    = "[No Name]"
	PaletteHelp    = "Palette: Ctrl+P"
	diagBlankWidth = 7 // " [E:1] " for one severity and one digit, reserved while the counts are unknown

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
	ExplorerOpen    bool
	ExplorerFocus   bool           // "Explorer" joins the menu bar while focus is there
	ExplorerWidth   explorer.Width // file_explorer.width; the zero value is gvim's automatic width
	ExplorerSide    explorer.Side  // file_explorer.side: the frame edge the column sits on
	ExplorerRows    int            // visible tree rows, used by hit testing
	FindRows        int            // rows the find bar takes at the bottom; zero when it is closed
	Lines           int            // line count of the document, which sizes the gutter
	Tabs            []TabInfo
	TabFirst        int  // the tab Strip's scroll position, a tab index
	Welcome         bool // no tab is open, so the welcome screen replaces the editor
	Recents         []string
	DoorKeys        []string // the welcome doors' shortcuts in the active keymap; nil for the defaults
	Status          StatusView
	HideMenuBar     bool       // the menu band is not drawn; the tab Strip takes row 0
	HideStatus      bool       // a suggestion list or the file browser is open
	HideLineNumbers bool       // the gutter is not drawn
	DockOpen        bool       // the terminal dock is shown (internal/tui/dock); it takes rows under the body
	HideTabBar      bool       // the tab Strip row is not drawn; the body takes it
	Style           Style      // the frame's look; nil is Classic, the workbench frame
	ShowPromptLine  bool       // reserve the prompt row under the status row (only while a prompt is open)
	HideVScroll     bool       // the vertical scrollbar is not drawn, so its column is text
	IconMode        icons.Mode // the glyphs of the explorer buttons and the tab buttons

	// Sidebar is the section stack of the explorer column, top to bottom: the
	// explorer and the Contents section while it is open. Nil is a column of
	// the explorer alone, the column gvim always had. The explorer's requested
	// rows are 0, so it shares what the fixed sections leave.
	Sidebar []sidebar.Spec

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

	// Filename is the active document's Fresh display name: its path relative to
	// the workspace root when it is under it, its absolute path otherwise, and the
	// document's fallback name ("[No Name]", "Untitled-N") when it has no path.
	// FilenameModified adds Fresh's " [+]" and the ReadOnly field adds " [RO]".
	Filename         string
	FilenameModified bool
	// Chord is the pending multi-key sequence as the status row shows it, such as
	// "[Ctrl+X]", or "" when no sequence is waiting.
	Chord string
	// Clock is the local time as Fresh's {clock} shows it, 24-hour "HH:MM", or ""
	// when the row has no clock.
	Clock string

	Line, Col int // the caret, one-based; the {cursor:compact} element
	Cursors   int // carets in the active document; {cursor_count} shows it above one

	Trust           string     // the workspace-trust pill ("Trusted", "Restricted", "Blocked"), or "" for no trust port
	TrustWarn       bool       // paint the trust pill in the warning role (Restricted and Blocked)
	LSP             string     // the language-server pill ("LSP", "LSP (off)"), or "" for a language with no server
	DiagSlot        bool       // reserve the blank diagnostic-count slot
	Diags           DiagCounts // diagnostics of the active document; shown as " [E:1 W:2 I:3] "
	ReadOnly        bool       // the document is read-only; shown as " [RO] "
	TerminalRestart string     // the {terminal_restart} label while the dock's shell has exited (Fresh: "⟳ Restart claude (exit 1)"), or ""
	// Update is the release the check found, as the {update} element shows it
	// ("Update: v0.4.2"), or "" while no newer release is known.
	Update string
	// HexView is the binary-view switch: "Hex" while the summary is shown and
	// "Summary" while the hex dump is shown. Empty for text and image documents.
	HexView string

	Extra []StatusItem // the items of plugins (plugin_status.go)

	PaletteHint string // the palette hint; empty for PaletteHelp

	// Row is the configured status row: the element lists of the two sides and the
	// separator between neighbours. The zero value shows Fresh's default lists.
	Row StatusRow
}

// StatusElementKind names one element of a configured status row side, as Fresh's
// editor.status_bar.left/right lists name them. The kinds gvim parses but does not
// paint — {warnings} and plugin tokens — are kept, so a config that names
// them loads and renders nothing (see docs/ui.md).
type StatusElementKind int

// The status-row elements. The names and order are Fresh's StatusBarElement enum.
const (
	StatusElementFilename StatusElementKind = iota
	StatusElementReadOnly
	StatusElementCursor
	StatusElementCursorCompact
	StatusElementDiagnostics
	StatusElementCursorCount
	StatusElementMessages
	StatusElementChord
	StatusElementLineEnding
	StatusElementEncoding
	StatusElementLanguage
	StatusElementLSP
	StatusElementWarnings
	StatusElementUpdate
	StatusElementTerminalRestart
	StatusElementPalette
	StatusElementClock
	StatusElementRemote
	StatusElementTrust
	StatusElementCustom
)

// statusElementNames maps the configured name, without its braces, to its kind.
var statusElementNames = map[string]StatusElementKind{
	"filename":         StatusElementFilename,
	"read_only":        StatusElementReadOnly,
	"cursor":           StatusElementCursor,
	"cursor:compact":   StatusElementCursorCompact,
	"diagnostics":      StatusElementDiagnostics,
	"cursor_count":     StatusElementCursorCount,
	"messages":         StatusElementMessages,
	"chord":            StatusElementChord,
	"line_ending":      StatusElementLineEnding,
	"encoding":         StatusElementEncoding,
	"language":         StatusElementLanguage,
	"lsp":              StatusElementLSP,
	"warnings":         StatusElementWarnings,
	"update":           StatusElementUpdate,
	"terminal_restart": StatusElementTerminalRestart,
	"palette":          StatusElementPalette,
	"clock":            StatusElementClock,
	"remote":           StatusElementRemote,
	"trust":            StatusElementTrust,
}

// StatusElement is one entry of a configured status row side. Key is the
// "plugin:token" name of a custom element; it is empty for the built-in elements.
type StatusElement struct {
	Kind StatusElementKind
	Key  string
}

// StatusRow is the configured status row: the element lists of the two sides and the
// text drawn between two neighbours of one side, used as-is (Fresh's
// editor.status_bar.separator).
type StatusRow struct {
	Left, Right []StatusElement
	Separator   string
}

// ParseStatusElement parses one configured element name, with or without its braces
// ("{cursor}" or "cursor"). ok is false for a name gvim does not know. A name with a
// colon is a plugin token: gvim parses it but paints nothing for it, because its
// plugins have status items, not registered tokens (see docs/ui.md).
func ParseStatusElement(name string) (StatusElement, bool) {
	inner := strings.TrimSpace(name)
	if len(inner) >= 2 && strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}") {
		inner = inner[1 : len(inner)-1]
	}
	if kind, ok := statusElementNames[inner]; ok {
		return StatusElement{Kind: kind}, true
	}
	if strings.Contains(inner, ":") {
		return StatusElement{Kind: StatusElementCustom, Key: inner}, true
	}
	return StatusElement{}, false
}

// ParseStatusElements parses a configured side. A nil list parses to nil; a non-nil
// empty list parses to an empty side, so a config can remove a whole group. Names the
// config layer rejects are dropped here: reaching this function with one means a
// hand-built test or an older config.
func ParseStatusElements(names []string) []StatusElement {
	if names == nil {
		return nil
	}
	out := make([]StatusElement, 0, len(names))
	for _, name := range names {
		if e, ok := ParseStatusElement(name); ok {
			out = append(out, e)
		}
	}
	return out
}

// DefaultStatusLeft is Fresh's default left element list
// (fresh-editor-core config.rs default_status_bar_left).
func DefaultStatusLeft() []StatusElement {
	return []StatusElement{
		{Kind: StatusElementTrust},
		{Kind: StatusElementRemote},
		{Kind: StatusElementTerminalRestart},
		{Kind: StatusElementCursor},
		{Kind: StatusElementDiagnostics},
		{Kind: StatusElementCursorCount},
		{Kind: StatusElementMessages},
	}
}

// DefaultStatusRight is Fresh's default right element list
// (fresh-editor-core config.rs default_status_bar_right).
func DefaultStatusRight() []StatusElement {
	return []StatusElement{
		{Kind: StatusElementReadOnly},
		{Kind: StatusElementLineEnding},
		{Kind: StatusElementEncoding},
		{Kind: StatusElementLanguage},
		{Kind: StatusElementLSP},
		{Kind: StatusElementWarnings},
		{Kind: StatusElementUpdate},
		{Kind: StatusElementPalette},
	}
}

// DiagCounts is the diagnostics of the active document by severity, counted as
// Fresh's status bar counts them: errors, warnings, and everything else (info and
// hints) together.
type DiagCounts struct {
	Errors, Warnings, Info int
}

// Any reports whether there is a diagnostic to count.
func (d DiagCounts) Any() bool { return d != DiagCounts{} }

// Text is the counts as Fresh's diagnostics element renders them: "E:1 W:2 I:3",
// each part only when its count is above zero, or "" for no diagnostics.
func (d DiagCounts) Text() string {
	var parts []string
	if d.Errors > 0 {
		parts = append(parts, "E:"+strconv.Itoa(d.Errors))
	}
	if d.Warnings > 0 {
		parts = append(parts, "W:"+strconv.Itoa(d.Warnings))
	}
	if d.Info > 0 {
		parts = append(parts, "I:"+strconv.Itoa(d.Info))
	}
	return strings.Join(parts, " ")
}

// RunStyle is the theme role a run paints with. The zero value is the surface's
// own style (the status bar's for a status run), so a run that names no role needs
// nothing special.
type RunStyle int

const (
	StyleDefault RunStyle = iota // the surface's own style
	StyleWarning                 // the theme's warning role: a warning state, such as a restricted workspace
	StyleMuted                   // the surface's style, faint: a secondary value such as a pending chord
)

// Run is a piece of text at a fixed column, together with what a click on it
// reaches. The painter draws runs and the hit test reads them, so a label and
// its click target cannot drift apart.
type Run struct {
	X, W  int // first column and width in cells
	Text  string
	Kind  HitKind
	Index int
	Style RunStyle
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

	// Row bands (section 4.1). The body is BodyTop..BodyBottom. It holds the
	// explorer column, which spans every body row, and the editor column on its
	// other side: the panes, the tab strip and the dock. The menu band is
	// MenuTop..MenuBottom when the menu bar is shown, and both are -1 when it is
	// hidden; MenuLabelY is the row of the labels. The tab Strip is stripY: row 2
	// under the menu band, row 0 without it, and -1 when the tab bar is hidden.
	// statusY is the status row, drawn only when statusOn. Without it the body
	// takes that row.
	BodyTop, BodyBottom   int
	FrameOn               bool
	FrameTop, FrameBottom int
	// Chrome is the decoration of the layout's Style. FrameOn is true while its
	// border takes cells from the frame; the painter draws these glyphs in them.
	Chrome              Chrome
	MenuOn              bool
	MenuTop, MenuBottom int
	MenuLabelY          int
	StripY              int
	StatusY             int
	StatusOn            bool
	Scrollbar           bool // content overflows, so the last column is the scrollbar

	// The notice band: a status message that does not fit the status row, wrapped onto
	// the rows right above it and painted over the body (see setNotice). notice is empty
	// when the message fits; noticeY is the first row of the band. It spans the editor
	// column and stops above the dock, so it never paints over the shell.
	NoticeX int // the band's first column, the editor's left edge
	NoticeW int // the band's width, the editor column's width
	NoticeY int
	Notice  []string

	// The prompt row sits under the status row, above the find bar. It is reserved
	// only while PromptOn is set, which the root does while a prompt overlay is
	// open; with no prompt open the status row is the last row of the frame.
	PromptOn bool
	PromptY  int

	// The terminal dock (internal/tui/dock) is the bottom of the editor column:
	// columns DockX..DockX+EditorW-1 and rows DockY..DockY+DockH-1, a title row and
	// then the shell's rows. The explorer column runs beside it to BodyBottom, so
	// the navigator never loses rows to the tool. DockOn is false while the dock is
	// hidden or does not fit.
	DockOn bool
	DockX  int
	DockY  int
	DockH  int

	// Explorer (sections 4.2 and 4.3). The panel covers columns 0..ex-1 and rows
	// ExTitleY..exBottom: the title row, which is the tab Strip row when the menu
	// bar is shown, and then the tree body from ExBodyTop down. ex is zero when
	// the panel is hidden.
	ExplorerOpen bool
	Ex           int  // the column's width
	ExX          int  // the column's first cell: 0 on the left, W-Ex on the right
	ExRight      bool // the column is against the right edge
	EditorW      int  // the editor area's width; its first cell is EditorX
	ExTitleY     int  // the title row, aligned with the tab Strip
	ExBodyTop    int  // the first row of the tree body
	ExBottom     int  // bottom border row, the last body row
	ExRows       int
	ExReloadX    int // column of the refresh button, -1 when the button is not drawn
	ExCloseX     int // column of the close button, -1 when the button is not drawn

	// Sections is the column's section stack: section 0 is the explorer, the
	// rest are the sections under it. SectionReload and SectionClose are the
	// header buttons of each section, parallel to Sections; -1 where the width
	// leaves no room. Sections is empty when the column is too short for even
	// one header row.
	Sections      []sidebar.Section
	SectionReload []int
	SectionClose  []int

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
	// The style's border is the row under the menu band and the row above the status
	// row, and a column on each side of the editor. It yields those cells when the
	// terminal would leave the workspace without a row.
	l.Chrome = in.chrome()
	if l.Chrome.Border && h-in.FindRows >= menuRows+5 {
		l.FrameOn = true
		l.FrameTop = menuRows
		if l.StripY >= 0 {
			l.StripY++
		}
		l.BodyTop++
		l.ExTitleY = menuRows + 1
	}
	l.ExBodyTop = l.ExTitleY + 1
	l.StatusY = h - 1 - in.FindRows
	if in.ShowPromptLine {
		l.PromptOn, l.PromptY = true, l.StatusY
		l.StatusY--
	}
	l.StatusOn = !in.HideStatus
	l.BodyBottom = l.StatusY - 1
	if !l.StatusOn {
		l.BodyBottom = l.StatusY
	}
	if l.FrameOn {
		l.FrameBottom = l.BodyBottom
		l.BodyBottom--
	}
	if in.DockOpen {
		l.setDock()
	}

	// The explorer column comes first: its side decides the editor rectangle the
	// notice band, the panes, the tab strip and the welcome screen are measured
	// against.
	l.setExplorer(in)
	l.EditorX, l.EditorW = 0, w-l.Ex
	if !l.ExRight {
		l.EditorX = l.Ex
	}
	if l.FrameOn {
		if !l.ExplorerOpen || l.ExRight {
			l.EditorX++
			l.EditorW--
		}
		if !l.ExRight {
			l.EditorW--
		}
	}
	// The dock is the bottom of the editor column, so it takes the editor's first
	// column once the explorer has placed that column.
	l.DockX = l.EditorX
	if l.StatusOn {
		l.setNotice(in.Status, l.EditorX, l.EditorW)
	}
	l.placePanes(in)
	if in.HideVScroll {
		l.hideScrollbars()
	}

	if l.MenuOn {
		l.MenuRuns = MenuRuns(in.ExplorerFocus)
	}
	l.Strip = PlaceStrip(l.EditorX, l.EditorX+l.EditorW, in.Tabs, in.TabFirst, in.IconMode)
	if in.Welcome {
		l.WelcomeOn = true
		l.Welcome = WelcomeLines(l.EditorX, l.EditorW, l.BodyTop, l.EditorRows(), in.Recents, in.DoorKeys)
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
// above it. The rows are the body's, drawn over, and the first body row stays. The
// band spans the editor area (x0, width), so an open explorer keeps its border, and
// it stops above the dock, whose rows belong to the running shell.
func (l *Layout) setNotice(s StatusView, x0, width int) {
	bottom := l.StatusY - 1
	if l.FrameOn {
		bottom = l.FrameBottom - 1
	}
	if l.DockOn {
		bottom = l.DockY - 1
	}
	room := min(bottom-l.BodyTop, MaxNoticeRows)
	if room <= 0 {
		return
	}
	if rows := statusNotice(l.W, x0, width, s, room); len(rows) > 0 {
		l.Notice = rows
		l.NoticeX = x0
		l.NoticeW = width
		l.NoticeY = bottom - len(rows) + 1
	}
}

// statusNotice is the message of s wrapped to the rows of the notice band, at most
// maxRows of them, or nil when the status row shows the whole message. The band
// spans the editor area: x0 is its first column and width its width. The status
// row itself still spans the whole frame. The full text is in Show Warnings.
func statusNotice(frameW, x0, width int, s StatusView, maxRows int) []string {
	if s.Message == "" || width < noticeMinWidth || width-2*space.PadX < noticeMinText {
		return nil
	}
	if _, cut := statusRunsCut(frameW, s); !cut {
		return nil
	}
	return noticeRows(s.Message, width-2*space.PadX, maxRows)
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

// setDock takes the dock's rows from the bottom of the editor column. It does
// nothing when the dock does not fit. The explorer column keeps its rows: the dock
// belongs to the workspace, so it never cuts the navigator. DockX and the dock's
// width follow the editor column, which is placed after this call.
func (l *Layout) setDock() {
	h := dockHeight(l.BodyBottom - l.BodyTop + 1)
	if h == 0 {
		return
	}
	l.DockOn = true
	l.DockH = h
	l.DockY = l.BodyBottom - h + 1
}

// EditorRows is the rows of the editor band: the body's rows, less the dock's when
// it is open. The explorer column ignores the dock and keeps every body row.
func (l Layout) EditorRows() int {
	if l.DockOn {
		return l.DockY - l.BodyTop
	}
	return l.BodyBottom - l.BodyTop + 1
}

// setExplorer fills the explorer fields. The panel spans ExTitleY..bodyBottom
// (section 4.1).
func (l *Layout) setExplorer(in Input) {
	l.ExReloadX, l.ExCloseX = -1, -1
	if !in.ExplorerOpen {
		return
	}
	l.ExplorerOpen = true
	l.Ex = ExplorerCols(l.W, in.ExplorerWidth)
	l.ExRight = in.ExplorerSide == explorer.SideRight
	l.ExX = 0
	if l.ExRight {
		l.ExX = l.W - l.Ex
	}
	l.ExBottom = l.BodyBottom
	if l.FrameOn {
		l.ExBottom = l.FrameBottom
	}
	specs := in.Sidebar
	if len(specs) == 0 {
		specs = []sidebar.Spec{{Kind: sidebar.KindExplorer}}
	}
	l.Sections = sidebar.Layout(l.ExTitleY, l.ExBottom, specs)
	l.SectionReload = make([]int, len(l.Sections))
	l.SectionClose = make([]int, len(l.Sections))
	for i, sec := range l.Sections {
		spec := sidebar.HeaderSpec{
			Kind:      sec.Kind,
			First:     sec.Index == 0,
			Several:   len(l.Sections) > 1,
			Collapsed: sec.Collapsed,
			Title:     sec.Title,
			NoClose:   sec.NoClose,
		}
		h := sidebar.HeaderFor(l.Ex, in.IconMode, spec)
		// The buttons are frame columns: the header is measured from the
		// column's first cell, which is 0 only on the left side.
		l.SectionReload[i], l.SectionClose[i] = buttonColumn(h.Reload, l.ExX), buttonColumn(h.Close, l.ExX)
	}
	if len(l.Sections) == 0 {
		l.ExBodyTop = l.ExTitleY + 1
		l.ExRows = 0
		return
	}
	first := l.Sections[0]
	l.ExBodyTop = first.BodyTop
	l.ExRows = min(in.ExplorerRows, first.Rows())
	l.ExReloadX, l.ExCloseX = l.SectionReload[0], l.SectionClose[0]
}

// GutterWidth is the width of the line-number gutter. It is the pane's gutter,
// so the columns the Layout reserves are the columns the pane paints.
func GutterWidth(lines int) int {
	return editorpane.GutterWidth(lines)
}

// ExplorerCols is the explorer width E (spec section 3). file_explorer.width
// gives it (a percent of the frame or a column count); the zero value keeps
// gvim's automatic width of 32 columns. Either way the width is clamped to
// [16, 60], and the upper bound drops to W-20 on a narrow frame so the editor
// keeps room. The width never exceeds the frame.
func ExplorerCols(w int, width explorer.Width) int {
	e := width.Cols(w)
	if width.Mode == explorer.WidthAutomatic {
		e = explorerDefaultWidth
	}
	hi := min(explorerMaxWidth, max(w-explorerEditorMin, explorerMinWidth))
	return min(max(e, explorerMinWidth), hi, w)
}

// buttonColumn is a header button's frame column: rel is measured from the
// column's first cell x, which is 0 on the left side. -1 stays -1.
func buttonColumn(rel, x int) int {
	if rel < 0 {
		return -1
	}
	return x + rel
}

// InExplorer reports whether column x is in the explorer column. A hidden or
// absent column has no cells.
func (l Layout) InExplorer(x int) bool {
	return l.ExplorerOpen && x >= l.ExX && x < l.ExX+l.Ex
}

// InEditor reports whether column x is in the editor area: the tab strip, the
// welcome screen, the panes and the notice band all share that width.
func (l Layout) InEditor(x int) bool {
	return x >= l.EditorX && x < l.EditorX+l.EditorW
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

// StatusRuns lays out the status row (section 4.5). The rows are the configured
// elements of Fresh's editor.status_bar (the default lists when none are configured).
// The left group is cut first to the room the right group leaves; the right group is
// dropped from its end until the left group keeps at least min(floor(2W/5), 40) cells.
func StatusRuns(w int, s StatusView) []Run {
	runs, _ := statusRunsCut(w, s)
	return runs
}

// statusSeg is one segment of the row before it is placed: an element, a separator,
// or a plugin item, with what a click on it reaches.
type statusSeg struct {
	text  string
	kind  HitKind
	id    int // the Run index: a statusSegment constant or a plugin item's place in Extra
	style RunStyle
	sep   bool // the configured separator between two elements
	// plugin marks an item of a plugin: the left ones share what the other elements
	// leave, and a plugin cannot push the status message out of the row.
	plugin bool
	// messages marks the {messages} element: a row that cuts it wraps the message on the
	// notice band above.
	messages bool
	// remote marks the {remote} element: on a row too narrow for the left group, the
	// "Local" label is dropped before the rest of the group.
	remote bool
}

// statusSide turns one configured side into the segments it paints: the element texts
// in order, the configured separator between neighbours, and the plugin items at their
// anchors. The search hint (Palette) and every clickable element keep their hit
// targets.
func statusSide(elements []StatusElement, s StatusView, right bool) []statusSeg {
	out := make([]statusSeg, 0, len(elements)+len(s.Extra)+2)
	add := func(seg statusSeg) {
		if seg.text == "" {
			return
		}
		if s.Row.Separator != "" && len(out) > 0 {
			out = append(out, statusSeg{text: s.Row.Separator, sep: true})
		}
		out = append(out, seg)
	}
	plugin := func(i int) {
		it := s.Extra[i]
		seg := statusSeg{text: space.Pad(it.Text), plugin: true}
		if it.Clickable {
			seg.kind, seg.id = HitStatusPlugin, i
		}
		add(seg)
	}
	// The large-file chip is a gvim extension, not a Fresh element: it leads the right
	// group, before the configured elements.
	if right && s.Large != "" {
		add(statusSeg{text: space.Pad(s.Large)})
	}
	// The binary-view switch is a gvim extension too: it follows the document rather
	// than the configured element lists, so a custom status bar cannot hide it.
	if right && s.HexView != "" {
		add(statusSeg{
			text: space.Pad("[" + s.HexView + "]"),
			kind: HitStatusSegment,
			id:   StatusSegmentHexView,
		})
	}
	plugins := make([]int, 0, len(s.Extra))
	for i, it := range s.Extra {
		if it.Right == right {
			plugins = append(plugins, i)
		}
	}
	inserted := false
	for _, e := range elements {
		if !inserted && statusPluginAnchor(e.Kind, right) {
			for _, i := range plugins {
				plugin(i)
			}
			inserted = true
		}
		if seg, ok := statusElementSeg(e, s); ok {
			add(seg)
		}
	}
	if !inserted {
		for _, i := range plugins {
			plugin(i)
		}
	}
	return out
}

// statusPluginAnchor reports whether the plugin items of a side are inserted before
// the element kind: the left items sit between the cursor position and the message,
// as docs/plugins/api-ui.md says, and the right items before the first of
// {diagnostics}, {warnings}, {update} and {palette}, after the language/LSP cluster.
func statusPluginAnchor(k StatusElementKind, right bool) bool {
	if !right {
		return k == StatusElementMessages
	}
	switch k {
	case StatusElementDiagnostics, StatusElementWarnings, StatusElementUpdate, StatusElementPalette:
		return true
	}
	return false
}

// statusElementSeg is the segment one element paints, or false when it has nothing to
// show. The elements gvim does not paint — {warnings}, {update} and plugin tokens —
// render nothing at all; docs/ui.md says why.
func statusElementSeg(e StatusElement, s StatusView) (statusSeg, bool) {
	pad := func(text string) string { return space.Pad(text) }
	switch e.Kind {
	case StatusElementFilename:
		if s.Filename == "" {
			return statusSeg{}, false
		}
		// Fresh's markers: " [+]" while the buffer is modified and " [RO]" while
		// it is read-only, both after the display name.
		text := s.Filename
		if s.FilenameModified {
			text += " [+]"
		}
		if s.ReadOnly {
			text += " [RO]"
		}
		return statusSeg{text: pad(text)}, true
	case StatusElementChord:
		if s.Chord == "" {
			return statusSeg{}, false
		}
		return statusSeg{text: pad(s.Chord), style: StyleMuted}, true
	case StatusElementClock:
		if s.Clock == "" {
			return statusSeg{}, false
		}
		return statusSeg{text: pad(s.Clock)}, true
	case StatusElementTrust:
		if s.Trust == "" {
			return statusSeg{}, false
		}
		seg := statusSeg{text: pad(s.Trust), kind: HitStatusSegment, id: StatusSegmentTrust}
		if s.TrustWarn {
			seg.style = StyleWarning
		}
		return seg, true
	case StatusElementRemote:
		return statusSeg{text: pad(frameText("gvim.status.remote_local", "Local")), remote: true}, true
	case StatusElementTerminalRestart:
		if s.TerminalRestart == "" {
			return statusSeg{}, false
		}
		return statusSeg{text: pad(s.TerminalRestart), kind: HitStatusSegment, id: StatusSegmentTerminalRestart}, true
	case StatusElementCursor:
		return statusSeg{text: pad(space.PadTo(s.Position, PositionWidth(s.Lines))), kind: HitStatusSegment, id: StatusSegmentPosition}, true
	case StatusElementCursorCompact:
		if s.Position == "" {
			return statusSeg{}, false
		}
		text := strconv.Itoa(s.Line) + ":" + strconv.Itoa(s.Col)
		return statusSeg{text: pad(space.PadTo(text, CompactPositionWidth(s.Lines))), kind: HitStatusSegment, id: StatusSegmentPosition}, true
	case StatusElementDiagnostics:
		switch {
		case s.Diags.Any():
			return statusSeg{text: pad("[" + s.Diags.Text() + "]")}, true
		case s.DiagSlot:
			return statusSeg{text: strings.Repeat(" ", diagBlankWidth)}, true
		}
		return statusSeg{}, false
	case StatusElementCursorCount:
		if s.Cursors <= 1 {
			return statusSeg{}, false
		}
		return statusSeg{text: pad(strconv.Itoa(s.Cursors) + " cursors")}, true
	case StatusElementMessages:
		return statusSeg{text: pad(s.Message), kind: HitStatusSegment, id: StatusSegmentMessages, messages: true}, true
	case StatusElementReadOnly:
		if !s.ReadOnly {
			return statusSeg{}, false
		}
		return statusSeg{text: pad("[RO]"), style: StyleWarning}, true
	case StatusElementLineEnding:
		if s.EOL == "" {
			return statusSeg{}, false
		}
		return statusSeg{text: pad(s.EOL), kind: HitStatusSegment, id: StatusSegmentEOL}, true
	case StatusElementEncoding:
		if s.Encoding == "" {
			return statusSeg{}, false
		}
		return statusSeg{text: pad(s.Encoding), kind: HitStatusSegment, id: StatusSegmentEncoding}, true
	case StatusElementLanguage:
		if s.Language == "" {
			return statusSeg{}, false
		}
		return statusSeg{text: pad(s.Language), kind: HitStatusSegment, id: StatusSegmentLanguage}, true
	case StatusElementLSP:
		if s.LSP == "" {
			return statusSeg{}, false
		}
		return statusSeg{text: pad(s.LSP), kind: HitStatusSegment, id: StatusSegmentLSP}, true
	case StatusElementPalette:
		hint := PaletteHelp
		if s.PaletteHint != "" {
			hint = s.PaletteHint
		}
		return statusSeg{text: pad(hint), kind: HitStatusPalette}, true
	case StatusElementUpdate:
		// Fresh's "Update: vX.Y.Z" indicator: shown only when the release check
		// found a newer version.
		if s.Update == "" {
			return statusSeg{}, false
		}
		// gvim shows the release and does not self-update: the click opens the
		// update details instead of starting an install.
		return statusSeg{text: pad(s.Update), kind: HitStatusSegment, id: StatusSegmentUpdate}, true
	}
	// {warnings} and plugin tokens: parsed, not painted by gvim.
	return statusSeg{}, false
}

// statusRunsCut is StatusRuns, and it also reports whether the status message was cut
// or dropped to fit the row (which wraps it onto the notice band above).
func statusRunsCut(w int, s StatusView) ([]Run, bool) {
	if s.Row.Left == nil && s.Row.Right == nil {
		s.Row = StatusRow{Left: DefaultStatusLeft(), Right: DefaultStatusRight()}
	}
	left := statusSide(s.Row.Left, s, false)
	right := statusSide(s.Row.Right, s, true)

	// The widths that decide the right group's drop: the left elements that are not
	// plugin items, the room the messages keep, and the plugin items' own share.
	essentialW, itemW, msgW := 0, 0, 0
	for _, seg := range left {
		switch {
		case seg.plugin:
			itemW += ansi.StringWidth(seg.text)
		case seg.messages:
			msgW = min(ansi.StringWidth(seg.text), statusMessageMin)
		default:
			essentialW += ansi.StringWidth(seg.text)
		}
	}
	itemW = min(itemW, statusItemKeep)
	remoteIdx := -1
	for i := range left {
		if left[i].remote {
			remoteIdx = i
			break
		}
	}
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
			minLeft = max(minLeft, essentialW+itemW+msgW)
		}
		budget := w - (minLeft + 1)
		right = full
		for rightWidth() > budget && len(right) > keep {
			right = right[:len(right)-1]
			// A dropped element takes the separator before it with it.
			for len(right) > 0 && right[len(right)-1].sep {
				right = right[:len(right)-1]
			}
		}
		if itemW == 0 || pass > 0 || w-rightWidth() >= essentialW+itemW+msgW {
			break
		}
		if remoteIdx < 0 {
			break
		}
		// Still no room: the "Local" label goes before the rest of the left group.
		// Its separator goes with it, so the neighbours are still separated once.
		essentialW -= ansi.StringWidth(left[remoteIdx].text)
		left = append(left[:remoteIdx], left[remoteIdx+1:]...)
		if remoteIdx < len(left) && left[remoteIdx].sep {
			essentialW -= ansi.StringWidth(left[remoteIdx].text)
			left = append(left[:remoteIdx], left[remoteIdx+1:]...)
		}
	}
	var runs []Run
	x := 0
	room := w - rightWidth()
	// The budget pass may have dropped the label; the items share what is left after
	// what is actually on the row.
	itemRoom := room - essentialW - msgW
	// A plugin item that got no room is dropped before painting, with one of its
	// separators, so the label of a modal plugin never leaves a doubled separator
	// behind. The rest of the items are cut to their share here.
	painted := make([]statusSeg, 0, len(left))
	skipSep := false
	for _, seg := range left {
		if seg.plugin {
			if itemRoom > 0 {
				seg.text = space.End(seg.text, itemRoom)
				itemRoom -= ansi.StringWidth(seg.text)
			}
			if seg.text == "" {
				if n := len(painted); n > 0 && painted[n-1].sep {
					painted = painted[:n-1]
				}
				skipSep = true
				continue
			}
		} else if seg.sep && skipSep {
			skipSep = false
			continue
		}
		painted = append(painted, seg)
	}
	left = painted
	sawMessages, hasMessages := false, false
	for _, seg := range left {
		if seg.messages {
			hasMessages = true
		}
	}
	cut := false
	for _, seg := range left {
		if room <= 0 {
			break
		}
		text := seg.text
		if text == "" {
			continue
		}
		tw := ansi.StringWidth(text)
		if tw > room {
			r := newRun(x, space.End(text, room), seg.kind, seg.id)
			r.Style = seg.style
			runs = append(runs, r)
			if seg.messages {
				cut = true
			}
			break
		}
		r := newRun(x, text, seg.kind, seg.id)
		r.Style = seg.style
		runs = append(runs, r)
		if seg.messages {
			sawMessages = true
			cut = strings.Contains(s.Message, "\n")
		}
		x += tw
		room -= tw
	}
	if hasMessages && !sawMessages && s.Message != "" {
		// The row ended before the message element: the band carries the message.
		cut = true
	}

	x = w - rightWidth()
	for _, seg := range right {
		runs = append(runs, newRun(x, seg.text, seg.kind, seg.id))
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

// CompactPositionWidth is the width the {cursor:compact} item is padded to: the
// line digits, the colon, and Fresh's three-cell column reserve. One digit gives 5.
func CompactPositionWidth(lines int) int {
	return 4 + len(strconv.Itoa(max(lines, 0)))
}
