package searchpanel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/overlay"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// The panel is an overlay of the root's stack.
var (
	_ overlay.Overlay = (*Panel)(nil)
	_ overlay.Caret   = (*Panel)(nil)
)

const (
	// searchDebounce is how long the panel waits after a keystroke before it searches,
	// so typing does not start one search per key.
	searchDebounce = 150 * time.Millisecond
	// gitResultLimit caps git grep, as Fresh's git grep plugin does.
	gitResultLimit = 100
	// fileOpenMark and fileClosedMark head a file row of the expanded and folded states.
	fileOpenMark   = "▾ "
	fileClosedMark = "▸ "
)

// focus is the part of the panel that takes the typed keys.
type focus int

const (
	focusSearch focus = iota
	focusReplace
	focusGlob
	focusResults
)

// rowKind is the kind of one result row.
type rowKind int

const (
	rowFile rowKind = iota
	rowMatch
)

// panelRow is one painted row of the results region: a file header or a match.
type panelRow struct {
	kind  rowKind
	file  int // index in fileMatches
	match int // index in the file's Matches, for a match row
	path  string
	count int
	line  int
	col   int
	text  string
	// hlStart and hlEnd are the matched runes of text, for the bold span.
	hlStart, hlEnd int
}

// styles are the panel's grid styles, derived from the overlay palette.
type styles struct {
	ov       style.Overlay
	label    grid.Style
	active   grid.Style
	field    grid.Style
	selected grid.Style
	stat     grid.Style
	file     grid.Style
	hint     grid.Style
	// syntax maps a syntax kind name to its style for the preview column. It is empty
	// when the root cannot highlight.
	syntax map[string]grid.Style
}

func newStyles(st style.Styles) styles {
	ov := st.Overlay
	return styles{
		ov:       ov,
		label:    ov.Muted,
		active:   ov.Focus,
		field:    ov.Idle,
		selected: ov.Selected,
		stat:     ov.Muted,
		file:     ov.Box,
		hint:     ov.Muted,
	}
}

// Options are the input of New.
type Options struct {
	Root string
	// Owner is the workspace model generation started this panel; every async and
	// root-facing message carries it so a stale model cannot act on another one.
	Owner     palette.Owner
	Files     Files
	OpenFiles []SourceFile
	Current   *SourceFile
	Limit     int
	Git       bool
	Modes     MatchOptions
	Scope     Scope
	Query     string
	// Glob is the initial file filter: comma or space separated patterns (see
	// parseGlob). Empty matches every file.
	Glob string
	// History is the previously searched patterns, most recent first, so a reopened
	// panel can walk them with Up and Down in the search field.
	History  []string
	Searcher Searcher
	Grepper  GitGrepper
	// Previewer reads the selected match's context for the preview column. Nil hides
	// the column.
	Previewer Previewer
	// SyntaxStyles maps a syntax kind name to its grid style, so the preview paints
	// coloured runs without importing the editor pane or the syntax package. Empty means
	// a plain preview.
	SyntaxStyles map[string]grid.Style
	// PreviewWrap starts the preview column with soft wrap on: a line wider than the
	// column continues on the next display row instead of being cut at its edge. Alt+P
	// toggles it in the panel.
	PreviewWrap bool
	Styles      style.Styles
	Theme       theme.Theme
}

// previewWheelRows is how many preview lines one wheel notch scrolls.
const previewWheelRows = 3

// previewBox is the preview column's cell rectangle from the last Draw, for wheel hit
// testing.
type previewBox struct{ x0, x1, y0, y1 int }

func (b previewBox) contains(x, y int) bool {
	return b.x1 > b.x0 && x >= b.x0 && x < b.x1 && y >= b.y0 && y <= b.y1
}

// Panel is the project-wide Search and Replace sheet, or the Git Grep sheet when Git is
// set. The zero value is not usable; call New.
type Panel struct {
	styles styles
	root   string
	owner  palette.Owner
	files  Files
	limit  int
	git    bool

	searcher Searcher
	grepper  GitGrepper

	// previewer reads the selected match's context; preview is the column's state, and
	// previewBox is where the column was last drawn.
	previewer   Previewer
	preview     previewState
	previewBox  previewBox
	previewWrap bool
	// previewContext is set when previewer also implements ContextPreviewer; previewCancel
	// cancels the in-flight preview read when the request is superseded or the panel closes.
	previewContext ContextPreviewer
	previewCancel  context.CancelFunc

	// openFiles and current are the snapshots the searches see for unsaved text. Their
	// source is the root, which refreshes them after a replacement.
	openFiles []SourceFile
	current   *SourceFile

	query, replace, glob textinput.Model
	// matcher is the parsed file filter of the glob field; nil matches every file.
	matcher *globMatcher
	focus   focus
	modes   MatchOptions
	scope   Scope

	open    bool
	seq     int
	loading bool
	// searchCancel cancels the in-flight batched search; nil when nothing streams. batchCh
	// is the live batch channel, read one batch at a time from commands.
	searchCancel chan struct{}
	batchCh      <-chan SearchBatch
	// scanned and totalFiles are the streaming progress the title shows.
	scanned, totalFiles int
	// pendingReset is set when a new stream starts over rows of an older result: the first
	// batch replaces those rows instead of appending to them.
	pendingReset bool
	// notice, when set, replaces the hints on the bottom border, so a summary (a
	// replacement's result, say) shows while the panel covers the status row.
	notice string

	// history is the session's searched patterns, most recent first, and the walk
	// state of Up and Down in the search field: histDraft is what the user had typed
	// before walking, restored when Down passes the newest entry.
	history   []string
	histIndex int
	histDraft string
	histWalk  bool

	fileMatches  []FileMatches
	truncated    bool
	skippedCount int
	skipped      []string
	// resultNotice is a non-fatal warning the search port attached (for example a
	// degraded remote ignore scope); it is shown on the bottom border until a new search.
	resultNotice string
	errText      string
	expanded     []bool
	// checked holds the match-tree checkboxes, parallel to fileMatches: a match row's
	// box is checked for replacement, and a file row's box is its children's.
	checked  [][]bool
	rows     []panelRow
	selected int
	top      int

	width, height int
	mouseGeo      geometry
	optionHits    []optionHit
}

// tickMsg and doneMsg bring a search back to the update loop.
type tickMsg struct {
	seq   int
	owner palette.Owner
}

func (m tickMsg) PaletteOwner() palette.Owner { return m.owner }

type searchDoneMsg struct {
	seq    int
	result SearchResult
	err    error
	owner  palette.Owner
}

