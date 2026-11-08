// Package browser is the file browser (Ctrl+O and File > Open File) and the
// folder picker (Switch Project). It lists one directory at a time with
// explorer.ListDir, filters the rows as the user types, and reports the choice
// as a message. It is an overlay on the root's stack: the root routes key and
// mouse input to it while it is open, and it closes itself when a choice is made.
//
// The browser cannot name ui.Overlay, because ui imports this package. Its
// Update returns *Browser, so the root wraps it in a small adapter that returns
// nil once Active reports false (see the Overlay contract in ui/ports.go).
//
// Listing runs in Update and in New. A directory is one ReadDir, which is what
// the legacy browser did on every keystroke; the root's overlay routing has no
// path for an asynchronous listing result yet.
package browser

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// Layout of the box. The box is drawn above the status row, is at most
// boxMaxWidth wide, and shows at most maxRows rows of the list.
const (
	boxMaxWidth = 72
	boxMinWidth = 16
	boxMinH     = 8
	maxRows     = 12
	defaultPage = 8
	wheelRows   = 3 // rows one wheel notch scrolls
)

// RowKind says what a row stands for.
type RowKind int

// The kinds of row, in the order the list shows them: the three special rows,
// then the entries.
const (
	RowParent RowKind = iota // "../", the parent directory
	RowRoot                  // "/", the filesystem root
	RowHome                  // "~/", the home directory
	RowDir                   // a directory entry
	RowFile                  // a file entry
)

// Row is one line of the list. Name is the text shown: "../", "/" and "~/" for
// the special rows, "name/" for a directory and "name" for a file. Path is the
// absolute path the row stands for.
type Row struct {
	Kind RowKind
	Name string
	Path string
}

// dirLike reports whether the row names a directory: the root, home and entries.
func (r Row) dirLike() bool {
	return r.Kind == RowRoot || r.Kind == RowHome || r.Kind == RowDir
}

// Open is the message a file choice sends. Line and Col are 1-based, and zero
// when the path was chosen without a position.
type Open struct {
	Path      string
	Line, Col int
}

// Choose is the message a folder pick sends. Path is the chosen directory.
type Choose struct {
	Path string
}

// sortMode is the order of the entries. Name is the order ListDir gives. Size and
// Modified sort ascending. Directories come first in every order.
type sortMode int

const (
	sortName sortMode = iota
	sortSize
	sortModified
	sortModes // the number of modes; Alt+S cycles through them in order
)

// label is the name of the mode, as the border notice says it.
func (m sortMode) label() string {
	return [...]string{"Name", "Size", "Modified"}[m]
}

// sorted returns a copy of listed in the order of mode. Ties keep the order of
// listed, which is by name.
func sorted(listed []explorer.Entry, mode sortMode) []explorer.Entry {
	out := slices.Clone(listed)
	if mode == sortName {
		return out
	}
	slices.SortStableFunc(out, func(a, b explorer.Entry) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		if mode == sortSize {
			return cmp.Compare(a.Size, b.Size)
		}
		return a.ModTime.Compare(b.ModTime)
	})
	return out
}

// Options are the browser's inputs.
type Options struct {
	Dir        string                                                                // directory to start in; empty means the working directory
	Title      string                                                                // the box title; empty means " Open File " (" Switch Project " for folders)
	FolderOnly bool                                                                  // Switch Project: list directories only, and Enter chooses one
	Home       string                                                                // path of the "~/" row; empty means the user's home directory
	Styles     style.Styles                                                          // the overlay styles of the box
	Now        func() time.Time                                                      // press times; nil means time.Now
	List       func(dir string, opts explorer.ListOptions) ([]explorer.Entry, error) // nil means explorer.ListDir
}

// Browser is the overlay. It is a pointer because Draw records the box it drew,
// which the mouse uses to find rows.
type Browser struct {
	dir        string
	folderOnly bool
	title      string
	home       string
	showHidden bool
	input      string
	styles     style.Styles
	now        func() time.Time
	list       func(string, explorer.ListOptions) ([]explorer.Entry, error)
	clicks     clickCounter

	loaded       bool // entries were read for loadedDir with loadedHidden
	loadedDir    string
	loadedHidden bool
	listed       []explorer.Entry // as ListDir returned them, in name order
	entries      []explorer.Entry // listed, in the order of sort
	sort         sortMode

	rows   []Row
	sel    int // index into rows of the selected row
	top    int // index into rows of the first row in the window
	vis    int // rows in the window, as last drawn
	box    box // the box as last drawn; zero before the first Draw
	notice string
	closed bool
}

