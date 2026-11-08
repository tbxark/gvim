// Package explorer is the explorer panel, the left column that shows the
// project tree. It paints the title row with its buttons, the tree rows, the
// scrollbar and the grip on the right border. It turns mouse and key input into
// tree moves and into messages that the root acts on: opening a file, reloading,
// closing the panel, giving the panel focus and resizing it.
//
// The panel is a pane, not an overlay. The root holds it as a value, calls
// Update for mouse messages, Action for the explorer key actions, and Draw to
// paint. Geometry comes from the root's layout on every call, so the hit map
// and the paint cannot drift apart. Update does no file IO, except the directory
// toggle, which reads one directory as the tree does on every expand.
package explorer

import (
	"image/color"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	tree "github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// Tree is the subset of *explorer.Tree the panel uses. ui.ExplorerTree has all of
// these methods, so the root passes its tree through unchanged.
type Tree interface {
	Root() string
	Rows() []tree.Row
	Selected() (tree.Row, bool)
	Move(delta int)
	Toggle(path string) error
	ScrollBy(n int)
	ScrollOffset() int
	ShowIgnored() bool
	SetShowIgnored(v bool)
}

// Geometry is the panel's place in the frame. The root takes it from the layout:
// X is the panel's first column, Width the layout's explorer width, Top the title
// row (the tab strip row), Bottom the bottom border row, and Focused reports
// whether the panel has keyboard focus, which shows the selection marker.
//
// X and Right place the column: the zero value is the left edge of the frame,
// which is where the explorer has always been. With Right set the panel sits
// against the frame's right edge and its grip is the left border, the side the
// editor is on.
type Geometry struct {
	X       int // the panel's first column
	Width   int
	Top     int // the title row; the tree body starts under it
	Bottom  int
	Right   bool
	Focused bool
	// Marks gives the git status letter of each changed path, keyed by its
	// absolute path. A row whose path is in Marks shows the letter in the column
	// before the border. Nil shows no marks.
	Marks map[string]rune
	// Icons picks the glyphs of the title buttons and of the expand marks. The zero
	// value is the plain set.
	Icons icons.Mode
}

// The rows of the panel are the rows of its Geometry: the title is row Top and
// the tree body starts at Top+1 (blueprint section 4.1). The body ends above
// Bottom.
const wheelRows = 3 // rows one wheel notch scrolls (blueprint section 3.2)

// Messages the panel sends. The root receives them through its Update.
type (
	// OpenFileMsg asks the root to open a file. Preview is true for a single
	// click, which opens a preview tab, and false for a double click or Enter,
	// which opens a permanent tab.
	OpenFileMsg struct {
		Path    string
		Preview bool
	}
	// ReloadMsg asks the root to reload the tree from disk: the refresh button and
	// Ctrl+R.
	ReloadMsg struct{}
	// CloseMsg asks the root to hide the panel: the close button.
	CloseMsg struct{}
	// FocusMsg asks the root to give the panel keyboard focus. A press on the
	// panel's title, border or empty body sends it.
	FocusMsg struct{}
	// ResizeMsg is a grip drag: the panel's new width in columns. The root clamps
	// it (layout section 4.2) and stores it as the explorer override.
	ResizeMsg struct{ Width int }
	// ErrorMsg reports a failed or refused tree action, such as a directory that
	// cannot be read, or a rename, delete or cut of the tree root. The root shows it
	// as a notice.
	ErrorMsg struct{ Err error }
)

// Panel is the explorer panel. Its zero value has no tree and draws nothing. New
// returns a usable panel.
type Panel struct {
	tree   Tree
	styles style.Styles
	now    func() time.Time
	clicks clickCounter
	grip   bool       // a grip drag is in progress
	thumb  bool       // a scrollbar drag is in progress
	st     *transient // picks, pending cut and search text, shared by the copies of the panel
}

// New returns the panel over t. styles are the chrome styles of the frame, and
// now supplies the press times for double-click detection (nil means time.Now).
func New(t Tree, styles style.Styles, now func() time.Time) Panel {
	if now == nil {
		now = time.Now
	}
	return Panel{tree: t, styles: styles, now: now, st: &transient{}}
}

// Update handles one mouse message in the panel's geometry g. A press, a drag,
// a release and a wheel notch are handled; other messages are ignored, and key
// presses go to Action. The returned command, when it is not nil, sends one of
// the messages in this package.
func (p Panel) Update(msg tea.Msg, g Geometry) (Panel, tea.Cmd) {
	if p.tree == nil || g.Width < 2 {
		return p, nil
	}
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		return p.press(tea.Mouse(msg), g)
	case tea.MouseMotionMsg:
		return p.drag(tea.Mouse(msg), g)
	case tea.MouseReleaseMsg:
		p.grip, p.thumb = false, false
	case tea.MouseWheelMsg:
		p.wheel(tea.Mouse(msg), g)
	}
	return p, nil
}