func (m searchDoneMsg) PaletteOwner() palette.Owner { return m.owner }

type gitDoneMsg struct {
	seq    int
	result GitResult
	err    error
	owner  palette.Owner
}

func (m gitDoneMsg) PaletteOwner() palette.Owner { return m.owner }

// searchBatchMsg carries one batch of a streaming project search.
type searchBatchMsg struct {
	seq   int
	ch    <-chan SearchBatch
	batch SearchBatch
	ok    bool
	owner palette.Owner
}

func (m searchBatchMsg) PaletteOwner() palette.Owner { return m.owner }

// previewState is the preview column's last target and its fetched context.
type previewState struct {
	seq     int
	req     PreviewRequest
	hasReq  bool
	loading bool
	result  PreviewResult
	err     string
	// viewStart is the first file line the user scrolled the preview to, and scrolled is
	// set once they scrolled away from the match-centred window. A new selected match
	// resets both.
	viewStart int
	scrolled  bool
}

// previewDoneMsg brings a preview back to the update loop.
type previewDoneMsg struct {
	seq    int
	result PreviewResult
	err    error
	owner  palette.Owner
}

func (m previewDoneMsg) PaletteOwner() palette.Owner { return m.owner }

// New returns the panel.
func New(o Options) *Panel {
	p := &Panel{
		styles:      newStyles(o.Styles),
		root:        o.Root,
		owner:       o.Owner,
		files:       o.Files,
		limit:       o.Limit,
		git:         o.Git,
		searcher:    o.Searcher,
		grepper:     o.Grepper,
		previewer:   o.Previewer,
		previewWrap: o.PreviewWrap,
		openFiles:   o.OpenFiles,
		current:     o.Current,
		modes:       o.Modes,
		scope:       o.Scope,
		open:        true,
		top:         0,
	}
	if cp, ok := o.Previewer.(ContextPreviewer); ok {
		p.previewContext = cp
	}
	if p.limit <= 0 {
		p.limit = DefaultLimit
		if p.git {
			p.limit = gitResultLimit
		}
	}
	p.history = append([]string(nil), o.History...)
	p.styles.syntax = o.SyntaxStyles
	p.query = newField(o.Query)
	p.replace = newField("")
	p.glob = newField(o.Glob)
	if m, err := parseGlob(o.Glob); err != nil {
		p.errText = "Bad glob: " + err.Error()
	} else {
		p.matcher = m
	}
	p.query.Focus()
	return p
}

// newField returns a text field with no prompt and no width limit, so the panel can
// scroll it to keep the caret in the room it paints.
func newField(value string) textinput.Model {
	m := textinput.New()
	m.Prompt = ""
	m.SetVirtualCursor(false)
	m.SetWidth(0)
	m.SetValue(value)
	return m
}

// Active reports whether the panel is open.
func (p *Panel) Active() bool { return p.open }

// SetNotice shows text on the bottom border in place of the key hints, until the next
// key. The root uses it for a summary that would otherwise be covered by the panel.
func (p *Panel) SetNotice(text string) { p.notice = text }

// Git reports whether the panel is the read-only Git Grep sheet.
func (p *Panel) Git() bool { return p.git }

// Init returns the command of the initial query, if any.
func (p *Panel) Init() tea.Cmd {
	if p.query.Value() == "" {
		return nil
	}
	return p.requery()
}

// Update handles one message: a key, or a search result coming back.
func (p *Panel) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return p.key(msg)
	case tickMsg:
		if msg.owner != p.owner || msg.seq != p.seq || p.query.Value() == "" {
			return p, nil
		}
		return p, p.searchCmd(msg.seq)
	case searchDoneMsg:
		p.applySearch(msg)
		return p, p.previewCmd()
	case gitDoneMsg:
		p.applyGit(msg)
		return p, p.previewCmd()
	case searchBatchMsg:
		// A batch can fill an empty result while the rows already have the focus; the
		// preview follows the new selected row. The target dedupe keeps this cheap.
		return p, tea.Batch(p.applyBatch(msg), p.previewCmd())
	case tea.MouseClickMsg:
		return p, p.mouseClick(msg)
	case tea.MouseWheelMsg:
		return p, p.previewWheel(msg)
	case previewDoneMsg:
		if msg.owner != p.owner {
			return p, nil
		}
		if msg.seq == p.preview.seq {
			p.preview.loading = false
			p.previewCancel = nil
			if msg.err != nil {
				p.preview.err = msg.err.Error()
				p.preview.result = PreviewResult{}
			} else {
				p.preview.result = msg.result
				p.preview.err = msg.result.Err
			}
		}
		return p, nil
	}
	return p, nil
}

type optionHit struct {
	x0, x1 int
	key    tea.KeyPressMsg
}

func (p *Panel) mouseClick(msg tea.MouseClickMsg) tea.Cmd {
	m := msg.Mouse()
	if m.Button != tea.MouseLeft || p.width == 0 || p.height == 0 {
		return nil
	}
	g := p.mouseGeo
	if m.Y == g.optY {
		for _, hit := range p.optionHits {
			if m.X >= hit.x0 && m.X < hit.x1 {
				_, cmd := p.key(hit.key)
				return cmd
			}
		}
		return nil
	}
	if m.X < g.x0 || m.X >= g.end {
		return nil
	}
	target := focusResults
	switch m.Y {
	case g.searchY:
		target = focusSearch
	case g.replY:
		if p.git {
			return nil
		}
		target = focusReplace
	case g.globY:
		target = focusGlob
	default:
		if m.Y < g.resultsTop || m.Y >= g.resultsTop+g.results || m.X >= g.listEnd || p.errText != "" || p.query.Value() == "" {
			return nil
		}
		i := p.top + m.Y - g.resultsTop
		if i < 0 || i >= len(p.rows) {
			return nil
		}
		for p.focus != focusResults {
			p.cycleFocus(1)
		}
		p.selectRow(i)
		p.notice = ""
		if m.X < g.x0+3 {
			p.toggleChecked()
			return p.previewCmd()
		}
		cmd := p.activate()
		if cmd != nil {
			return cmd
		}
		return p.previewCmd()
	}
	for p.focus != target {
		p.cycleFocus(1)
	}
	p.notice = ""
	return nil
}

