// Package palette is the command palette overlay of gvim. It is a query line on
// the bottom row, a box of matches above it and a hint row, laid out as the
// legacy palette was (BLUEPRINT-v2 section 4.4).
//
// The query picks the mode with internal/palette: no prefix searches files, ">"
// runs commands, ":" goes to a line and "#" picks a buffer. Command matching is
// the domain model's, file ranking is the Ranker's, and the list navigation
// (the highlight and the visible window) is palette.Palette's.
//
// Palette runs no action. A chosen row closes the palette and emits one of the
// messages in this package, which the root handles. Overlay.Update returns the
// ui type, which this package cannot name without importing ui, so the root
// wraps *Palette in a small adapter that forwards Update, Draw and Active.
package palette

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
	model "github.com/tbxark/gvim/internal/palette"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

const (
	// fileLimit is how many matches the file mode asks the Ranker for. It is
	// finder.MaxResults; ui may not import finder, so the value is repeated here.
	fileLimit = 100
	// pollInterval is how long the file mode waits before it ranks again while
	// the file index is still loading.
	pollInterval = 100 * time.Millisecond
	// wheelRows is the number of rows one wheel notch scrolls (BLUEPRINT-v2 3.2).
	wheelRows = 3
	// loadingText is the file mode's message while the index runs. It is
	// finder.LoadingStatus, repeated for the same reason as fileLimit.
	loadingText = "Loading files…"
	// noNameText labels a buffer with no name in the buffer mode.
	noNameText = "[No Name]"
	// hint is the row under the box that lists the four modes.
	hint = "  file  |  >command  |  :line  |  #buffer"
)

// Files is the file list the file mode searches. *finder.Index satisfies it.
// Snapshot is called from a tea.Cmd, so it must be safe for concurrent use.
type Files interface {
	// Snapshot returns the files found so far and whether the enumeration is
	// still running.
	Snapshot() (files []string, loading bool)
}

// Ranker scores paths against a query and returns at most limit of them, best
// first. The app adapter fills it over internal/finder and internal/fuzzy. Rank
// is called from a tea.Cmd, never from Update or Draw, so it may take a while
// over a large list.
type Ranker interface {
	Rank(query string, files []string, limit int) []Ranked
}

// Ranked is one scored path. Indexes are the matched rune offsets in Path, which
// the row shows in bold.
type Ranked struct {
	Path    string
	Indexes []int
}

// Buffer is one open tab, as the buffer mode lists it.
type Buffer struct {
	Name     string
	Modified bool
}

// Options is the input of New.
type Options struct {
	// Query is the raw query, mode prefix included. Ctrl+P opens ">" in the
	// root; the empty query is the file mode.
	Query string
	// Root is the directory the file rows are relative to. OpenFileMsg.Path is
	// Root joined with the row's path.
	Root string
	// Commands is the command table, as internal/palette builds it.
	Commands []model.Entry
	// State is the editor state the commands are checked against. Nil means
	// model.DefaultState.
	State *model.State
	// Buffers are the open tabs, in tab order, for the buffer mode.
	Buffers []Buffer
	// Files is the file index. Nil until the index is wired.
	Files Files
	// Ranker scores the file rows. Nil until it is wired; the file mode then shows
	// its loading row.
	Ranker Ranker
	// Theme gives the chrome colors.
	Theme theme.Theme
}

// RunCommandMsg asks the root to run a command by its Fresh action name. It is
// sent after the palette has closed.
type RunCommandMsg struct {
	Action string
}

// GotoLineMsg asks the root to move the caret to a line and a column. Both are
// 1-based, as the user typed them.
type GotoLineMsg struct {
	Line, Col int
}

// OpenFileMsg asks the root to open a file. Rel is the path the Ranker returned
// and Path is Rel joined with Options.Root.
type OpenFileMsg struct {
	Path, Rel string
}

// ActivateBufferMsg asks the root to activate the tab at Index, which is its
// position in Options.Buffers.
type ActivateBufferMsg struct {
	Index int
}

// FileRankedMsg carries the file rows ranked for one file query. The palette
// applies it only when Seq is the query it sent last, so a slow ranking of an
// old query does not replace the rows of a newer one.
type FileRankedMsg struct {
	Seq     int
	Text    string // the query text that was ranked
	Matches []Ranked
	// Loading is the index's state when it was read. While it is true the
	// palette ranks again after pollInterval.
	Loading bool
}