// Action runs one explorer key action and reports whether it handled it. The
// root resolves keys through the keymap in the file_explorer context and passes
// the action names here: file_explorer_up, _down, _page_up, _page_down, _open,
// _expand, _collapse, _refresh, _toggle_select, _extend_selection_up and _down,
// _select_all, _toggle_gitignored, _new_file, _new_directory, _rename and
// _delete, and the clipboard actions copy, cut and paste. Up and Down preview a
// file they land on. File operations are sent as the messages in ops.go. The
// search keys are not here: the root calls TypeSearch, SearchBackspace and
// ClearSearch. Any other name is not handled.
func (p Panel) Action(action string, g Geometry) (Panel, tea.Cmd, bool) {
	if p.tree == nil {
		return p, nil, false
	}
	var cmd tea.Cmd
	switch action {
	case "file_explorer_new_file":
		cmd = emit(NewFileMsg{Dir: p.targetDir()})
	case "file_explorer_new_directory":
		cmd = emit(NewFolderMsg{Dir: p.targetDir()})
	case "file_explorer_rename":
		cmd = p.selectedMsg("renamed", func(path string) tea.Msg { return RenameMsg{Path: path} })
	case "file_explorer_delete":
		cmd = p.deleteCmd()
	case "copy":
		if paths := p.targets(); len(paths) > 0 {
			cmd = emit(clipboardMsg(paths, false))
			p.state().cut = nil
		}
	case "cut":
		cmd = p.cutCmd()
	case "paste":
		// The cut dimming stays until the root clears the clipboard, so a paste that
		// stops partway leaves its entries dimmed.
		cmd = emit(PasteMsg{Dir: p.targetDir()})
	case "file_explorer_up":
		cmd = p.moveCursor(-1)
	case "file_explorer_down":
		cmd = p.moveCursor(1)
	case "file_explorer_page_up":
		p.tree.Move(-max(g.bodyRows(), 1))
	case "file_explorer_page_down":
		p.tree.Move(max(g.bodyRows(), 1))
	case "file_explorer_first":
		cmd = p.moveCursor(-len(p.tree.Rows()))
	case "file_explorer_last":
		cmd = p.moveCursor(len(p.tree.Rows()))
	case "file_explorer_open":
		cmd = p.openSelected()
	case "file_explorer_expand":
		p.expand()
	case "file_explorer_collapse":
		p.collapse()
	case "file_explorer_refresh":
		return p, emit(ReloadMsg{}), true
	case "file_explorer_toggle_select":
		p.togglePick()
	case "file_explorer_extend_selection_up":
		p.extend(-1)
	case "file_explorer_extend_selection_down":
		p.extend(1)
	case "file_explorer_select_all":
		p.selectAll()
	case "file_explorer_toggle_gitignored":
		p.tree.SetShowIgnored(!p.tree.ShowIgnored())
		cmd = emit(ReloadMsg{})
	default:
		return p, nil, false
	}
	p.follow(g)
	return p, cmd, true
}

// Draw paints the panel into dst, in the columns g.X to g.X+g.Width-1 and the
// rows g.Top to g.Bottom. Cells outside the panel are not touched.
func (p Panel) Draw(dst *grid.Grid, g Geometry) {
	if p.tree == nil || g.Width < 2 {
		return
	}
	body := style.Cell(p.styles.Explorer)
	head := style.Cell(p.styles.ExplorerTitle)
	right := g.X + g.Width
	dst.Fill(g.X, right, g.Top, head)
	dst.Put(g.X, g.Top, right, HeaderFor(g.Width, g.Icons).Text, head)
	p.drawBody(dst, g)
	if g.Bottom > g.Top {
		dst.Set(g.X, g.Bottom, "└", body)
		for x := g.X + 1; x < right-1; x++ {
			dst.Set(x, g.Bottom, "─", body)
		}
		dst.Set(right-1, g.Bottom, "┘", body)
	}
}

// DrawBody paints a sidebar section's tree body: the rows g.Top to g.Bottom-1,
// with their walls, the scrollbar and the selection. g.Top is the section's
// header row, which the section owns and this method does not paint, and
// g.Bottom is the last body row plus one. It is Draw without the title row and
// without the bottom border, so a stacked section shares its border rows with
// its neighbours.
func (p Panel) DrawBody(dst *grid.Grid, g Geometry) {
	if p.tree == nil || g.Width < 2 {
		return
	}
	p.drawBody(dst, g)
}