// box is where Draw put the list, so a mouse press can find its row.
type box struct {
	ok         bool
	x, y, w, h int // top-left corner, width, and the rows of the list
}

// New returns a browser that starts in opts.Dir. It lists the directory once.
func New(opts Options) *Browser {
	dir := opts.Dir
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	home := opts.Home
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	b := &Browser{
		dir:        dir,
		folderOnly: opts.FolderOnly,
		title:      opts.Title,
		home:       home,
		styles:     opts.Styles,
		now:        opts.Now,
		list:       opts.List,
		vis:        defaultPage,
	}
	if b.now == nil {
		b.now = time.Now
	}
	if b.list == nil {
		b.list = explorer.ListDir
	}
	b.refresh()
	return b
}

// Active reports whether the browser is still open. It is false after a choice
// or Esc.
func (b *Browser) Active() bool { return !b.closed }

// Dir is the directory the list shows.
func (b *Browser) Dir() string { return b.dir }

// Input is the text typed into the filter.
func (b *Browser) Input() string { return b.input }

// Rows is the list as it is now: the special rows, then the entries that match
// the filter.
func (b *Browser) Rows() []Row { return b.rows }

// Notice is the message on the bottom border: an error or a toggle result. It
// is empty when there is none.
func (b *Browser) Notice() string { return b.notice }

// Update handles one key or mouse message. It returns the browser that takes
// the place of this one, which is the same pointer, and the command the message
// starts. A choice returns a command that sends an Open or a Choose, and closes
// the browser.
func (b *Browser) Update(msg tea.Msg) (*Browser, tea.Cmd) {
	if b.closed {
		return b, nil
	}
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		cmd = b.key(tea.Key(msg))
	case tea.PasteMsg:
		b.paste(msg.Content)
	case tea.MouseClickMsg:
		cmd = b.click(tea.Mouse(msg))
	case tea.MouseWheelMsg:
		b.wheel(tea.Mouse(msg))
	}
	return b, cmd
}

// key handles one key press. Alt+. toggles hidden entries, Alt+S cycles the sort
// order, and printable text extends the filter. Enter, Backspace, the arrows, the page keys, Home, End,
// Right and Tab move through the list. Esc closes the browser; the root does not
// normally forward it.
func (b *Browser) key(k tea.Key) tea.Cmd {
	if k.Mod&tea.ModAlt != 0 && k.Code == '.' {
		b.toggleHidden()
		return nil
	}
	if k.Mod&tea.ModAlt != 0 && k.Code == 's' {
		b.cycleSort()
		return nil
	}
	if k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 && printable(k.Text) {
		b.setInput(b.input + k.Text)
		return nil
	}
	switch k.Code {
	case tea.KeyEnter:
		return b.enter()
	case tea.KeyBackspace:
		b.backspace()
	case tea.KeyUp:
		b.move(-1)
	case tea.KeyDown:
		b.move(1)
	case tea.KeyPgUp:
		b.move(-b.vis)
	case tea.KeyPgDown:
		b.move(b.vis)
	case tea.KeyHome:
		b.jump(0)
	case tea.KeyEnd:
		b.jump(len(b.rows) - 1)
	case tea.KeyRight, tea.KeyTab:
		b.descend()
	case tea.KeyEscape:
		return b.close()
	}
	return nil
}

// click handles a left press in the list. A press outside the list does nothing,
// and the browser stays open. A press on a directory enters it, or chooses it
// in folder mode, on a single press. A press on a file selects it, and a double
// press opens it.
func (b *Browser) click(m tea.Mouse) tea.Cmd {
	if m.Button != tea.MouseLeft {
		return nil
	}
	n := b.clicks.press(b.now(), m.X, m.Y)
	i, ok := b.rowAt(m.X, m.Y)
	if !ok {
		return nil
	}
	b.sel = i
	r := b.rows[i]
	switch {
	case r.Kind == RowParent && n == 1:
		b.enterDir(r.Path)
	case r.dirLike() && b.folderOnly && n == 1:
		return b.finish(Choose{Path: r.Path})
	case r.dirLike() && !b.folderOnly && n == 1:
		b.enterDir(r.Path)
	case r.Kind == RowFile && n == 2:
		return b.finish(Open{Path: r.Path})
	}
	b.follow()
	return nil
}