// fileTickMsg asks the palette to rank the file rows again. It is sent after
// pollInterval while the index is loading, and it is ignored once Seq is stale.
type fileTickMsg struct {
	Seq int
}

// rowKind says what a row does when it is chosen.
type rowKind int

const (
	rowCommand rowKind = iota
	rowFile
	rowBuffer
	rowLine
	rowMessage // informational; choosing it closes the palette and does nothing
)

// row is one line of the box.
type row struct {
	kind     rowKind
	name     string
	keys     string // shortcut column, commands only
	desc     string
	source   string
	disabled bool  // a command the editor state does not allow; choosing it does nothing
	hl       []int // matched rune offsets in name, for file rows
	action   string
	rel      string
	index    int
	line     int
	col      int
}

// chrome holds the grid styles of the palette, built from the theme.
type chrome struct {
	panel    grid.Style // the box, the hint row and the empty cells
	selected grid.Style // the highlighted row
	prompt   grid.Style // the query line
}

// Palette is the open command palette. Use New to make one.
type Palette struct {
	opts   Options
	state  model.State
	input  textinput.Model
	rows   []row
	nav    model.Palette
	chrome chrome

	open          bool
	width, height int // the frame size of the last Draw; mouse hit tests read them
	synced        int // the height the list navigation was last sized for

	// seq numbers the queries. A FileRankedMsg or fileTickMsg for an older seq is
	// dropped.
	seq int
	// inFile is true while the query is a file query. fileText is its text, and
	// shown is the text of the file rows on screen, so a new text puts the
	// highlight back on the top row.
	inFile   bool
	fileText string
	shown    string
}

// New returns an open palette for opts. The query is the input and its rows are
// computed at once, except that a file query first shows the loading row. The
// box height is known after the first Draw. The ranking of a file query starts
// with Init.
func New(opts Options) *Palette {
	st := model.DefaultState()
	if opts.State != nil {
		st = *opts.State
	}
	ss := style.New(opts.Theme)
	p := &Palette{
		opts:  opts,
		state: st,
		open:  true,
		chrome: chrome{
			panel:    style.Cell(ss.Menu),
			selected: style.Cell(ss.MenuActive),
			prompt:   style.Cell(ss.Editor),
		},
	}
	p.input = textinput.New()
	p.input.Prompt = ""
	p.input.SetValue(opts.Query)
	p.input.CursorEnd()
	p.input.Focus() // the blink command is not needed: Draw paints the cursor cell
	p.requery()
	return p
}

// Init returns the command that ranks the file rows for the opening query. It is
// nil for any other mode, and when no Ranker is wired. The root runs it once.
func (p *Palette) Init() tea.Cmd {
	if !p.open || !p.inFile {
		return nil
	}
	return p.rankCmd(p.seq, p.fileText)
}

// Active reports whether the palette is open. It is false after a row was
// chosen or a click ran one, and after an Enter with no rows.
func (p *Palette) Active() bool { return p.open }

// Update handles one key or mouse message. Keys edit the query and move the
// highlight; Enter runs the highlighted row. A left click runs the row under the
// pointer, and a click anywhere else is swallowed, so it does not dismiss the
// palette. The wheel scrolls the box by wheelRows and hovering moves the
// highlight. Esc is not sent here: the root pops the palette on Esc.
//
// A file query is ranked by a command, and its result comes back as a
// FileRankedMsg, which Update applies. The returned command emits the message of
// the chosen row, if the row has one, or the next ranking step of the file mode.
func (p *Palette) Update(msg tea.Msg) tea.Cmd {
	if !p.open {
		return nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return p.key(msg)
	case tea.PasteMsg:
		return p.edit(msg)
	case FileRankedMsg:
		return p.applyFiles(msg)
	case fileTickMsg:
		if msg.Seq == p.seq && p.inFile {
			return p.rankCmd(p.seq, p.fileText)
		}
	case tea.MouseClickMsg:
		return p.click(tea.Mouse(msg))
	case tea.MouseMotionMsg:
		m := tea.Mouse(msg)
		if i, ok := p.rowAt(m.X, m.Y); ok {
			p.nav.Select(i)
		}
	case tea.MouseWheelMsg:
		switch tea.Mouse(msg).Button {
		case tea.MouseWheelUp:
			p.nav.ScrollBy(-wheelRows)
		case tea.MouseWheelDown:
			p.nav.ScrollBy(wheelRows)
		}
	}
	return nil
}