// key runs one key press.
func (p *Panel) key(kp tea.KeyPressMsg) (overlay.Overlay, tea.Cmd) {
	name := tea.Key(kp).Keystroke()
	p.notice = ""
	switch name {
	case "esc":
		p.cancelBatches()
		p.open = false
		return nil, nil
	case "tab":
		p.cycleFocus(1)
		return p, p.previewCmd()
	case "shift+tab":
		p.cycleFocus(-1)
		return p, p.previewCmd()
	case "alt+c":
		p.modes.CaseSensitive = !p.modes.CaseSensitive
		return p, p.requery()
	case "alt+a":
		if p.git {
			return p, nil
		}
		// All Files -> Open Buffers -> Current File -> All Files.
		p.scope = Scope((int(p.scope) + 1) % 3)
		return p, p.requery()
	case "alt+r":
		if p.git {
			return p, nil
		}
		p.modes.Regex = !p.modes.Regex
		return p, p.requery()
	case "alt+w":
		if p.git {
			return p, nil
		}
		p.modes.WholeWord = !p.modes.WholeWord
		return p, p.requery()
	case "alt+p":
		// Toggle soft wrap in the preview column: the same preview, one row per file
		// line or as many display rows as the line needs.
		p.previewWrap = !p.previewWrap
		return p, nil
	case "alt+enter":
		return p, p.replaceCmd(false)
	case "alt+q":
		return p, p.exportCmd()
	}
	if p.focus == focusSearch && (name == "up" || name == "down") {
		return p, p.walkHistory(name == "up")
	}
	if p.focus == focusResults {
		// A key that runs a command (Enter, Shift+Enter) keeps its command; the others
		// move or fold, so the preview follows the new selection.
		if cmd := p.resultsKey(name); cmd != nil {
			return p, cmd
		}
		return p, p.previewCmd()
	}
	// A field takes the key. A change of the search text starts a new search after the
	// debounce; a change of the replacement text changes nothing until it is applied.
	f := p.field()
	before := f.Value()
	next, cmd := f.Update(kp)
	*f = next
	if (p.focus == focusSearch && p.query.Value() != before) ||
		(p.focus == focusGlob && p.glob.Value() != before) {
		return p, p.requery()
	}
	if p.focus == focusReplace && p.replace.Value() != before {
		p.errText = ""
	}
	return p, cmd
}

// resultsKey runs a key while the result rows have the focus.
func (p *Panel) resultsKey(name string) tea.Cmd {
	switch name {
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "home":
		p.selectRow(0)
	case "end":
		p.selectRow(len(p.rows) - 1)
	case "enter":
		return p.activate()
	case "space":
		p.toggleChecked()
	case "a", "A":
		p.setAllChecked(true)
	case "n", "N":
		p.setAllChecked(false)
	case "shift+enter":
		return p.replaceCmd(true)
	case "pgup":
		return p.scrollPreview(-p.previewPage())
	case "pgdown":
		return p.scrollPreview(p.previewPage())
	case "ctrl+home":
		return p.scrollPreviewTo(0)
	case "ctrl+end":
		return p.scrollPreviewTo(p.previewMaxStart())
	}
	return nil
}

// exportCmd asks the root to show the current result set as a navigable list.
func (p *Panel) exportCmd() tea.Cmd {
	if p.git {
		if len(p.fileMatches) == 0 {
			p.errText = "No Git Grep results to list"
			return nil
		}
	} else if len(p.fileMatches) == 0 {
		p.errText = "No matches to list"
		return nil
	}
	// The export opens its list over the panel's result view: an in-flight preview read
	// is no longer needed and is cancelled before the command returns.
	p.cancelPreviewRead()
	owner := p.owner
	return func() tea.Msg { return ExportMsg{Owner: owner} }
}

// ExportItems returns the current result set as flat list rows for the root's list
// overlay: a file header before each file's matches, every match of every file, folded
// or not. The panel keeps ownership of the result; the list is a read-only view that
// jumps through the same positions.
func (p *Panel) ExportItems() []ExportItem {
	items, _ := p.ExportItemsCapped(0)
	return items
}

// ExportItemsCapped is ExportItems with an independent bound on exported matches: it
// stops after limit match rows (a file header is added only for files that contribute
// one) and returns how many matches were left out. A limit <= 0 means no bound, which
// is what ExportItems uses for the panel's existing callers. The root passes a cap so a
// 10000-match project search cannot build an unbounded quickfix list; the omitted count
// becomes the panel notice.
func (p *Panel) ExportItemsCapped(limit int) ([]ExportItem, int) {
	if limit <= 0 {
		limit = int(^uint(0) >> 1)
	}
	var out []ExportItem
	exported, omitted := 0, 0
	for _, f := range p.fileMatches {
		if len(f.Matches) == 0 {
			continue
		}
		remaining := limit - exported
		if remaining <= 0 {
			omitted += len(f.Matches)
			continue
		}
		take := min(len(f.Matches), remaining)
		out = append(out, ExportItem{Header: true, Name: fmt.Sprintf("%s (%d)", f.Name, len(f.Matches))})
		for _, m := range f.Matches[:take] {
			out = append(out, ExportItem{
				Path: f.Path,
				Line: m.Line,
				Col:  m.Col,
				Text: fmt.Sprintf("%d:%d  %s", m.Line, m.Col, strings.TrimRight(m.Text, " ")),
			})
		}
		exported += take
		omitted += len(f.Matches) - take
	}
	return out, omitted
}

// History returns the session's searched patterns, most recent first, for the root to
// pass to the next panel.
func (p *Panel) History() []string { return append([]string(nil), p.history...) }

// historyCap is the most patterns the panel keeps.
const historyCap = 20

// recordHistory puts a settled search pattern at the front of the history, once, and
// drops the oldest past the cap. It does nothing while the field is walking history.
func (p *Panel) recordHistory(pattern string) {
	if p.histWalk || strings.TrimSpace(pattern) == "" {
		return
	}
	for i, old := range p.history {
		if old == pattern {
			p.history = append(p.history[:i], p.history[i+1:]...)
			break
		}
	}
	p.history = append([]string{pattern}, p.history...)
	if len(p.history) > historyCap {
		p.history = p.history[:historyCap]
	}
}

// walkHistory applies the next older (up) or newer (down) pattern to the search field
// and searches for it. Down past the newest entry puts back the draft the user had
// typed before walking.
func (p *Panel) walkHistory(up bool) tea.Cmd {
	if len(p.history) == 0 {
		return nil
	}
	if !p.histWalk {
		if !up {
			return nil
		}
		p.histWalk = true
		p.histIndex = 0
		p.histDraft = p.query.Value()
	} else if up {
		if p.histIndex >= len(p.history)-1 {
			return nil
		}
		p.histIndex++
	} else {
		if p.histIndex == 0 {
			p.histWalk = false
			p.query.SetValue(p.histDraft)
			p.histDraft = ""
			return p.requery()
		}
		p.histIndex--
	}
	p.query.SetValue(p.history[p.histIndex])
	return p.requery()
}

