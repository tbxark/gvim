package palette

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/textsafe"
)

// Live Grep is the palette's text search over the workspace files and the open
// buffers. Its query is the whole query line, with no mode prefix, and its rows are the
// matching lines. Searching reads the files, so it runs in a tea.Cmd and its result
// comes back as a GrepResultMsg. The app adapter implements Grepper over internal/search.
//
// Live Grep has a toolbar row above its rows: the match modes (case, regex, whole
// word) and the scopes (files, buffers, the dock's terminal transcripts, and
// ignored/hidden files inside the file scope). Its keys are Fresh's prompt keys,
// handled here because gvim's overlays answer their keys in code: Alt+C case, Alt+G
// regex, Alt+O whole word, Alt+L files, Alt+U buffers, Alt+T terminals and Alt+I
// ignored. The title shows the result count after a search.

// SearchOptions are how a Live Grep query is interpreted.
type SearchOptions struct {
	// CaseSensitive matches upper and lower case exactly. The starting value is
	// editor.search.case_sensitive.
	CaseSensitive bool
	// Regex treats the query as a regular expression. It starts on, as Fresh's Live
	// Grep does.
	Regex bool
	// WholeWord matches whole words only. It starts off.
	WholeWord bool
}

// GrepSources select what a Live Grep query searches. Files, Buffers and Terminals
// are the sources; Ignored widens the file source to hidden and .gitignore-matched
// files.
type GrepSources struct {
	Files     bool
	Buffers   bool
	Terminals bool
	Ignored   bool
}

// BufferSource is one open buffer's text, for the Buffers scope. Index is the tab's
// position in Options.Buffers, so a hit can activate it.
type BufferSource struct {
	Name  string // the tab's label
	Path  string // the buffer's file, when it has one
	Index int    // the tab position
	Text  string // the buffer's text, LF-normalised
}

// TerminalSource is one open dock terminal's transcript window for the Terminals
// scope. Index is the dock tab position, so a hit can focus it. Lines is the newest
// bounded transcript window (the root snapshots terminal.SavedState rows: at most
// GrepTerminalsMaxRows rows and GrepTerminalsMaxTabBytes per tab; the whole request
// is capped at GrepTerminalsMaxTabs/GrepTerminalsMaxBytes). Closed terminals have no
// backing file to search in gvim; only the open dock's window is searched.
type TerminalSource struct {
	Name  string   // the dock tab's label, "" falls back to "terminal"
	Index int      // the dock tab position
	Lines []string // the transcript window, oldest first
}

// Terminal snapshot bounds. The root applies them when it snapshots the dock, and the
// palette re-applies them defensively when it builds a request, so a bad caller can
// never make Live Grep copy or search an unbounded terminal history. The per-tab row
// and byte caps match terminal.SavedState's transcript bounds.
const (
	GrepTerminalsMaxTabs     = 16
	GrepTerminalsMaxRows     = 500
	GrepTerminalsMaxTabBytes = 1 << 20 // per tab
	GrepTerminalsMaxBytes    = 4 << 20 // the whole snapshot
)