// Draw paints the box, the hint row and the query line over g. It records the
// frame size, so that a mouse position maps to a row. Draw does no ranking: the
// file rows change only through Update.
func (p *Palette) Draw(g *grid.Grid) {
	if !p.open {
		return
	}
	p.width, p.height = g.Width(), g.Height()
	if p.height != p.synced {
		// Only a height change resizes the list. SetSize moves the view back to the
		// highlight, so calling it on every frame would undo a wheel scroll.
		p.syncSize()
		p.synced = p.height
	}
	if p.width <= 0 || p.height <= 0 {
		return
	}
	p.paint(g)
}

// key runs the key press: a list key moves the highlight or runs the row, and any
// other key goes to the query line.
func (p *Palette) key(msg tea.KeyPressMsg) tea.Cmd {
	switch tea.Key(msg).String() {
	case "enter":
		return p.run(p.nav.Selected())
	case "up":
		p.nav.Select(p.nav.Selected() - 1)
		return nil
	case "down":
		p.nav.Select(p.nav.Selected() + 1)
		return nil
	}
	return p.edit(msg)
}

// edit gives msg to the query line and requeries when the query changed.
func (p *Palette) edit(msg tea.Msg) tea.Cmd {
	before := p.input.Value()
	p.input, _ = p.input.Update(msg)
	if p.input.Value() == before {
		return nil
	}
	return p.requery()
}

// click runs the row under a left click. Other buttons, and clicks that miss the
// rows, change nothing.
func (p *Palette) click(m tea.Mouse) tea.Cmd {
	if m.Button != tea.MouseLeft {
		return nil
	}
	i, ok := p.rowAt(m.X, m.Y)
	if !ok {
		return nil
	}
	p.nav.Select(i)
	return p.run(i)
}

// run closes the palette and returns the command of row i. With no rows it only
// closes. A disabled command stays open, so the user can pick another row.
func (p *Palette) run(i int) tea.Cmd {
	if len(p.rows) == 0 {
		p.open = false
		return nil
	}
	if i < 0 || i >= len(p.rows) {
		return nil
	}
	r := p.rows[i]
	if r.disabled {
		return nil
	}
	p.open = false
	return r.command(p.opts.Root)
}

// rowAt maps a frame cell to the index of the row under it. The row spans the
// box minus its left border, and only rows in view count.
func (p *Palette) rowAt(x, y int) (int, bool) {
	if p.height <= 0 {
		return 0, false
	}
	geo := layoutFor(p.width, p.height)
	if x < 1 || x >= geo.w {
		return 0, false
	}
	k := y - geo.top - 1
	first, count := p.nav.Window()
	if k < 0 || k >= geo.rows || k >= count {
		return 0, false
	}
	return first + k, true
}

// syncSize gives the list navigation the row count and the box height.
func (p *Palette) syncSize() {
	p.nav.SetSize(len(p.rows), visibleRows(p.height))
}

// requery recomputes the rows after the query changed and returns the command
// that ranks a file query. A command, line or buffer query gets its rows at once,
// and the highlight goes back to the top. A file query keeps the rows on screen
// until its result arrives. The first file query shows the loading row.
func (p *Palette) requery() tea.Cmd {
	p.seq++
	query := p.input.Value()
	mode, text := model.ParseQuery(query)
	if mode != model.ModeFile {
		p.inFile = false
		p.rows = p.rowsFor(query)
		p.syncSize()
		p.nav.Select(0)
		return nil
	}
	if !p.inFile {
		p.rows = loadingRows()
		p.shown = ""
		p.syncSize()
		p.nav.Select(0)
	}
	p.inFile = true
	p.fileText = text
	return p.rankCmd(p.seq, text)
}

