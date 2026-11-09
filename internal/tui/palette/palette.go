// Package palette is the command palette overlay of gvim. It is the bottom sheet of
// the frame: a card docked to the last row, with the mode title on its top border,
// the item rows, the query row and the mode hints on its bottom border. Painting and
// hit testing share one geometry function, so the sheet and its click targets cannot
// drift apart.
//
// Inside the card the file mode can paint the selected file's head in a preview column
// on the right, and Live Grep adds a toolbar row of match-mode and scope toggles above
// its rows. Both are regions of the same sheet; a frame too narrow for the preview
// column keeps the full-width list.
//
// The query picks the mode with commands: no prefix searches files, ">"
// runs commands, ":" goes to a line and "#" picks a buffer. Command matching is
// the domain model's, file ranking is the Ranker's, and the list navigation
// (the highlight and the visible window) is palette.Palette's.
//
// Palette runs no action. A chosen row closes the palette and emits one of the
// messages in this package, which the root handles. *Palette implements
// overlay.Overlay directly, so the root stacks it without an adapter.
package palette

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/overlay"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// The palette is an overlay of the root's stack.
var _ overlay.Overlay = (*Palette)(nil)

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
	// hint is the text on the bottom border of the sheet, listing the four modes.
	hint = "file · >command · :line · #buffer"

	// liveHint is the bottom border of Live Grep, which has no prefixes: the whole
	// query is the text searched for, the toolbar row carries the toggles and their
	// keys, and Enter opens the selected row.
	liveHint = "type text to search · Enter open · Esc close"
)

// Files is the file list the file mode searches. *finder.Index satisfies it.
// Snapshot is called from a tea.Cmd, so it must be safe for concurrent use.
type Files interface {
	// Snapshot returns the files found so far and whether the enumeration is
	// still running.
	Snapshot() (files []string, loading bool)
}

// Ranker scores paths against a query and returns at most limit of them, best
// first. The app adapter fills it over finder and fuzzy. Rank
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
	// Commands is the command table, as commands builds it.
	Commands []commands.Entry
	// State is the editor state the commands are checked against. Nil means
	// commands.DefaultState.
	State *commands.State
	// Buffers are the open tabs, in tab order, for the buffer mode.
	Buffers []Buffer
	// Files is the file index. Nil until the index is wired.
	Files Files
	// Ranker scores the file rows. Nil until it is wired; the file mode then shows
	// its loading row.
	Ranker Ranker
	// Remote marks a remote workspace's file mode: Root is the remote workspace and
	// the rows' relative paths are joined with slashes; the caller passes a nil
	// Preview (no local file preview for remote rows).
	Remote bool
	// Theme gives the chrome colors.
	Theme theme.Theme
	// Live opens the palette in Live Grep mode: the whole query is searched as text
	// in the files, and the rows are the matching lines. Grep does the search. Live
	// with a nil Grep shows no rows.
	Live bool
	Grep Grepper
	// LiveSearch is the starting state of the Live Grep match modes. Nil means Fresh's
	// defaults: regex on, whole word off, case folded.
	LiveSearch *SearchOptions
	// SourceBuffers are the open buffers' text, for Live Grep's Buffers scope. Nil means
	// the scope is not available: its toggle is not painted and it stays off.
	SourceBuffers []BufferSource
	// Preview reads the start of a file for the file mode's preview column. Nil paints
	// no column.
	Preview Previewer
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
// and Path is Rel joined with Options.Root. Line and Col are the 1-based position a
// "path:line[:col]" query named, or 0 when it named none.
type OpenFileMsg struct {
	Path, Rel string
	Line, Col int
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
	// ErrText is the file index's error once it finished ("" when it completed): the
	// file rows show it instead of presenting an empty/partial list as the project.
	ErrText string
	// Truncated is the index's truncation state: the file rows say the list is
	// incomplete rather than presenting it as the whole project.
	Truncated bool
	// Notice is the index's non-fatal degradation message (for example a remote fallback
	// from the git file list to a directory walk). It is shown as one informative row; it
	// never hides or disables the results.
	Notice string
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
	rowGrep    // a Live Grep match: rel, line and col
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
	// A grep row keeps its line text apart from name, so the label can be cut around the
	// match: text is the trimmed line, mcol the match's rune offset in it and qlen the
	// match's length in runes.
	text       string
	mcol, qlen int
	// grepSource says whether a grep row is in a file or an open buffer; a buffer row
	// also carries the tab index Enter activates.
	grepSource GrepSource
}