// selectedMatch returns the match the preview follows: the selected match row's match,
// or the first match of the selected file row.
func (p *Panel) selectedMatch() (Match, FileMatches, bool) {
	if len(p.rows) == 0 {
		return Match{}, FileMatches{}, false
	}
	r := p.rows[clamp(p.selected, 0, len(p.rows)-1)]
	if r.file < 0 || r.file >= len(p.fileMatches) {
		return Match{}, FileMatches{}, false
	}
	f := p.fileMatches[r.file]
	if len(f.Matches) == 0 {
		return Match{}, FileMatches{}, false
	}
	if r.kind == rowFile {
		return f.Matches[0], f, true
	}
	if r.match < 0 || r.match >= len(f.Matches) {
		return Match{}, FileMatches{}, false
	}
	return f.Matches[r.match], f, true
}

// previewTarget builds the preview request of the selected row. The open buffer's
// snapshot text travels with it, so an unsaved file previews without a disk read.
func (p *Panel) previewTarget() (PreviewRequest, bool) {
	if p.previewer == nil {
		return PreviewRequest{}, false
	}
	m, f, ok := p.selectedMatch()
	if !ok {
		return PreviewRequest{}, false
	}
	endLine, endCol := m.EndLine, m.EndCol
	if !m.MultiLine() || endLine <= 0 {
		endLine, endCol = m.Line, m.Col+max(m.Len, 1)
	}
	req := PreviewRequest{
		Path: f.Path, Name: f.Name, Dirty: f.Dirty,
		Line: m.Line, Col: m.Col, EndLine: endLine, EndCol: endCol,
		Start: -1, Lines: p.previewFetchLines(),
	}
	for _, s := range p.openFiles {
		if s.Path != "" && s.Path == f.Path {
			req.Text = s.Text
			break
		}
	}
	return req, true
}

// previewContextLines is the most context lines the preview column can show: the result
// rows minus the header row.
func (p *Panel) previewContextLines() int {
	if p.width > 0 && p.height > 0 {
		if geo := layoutFor(p.width, p.height, p.git); geo.results > 1 {
			return geo.results - 1
		}
	}
	return 18
}

// wantsPreview reports whether the preview column should be visible: the result rows
// have the focus, there are rows, and a previewer is wired.
func (p *Panel) wantsPreview() bool {
	return p.previewer != nil && p.focus == focusResults && len(p.rows) > 0
}

// samePreview reports whether two preview requests name the same context. The line
// capacity is not part of the identity: a bigger column can paint more of the lines the
// same result already has, and a smaller one shows fewer.
func samePreview(a, b PreviewRequest) bool {
	a.Lines, b.Lines = 0, 0
	return a == b
}

// previewCmd fetches the selected row's context unless the column already shows it. It
// is the only path that reads a file, and it runs as a tea.Cmd.
func (p *Panel) previewCmd() tea.Cmd {
	req, ok := p.previewTarget()
	if !ok || !p.wantsPreview() {
		p.cancelPreviewRead()
		if p.preview.hasReq {
			p.preview.seq++
			p.preview.hasReq = false
			p.preview.loading = false
			p.preview.result = PreviewResult{}
			p.preview.err = ""
			p.preview.scrolled = false
		}
		return nil
	}
	// A newly selected match resets the scroll to the match-centred window.
	if !p.preview.hasReq || !sameMatch(p.preview.req, req) {
		p.preview.scrolled = false
		p.preview.viewStart = 0
		p.preview.result = PreviewResult{}
	}
	if p.preview.scrolled {
		req.Start = p.preview.viewStart
	} else {
		req.Start = -1
	}
	if p.preview.hasReq && samePreview(p.preview.req, req) && (p.preview.loading || p.preview.err == "") {
		return nil
	}
	// Supersede any read still running for the previous request; its answer is dropped
	// by the new sequence too. A context-capable previewer gets a cancellation channel
	// for the file/authority read itself.
	p.cancelPreviewRead()
	p.preview.seq++
	seq, owner := p.preview.seq, p.owner
	p.preview.req = req
	p.preview.hasReq = true
	p.preview.loading = true
	p.preview.err = ""
	// Prefer the window seam: a remote preview reads around the match through the
	// authority instead of only the file head. The context-capable form cancels the
	// authority read when a newer selection supersedes it.
	if cw, ok := p.previewer.(ContextWindowPreviewer); ok {
		ctx, cancel := context.WithCancel(context.Background())
		p.previewCancel = cancel
		return func() tea.Msg {
			res, err := cw.PreviewWindowContext(ctx, req)
			return previewDoneMsg{seq: seq, result: res, err: err, owner: owner}
		}
	}
	if w, ok := p.previewer.(WindowPreviewer); ok {
		return func() tea.Msg {
			res, err := w.PreviewWindow(req)
			return previewDoneMsg{seq: seq, result: res, err: err, owner: owner}
		}
	}
	if cp := p.previewContext; cp != nil {
		ctx, cancel := context.WithCancel(context.Background())
		p.previewCancel = cancel
		return func() tea.Msg {
			res, err := cp.PreviewContext(ctx, req)
			return previewDoneMsg{seq: seq, result: res, err: err, owner: owner}
		}
	}
	rev := p.previewer
	return func() tea.Msg {
		res, err := rev.Preview(req)
		return previewDoneMsg{seq: seq, result: res, err: err}
	}
}

// cancelPreviewRead cancels the context-capable preview read in flight, if any. The
// caller also invalidates/advances the preview sequence when the result must be dropped.
func (p *Panel) cancelPreviewRead() {
	if p.previewCancel != nil {
		p.previewCancel()
		p.previewCancel = nil
	}
}

// sameMatch reports whether two preview requests name the same selected match, ignoring
// the requested window: scrolling keeps the match, so it does not reset the scroll.
func sameMatch(a, b PreviewRequest) bool {
	return a.Path == b.Path && a.Dirty == b.Dirty &&
		a.Line == b.Line && a.Col == b.Col && a.EndLine == b.EndLine && a.EndCol == b.EndCol
}

// previewFetchLines is how many lines the panel asks for at a time: the visible context
// lines plus a page of margin on each side, bounded so the cached window stays small.
func (p *Panel) previewFetchLines() int {
	h := p.previewContextLines()
	if h <= 0 {
		return 0
	}
	return min(max(h*3, h), 60)
}