// rankCmd returns the command that ranks the files for text against the index as
// it is now. The result comes back as a FileRankedMsg. It returns nil when no
// Ranker is wired, and the file mode then keeps the loading row.
func (p *Palette) rankCmd(seq int, text string) tea.Cmd {
	if p.opts.Ranker == nil {
		return nil
	}
	files, rk := p.opts.Files, p.opts.Ranker
	return func() tea.Msg {
		var list []string
		loading := true
		if files != nil {
			list, loading = files.Snapshot()
		}
		return FileRankedMsg{Seq: seq, Text: text, Matches: rk.Rank(text, list, fileLimit), Loading: loading}
	}
}

// applyFiles shows a ranked result when it answers the current query. The
// highlight goes to the top when the text changed. A result read while the index
// was still loading asks for another ranking after pollInterval, so the rows
// follow the index without a key press.
func (p *Palette) applyFiles(msg FileRankedMsg) tea.Cmd {
	if msg.Seq != p.seq || !p.inFile {
		return nil
	}
	p.rows = fileRows(msg.Matches, msg.Loading)
	p.syncSize()
	if msg.Text != p.shown {
		p.nav.Select(0)
	}
	p.shown = msg.Text
	if !msg.Loading || p.opts.Files == nil {
		return nil
	}
	seq := msg.Seq
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return fileTickMsg{Seq: seq} })
}

// rowsFor returns the rows the query matches in its command, line or buffer
// mode. The file mode is ranked by rankCmd and has no rows here.
func (p *Palette) rowsFor(query string) []row {
	mode, text := model.ParseQuery(query)
	switch mode {
	case model.ModeCommand:
		return commandRows(model.SearchIn(p.opts.Commands, query, p.state))
	case model.ModeLine:
		line, col, ok := editor.ParseLineCol(text)
		if !ok {
			return nil
		}
		return []row{{kind: rowLine, name: "Go to line " + strconv.Itoa(line), line: line, col: col}}
	case model.ModeBuffer:
		return p.bufferRows(text)
	}
	return nil
}

// commandRows turns the matched command entries into rows.
func commandRows(entries []model.Entry) []row {
	rows := make([]row, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, row{
			kind:     rowCommand,
			name:     e.Name,
			keys:     e.Keybinding,
			desc:     e.Description,
			source:   e.Source,
			disabled: e.Disabled,
			action:   e.Action,
		})
	}
	return rows
}

// bufferRows lists the open tabs whose label contains text, ignoring case. The
// label is the name, with "*" after a modified buffer.
func (p *Palette) bufferRows(text string) []row {
	needle := strings.ToLower(text)
	var rows []row
	for i, b := range p.opts.Buffers {
		name := b.Name
		if name == "" {
			name = noNameText
		}
		label := name
		if b.Modified {
			label += "*"
		}
		if !strings.Contains(strings.ToLower(label), needle) {
			continue
		}
		rows = append(rows, row{kind: rowBuffer, name: name, source: "buffer", index: i})
	}
	return rows
}

// fileRows turns a ranked result into rows. With no match while the index still
// runs, the loading row shows.
func fileRows(matches []Ranked, loading bool) []row {
	if len(matches) == 0 && loading {
		return loadingRows()
	}
	rows := make([]row, 0, len(matches))
	for _, m := range matches {
		rows = append(rows, row{kind: rowFile, name: m.Path, source: "file", rel: m.Path, hl: m.Indexes})
	}
	return rows
}

// loadingRows is the file mode's only row while the index runs and nothing is
// matched yet.
func loadingRows() []row {
	return []row{{kind: rowMessage, name: loadingText}}
}

// command returns the message that runs the row, or nil for a row that only
// informs.
func (r row) command(root string) tea.Cmd {
	var msg tea.Msg
	switch r.kind {
	case rowCommand:
		msg = RunCommandMsg{Action: r.action}
	case rowFile:
		msg = OpenFileMsg{Path: filepath.Join(root, r.rel), Rel: r.rel}
	case rowBuffer:
		msg = ActivateBufferMsg{Index: r.index}
	case rowLine:
		msg = GotoLineMsg{Line: r.line, Col: r.col}
	default:
		return nil
	}
	return func() tea.Msg { return msg }
}

// emptyText is what an empty box says in the mode of the query.
func emptyText(mode model.Mode) string {
	switch mode {
	case model.ModeCommand:
		return "No matching commands"
	case model.ModeLine:
		return "No matching lines"
	case model.ModeBuffer:
		return "No matching buffers"
	}
	return "No matching files"
}