// wheel scrolls the window by wheelRows per notch. The selection moves with the
// window so that it stays visible.
func (b *Browser) wheel(m tea.Mouse) {
	delta := 0
	switch m.Button {
	case tea.MouseWheelUp:
		delta = -wheelRows
	case tea.MouseWheelDown:
		delta = wheelRows
	}
	if delta == 0 {
		return
	}
	b.top = clamp(b.top+delta, 0, max(len(b.rows)-b.vis, 0))
	b.sel = clamp(b.sel, b.top, min(b.top+b.vis, len(b.rows))-1)
}

// enter acts on the selected row, or on a typed "path:line:col" in file mode.
// A parent or a directory opens it, a folder pick chooses it, and a file is
// opened. With no row selected, Enter does nothing.
func (b *Browser) enter() tea.Cmd {
	if !b.folderOnly {
		if msg, ok := b.typedLocation(); ok {
			return b.finish(msg)
		}
	}
	r, ok := b.selected()
	if !ok {
		return nil
	}
	switch {
	case r.Kind == RowParent:
		b.enterDir(r.Path)
	case r.dirLike() && b.folderOnly:
		return b.finish(Choose{Path: r.Path})
	case r.dirLike():
		b.enterDir(r.Path)
	default:
		return b.finish(Open{Path: r.Path})
	}
	return nil
}

// typedLocation reads a filter of the form "path:line" or "path:line:col". The
// path is taken relative to the listed directory and must be an existing file.
func (b *Browser) typedLocation() (Open, bool) {
	name, rest, found := strings.Cut(b.input, ":")
	if !found || name == "" {
		return Open{}, false
	}
	line, col, ok := editor.ParseLineCol(rest)
	if !ok {
		return Open{}, false
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(b.dir, path)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return Open{}, false
	}
	return Open{Path: path, Line: line, Col: col}, true
}

// descend enters the selected directory, or the parent, on Right and Tab. A file
// does nothing.
func (b *Browser) descend() {
	if r, ok := b.selected(); ok && (r.dirLike() || r.Kind == RowParent) {
		b.enterDir(r.Path)
	}
}

// backspace deletes the last character of the filter. With an empty filter it
// goes to the parent directory.
func (b *Browser) backspace() {
	if b.input != "" {
		r := []rune(b.input)
		b.setInput(string(r[:len(r)-1]))
		return
	}
	if parent := filepath.Dir(b.dir); parent != b.dir {
		b.enterDir(parent)
	}
}

// enterDir lists dir next and clears the filter and the notice.
func (b *Browser) enterDir(dir string) {
	b.dir = dir
	b.input = ""
	b.sel, b.top = 0, 0
	b.notice = ""
	b.refresh()
}

// setInput replaces the filter. The selection goes back to the first row, as
// the rows change with the filter.
func (b *Browser) setInput(s string) {
	b.input = s
	b.sel, b.top = 0, 0
	b.refresh()
}

// toggleHidden flips Show Hidden and says so on the border.
func (b *Browser) toggleHidden() {
	b.showHidden = !b.showHidden
	b.notice = onOff("Show Hidden", b.showHidden)
	b.refresh()
}

// cycleSort moves to the next sort order and says so on the border.
func (b *Browser) cycleSort() {
	b.sort = (b.sort + 1) % sortModes
	b.notice = "Sort by " + b.sort.label()
	b.entries = sorted(b.listed, b.sort)
	b.refresh()
}

// move changes the selection by delta rows, clamped to the list.
func (b *Browser) move(delta int) {
	if len(b.rows) == 0 {
		return
	}
	b.sel = clamp(b.sel+delta, 0, len(b.rows)-1)
	b.follow()
}