// chrome holds the styles of the palette: the overlay card for the sheet frame, and
// the overlay roles for the rows, the query row and the empty-list message.
type chrome struct {
	ov       style.Overlay // the card frame: border, fill, title and hints
	panel    grid.Style    // the rows and their text
	selected grid.Style    // the highlighted row
	input    grid.Style    // the query row and its text
	muted    grid.Style    // the empty-list message
}

// Palette is the open command palette. Use New to make one.
type Palette struct {
	opts   Options
	state  commands.State
	input  textinput.Model
	rows   []row
	nav    commands.Palette
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
	// live is true in Live Grep mode, which has no prefixes.
	live bool
	// preview is the file mode's file preview: its reader, cache and shown file.
	preview previewState
	// fileLine and fileCol are the position a "path:line[:col]" file query names, both
	// 1-based; 0 when the query names none.
	fileLine, fileCol int
	// grep is the Live Grep state: the match modes, the scope toggles and the last
	// result's count.
	grep grepState
}

// New returns an open palette for opts. The query is the input and its rows are
// computed at once, except that a file query first shows the loading row. The
// box height is known after the first Draw. The ranking of a file query starts
// with Init.
func New(opts Options) *Palette {
	st := commands.DefaultState()
	if opts.State != nil {
		st = *opts.State
	}
	ss := style.New(opts.Theme)
	ov := ss.Overlay
	p := &Palette{
		opts:    opts,
		state:   st,
		open:    true,
		live:    opts.Live,
		preview: previewState{},
		grep:    newGrepState(opts),
		chrome: chrome{
			ov:       ov,
			panel:    ov.Box,
			selected: ov.Selected,
			input:    ov.Idle,
			muted:    ov.Muted,
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
	if !p.open {
		return nil
	}
	if p.live {
		return p.grepCmd(p.seq, p.input.Value())
	}
	if !p.inFile {
		return nil
	}
	return p.rankCmd(p.seq, p.fileText)
}

// Active reports whether the palette is open. It is false after a row was
// chosen or a click ran one, and after an Enter with no rows.
func (p *Palette) Active() bool { return p.open }

// update handles one key or mouse message. Keys edit the query and move the
// highlight; Enter runs the highlighted row. A left click runs the row under the
// pointer, and a click anywhere else is swallowed, so it does not dismiss the
// palette. The wheel scrolls the box by wheelRows and hovering moves the
// highlight. Esc is not sent here: the root pops the palette on Esc.
//
// A file query is ranked by a command, and its result comes back as a
// FileRankedMsg, which Update applies. The returned command emits the message of
// the chosen row, if the row has one, or the next ranking step of the file mode.
func (p *Palette) update(msg tea.Msg) tea.Cmd {
	if !p.open {
		return nil
	}
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		cmd = p.key(msg)
	case tea.PasteMsg:
		cmd = p.edit(msg)
	case FileRankedMsg:
		cmd = p.applyFiles(msg)
	case GrepResultMsg:
		cmd = p.applyGrep(msg)
	case previewMsg:
		p.applyPreview(msg)
	case fileTickMsg:
		if msg.Seq == p.seq && p.inFile {
			cmd = p.rankCmd(p.seq, p.fileText)
		}
	case tea.MouseClickMsg:
		cmd = p.click(tea.Mouse(msg))
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
	default:
		// The clipboard text that Ctrl+V asked for reaches the query line here.
		cmd = p.edit(msg)
	}
	// Every message may have moved the highlight, so the preview of the selected row is
	// read here, after the rows and the highlight are settled.
	if next := p.previewCmd(); next != nil {
		return tea.Batch(cmd, next)
	}
	return cmd
}

// Update handles one message through the overlay contract: it runs update and
// returns the palette, or nil once it is closed so the stack pops it.
func (p *Palette) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	cmd := p.update(msg)
	if !p.Active() {
		return nil, cmd
	}
	return p, cmd
}

// Draw paints the sheet over g: the card frame, the item rows, the query row and
// the hints on the bottom border. It records the frame size, so that a mouse
// position maps to a row. Draw does no ranking: the file rows change only through
// Update.
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

// key runs the key press: a list key moves the highlight or runs the row, a Live Grep
// toggle key flips its mode or scope, and any other key goes to the query line.
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
	if p.live {
		if cmd, ok := p.liveKey(tea.Key(msg).Keystroke()); ok {
			return cmd
		}
	}
	return p.edit(msg)
}