// drawBody paints the tree rows between the header row g.Top and g.Bottom: the
// body of the panel, or of its section in the sidebar column. The sticky band
// comes first: the ancestors of the run's first row, pinned above it on the
// gutter ground (or faint when the theme has none), then the run itself. The band
// never takes the whole body; the run keeps at least one row (see tree.Viewport).
func (p Panel) drawBody(dst *grid.Grid, g Geometry) {
	body := style.Cell(p.styles.Explorer)
	rows := p.tree.Rows()
	n := g.bodyRows()
	off, pins := tree.Viewport(rows, p.tree.ScrollOffset(), n)
	over := overflows(g, rows)
	sel, hasSel := p.tree.Selected()
	thumbStart, thumbLen := 0, 0
	if over {
		thumbStart, thumbLen = thumbOf(len(rows), n, off)
	}
	track := grid.Style{BG: style.Cell(p.styles.Scrollbar).FG}
	thumb := grid.Style{BG: style.Cell(p.styles.ScrollbarThumb).FG}
	accent := style.Cell(p.styles.Accent).BG
	pinned := style.Cell(p.styles.Gutter)
	sep := body
	if pinned.FG != nil {
		sep.FG = pinned.FG
	}
	right := g.X + g.Width

	for y := g.Top + 1; y < g.Bottom; y++ {
		dst.Fill(g.X, right, y, body)
		dst.Set(g.X, y, "│", body)
		dst.Set(right-1, y, "│", body)
		row := y - g.Top - 1
		idx, isPin := -1, false
		switch {
		case row < len(pins):
			idx, isPin = pins[row], true
		case row-len(pins) >= 0:
			idx = off + row - len(pins)
		}
		if idx >= 0 && idx < len(rows) {
			r := rows[idx]
			st := p.rowStyle(r, body)
			marker := g.Focused && hasSel && r.Path == sel.Path
			if isPin && !marker {
				// A pinned row keeps its ink on the band's muted ground, so the
				// strip reads as pinned above the tree rather than as part of it.
				if pinned.BG != nil {
					st.BG = pinned.BG
				} else {
					st.Faint = true
				}
			}
			dst.Fill(g.X+1, right-1, y, st)
			drawRow(dst, r, y, g.X, g.Width, over, marker, g.Marks[r.Path], g.Icons, st, sep, accent)
		}
		if over {
			st := track
			if row >= thumbStart && row < thumbStart+thumbLen {
				st = thumb
			}
			dst.Set(right-2, y, " ", st)
		}
	}
}

// drawRow paints one tree row at y, in the panel's columns left..left+width-1.
// Column 1 is the padding cell, which holds the selection marker when marker is
// set. The expand icon is at column 2 and the name at column 4, plus two cells
// per depth level. A compact row's folded ancestors are drawn before its own
// name as "a/b/c", their separators in the muted style. Names stop one padding
// cell before the border, and before the scrollbar column when over is set; a
// name that does not fit ends with "…". A non-zero mark is the git status
// letter, drawn in the last column before that padding, and the name stops one
// cell before it.
func drawRow(dst *grid.Grid, r tree.Row, y, left, width int, over, marker bool, mark rune, ic icons.Mode, st, sep grid.Style, accent color.Color) {
	end := left + width - 1 - space.PadX
	if over {
		end--
	}
	// Column 1 is the padding cell, so the expand icon is at column 2 and the name
	// at column 4, plus two cells per depth level. A set with file icons draws the
	// icon at the name's place and moves the name two cells right.
	iconX := left + 1 + space.PadX + 2*r.Depth
	nameX := iconX + 2
	fileIcon := ic.FileIcon(r.Name, r.IsDir, r.Expanded)
	if fileIcon != "" {
		nameX += 2
	}
	if mark != 0 {
		if x := end - 1; x > nameX {
			dst.Set(x, y, ic.GitMark(mark), st)
			end = x - 1
		}
	}
	icon := " "
	if r.IsDir {
		icon = ic.Glyph(icons.DirCollapsed)
		if r.Expanded {
			icon = ic.Glyph(icons.DirExpanded)
		}
	}
	if iconX < end {
		dst.Set(iconX, y, icon, st)
	}
	if fileIcon != "" && nameX-2 < end {
		dst.Set(nameX-2, y, fileIcon, st)
	}
	if marker && left+1 < end {
		bar := st
		bar.FG = accent
		dst.Set(left+1, y, "▌", bar)
	}
	x := nameX
	for _, part := range r.Chain {
		if x >= end {
			return
		}
		name := space.Name(part.Name, end-x)
		dst.Put(x, y, end, name, st)
		x += ansi.StringWidth(name)
		if x < end {
			dst.Set(x, y, "/", sep)
			x++
		}
	}
	if x < end {
		dst.Put(x, y, end, space.Name(r.Name, end-x), st)
	}
}