// boundTerminalSources copies src under the terminal snapshot bounds: at most
// GrepTerminalsMaxTabs tabs, each with its newest ≤GrepTerminalsMaxRows rows and
// ≤GrepTerminalsMaxTabBytes of text, and at most GrepTerminalsMaxBytes overall (later
// tabs are left out once the budget is spent). The input is not searched directly;
// the request owns the copy.
func boundTerminalSources(src []TerminalSource) []TerminalSource {
	if len(src) == 0 {
		return nil
	}
	out := make([]TerminalSource, 0, min(len(src), GrepTerminalsMaxTabs))
	total := 0
	for _, term := range src {
		if len(out) >= GrepTerminalsMaxTabs || total >= GrepTerminalsMaxBytes {
			break
		}
		// Keep the newest rows: the tail is the transcript window nearest the live
		// screen. Count rows and bytes from the tail, so a single oversized line still
		// contributes (bounded by the byte test after the first row).
		start, rows, bytes := len(term.Lines), 0, 0
		for start > 0 && rows < GrepTerminalsMaxRows {
			line := term.Lines[start-1]
			if bytes+len(line)+1 > GrepTerminalsMaxTabBytes {
				break
			}
			bytes += len(line) + 1
			rows++
			start--
		}
		lines := append([]string(nil), term.Lines[start:]...)
		if len(lines) == 0 {
			// Preserve an available-but-empty tab so a hit index still points at it
			// when another tab has matches.
			lines = nil
		}
		if total+bytes > GrepTerminalsMaxBytes {
			break
		}
		total += bytes
		out = append(out, TerminalSource{Name: term.Name, Index: term.Index, Lines: lines})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// GrepRequest is the input of one Live Grep search. The palette fills what the
// sources need: the file index snapshot for Files and Ignored, the buffer snapshot for
// Buffers.
type GrepRequest struct {
	Root      string
	Text      string           // the query
	Query     SearchOptions    // how Text is matched
	Sources   GrepSources      // what is searched
	Files     []string         // the file index snapshot, for the file sources
	Buffers   []BufferSource   // the open buffers, for the Buffers scope
	Terminals []TerminalSource // the dock's transcript windows, for the Terminals scope
	Limit     int              // the most hits to return
}

// Grepper searches the text the request names. It returns at most Limit hits, one per
// matching line: the file hits in file order, the buffer hits in buffer order, then the
// terminal hits in dock-tab/line order. Only text is searched. Search is called from a
// tea.Cmd, never from Update or Draw.
type Grepper interface {
	Search(req GrepRequest) ([]GrepHit, error)
}

// GrepSource says what a hit matched.
type GrepSource int

const (
	// GrepFiles is a hit in a project file.
	GrepFiles GrepSource = iota
	// GrepBuffers is a hit in an open buffer.
	GrepBuffers
	// GrepTerminals is a hit in an open dock terminal's transcript window.
	GrepTerminals
)

// GrepHit is one matching line. Rel is the file relative to the root, the buffer's
// label or the terminal tab's label; Line and Col are the 1-based position of the match,
// and Text is the line. Source says whether the hit is in a file, a buffer or a
// terminal; Index is the buffer tab position for a buffer hit and the dock tab position
// for a terminal hit.
type GrepHit struct {
	Rel       string
	Line, Col int
	Text      string
	Source    GrepSource
	Index     int
}

// GrepResultMsg carries the hits of one Live Grep query. The palette applies it only
// when Seq is the query it sent last. Text is the query that was searched.
// FilesGeneration is the file-index generation the search saw, so Resume can tell
// whether its cached hits still belong to the current index.
type GrepResultMsg struct {
	Seq                int
	Text               string
	Hits               []GrepHit
	Err                error
	FilesGeneration    uint64
	HasFilesGeneration bool
}

// LiveResume is the state Resume restores: the query, match modes, scope toggles,
// selected row and the cached on-screen matches, with the file-index generation they
// came from. Done false means there is no cached result (the query changed after the
// last result, or none arrived); the palette then restores the controls and runs the
// search honestly.
type LiveResume struct {
	Text               string
	Options            SearchOptions
	Sources            GrepSources
	Hits               []GrepHit
	Err                error
	Selected           int
	Done               bool
	FilesGeneration    uint64
	HasFilesGeneration bool
}

// OpenLineMsg asks the root to open Path at Line and Col, both 1-based. It is sent
// when a live grep row of a file is chosen, after the palette has closed.
type OpenLineMsg struct {
	Path      string
	Line, Col int
}

// OpenBufferLineMsg asks the root to activate the tab at Index and put the caret at
// Line and Col, both 1-based. It is sent when a live grep row of an open buffer is
// chosen, after the palette has closed.
type OpenBufferLineMsg struct {
	Index     int
	Line, Col int
}

// OpenTerminalMsg asks the root to show the dock and focus the terminal tab at Index.
// It is sent when a live grep row of a terminal transcript is chosen, after the palette
// has closed. It only focuses the open tab: no shell is started, no PTY is touched.
type OpenTerminalMsg struct {
	Index int
}

// GrepExportMsg asks the root to show the last Live Grep result set as its
// quickfix-style list, after the palette has closed. Root is the local workspace root
// the file rows are relative to. Total is the full hit count when the export was capped
// below it, so the root can carry a notice.
type GrepExportMsg struct {
	Root  string
	Query string
	Hits  []GrepHit
	Total int
}

// liveExportCap is the independent bound on rows a Live Grep quickfix export hands to
// the list overlay; the palette's result cap is smaller today, but the export stays
// bounded if that changes. Rows past the cap are counted in GrepExportMsg.Total.
const liveExportCap = 100

// grepLimit is how many hits a live grep query asks for: the palette's row cap, the
// same as the file mode's. A result of that many is marked "N+" because there may be
// more.
const grepLimit = fileLimit

// toolbarSep separates the match modes from the scopes on the Live Grep toolbar.
const toolbarSep = " │ "

// grepMode names one match mode toggle.
type grepMode int

const (
	modeCase grepMode = iota
	modeRegex
	modeWord
)

// grepScope names one source scope toggle.
type grepScope int

const (
	scopeFiles grepScope = iota
	scopeBuffers
	scopeTerminals
	scopeIgnored
)

// grepState is Live Grep's state: the match modes, the scope toggles, whether the
// Buffers/Terminals scopes are available, and the last result, which the title shows.
type grepState struct {
	opts    SearchOptions
	sources GrepSources

	count   int    // the last result's hit count
	done    bool   // a result arrived for the current query
	text    string // the query of the last result
	err     error  // the last search's error, if any
	buffers bool   // the Buffers scope is available: a buffer snapshot was supplied
	// terminals says the Terminals scope is available: the root supplied a bounded
	// dock transcript snapshot (the scope starts on, as Fresh's default does).
	terminals bool
	hits      []GrepHit
	// gen is the file-index generation the last result was searched against.
	gen    uint64
	genSet bool
}

// newGrepState returns the state of the palette's Live Grep modes and scopes. The match
// modes start at Options.LiveSearch, or at Fresh's defaults when it is nil: regex on,
// whole word off and case folded. The Files scope starts on; the Buffers scope starts
// off when a buffer snapshot was supplied; the Terminals scope starts on when a dock
// snapshot was supplied, matching Fresh's default for terminal scrollback; the Ignored
// scope starts off.
func newGrepState(opts Options) grepState {
	st := grepState{sources: GrepSources{Files: true}}
	if opts.LiveSearch != nil {
		st.opts = *opts.LiveSearch
	} else {
		st.opts.Regex = true
	}
	st.buffers = opts.SourceBuffers != nil
	st.terminals = opts.SourceTerminals != nil
	st.sources.Terminals = st.terminals
	return st
}

// liveKey runs a Live Grep toggle key. It reports whether name was one of them, so any
// other key edits the query. The Buffers key does nothing when the scope is unavailable.
func (p *Palette) liveKey(name string) (tea.Cmd, bool) {
	switch name {
	case "alt+c":
		return p.toggleMode(modeCase), true
	case "alt+g":
		return p.toggleMode(modeRegex), true
	case "alt+o":
		return p.toggleMode(modeWord), true
	case "alt+l":
		return p.toggleScope(scopeFiles), true
	case "alt+u":
		if !p.grep.buffers {
			return nil, false
		}
		return p.toggleScope(scopeBuffers), true
	case "alt+t":
		if !p.grep.terminals {
			return nil, false
		}
		return p.toggleScope(scopeTerminals), true
	case "alt+i":
		return p.toggleScope(scopeIgnored), true
	case "alt+q":
		return p.exportGrep(), true
	}
	return nil, false
}

// toggleMode flips one match mode and re-runs the search.
func (p *Palette) toggleMode(m grepMode) tea.Cmd {
	switch m {
	case modeCase:
		p.grep.opts.CaseSensitive = !p.grep.opts.CaseSensitive
	case modeRegex:
		p.grep.opts.Regex = !p.grep.opts.Regex
	case modeWord:
		p.grep.opts.WholeWord = !p.grep.opts.WholeWord
	}
	return p.refreshGrep()
}

// toggleScope flips one source scope and re-runs the search.
func (p *Palette) toggleScope(s grepScope) tea.Cmd {
	switch s {
	case scopeFiles:
		p.grep.sources.Files = !p.grep.sources.Files
	case scopeBuffers:
		p.grep.sources.Buffers = !p.grep.sources.Buffers
	case scopeTerminals:
		p.grep.sources.Terminals = !p.grep.sources.Terminals
	case scopeIgnored:
		p.grep.sources.Ignored = !p.grep.sources.Ignored
	}
	return p.refreshGrep()
}

// exportGrep closes the palette and asks the root to show the current Live Grep hits as
// its quickfix-style list. It is Fresh's live_grep_export_quickfix (Alt+Q). A search
// with no hits is a no-op: there is nothing to export and the palette stays open.
func (p *Palette) exportGrep() tea.Cmd {
	if len(p.grep.hits) == 0 {
		return nil
	}
	hits := append([]GrepHit(nil), p.grep.hits...)
	total := p.grep.count
	if total < len(hits) {
		total = len(hits)
	}
	if len(hits) > liveExportCap {
		hits = hits[:liveExportCap]
	}
	query, root := p.grep.text, p.opts.Root
	p.open = false
	p.cancelProbe()
	p.preview.reset()
	return func() tea.Msg {
		return GrepExportMsg{Root: root, Query: query, Hits: hits, Total: total}
	}
}

// refreshGrep re-runs the search after a mode or scope changed. It numbers the search
// as a new one, so a result still in flight is dropped, and it keeps the rows on screen
// until the new result arrives.
func (p *Palette) refreshGrep() tea.Cmd {
	p.seq++
	p.grep.done = false
	p.grep.hits, p.grep.gen, p.grep.genSet = nil, 0, false
	return p.grepCmd(p.seq, p.input.Value())
}

// liveQuery is requery in Live Grep mode. An empty query has no rows and searches
// nothing. Any other query searches, and its rows arrive as a GrepResultMsg, so the
// rows on screen stay until then.
func (p *Palette) liveQuery(seq int, text string) tea.Cmd {
	if text == "" {
		p.rows = nil
		p.grep.done, p.grep.count, p.grep.err, p.grep.text = false, 0, nil, ""
		p.grep.hits, p.grep.gen, p.grep.genSet = nil, 0, false
		p.syncSize()
		p.nav.Select(0)
		return nil
	}
	return p.grepCmd(seq, text)
}

// grepCmd returns the command that searches for text with the current modes and scopes.
// The file list and the search run in the command, never in Update or Draw. It returns
// nil when no Grepper is wired.
func (p *Palette) grepCmd(seq int, text string) tea.Cmd {
	if p.opts.Grep == nil || text == "" {
		return nil
	}
	g, files, root := p.opts.Grep, p.opts.Files, p.opts.Root
	buffers, terminals, query, sources := p.opts.SourceBuffers, p.opts.SourceTerminals, p.grep.opts, p.grep.sources
	gen, hasGen := filesGeneration(files)
	return func() tea.Msg {
		req := GrepRequest{Root: root, Text: text, Query: query, Sources: sources, Limit: grepLimit}
		if sources.Files || sources.Ignored {
			if files != nil {
				req.Files, _ = files.Snapshot()
			}
		}
		if sources.Buffers {
			req.Buffers = buffers
		}
		if sources.Terminals {
			req.Terminals = boundTerminalSources(terminals)
		}
		hits, err := g.Search(req)
		return GrepResultMsg{Seq: seq, Text: text, Hits: hits, Err: err, FilesGeneration: gen, HasFilesGeneration: hasGen}
	}
}

// applyGrep shows a live grep result when it answers the current query. The highlight
// goes to the top when the text changed. The result also sets the count the title
// shows.
func (p *Palette) applyGrep(msg GrepResultMsg) tea.Cmd {
	if msg.Seq != p.seq || !p.live {
		return nil
	}
	p.grep.done, p.grep.text, p.grep.err = true, msg.Text, msg.Err
	p.grep.count = len(msg.Hits)
	p.grep.hits = append([]GrepHit(nil), msg.Hits...)
	p.grep.gen, p.grep.genSet = msg.FilesGeneration, msg.HasFilesGeneration
	p.rows = grepRows(msg)
	p.syncSize()
	if msg.Text != p.shown {
		p.nav.Select(0)
	}
	p.shown = msg.Text
	return nil
}

// restoreLive applies a Resume snapshot. The controls are always restored; cached
// matches are used only when they still match the query, the previous result was
// complete, and the file index generation is unchanged. Otherwise the palette starts
// with the restored query/modes/scopes and Init runs the search honestly.
func (p *Palette) restoreLive(resume LiveResume) {
	p.grep.opts = resume.Options
	p.grep.sources = resume.Sources
	// Buffer and terminal hits index an in-memory tab snapshot that can change while the
	// palette is closed (a tab can be renamed, reordered or closed), so cached hits for
	// those volatile scopes are never restored: the query/scopes return and Init runs the
	// search honestly against the current snapshot.
	if !p.grep.buffers && p.grep.sources.Buffers {
		p.grep.sources.Buffers = false
		return
	}
	if !p.grep.terminals && p.grep.sources.Terminals {
		p.grep.sources.Terminals = false
		return
	}
	if resume.Sources.Buffers || resume.Sources.Terminals {
		return
	}
	if !resume.Done || resume.Text != p.input.Value() {
		return
	}
	gen, hasGen := filesGeneration(p.opts.Files)
	if hasGen != resume.HasFilesGeneration || (hasGen && gen != resume.FilesGeneration) {
		return
	}
	hits := resume.Hits
	if len(hits) > grepLimit {
		hits = hits[:grepLimit]
	}
	p.grep.hits = append([]GrepHit(nil), hits...)
	p.grep.gen, p.grep.genSet = resume.FilesGeneration, resume.HasFilesGeneration
	p.grep.done, p.grep.text, p.grep.err = true, resume.Text, resume.Err
	p.grep.count = len(hits)
	p.rows = grepRows(GrepResultMsg{Text: resume.Text, Hits: hits, Err: resume.Err})
	p.syncSize()
	p.nav.Select(resume.Selected)
	p.shown = resume.Text
	p.liveResumed = true
}

// filesGeneration returns the file index generation when Files exposes one, so a
// Live Grep result can record which index it was searched against.
func filesGeneration(files Files) (uint64, bool) {
	if g, ok := files.(interface{ Generation() uint64 }); ok {
		return g.Generation(), true
	}
	return 0, false
}

// LiveResumeState snapshots the Live Grep session for the root's Resume command. It
// returns nil for a non-live palette. The cached hits are copied and bounded to the
// palette's row cap; Done is true only when the on-screen result still matches the
// current query (Fresh invalidates the cache on any query keystroke).
func (p *Palette) LiveResumeState() *LiveResume {
	if p == nil || !p.live {
		return nil
	}
	text := p.input.Value()
	state := &LiveResume{
		Text:     text,
		Options:  p.grep.opts,
		Sources:  p.grep.sources,
		Selected: p.nav.Selected(),
	}
	if !p.grep.done || text != p.grep.text {
		return state
	}
	hits := p.grep.hits
	if len(hits) > grepLimit {
		hits = hits[:grepLimit]
	}
	state.Hits = append([]GrepHit(nil), hits...)
	state.Err = p.grep.err
	state.Done = true
	state.FilesGeneration, state.HasFilesGeneration = p.grep.gen, p.grep.genSet
	return state
}

// liveTitle is the top border of Live Grep: the name alone before a result, and the
// result count after one. A count at the row cap is marked "N+" because there may be
// more hits.
func (p *Palette) liveTitle() string {
	const title = "Live Grep"
	if !p.grep.done || p.grep.text == "" {
		return title
	}
	if p.grep.err != nil {
		return title + " · error"
	}
	if p.grep.count == 0 {
		return title + " · no matches"
	}
	mark := ""
	if p.grep.count >= grepLimit {
		mark = "+"
	}
	return fmt.Sprintf("%s · %d%s matches", title, p.grep.count, mark)
}

// toolbarSeg is one segment of the Live Grep toolbar: the key, the label, the state, and
// whether the state is on.
type toolbarSeg struct {
	text string
	on   bool
}

// toolbarSegments is the Live Grep toolbar, left to right: the match modes, then the
// scopes. The Buffers scope is left out when no buffer snapshot was supplied, so a
// toggle that cannot work is never painted.
func (p *Palette) toolbarSegments() []toolbarSeg {
	onOff := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	segs := []toolbarSeg{
		{text: "Alt+C Case:" + onOff(p.grep.opts.CaseSensitive) + "  ", on: p.grep.opts.CaseSensitive},
		{text: "Alt+G Regex:" + onOff(p.grep.opts.Regex) + "  ", on: p.grep.opts.Regex},
		{text: "Alt+O Word:" + onOff(p.grep.opts.WholeWord) + "  ", on: p.grep.opts.WholeWord},
		{text: toolbarSep, on: true},
		{text: "Alt+L Files:" + onOff(p.grep.sources.Files) + "  ", on: p.grep.sources.Files},
	}
	if p.grep.buffers {
		segs = append(segs, toolbarSeg{text: "Alt+U Buffers:" + onOff(p.grep.sources.Buffers) + "  ", on: p.grep.sources.Buffers})
	}
	if p.grep.terminals {
		segs = append(segs, toolbarSeg{text: "Alt+T Terminals:" + onOff(p.grep.sources.Terminals) + "  ", on: p.grep.sources.Terminals})
	}
	return append(segs, toolbarSeg{text: "Alt+I Ignored:" + onOff(p.grep.sources.Ignored), on: p.grep.sources.Ignored})
}

// grepRowDisplay turns one hit's line into the sanitized row text plus the match's
// display-rune offset and query length. The tool's tab is drawn as one space; Cc/Cf and
// invalid UTF-8 are removed with a raw→clean rune map, so a bidi override before the
// match cannot move the bold run past the visible query.
func grepRowDisplay(h GrepHit, query string) (text string, mcol, qlen int) {
	spaced := strings.ReplaceAll(h.Text, "\t", " ")
	cleanSpaced, mapping := textsafe.CleanWithMap(spaced)
	cleanText := strings.TrimSpace(cleanSpaced)
	cleanLead := utf8.RuneCountInString(cleanSpaced) - utf8.RuneCountInString(strings.TrimLeftFunc(cleanSpaced, unicode.IsSpace))
	rawRunes := []rune(spaced)
	rawHit := min(max(h.Col-1, 0), len(rawRunes))
	hitClean := textsafe.MapIndex(mapping, rawHit)
	display := []rune(cleanText)
	mcol = min(max(hitClean-cleanLead, 0), len(display))
	qlen = utf8.RuneCountInString(textsafe.Clean(query))
	if qlen < 1 {
		qlen = 1
	}
	if mcol+qlen > len(display) {
		qlen = max(len(display)-mcol, 1)
	}
	return cleanText, mcol, qlen
}

// grepRows turns a result into rows: one row per hit, or one row with the error. Tabs in
// a hit's line become spaces, so the row keeps its columns. A buffer hit is tagged so
// Enter activates its tab instead of opening a file.
func grepRows(msg GrepResultMsg) []row {
	if msg.Err != nil {
		return []row{{kind: rowMessage, name: "Live Grep: " + msg.Err.Error()}}
	}
	rows := make([]row, 0, len(msg.Hits))
	for _, h := range msg.Hits {
		// The row is display text: a hostile file name or transcript line may contain
		// bidi/C1/Cf and must not repaint the palette. r.rel/line/col stay raw for the
		// open/focus action; only name/text/mcol/qlen are display coordinates.
		text, mcol, qlen := grepRowDisplay(h, msg.Text)
		r := row{
			kind: rowGrep,
			name: fmt.Sprintf("%s:%d: %s", textsafe.Clean(h.Rel), h.Line, text),
			rel:  h.Rel,
			line: h.Line,
			col:  h.Col,
			text: text,
			mcol: mcol,
			qlen: qlen,
		}
		switch h.Source {
		case GrepBuffers:
			r.source = "buffer"
			r.grepSource = GrepBuffers
			r.index = h.Index
		case GrepTerminals:
			r.source = "terminal"
			r.grepSource = GrepTerminals
			r.index = h.Index
		}
		rows = append(rows, r)
	}
	return rows
}