// jump selects row i, clamped to the list.
func (b *Browser) jump(i int) {
	if len(b.rows) == 0 {
		return
	}
	b.sel = clamp(i, 0, len(b.rows)-1)
	b.follow()
}

// selected returns the selected row. ok is false when the list is empty.
func (b *Browser) selected() (Row, bool) {
	if b.sel < 0 || b.sel >= len(b.rows) {
		return Row{}, false
	}
	return b.rows[b.sel], true
}

// close ends the browser without a choice.
func (b *Browser) close() tea.Cmd {
	b.closed = true
	return nil
}

// finish ends the browser and returns the command that sends msg.
func (b *Browser) finish(msg tea.Msg) tea.Cmd {
	b.closed = true
	return func() tea.Msg { return msg }
}

// refresh reads the directory when it or the hidden setting changed, and then
// rebuilds the rows from the entries and the filter. Hidden entries are listed
// when Show Hidden is on, or when the filter starts with ".".
func (b *Browser) refresh() {
	hidden := b.showHidden || strings.HasPrefix(b.input, ".")
	if !b.loaded || b.loadedDir != b.dir || b.loadedHidden != hidden {
		listed, err := b.list(b.dir, explorer.ListOptions{ShowHidden: hidden, ShowIgnored: true})
		b.listed = listed
		b.entries = sorted(listed, b.sort)
		b.loaded, b.loadedDir, b.loadedHidden = true, b.dir, hidden
		if err != nil {
			b.listed, b.entries = nil, nil
			b.notice = capitalize(err.Error())
		}
	}
	b.rows = b.buildRows()
	b.sel = clamp(b.sel, 0, len(b.rows)-1)
	b.follow()
}

// buildRows lists the special rows, when the filter is empty, and then the
// entries whose names contain the filter, case-insensitively. In folder mode the
// entries are the directories only.
func (b *Browser) buildRows() []Row {
	var rows []Row
	if b.input == "" {
		if parent := filepath.Dir(b.dir); parent != b.dir {
			rows = append(rows, Row{Kind: RowParent, Name: "../", Path: parent})
		}
		rows = append(rows, Row{Kind: RowRoot, Name: "/", Path: string(filepath.Separator)})
		if b.home != "" {
			rows = append(rows, Row{Kind: RowHome, Name: "~/", Path: b.home})
		}
	}
	needle := strings.ToLower(b.input)
	for _, e := range b.entries {
		if b.folderOnly && !e.IsDir {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.Name), needle) {
			continue
		}
		path := filepath.Join(b.dir, e.Name)
		if e.IsDir {
			rows = append(rows, Row{Kind: RowDir, Name: e.Name + "/", Path: path})
		} else {
			rows = append(rows, Row{Kind: RowFile, Name: e.Name, Path: path})
		}
	}
	return rows
}

// follow moves the window so that the selected row is in it. The window size is
// the one Draw used last.
func (b *Browser) follow() {
	vis := max(b.vis, 1)
	b.top = clamp(b.top, 0, max(len(b.rows)-vis, 0))
	switch {
	case b.sel < b.top:
		b.top = b.sel
	case b.sel >= b.top+vis:
		b.top = b.sel - vis + 1
	}
}

// rowAt returns the row index under the cell (x, y), from the last Draw. ok is
// false outside the list.
func (b *Browser) rowAt(x, y int) (int, bool) {
	bx := b.box
	if !bx.ok || x <= bx.x || x >= bx.x+bx.w-1 {
		return 0, false
	}
	k := y - (bx.y + 3)
	if k < 0 || k >= bx.h {
		return 0, false
	}
	i := b.top + k
	if i >= len(b.rows) {
		return 0, false
	}
	return i, true
}

// onOff is the notice for a toggle that was just set.
func onOff(name string, on bool) string {
	if on {
		return name + " enabled"
	}
	return name + " disabled"
}

// capitalize upper-cases the first letter of s, for notices from lower-case
// package errors.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// paste extends the filter with pasted text. A line break becomes a space and any other
// unprintable rune is dropped.
func (b *Browser) paste(s string) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return ' '
		case unicode.IsPrint(r):
			return r
		}
		return -1
	}, s)
	if s != "" {
		b.setInput(b.input + s)
	}
}

