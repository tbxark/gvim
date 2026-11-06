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
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	tree "github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
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
// Width is the layout's explorer width, Bottom its bottom border row, and
// Focused reports whether the panel has keyboard focus, which shows the
// selection marker.
type Geometry struct {
	Width   int
	Bottom  int
	Focused bool
	// Marks gives the git status letter of each changed path, keyed by its
	// absolute path. A row whose path is in Marks shows the letter in the column
	// before the border. Nil shows no marks.
	Marks map[string]rune
}

// Rows of the panel. The title is row 1 and the tree body starts at row 2
// (blueprint section 4.1). The body ends above Bottom.
const (
	titleRow  = 1
	bodyTop   = 2
	wheelRows = 3 // rows one wheel notch scrolls (blueprint section 3.2)
)

// Messages the panel sends. The root receives them through its Update.
type (
	// OpenFileMsg asks the root to open a file. Preview is true for a single
	// click, which opens a preview tab, and false for a double click or Enter,
	// which opens a permanent tab.
	OpenFileMsg struct {
		Path    string
		Preview bool
	}
	// ReloadMsg asks the root to reload the tree from disk: the "⟳" button and
	// Ctrl+R.
	ReloadMsg struct{}
	// CloseMsg asks the root to hide the panel: the "×" button.
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
		cmd = p.selectedMsg("deleted", func(path string) tea.Msg { return DeleteMsg{Path: path} })
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

// Draw paints the panel into dst, in the columns 0 to g.Width-1 and the rows 1
// to g.Bottom. Cells outside the panel are not touched.
func (p Panel) Draw(dst *grid.Grid, g Geometry) {
	if p.tree == nil || g.Width < 2 {
		return
	}
	body := style.Cell(p.styles.Explorer)
	head := style.Cell(p.styles.ExplorerTitle)
	dst.Fill(0, g.Width, titleRow, head)
	dst.Put(0, titleRow, g.Width, HeaderFor(g.Width).Text, head)

	rows := p.tree.Rows()
	off := p.tree.ScrollOffset()
	n := g.bodyRows()
	over := overflows(g, rows)
	sel, hasSel := p.tree.Selected()
	thumbStart, thumbLen := 0, 0
	if over {
		thumbStart, thumbLen = thumbOf(len(rows), n, off)
	}
	track := grid.Style{BG: style.Cell(p.styles.Scrollbar).FG}
	thumb := grid.Style{BG: style.Cell(p.styles.ScrollbarThumb).FG}

	for y := bodyTop; y < g.Bottom; y++ {
		dst.Fill(0, g.Width, y, body)
		dst.Set(0, y, "│", body)
		dst.Set(g.Width-1, y, "│", body)
		row := y - bodyTop
		if idx := off + row; idx < len(rows) {
			r := rows[idx]
			st := p.rowStyle(r, body)
			dst.Fill(1, g.Width-1, y, st)
			marker := g.Focused && hasSel && r.Path == sel.Path
			drawRow(dst, r, y, g.Width, over, marker, g.Marks[r.Path], st)
		}
		if over {
			st := track
			if row >= thumbStart && row < thumbStart+thumbLen {
				st = thumb
			}
			dst.Set(g.Width-2, y, " ", st)
		}
	}
	if g.Bottom > titleRow {
		dst.Set(0, g.Bottom, "└", body)
		for x := 1; x < g.Width-1; x++ {
			dst.Set(x, g.Bottom, "─", body)
		}
		dst.Set(g.Width-1, g.Bottom, "┘", body)
	}
}

// drawRow paints one tree row at y. Column 1 holds the expand icon, or the
// selection marker when marker is set. The name starts at column 3 plus two
// cells per depth level. Names stop before the scrollbar column when over is set.
// A non-zero mark is the git status letter, drawn in the last column before the
// border, and the name stops one cell before it.
func drawRow(dst *grid.Grid, r tree.Row, y, width int, over, marker bool, mark rune, st grid.Style) {
	end := width - 1
	if over {
		end--
	}
	if mark != 0 {
		if x := end - 1; x > 3+2*r.Depth {
			dst.Set(x, y, string(mark), st)
			end = x - 1
		}
	}
	icon := " "
	if r.IsDir {
		icon = "▶"
		if r.Expanded {
			icon = "▼"
		}
	}
	if x := 1 + 2*r.Depth; x < end {
		dst.Set(x, y, icon, st)
	}
	if marker && 1 < end {
		dst.Set(1, y, "▌", st)
	}
	dst.Put(3+2*r.Depth, y, end, r.Name, st)
}

// bodyRows is the number of tree rows the body shows.
func (g Geometry) bodyRows() int {
	return max(g.Bottom-bodyTop, 0)
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
	if x < 0 || x >= g.Width || y < titleRow || y > g.Bottom {
		return target{}
	}
	if y == titleRow {
		h := HeaderFor(g.Width)
		switch x {
		case h.Reload:
			return target{zone: zoneReload}
		case h.Close:
			return target{zone: zoneClose}
		}
		return target{zone: zoneBody}
	}
	if x == g.Width-1 {
		return target{zone: zoneGrip}
	}
	if x == 0 || y == g.Bottom {
		return target{zone: zoneBody}
	}
	row := y - bodyTop
	if overflows(g, p.tree.Rows()) && x == g.Width-2 {
		return target{zone: zoneScroll, row: row}
	}
	if idx := p.tree.ScrollOffset() + row; idx < len(p.tree.Rows()) {
		return target{zone: zoneRow, row: row}
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
	idx := p.tree.ScrollOffset() + row
	r := rows[idx]
	p.selectIndex(rows, idx)
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
		if w := m.X + 1; w != g.Width {
			return p, emit(ResizeMsg{Width: w})
		}
	case p.thumb:
		p.scrollTo(m.Y-bodyTop, g)
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
	if delta == 0 || m.X < 0 || m.X >= g.Width || m.Y < bodyTop || m.Y > g.Bottom {
		return
	}
	p.tree.ScrollBy(delta)
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
	top := min(max(row*len(rows)/body-body/2, 0), len(rows)-body)
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
	for i := cur - 1; i >= 0 && sel.Depth > 0; i-- {
		if rows[i].Depth == sel.Depth-1 {
			p.selectIndex(rows, i)
			return
		}
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
	idx := slices.IndexFunc(p.tree.Rows(), func(r tree.Row) bool { return r.Path == sel.Path })
	if idx < 0 {
		return
	}
	off := p.tree.ScrollOffset()
	switch {
	case idx < off:
		p.tree.ScrollBy(idx - off)
	case idx >= off+body:
		p.tree.ScrollBy(idx - off - body + 1)
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