// bodyRows is the number of tree rows the body shows.
func (g Geometry) bodyRows() int {
	return max(g.Bottom-g.Top-1, 0)
}

// overflows reports whether the rows do not fit the body, so the scrollbar shows.
// It needs room for the scrollbar column beside the border.
func overflows(g Geometry, rows []tree.Row) bool {
	n := g.bodyRows()
	return g.Width >= 4 && n > 0 && len(rows) > n
}

// zone names the part of the panel under a cell.
type zone int

const (
	zoneNone zone = iota
	zoneReload
	zoneClose
	zoneGrip
	zoneBody   // the title, the border and the empty body
	zoneRow    // a tree row
	zoneScroll // the scrollbar column
)

// target is the zone under a cell. row is the body row for zoneRow and zoneScroll.
type target struct {
	zone zone
	row  int
}

// hitAt returns what the cell (x, y) is in the panel. Cells outside the panel
// are zoneNone. The grip is the right border from the body down to the bottom
// border, and the title's buttons sit on the title row.
func (p Panel) hitAt(g Geometry, x, y int) target {
	if x < g.X || x >= g.X+g.Width || y < g.Top || y > g.Bottom {
		return target{}
	}
	lx := x - g.X
	if y == g.Top {
		h := HeaderFor(g.Width, g.Icons)
		switch lx {
		case h.Reload:
			return target{zone: zoneReload}
		case h.Close:
			return target{zone: zoneClose}
		}
		return target{zone: zoneBody}
	}
	editorBorder := g.Width - 1
	if g.Right {
		editorBorder = 0
	}
	if lx == editorBorder {
		return target{zone: zoneGrip}
	}
	if lx == 0 || lx == g.Width-1 || y == g.Bottom {
		return target{zone: zoneBody}
	}
	rows := p.tree.Rows()
	off, pins := tree.Viewport(rows, p.tree.ScrollOffset(), g.bodyRows())
	row := y - g.Top - 1
	if overflows(g, rows) && lx == g.Width-2 {
		return target{zone: zoneScroll, row: row}
	}
	idx := -1
	switch {
	case row < len(pins):
		idx = pins[row]
	case row-len(pins) >= 0:
		idx = off + row - len(pins)
	}
	if idx >= 0 && idx < len(rows) {
		return target{zone: zoneRow, row: idx}
	}
	return target{zone: zoneBody}
}

// press handles a left press. The press count comes from the clicks in the same
// cell: 1 is a single click, 2 a double click, and 3 a triple press that acts
// like none of them.
func (p Panel) press(m tea.Mouse, g Geometry) (Panel, tea.Cmd) {
	if m.Button != tea.MouseLeft {
		return p, nil
	}
	n := p.clicks.press(p.now(), m.X, m.Y)
	p.grip, p.thumb = false, false
	t := p.hitAt(g, m.X, m.Y)
	switch t.zone {
	case zoneReload:
		return p, emit(ReloadMsg{})
	case zoneClose:
		return p, emit(CloseMsg{})
	case zoneGrip:
		p.grip = true
	case zoneScroll:
		p.thumb = true
		p.scrollTo(t.row, g)
	case zoneRow:
		return p.rowPress(t.row, n)
	case zoneBody:
		return p, emit(FocusMsg{})
	}
	return p, nil
}

// rowPress selects the row under a press. A directory toggles on a single
// press, so a double press toggles it once. A file opens as a preview on a
// single press and permanently on a double press (blueprint section 3.2).
func (p Panel) rowPress(row, n int) (Panel, tea.Cmd) {
	rows := p.tree.Rows()
	if row < 0 || row >= len(rows) {
		return p, nil
	}
	r := rows[row]
	p.selectIndex(rows, row)
	if r.IsDir {
		if n != 1 {
			return p, emit(FocusMsg{})
		}
		if err := p.tree.Toggle(r.Path); err != nil {
			return p, emit(ErrorMsg{Err: err})
		}
		return p, emit(FocusMsg{})
	}
	switch n {
	case 1:
		return p, previewCmd(r.Path)
	case 2:
		return p, emit(OpenFileMsg{Path: r.Path})
	}
	return p, emit(FocusMsg{})
}