// previewPage is how many lines one PgUp or PgDn scrolls the preview.
func (p *Panel) previewPage() int {
	h := p.previewContextLines()
	if h <= 0 {
		return 1
	}
	return h
}

// previewVisibleStart is the first file line the painted preview window shows.
func (p *Panel) previewVisibleStart() int {
	if p.preview.scrolled {
		return p.preview.viewStart
	}
	return p.preview.result.Start
}

// previewMaxStart is the last scroll position that still fills the window.
func (p *Panel) previewMaxStart() int {
	total := p.preview.result.Total
	if total <= 0 {
		return max(p.previewVisibleStart(), 0)
	}
	return max(total-p.previewPage(), 0)
}

// scrollPreview scrolls the preview by delta lines and refetches only when the new window
// is outside the cached one.
func (p *Panel) scrollPreview(delta int) tea.Cmd {
	if !p.wantsPreview() || p.preview.loading && len(p.preview.result.Lines) == 0 {
		return nil
	}
	target := p.previewVisibleStart() + delta
	if limit := p.previewMaxStart(); p.preview.result.Total > 0 {
		target = clamp(target, 0, limit)
	} else {
		target = max(target, 0)
	}
	if target == p.previewVisibleStart() {
		return nil
	}
	p.preview.scrolled = true
	p.preview.viewStart = target
	h := p.previewPage()
	r := p.preview.result
	if r.Start <= target && target+h <= r.Start+len(r.Lines) {
		return nil
	}
	return p.previewCmd()
}

// scrollPreviewTo scrolls the preview to an absolute first line.
func (p *Panel) scrollPreviewTo(first int) tea.Cmd {
	if !p.wantsPreview() || p.preview.loading && len(p.preview.result.Lines) == 0 {
		return nil
	}
	target := clamp(first, 0, p.previewMaxStart())
	if target == p.previewVisibleStart() {
		return nil
	}
	p.preview.scrolled = true
	p.preview.viewStart = target
	h := p.previewPage()
	r := p.preview.result
	if r.Start <= target && target+h <= r.Start+len(r.Lines) {
		return nil
	}
	return p.previewCmd()
}

// previewWheel scrolls the preview when the wheel notch is over its column.
func (p *Panel) previewWheel(msg tea.MouseWheelMsg) tea.Cmd {
	mo := msg.Mouse()
	if !p.previewBox.contains(mo.X, mo.Y) {
		return nil
	}
	switch mo.Button {
	case tea.MouseWheelUp:
		return p.scrollPreview(-previewWheelRows)
	case tea.MouseWheelDown:
		return p.scrollPreview(previewWheelRows)
	}
	return nil
}

// previewWindow returns the first file line and the number of lines the painted preview
// window should show, clamped to the cached result.
func (p *Panel) previewWindow(height int) (first, count int) {
	s := p.preview
	if len(s.result.Lines) == 0 || height <= 0 {
		return s.result.Start, 0
	}
	first = s.result.Start
	if s.scrolled {
		first = s.viewStart
	}
	offset := first - s.result.Start
	if offset < 0 {
		offset, first = 0, s.result.Start
	}
	if offset >= len(s.result.Lines) {
		return first, 0
	}
	return first, min(height, len(s.result.Lines)-offset)
}

// field returns the focused text field.
func (p *Panel) field() *textinput.Model {
	switch p.focus {
	case focusReplace:
		if !p.git {
			return &p.replace
		}
	case focusGlob:
		return &p.glob
	}
	return &p.query
}

// cycleFocus moves the focus by delta around the areas of the panel. Git Grep has no
// replacement field.
func (p *Panel) cycleFocus(delta int) {
	stops := []focus{focusSearch, focusReplace, focusGlob, focusResults}
	if p.git {
		stops = []focus{focusSearch, focusGlob, focusResults}
	}
	at := 0
	for i, f := range stops {
		if f == p.focus {
			at = i
		}
	}
	at = (at + delta + len(stops)) % len(stops)
	p.focus = stops[at]
	switch p.focus {
	case focusSearch:
		p.query.Focus()
		p.replace.Blur()
		p.glob.Blur()
	case focusReplace:
		p.query.Blur()
		p.replace.Focus()
		p.glob.Blur()
	case focusGlob:
		p.query.Blur()
		p.replace.Blur()
		p.glob.Focus()
	default:
		p.query.Blur()
		p.replace.Blur()
		p.glob.Blur()
	}
}

// requery starts the debounce of a new search. An empty query clears the result and
// searches nothing.
func (p *Panel) requery() tea.Cmd {
	p.cancelBatches()
	p.loading = false
	p.scanned, p.totalFiles = 0, 0
	p.pendingReset = len(p.fileMatches) > 0
	p.seq++
	p.errText = ""
	// The glob field is validated in both modes: project search uses the compiled matcher
	// on its file snapshot, Git Grep passes the raw patterns as pathspecs.
	m, err := parseGlob(p.glob.Value())
	if err != nil {
		p.matcher = nil
		p.errText = "Bad glob: " + err.Error()
		p.loading = false
		p.fileMatches, p.rows, p.checked = nil, nil, nil
		p.truncated, p.skippedCount, p.skipped = false, 0, nil
		p.resultNotice = ""
		p.selected, p.top = 0, 0
		return nil
	}
	p.matcher = m
	if p.query.Value() == "" {
		p.loading = false
		p.fileMatches, p.rows = nil, nil
		p.truncated, p.skippedCount, p.skipped = false, 0, nil
		p.resultNotice = ""
		p.selected, p.top = 0, 0
		return nil
	}
	p.loading = true
	seq := p.seq
	return tea.Tick(searchDebounce, func(time.Time) tea.Msg { return tickMsg{seq: seq, owner: p.owner} })
}

// ReSearch re-runs the current search immediately, without the debounce. The root uses
// it after a replacement, so the shown results are planned from the new text.
func (p *Panel) ReSearch() tea.Cmd {
	if p.query.Value() == "" {
		return nil
	}
	p.cancelBatches()
	p.loading = false
	p.scanned, p.totalFiles = 0, 0
	p.pendingReset = len(p.fileMatches) > 0
	p.seq++
	p.errText = ""
	p.loading = true
	return p.searchCmd(p.seq)
}