// printable reports whether s is text the user typed: non-empty, with only
// printable runes.
func printable(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// clamp limits v to the range lo..hi. When hi is below lo, it returns lo.
func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

// truncateLeft shortens s to at most w cells, keeping its end and marking the
// cut with "…" at the start. Paths are cut from the front so the file name stays
// visible.
func truncateLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	var kept []rune
	width := 0
	r := []rune(s)
	for _, v := range slices.Backward(r) {
		cw := ansi.StringWidth(string(v))
		if width+cw > w-1 {
			break
		}
		width += cw
		kept = append(kept, v)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return "…" + string(kept)
}

// Draw paints the box over the frame in g. The box sits above the status row,
// centered, and is clipped to the frame. A frame too small for it is left alone.
// Draw also fixes the window size and records the box for the mouse.
func (b *Browser) Draw(g *grid.Grid) {
	b.box = box{}
	w, h := g.Width(), g.Height()
	bw := min(boxMaxWidth, w-2*space.ListMargin(w))
	if bw < boxMinWidth || h < boxMinH {
		return
	}
	vis := max(1, min(len(b.rows), min(maxRows, h-6)))
	b.vis = vis
	b.follow()

	x0 := (w - bw) / 2
	top := h - 2 - 3 - vis // the top border; the bottom border sits on row h-2
	b.box = box{ok: true, x: x0, y: top, w: bw, h: vis}

	ov := b.styles.Overlay
	title := " Open File "
	if b.folderOnly {
		title = " Switch Project "
	}
	if b.title != "" {
		title = " " + b.title + " "
	}
	ov.PaintBox(g, x0, top, bw, vis+4, title)
	boxLine(g, x0, top+1, bw, "> "+b.input, ov.Idle, ov.Border, 0)
	boxLine(g, x0, top+2, bw, truncateLeft(b.dir, bw-4), ov.Muted, ov.Border, 0)

	over := len(b.rows) > vis
	thumbStart, thumbLen := 0, 0
	if over {
		thumbStart, thumbLen = thumbOf(len(b.rows), vis, b.top)
	}
	track := grid.Style{BG: ov.Idle.BG}
	thumbSt := grid.Style{BG: ov.Muted.FG}
	reserve := 0
	if over {
		reserve = 1
	}
	for k := range vis {
		y := top + 3 + k
		idx := b.top + k
		st := ov.Box
		text := ""
		switch {
		case idx < len(b.rows):
			text = b.rows[idx].Name
			if idx == b.sel {
				st = ov.Selected
			}
		case len(b.rows) == 0:
			text = "No matches"
		}
		boxLine(g, x0, y, bw, text, st, ov.Border, reserve)
		if over {
			s := track
			if k >= thumbStart && k < thumbStart+thumbLen {
				s = thumbSt
			}
			g.Set(x0+bw-2, y, " ", s)
		}
	}
	hint := " Enter open · Alt+. hidden "
	if b.folderOnly {
		hint = " Enter choose · Right enter · Alt+. hidden "
	}
	if b.notice != "" {
		hint = " " + b.notice + " "
	}
	g.Put(x0+2, top+3+vis, x0+bw-2, hint, ov.Muted)
}

// boxLine draws one row of the box: the side borders, the inner fill, and text
// from the third column to one padding cell before the right border. reserve cells
// at the right stay free for the scrollbar. Text that does not fit ends with "…".
func boxLine(g *grid.Grid, x0, y, bw int, text string, inner, edge grid.Style, reserve int) {
	g.Fill(x0+1, x0+bw-1, y, inner)
	g.Set(x0, y, "│", edge)
	g.Set(x0+bw-1, y, "│", edge)
	end := x0 + bw - 1 - space.PadX - reserve
	g.Put(x0+2, y, end, space.End(text, end-(x0+2)), inner)
}

// thumbOf sizes the scrollbar thumb for lines rows in a window of h rows, with
// the first window row top. The thumb is at least one row long.
func thumbOf(lines, h, top int) (start, length int) {
	length = min(h, max(1, (h*h+lines/2)/lines))
	start = min(top, lines-1) * h / lines
	return min(start, max(h-length, 0)), length
}