// drag handles a motion with the left button. A grip drag sends the new width,
// with the grip under the pointer. A scrollbar drag moves the view to the pointer.
func (p Panel) drag(m tea.Mouse, g Geometry) (Panel, tea.Cmd) {
	if m.Button != tea.MouseLeft {
		return p, nil
	}
	switch {
	case p.grip:
		// The grip is the border facing the editor: the right one on the left
		// side, the left one on the right side.
		w := m.X - g.X + 1
		if g.Right {
			w = g.X + g.Width - m.X
		}
		if w != g.Width {
			return p, emit(ResizeMsg{Width: w})
		}
	case p.thumb:
		p.scrollTo(m.Y-g.Top-1, g)
	}
	return p, nil
}

// wheel scrolls the tree by wheelRows per notch when the pointer is over the
// panel's body. The selection stays where it is.
func (p Panel) wheel(m tea.Mouse, g Geometry) {
	delta := 0
	switch m.Button {
	case tea.MouseWheelUp:
		delta = -wheelRows
	case tea.MouseWheelDown:
		delta = wheelRows
	}
	if delta == 0 || m.X < g.X || m.X >= g.X+g.Width || m.Y < g.Top+1 || m.Y > g.Bottom {
		return
	}
	rows := p.tree.Rows()
	off, _ := tree.Viewport(rows, p.tree.ScrollOffset(), g.bodyRows())
	target := min(max(off+delta, 0), tree.MaxScroll(rows, g.bodyRows()))
	p.tree.ScrollBy(target - p.tree.ScrollOffset())
}

// scrollTo centers the scrollbar thumb on body row row. Nothing moves when the
// rows fit the body.
func (p Panel) scrollTo(row int, g Geometry) {
	rows := p.tree.Rows()
	body := g.bodyRows()
	if !overflows(g, rows) {
		return
	}
	row = min(max(row, 0), body-1)
	top := min(max(row*len(rows)/body-body/2, 0), tree.MaxScroll(rows, body))
	p.tree.ScrollBy(top - p.tree.ScrollOffset())
}

// openSelected opens the selected file permanently, or toggles the selected
// directory. A failed toggle is an ErrorMsg.
func (p Panel) openSelected() tea.Cmd {
	sel, ok := p.tree.Selected()
	if !ok {
		return nil
	}
	if sel.IsDir {
		if err := p.tree.Toggle(sel.Path); err != nil {
			return emit(ErrorMsg{Err: err})
		}
		return nil
	}
	return emit(OpenFileMsg{Path: sel.Path})
}

// expand opens a collapsed directory. Other rows are left alone.
func (p Panel) expand() {
	if sel, ok := p.tree.Selected(); ok && sel.IsDir && !sel.Expanded {
		_ = p.tree.Toggle(sel.Path)
	}
}

// collapse closes an open directory. On any other row it selects the parent
// directory, the nearest row above with one level less of depth.
func (p Panel) collapse() {
	sel, ok := p.tree.Selected()
	if !ok {
		return
	}
	if sel.IsDir && sel.Expanded {
		_ = p.tree.Toggle(sel.Path)
		return
	}
	rows := p.tree.Rows()
	cur := slices.IndexFunc(rows, func(r tree.Row) bool { return r.Path == sel.Path })
	if cur >= 0 && rows[cur].Parent >= 0 {
		p.selectIndex(rows, rows[cur].Parent)
	}
}

// selectIndex selects the row at idx of rows, which are the visible rows.
func (p Panel) selectIndex(rows []tree.Row, idx int) {
	sel, ok := p.tree.Selected()
	if !ok {
		return
	}
	cur := slices.IndexFunc(rows, func(r tree.Row) bool { return r.Path == sel.Path })
	if cur >= 0 {
		p.tree.Move(idx - cur)
	}
}

// follow scrolls the tree so the selected row is inside the body.
func (p Panel) follow(g Geometry) {
	sel, ok := p.tree.Selected()
	body := g.bodyRows()
	if !ok || body <= 0 {
		return
	}
	rows := p.tree.Rows()
	idx := slices.IndexFunc(rows, func(r tree.Row) bool { return r.Path == sel.Path })
	if idx < 0 {
		return
	}
	off := p.tree.ScrollOffset()
	if target := tree.FollowScroll(rows, idx, off, body); target != off {
		p.tree.ScrollBy(target - off)
	}
}

// emit returns a command that sends msg.
func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// thumbOf sizes the scrollbar thumb for lines rows in a body of h rows, with the
// first visible row top. The thumb is at least one row and at most the body.
func thumbOf(lines, h, top int) (start, length int) {
	length = min(h, max(1, (h*h+lines/2)/lines))
	start = min(top, lines-1) * h / lines
	return min(start, max(h-length, 0)), length
}