// searchCmd returns the command that runs the current search. The file index snapshot is
// read in the command, so the update loop does no IO.
func (p *Panel) searchCmd(seq int) tea.Cmd {
	owner := p.owner
	query := p.query.Value()
	if p.git {
		g := p.grepper
		req := GitRequest{
			Root: p.root, Query: query, CaseSensitive: p.modes.CaseSensitive,
			Pathspecs: globPatterns(p.glob.Value()), Limit: p.limit,
		}
		if g == nil {
			return func() tea.Msg {
				return gitDoneMsg{seq: seq, err: fmt.Errorf("git grep is not supported in this port"), owner: owner}
			}
		}
		return func() tea.Msg {
			res, err := g.GitGrep(req)
			return gitDoneMsg{seq: seq, result: res, err: err, owner: owner}
		}
	}
	s := p.searcher
	files := p.files
	matcher := p.matcher
	req := SearchRequest{
		Root:      p.root,
		Query:     query,
		Options:   p.modes,
		Scope:     p.scope,
		Current:   p.current,
		OpenFiles: p.openFiles,
		Limit:     p.limit,
		Glob:      p.glob.Value(),
	}
	if s == nil {
		return func() tea.Msg {
			return searchDoneMsg{seq: seq, err: fmt.Errorf("project search is not supported in this port"), owner: owner}
		}
	}
	if bs, ok := s.(BatchSearcher); ok {
		// The streaming path: one goroutine walks the files and the panel reads its
		// batches one command at a time. The cancel channel stops it when the search is
		// superseded or the panel closes.
		p.cancelBatches()
		cancel := make(chan struct{})
		p.searchCancel = cancel
		return func() tea.Msg {
			if files != nil {
				var st FileIndexState
				req.Files, st = SnapshotWithState(files)
				req.FilesKnown, req.FilesReady = st.Known, st.Ready
				req.FilesUnknown, req.FilesIdle, req.FilesTruncated, req.FilesClosed = st.Unknown, st.Idle, st.Truncated, st.Closed
				req.FilesError, req.FilesNotice = st.Error, st.Notice
			}
			req.Files = matcher.filter(req.Files)
			ch := bs.ProjectSearchBatches(req, cancel)
			batch, ok := <-ch
			return searchBatchMsg{seq: seq, ch: ch, batch: batch, ok: ok, owner: owner}
		}
	}
	return func() tea.Msg {
		if files != nil {
			var st FileIndexState
			req.Files, st = SnapshotWithState(files)
			req.FilesKnown, req.FilesReady = st.Known, st.Ready
			req.FilesUnknown, req.FilesIdle, req.FilesTruncated, req.FilesClosed = st.Unknown, st.Idle, st.Truncated, st.Closed
			req.FilesError, req.FilesNotice = st.Error, st.Notice
		}
		req.Files = matcher.filter(req.Files)
		res, err := s.ProjectSearch(req)
		return searchDoneMsg{seq: seq, result: res, err: err, owner: owner}
	}
}

// waitBatch returns the command that reads the next batch of the live stream.
func waitBatch(seq int, ch <-chan SearchBatch, owner palette.Owner) tea.Cmd {
	return func() tea.Msg {
		batch, ok := <-ch
		return searchBatchMsg{seq: seq, ch: ch, batch: batch, ok: ok, owner: owner}
	}
}

// applyBatch appends one streaming batch and keeps reading, or finishes the search on the
// done batch. A batch for an older search is dropped (its stream was cancelled).
func (p *Panel) applyBatch(msg searchBatchMsg) tea.Cmd {
	if msg.owner != p.owner || msg.seq != p.seq {
		return nil
	}
	if !msg.ok {
		// The stream closed without a done batch: it was cancelled.
		p.loading = false
		p.batchCh = nil
		p.searchCancel = nil
		return nil
	}
	p.batchCh = msg.ch
	p.scanned, p.totalFiles = msg.batch.Scanned, msg.batch.Total
	if p.pendingReset && msg.seq == p.seq {
		// The first batch of a new search replaces the rows of the older result, so the
		// stale rows never mix with the streamed ones.
		p.fileMatches, p.rows, p.checked, p.expanded = nil, nil, nil, nil
		p.selected, p.top = 0, 0
		p.pendingReset = false
	}
	if len(msg.batch.Files) > 0 {
		p.appendBatchFiles(msg.batch.Files)
	}
	p.truncated = p.truncated || msg.batch.Truncated
	if msg.batch.SkippedCount > 0 || len(msg.batch.Skipped) > 0 {
		p.skippedCount, p.skipped = msg.batch.SkippedCount, msg.batch.Skipped
	}
	if msg.batch.Notice != "" {
		p.resultNotice = msg.batch.Notice
	}
	if msg.batch.Done {
		p.loading = false
		p.batchCh = nil
		p.searchCancel = nil
		if msg.batch.Err != "" {
			p.errText = msg.batch.Err
		}
		p.recordHistory(p.query.Value())
		return nil
	}
	return waitBatch(msg.seq, msg.ch, p.owner)
}

// appendBatchFiles adds whole files from a streaming batch. Existing rows do not move, so
// the highlighted row and the window stay where the user left them.
func (p *Panel) appendBatchFiles(files []FileMatches) {
	wasEmpty := len(p.fileMatches) == 0
	for _, f := range files {
		p.fileMatches = append(p.fileMatches, f)
		p.expanded = append(p.expanded, true)
		checked := make([]bool, len(f.Matches))
		for i := range checked {
			checked[i] = true
		}
		p.checked = append(p.checked, checked)
	}
	if wasEmpty {
		p.selected, p.top = 0, 0
	}
	p.rebuildRows()
}

// cancelBatches stops the live stream, if any, and forgets it. The search goroutine sees
// the closed channel and stops sending.
func (p *Panel) cancelBatches() {
	if p.searchCancel != nil {
		close(p.searchCancel)
		p.searchCancel = nil
	}
	p.batchCh = nil
}

// CancelWork cancels the in-flight search stream and preview read without closing the
// panel. The daemon workspace host calls it before its model is hidden (switch, evict,
// close) so a hidden model does no further background search work; the results already
// on screen stay, and a later return can search again. The sequence counters are
// advanced so a late result cannot paint into the panel.
func (p *Panel) CancelWork() {
	p.cancelBatches()
	p.cancelPreviewRead()
	p.seq++
	p.preview.seq++
	p.preview.loading = false
	p.loading = false
}

// Dismiss closes the panel and cancels its stream; the root calls it when Esc closes the
// overlay.
func (p *Panel) Dismiss() {
	p.cancelBatches()
	p.cancelPreviewRead()
	p.preview.seq++
	p.preview.loading = false
	p.open = false
}