// edit gives msg to the query line and requeries when the query changed. The command of
// Ctrl+V, which reads the clipboard, is returned too.
func (p *Palette) edit(msg tea.Msg) tea.Cmd {
	before := p.input.Value()
	var read tea.Cmd
	p.input, read = p.input.Update(msg)
	if kp, ok := msg.(tea.KeyPressMsg); !ok || tea.Key(kp).Keystroke() != "ctrl+v" {
		read = nil
	}
	if p.input.Value() == before {
		return read
	}
	return tea.Batch(read, p.requery())
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
	return r.command(p.opts.Root, p.opts.Remote)
}

// Covers reports whether the point (x, y) of a w by h frame is on the palette card:
// its rows, its query row and its borders, within the card columns. A press
// elsewhere is not the palette's, and the frame takes it.
func (p *Palette) Covers(x, y, w, h int) bool {
	if !p.open || w <= 0 || h <= 0 {
		return false
	}
	geo := p.layout(w, h)
	return x > geo.left && x < geo.right && y >= geo.top && y <= geo.bottom
}

// rowAt maps a frame cell to the index of the row under it. The row spans the
// box between its borders, and only rows in view count.
func (p *Palette) rowAt(x, y int) (int, bool) {
	if p.height <= 0 {
		return 0, false
	}
	geo := p.layout(p.width, p.height)
	if x <= geo.left || x >= geo.right {
		return 0, false
	}
	if geo.preview && x >= geo.divX {
		return 0, false // the preview column is not a row
	}
	k := y - geo.rowsTop
	first, count := p.nav.Window()
	if k < 0 || k >= geo.rows || k >= count {
		return 0, false
	}
	return first + k, true
}

// syncSize gives the list navigation the row count and the box height. The Live Grep
// toolbar takes one row from the frame, so the height comes from the layout.
func (p *Palette) syncSize() {
	p.nav.SetSize(len(p.rows), layoutForMode(0, p.height, p.layoutMode()).rows)
}

// requery recomputes the rows after the query changed and returns the command
// that ranks a file query. A command, line or buffer query gets its rows at once,
// and the highlight goes back to the top. A file query keeps the rows on screen
// until its result arrives. The first file query shows the loading row.
func (p *Palette) requery() tea.Cmd {
	p.seq++
	query := p.input.Value()
	if p.live {
		return p.liveQuery(p.seq, query)
	}
	mode, text := commands.ParseQuery(query)
	if mode != commands.ModeFile {
		p.inFile = false
		p.fileLine, p.fileCol = 0, 0
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
	// A "path:line[:col]" query ranks the path and names the position the preview
	// highlights and Enter jumps to, as Fresh's file finder documents.
	p.fileText, p.fileLine, p.fileCol = parseFileTarget(text)
	return p.rankCmd(p.seq, p.fileText)
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
		errText := ""
		truncated := false
		notice := ""
		if files != nil {
			list, loading = files.Snapshot()
			if n, ok := files.(interface{ Notice() string }); ok {
				notice = n.Notice()
			}
			if !loading {
				if e, ok := files.(interface{ Err() error }); ok {
					if err := e.Err(); err != nil {
						errText = err.Error()
					}
				}
				if t, ok := files.(interface{ Truncated() bool }); ok {
					truncated = t.Truncated()
				}
			}
		}
		return FileRankedMsg{Seq: seq, Text: text, Matches: rk.Rank(text, list, fileLimit), Loading: loading, ErrText: errText, Truncated: truncated, Notice: notice}
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
	p.rows = fileRows(msg.Matches, msg.Loading, p.fileLine, p.fileCol, msg.ErrText, msg.Truncated, msg.Notice)
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
	mode, text := commands.ParseQuery(query)
	switch mode {
	case commands.ModeCommand:
		return commandRows(commands.SearchIn(p.opts.Commands, query, p.state))
	case commands.ModeLine:
		line, col, ok := editor.ParseLineCol(text)
		if !ok {
			return []row{{kind: rowMessage, name: notLineText(text), disabled: true}}
		}
		return []row{{kind: rowLine, name: "Go to line " + strconv.Itoa(line), line: line, col: col}}
	case commands.ModeBuffer:
		return p.bufferRows(text)
	}
	return nil
}

// notLineText says why text is not a line number, for the ':' mode's message row.
func notLineText(text string) string {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return paletteText("gvim.palette.line_number_example", "Type a line number, for example 120 or 120:5")
	case strings.Trim(text, "0") == "":
		return paletteText("gvim.palette.line_numbers_start", "Line numbers start at 1")
	}
	return paletteTextVars("gvim.prompt.not_a_line", "Not a line number: %{line}", map[string]any{"line": text})
}