// applySearch shows a project search result when it answers the current search.
func (p *Panel) applySearch(msg searchDoneMsg) {
	if msg.owner != p.owner || msg.seq != p.seq {
		return
	}
	p.loading = false
	if msg.err != nil {
		p.errText = msg.err.Error()
		p.fileMatches, p.rows = nil, nil
		p.truncated, p.skippedCount, p.skipped = false, 0, nil
		p.resultNotice = ""
		p.selected, p.top = 0, 0
		return
	}
	p.fileMatches = msg.result.Files
	p.truncated = msg.result.Truncated
	p.skippedCount = msg.result.SkippedCount
	p.skipped = msg.result.Skipped
	p.resultNotice = msg.result.Notice
	p.errText = msg.result.Err
	p.pendingReset = false
	p.recordHistory(p.query.Value())
	p.resetRows()
}

// applyGit shows a git grep result when it answers the current search.
func (p *Panel) applyGit(msg gitDoneMsg) {
	if msg.owner != p.owner || msg.seq != p.seq {
		return
	}
	p.loading = false
	if msg.err != nil {
		p.errText = msg.err.Error()
		p.fileMatches, p.rows = nil, nil
		p.truncated, p.skippedCount, p.skipped = false, 0, nil
		p.selected, p.top = 0, 0
		return
	}
	p.fileMatches = msg.result.Files
	p.truncated = false
	p.skippedCount, p.skipped = 0, nil
	p.errText = msg.result.Err
	p.pendingReset = false
	p.resetRows()
}

// resetRows rebuilds the rows of a new result: every file starts expanded, every match
// starts checked, and the first row is selected.
func (p *Panel) resetRows() {
	p.expanded = make([]bool, len(p.fileMatches))
	p.checked = make([][]bool, len(p.fileMatches))
	for i, f := range p.fileMatches {
		p.expanded[i] = true
		p.checked[i] = make([]bool, len(f.Matches))
		for j := range p.checked[i] {
			p.checked[i][j] = true
		}
	}
	p.selected, p.top = 0, 0
	p.rebuildRows()
}

// rebuildRows flattens the file and match rows, honoring the folded files, and keeps the
// selection in range.
func (p *Panel) rebuildRows() {
	p.rows = p.rows[:0]
	for i, f := range p.fileMatches {
		p.rows = append(p.rows, panelRow{kind: rowFile, file: i, path: f.Name, count: len(f.Matches)})
		if i < len(p.expanded) && !p.expanded[i] {
			continue
		}
		for j, m := range f.Matches {
			text, start, end := matchText(m)
			p.rows = append(p.rows, panelRow{
				kind: rowMatch, file: i, match: j,
				line: m.Line, col: m.Col, text: text, hlStart: start, hlEnd: end,
			})
		}
	}
	p.selected = clamp(p.selected, 0, max(len(p.rows)-1, 0))
	p.clampTop()
}

// matchText makes one match's line paintable: tabs become four spaces and the matched
// runes are returned as the span to bold.
func matchText(m Match) (text string, hlStart, hlEnd int) {
	line := strings.TrimRight(m.Text, " ")
	start := max(m.Col-1, 0)
	end := start + max(m.Len, 1)
	var b strings.Builder
	disp := 0
	hlStart, hlEnd = -1, -1
	for i, r := range []rune(line) {
		if i == start {
			hlStart = disp
		}
		if i == end {
			hlEnd = disp
		}
		if r == '\t' {
			b.WriteString("    ")
			disp += 4
			continue
		}
		b.WriteRune(r)
		disp++
	}
	if hlStart < 0 {
		hlStart = disp
	}
	if hlEnd < 0 {
		hlEnd = disp
	}
	return b.String(), hlStart, hlEnd
}

// activate runs the selected row: a file row folds or unfolds its matches, and a match
// row sends the position it names to the root.
func (p *Panel) activate() tea.Cmd {
	if p.selected < 0 || p.selected >= len(p.rows) {
		return nil
	}
	r := p.rows[p.selected]
	if r.kind == rowFile {
		if r.file < len(p.expanded) {
			p.expanded[r.file] = !p.expanded[r.file]
		}
		p.rebuildRows()
		return nil
	}
	f := p.fileMatches[r.file]
	owner := p.owner
	return func() tea.Msg {
		return OpenMsg{Path: f.Path, Line: r.line, Col: r.col, Owner: owner}
	}
}

// replaceCmd asks the root to confirm and apply a replacement. Replace All (Alt+Enter)
// replaces every checked match; the scoped form (Shift+Enter) replaces the focused row
// alone: a file row means its checked matches, a match row means that single match.
func (p *Panel) replaceCmd(scoped bool) tea.Cmd {
	if p.git {
		p.errText = "Git Grep is read-only"
		return nil
	}
	var req ReplaceRequest
	var ok bool
	if scoped {
		req, ok = p.ScopedReplaceRequest()
	} else {
		if matches, _ := p.ReplaceCount(); matches == 0 {
			if total, _, _ := p.Stats(); total > 0 {
				p.errText = "No matches selected"
			} else {
				p.errText = "No matches to replace"
			}
			return nil
		}
		req, ok = p.ReplaceRequest()
	}
	if !ok {
		return nil
	}
	owner := p.owner
	return func() tea.Msg { return ReplaceRequestMsg{Request: req, Owner: owner} }
}

// ReplaceRequest returns the replacement of the checked matches. ok is false in Git Grep
// mode, with no query, or when no match is checked.
func (p *Panel) ReplaceRequest() (ReplaceRequest, bool) {
	if p.git || p.query.Value() == "" {
		return ReplaceRequest{}, false
	}
	var files []ReplaceFile
	for i, f := range p.fileMatches {
		var selected []Position
		for j, m := range f.Matches {
			if p.matchChecked(i, j) {
				selected = append(selected, Position{Line: m.Line - 1, Col: m.Col - 1})
			}
		}
		if len(selected) == 0 {
			continue
		}
		files = append(files, ReplaceFile{
			Path: f.Path, Name: f.Name, Hash: f.Hash, Dirty: f.Dirty, Selected: selected,
		})
	}
	if len(files) == 0 {
		return ReplaceRequest{}, false
	}
	matches, _ := p.ReplaceCount()
	return ReplaceRequest{
		Root:        p.root,
		Query:       p.query.Value(),
		Replacement: p.replace.Value(),
		Options:     p.modes,
		Files:       files,
		Matches:     matches,
		Limit:       p.limit,
	}, true
}

// ScopedReplaceRequest returns the replacement of the focused result row alone: for a
// file row, its checked matches; for a match row, that match when it is checked. It sets
// the notice that says why there is nothing to replace.
func (p *Panel) ScopedReplaceRequest() (ReplaceRequest, bool) {
	if p.git || p.query.Value() == "" || len(p.rows) == 0 {
		p.errText = "No matches to replace"
		return ReplaceRequest{}, false
	}
	r := p.rows[clamp(p.selected, 0, len(p.rows)-1)]
	f := p.fileMatches[r.file]
	var selected []Position
	if r.kind == rowFile {
		for j, m := range f.Matches {
			if p.matchChecked(r.file, j) {
				selected = append(selected, Position{Line: m.Line - 1, Col: m.Col - 1})
			}
		}
		if len(selected) == 0 {
			p.errText = "No checked matches in this file"
			return ReplaceRequest{}, false
		}
	} else {
		if !p.matchChecked(r.file, r.match) {
			p.errText = "The focused match is not checked"
			return ReplaceRequest{}, false
		}
		m := f.Matches[r.match]
		selected = []Position{{Line: m.Line - 1, Col: m.Col - 1}}
	}
	return ReplaceRequest{
		Root:        p.root,
		Query:       p.query.Value(),
		Replacement: p.replace.Value(),
		Options:     p.modes,
		Files: []ReplaceFile{{
			Path: f.Path, Name: f.Name, Hash: f.Hash, Dirty: f.Dirty, Selected: selected,
		}},
		Matches: len(selected),
		Limit:   p.limit,
	}, true
}

// ReplaceCount returns the checked matches and the files they are in.
func (p *Panel) ReplaceCount() (matches, files int) {
	if p.git {
		return 0, 0
	}
	for i, f := range p.fileMatches {
		n := 0
		for j := range f.Matches {
			if p.matchChecked(i, j) {
				n++
			}
		}
		if n > 0 {
			files++
			matches += n
		}
	}
	return matches, files
}

// matchChecked reports whether match j of file i is checked for replacement.
func (p *Panel) matchChecked(i, j int) bool {
	if i < 0 || i >= len(p.checked) || j < 0 || j >= len(p.checked[i]) {
		return false
	}
	return p.checked[i][j]
}

// fileChecked reports whether every match of file i is checked. A file with no matches
// is not checked.
func (p *Panel) fileChecked(i int) bool {
	if i < 0 || i >= len(p.checked) || len(p.checked[i]) == 0 {
		return false
	}
	for _, on := range p.checked[i] {
		if !on {
			return false
		}
	}
	return true
}

// toggleChecked flips the focused row: a match row's own box, or every match of a file
// row (in or out together).
func (p *Panel) toggleChecked() {
	if p.git || len(p.rows) == 0 {
		return
	}
	r := p.rows[clamp(p.selected, 0, len(p.rows)-1)]
	if r.kind == rowFile {
		on := !p.fileChecked(r.file)
		for j := range p.checked[r.file] {
			p.checked[r.file][j] = on
		}
		return
	}
	if r.file < len(p.checked) && r.match < len(p.checked[r.file]) {
		p.checked[r.file][r.match] = !p.checked[r.file][r.match]
	}
}

// setAllChecked checks or unchecks every match of the result.
func (p *Panel) setAllChecked(on bool) {
	if p.git {
		return
	}
	for i := range p.checked {
		for j := range p.checked[i] {
			p.checked[i][j] = on
		}
	}
}

// NextMatch moves the selection to the next match row, wrapping around, and returns the
// position it names. It is the next and previous match action of the editor, and it
// works while the panel is closed: the root keeps the panel for it.
func (p *Panel) NextMatch(forward bool) (OpenMsg, bool) {
	if len(p.rows) == 0 {
		return OpenMsg{}, false
	}
	start, step := p.selected, 1
	if !forward {
		step = -1
	}
	for n := 1; n <= len(p.rows); n++ {
		i := (start + step*n) % len(p.rows)
		if i < 0 {
			i += len(p.rows)
		}
		if p.rows[i].kind != rowMatch {
			continue
		}
		p.selectRow(i)
		r := p.rows[i]
		f := p.fileMatches[r.file]
		return OpenMsg{Path: f.Path, Line: r.line, Col: r.col, Owner: p.owner}, true
	}
	return OpenMsg{}, false
}

// Settings returns the session match options and scope, so the root can open the next
// panel with them.
func (p *Panel) Settings() (MatchOptions, Scope) { return p.modes, p.scope }

// PreviewWrap reports whether the preview column soft-wraps, so the root can open the next
// panel with the same setting.
func (p *Panel) PreviewWrap() bool { return p.previewWrap }

// SetSources replaces the unsaved-text snapshots of the searches, after the root applied
// a replacement and re-reads the open buffers.
func (p *Panel) SetSources(openFiles []SourceFile, current *SourceFile) {
	p.openFiles, p.current = openFiles, current
}

// Stats returns the match count, the file count and whether the result was capped, for
// the title and the tests.
func (p *Panel) Stats() (matches, files int, truncated bool) {
	for _, f := range p.fileMatches {
		matches += len(f.Matches)
		if len(f.Matches) > 0 {
			files++
		}
	}
	return matches, files, p.truncated
}

// Query, Replacement and Glob report the field values, for the root's tests.
func (p *Panel) Query() string       { return p.query.Value() }
func (p *Panel) Replacement() string { return p.replace.Value() }
func (p *Panel) Glob() string        { return p.glob.Value() }

// Move selects the row delta from the selection.
func (p *Panel) Move(delta int) { p.move(delta) }

// Select opens the selected row, as Enter does.
func (p *Panel) Select() tea.Cmd { return p.activate() }

// move selects the row delta away and keeps it in view.
func (p *Panel) move(delta int) { p.selectRow(p.selected + delta) }

// selectRow selects row i, clamped into the result, and keeps it in view.
func (p *Panel) selectRow(i int) {
	if len(p.rows) == 0 {
		p.selected = 0
		return
	}
	p.selected = clamp(i, 0, len(p.rows)-1)
	p.clampTop()
}

// clampTop keeps the selected row inside the visible result rows.
func (p *Panel) clampTop() {
	h := p.resultsHeight()
	if h <= 0 {
		p.top = 0
		return
	}
	if p.selected < p.top {
		p.top = p.selected
	}
	if p.selected >= p.top+h {
		p.top = p.selected - h + 1
	}
	p.top = clamp(p.top, 0, max(len(p.rows)-h, 0))
}

// resultsHeight is the number of result rows the current frame gives the panel. It is
// the layout of the last Draw, so the top clamp works from the first frame on.
func (p *Panel) resultsHeight() int {
	if p.width <= 0 || p.height <= 0 {
		return 0
	}
	return layoutFor(p.width, p.height, p.git).results
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