// commandRows turns the matched command entries into rows.
func commandRows(entries []commands.Entry) []row {
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
// runs, the loading row shows. line and col are the position the query named, which
// Enter opens at and the preview highlights; both are 0 when it named none.
func fileRows(matches []Ranked, loading bool, line, col int, errText string, truncated bool, notice string) []row {
	if len(matches) == 0 && loading {
		return loadingRows()
	}
	rows := make([]row, 0, len(matches)+3)
	for _, m := range matches {
		rows = append(rows, row{kind: rowFile, name: m.Path, source: "file", rel: m.Path, hl: m.Indexes, line: line, col: col})
	}
	if errText != "" {
		rows = append(rows, row{kind: rowMessage, name: paletteTextVars("gvim.palette.file_search_failed", "File search failed: %{error}", map[string]any{"error": errText}), disabled: true})
	} else if truncated {
		rows = append(rows, row{kind: rowMessage, name: paletteText("gvim.palette.file_list_truncated", "File list truncated: too many files; the list is incomplete"), disabled: true})
	}
	if notice != "" {
		// The notice is already a complete English sentence from the finder port; it is
		// shown verbatim as one informative row (the next i18n pass can extract it).
		rows = append(rows, row{kind: rowMessage, name: notice, disabled: true})
	}
	return rows
}

// loadingRows is the file mode's only row while the index runs and nothing is
// matched yet.
func loadingRows() []row {
	return []row{{kind: rowMessage, name: loadingText}}
}

// filePath joins a file row relative path to the file mode root: local roots use the
// platform join, a remote root keeps slash-separated remote paths (the remote shell and
// the remote file system never see a Windows separator).
func filePath(root, rel string, remote bool) string {
	if !remote {
		return filepath.Join(root, rel)
	}
	return strings.TrimSuffix(root, "/") + "/" + rel
}

// command returns the message that runs the row, or nil for a row that only
// informs.
func (r row) command(root string, remote bool) tea.Cmd {
	var msg tea.Msg
	switch r.kind {
	case rowCommand:
		msg = RunCommandMsg{Action: r.action}
	case rowFile:
		msg = OpenFileMsg{Path: filePath(root, r.rel, remote), Rel: r.rel, Line: r.line, Col: r.col}
	case rowBuffer:
		msg = ActivateBufferMsg{Index: r.index}
	case rowLine:
		msg = GotoLineMsg{Line: r.line, Col: r.col}
	case rowGrep:
		if r.grepSource == GrepBuffers {
			msg = OpenBufferLineMsg{Index: r.index, Line: r.line, Col: r.col}
		} else {
			msg = OpenLineMsg{Path: filePath(root, r.rel, remote), Line: r.line, Col: r.col}
		}
	default:
		return nil
	}
	return func() tea.Msg { return msg }
}

// emptyText is what an empty box says in the mode of the query. Live Grep has its own
// text: a hint before the first search, why nothing can be searched when both sources
// are off, and the no-results state after one.
func (p *Palette) emptyText() string {
	if p.live {
		switch {
		case p.input.Value() == "":
			return paletteText("gvim.palette.type_to_search", "Type to search the files")
		case !p.grep.sources.Files && !p.grep.sources.Buffers && !p.grep.sources.Ignored:
			return paletteText("gvim.palette.turn_on_sources", "Turn on Files or Buffers to search")
		}
		return paletteText("gvim.palette.no_matches", "No matches")
	}
	return modeEmptyText(p.mode())
}

// modeEmptyText is what an empty box says in the mode of the query.
func modeEmptyText(mode commands.Mode) string {
	switch mode {
	case commands.ModeCommand:
		return paletteText("gvim.palette.no_commands", "No matching commands")
	case commands.ModeLine:
		return paletteText("gvim.palette.no_lines", "No matching lines")
	case commands.ModeBuffer:
		return paletteText("gvim.palette.no_buffers", "No matching buffers")
	}
	return paletteText("gvim.palette.no_files", "No matching files")
}
