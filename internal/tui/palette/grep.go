package palette

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/textsafe"
	"github.com/tbxark/gvim/search"
)

// Live Grep is the palette's text search over the workspace files and the open
// buffers. Its query is the whole query line, with no mode prefix, and its rows are the
// matching lines. Searching reads the files, so it runs in a tea.Cmd and its result
// comes back as a GrepResultMsg. The app adapter implements Grepper over internal/search.
//
// Live Grep has a toolbar row above its rows: the match modes (case, regex, whole
// word) and the scopes (files, buffers, the dock's terminal transcripts, language-server
// diagnostics, and ignored/hidden files inside the file scope). Its keys are Fresh's
// prompt keys, handled here because gvim's overlays answer their keys in code: Alt+C case,
// Alt+G regex, Alt+O whole word, Alt+L files, Alt+U buffers, Alt+T terminals, Alt+D
// diagnostics and Alt+I ignored. The title shows the result count after a search.

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
	Files       bool
	Buffers     bool
	Terminals   bool
	Diagnostics bool
	Ignored     bool
}

// DiagnosticSource is one language-server diagnostic for Live Grep's Diagnostics scope.
// Path is the document path to open; Rel is the label shown in the row. Line and Col are
// 1-based. Text is the diagnostic message and Severity is the LSP severity number.
type DiagnosticSource struct {
	Path      string
	Rel       string
	Line, Col int
	Text      string
	Severity  int
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
	// ID is the dock tab's stable runtime identity: a hit is opened by it, so closing
	// or reordering another tab cannot focus the wrong terminal. 0 means an older
	// caller that only has Index (tests/fallback); a runtime ID is never reused.
	ID    uint64
	Name  string // the dock tab's label, "" falls back to "terminal"
	Index int    // the dock tab position at snapshot time
	// First is the combined scrollback row of Lines[0] (prior transcript, then
	// history, then screen), so a matched line can be revealed in the pager.
	First int
	// Total is the terminal's scrollback length at snapshot time. A reveal compares it
	// with the current length and the expected line/context: when they no longer line
	// up, only a unique content match relocates, otherwise the hit refuses honestly.
	Total int
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
		out = append(out, TerminalSource{ID: term.ID, Name: term.Name, Index: term.Index, First: term.First + start, Total: term.Total, Lines: lines})
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
	// GrepDiagnostics is a hit in a language-server diagnostic message.
	GrepDiagnostics
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
	// TextCol and TextLen locate the match inside Text: TextCol is 1-based in runes
	// and TextLen is the rune length of the match (the regex match, not the query
	// length). Diagnostics need them because Col is the document column, not an offset
	// into the message; the other sources leave them zero and derive the highlight from
	// Col and the query.
	TextCol, TextLen int
	Index            int
	// TerminalID is the stable dock tab id of a terminal hit (0 for older callers):
	// opening the hit uses it so a close/reorder cannot focus another tab.
	TerminalID uint64
	// TranscriptFirst is the combined scrollback row of the terminal hit's snapshot
	// window; Line is relative to it, so Line+First-1 is the absolute pager row.
	TranscriptFirst int
	// TerminalTotal is the terminal's scrollback length at snapshot time, and
	// TerminalBefore/TerminalAfter are the hit's snapshot neighbours (empty at the
	// window edge). A reveal uses them to validate/relocate the snapshot row so new
	// output or eviction never lands on the wrong transcript line.
	TerminalTotal  int
	TerminalBefore string
	TerminalAfter  string
	// TerminalSnapshot marks a hit that carries the snapshot validation payload (all
	// app-produced terminal hits); TerminalValid is false for a malformed export
	// payload, which the reveal refuses instead of clamping. Before/AfterKnown tell an
	// empty expected line/neighbour from "no neighbour at the window edge" (an empty
	// matched line is a real blank-line hit).
	TerminalSnapshot    bool
	TerminalValid       bool
	TerminalBeforeKnown bool
	TerminalAfterKnown  bool
	// Path is the absolute document path of a diagnostic hit; the row opens it
	// through the root's LSP goto rather than through a workspace-relative join.
	Path string
}

// GrepResultMsg carries the hits of one Live Grep query. The palette applies it only
// when Seq is the query it sent last. Text is the query that was searched.
type GrepResultMsg struct {
	Token     uint64
	Seq       int
	Text      string
	Hits      []GrepHit
	Err       error
	Notice    string // non-fatal degradation/cap explanation from the adapter
	Truncated bool   // the adapter's result is incomplete
	Owner     Owner
}

func (m GrepResultMsg) PaletteOwner() Owner { return m.Owner }

// LiveResume is the state Resume restores: the previous query, match modes and scope
// toggles. It deliberately carries no hits or selected row: a hit's file contents may
// have changed while the palette was closed and there is no low-cost content
// fingerprint, so Resume always searches again with these controls.
type LiveResume struct {
	Text    string
	Options SearchOptions
	Sources GrepSources
}

// OpenLineMsg asks the root to open Path at Line and Col, both 1-based. It is sent
// when a live grep row of a file is chosen, after the palette has closed.
type OpenLineMsg struct {
	Path      string
	Line, Col int
	Owner     Owner
}

func (m OpenLineMsg) PaletteOwner() Owner { return m.Owner }

// OpenDiagnosticMsg asks the root to open the language-server diagnostic's document at
// Line and Col, both 1-based. Path is absolute (or an authority path), so the root uses
// its LSP goto rather than joining it to the palette root.
type OpenDiagnosticMsg struct {
	Path      string
	Line, Col int
	Owner     Owner
}

func (m OpenDiagnosticMsg) PaletteOwner() Owner { return m.Owner }

// OpenBufferLineMsg asks the root to activate the tab at Index and put the caret at
// Line and Col, both 1-based. It is sent when a live grep row of an open buffer is
// chosen, after the palette has closed.
type OpenBufferLineMsg struct {
	Index     int
	Line, Col int
	Owner     Owner
}

func (m OpenBufferLineMsg) PaletteOwner() Owner { return m.Owner }

// OpenTerminalMsg asks the root to show the dock and focus the terminal tab at Index.
// It is sent when a live grep row of a terminal transcript is chosen, after the palette
// has closed. It only focuses the open tab: no shell is started, no PTY is touched.
type OpenTerminalMsg struct {
	// ID is the stable dock tab identity from the snapshot. It wins over Index: the
	// snapshot position may be stale after another tab closed or moved. 0 falls back
	// to Index (older tests/callers).
	ID uint64
	// Row is the combined transcript row (0-based, prior rows then history then
	// screen) to reveal in the tab's read-only scrollback. 0 reveals the top when the
	// caller has no line (legacy).
	Row   int
	Index int
	Owner Owner
	// Total is the terminal's scrollback length at snapshot time and Text/Before/After
	// are the expected line and its snapshot neighbours; they let the reveal validate
	// the row and relocate it by unique content after output/eviction (or refuse).
	Total  int
	Text   string
	Before string
	After  string
	// Snapshot marks a hit whose stored row must be validated before revealing (all
	// app-produced terminal hits). Valid false (a malformed quickfix payload) is
	// refused honestly. BeforeKnown/AfterKnown keep blank neighbours distinct from a
	// window edge, so an empty matched line is validated, not clamped.
	Snapshot    bool
	Valid       bool
	BeforeKnown bool
	AfterKnown  bool
}

func (m OpenTerminalMsg) PaletteOwner() Owner { return m.Owner }

// GrepExportMsg asks the root to show the last Live Grep result set as its
// quickfix-style list, after the palette has closed. Root is the local workspace root
// the file rows are relative to. Total is the full hit count when the export was capped
// below it, so the root can carry a notice.
type GrepExportMsg struct {
	Root  string
	Query string
	Hits  []GrepHit
	Total int
	// Remote says Root/Rel are remote slash-separated paths: the root list must join and
	// open them through the remote document access, never the local filepath/open path.
	Remote bool
	// Notice/Truncated carry the search's non-fatal completeness signal into the export
	// list, so the quickfix view says why it is incomplete instead of presenting a bounded
	// result as complete.
	Notice    string
	Truncated bool
	Owner     Owner
}

func (m GrepExportMsg) PaletteOwner() Owner { return m.Owner }

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
	scopeDiagnostics
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
	// diagnostics says the Diagnostics scope is available: the root supplied the
	// language-server diagnostics. It starts off, as Fresh's default does.
	diagnostics bool
	// hits keeps the raw result for the Alt+Q quickfix export; they are not used by
	// Resume (which always searches again).
	hits []GrepHit
	// notice/truncated are the last result's non-fatal completeness signal, shown in
	// the palette chrome; they are replaced only by a result whose seq is current.
	notice    string
	truncated bool
	// cancel stops the in-flight ContextGrepper search when a newer query/scope
	// supersedes it or the palette closes. Nil for the plain Grepper seam.
	cancel context.CancelFunc
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
	st.diagnostics = opts.SourceDiagnostics != nil
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
	case "alt+d":
		if !p.grep.diagnostics {
			return nil, false
		}
		return p.toggleScope(scopeDiagnostics), true
	case "alt+i":
		return p.toggleScope(scopeIgnored), true
	case "alt+q", "alt+m":
		// Fresh's prompt context binds both Alt+Q and Alt+M to
		// live_grep_export_quickfix; both export the same result set.
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
	case scopeDiagnostics:
		p.grep.sources.Diagnostics = !p.grep.sources.Diagnostics
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
	query, root, owner := p.grep.text, p.opts.Root, p.owner
	p.invalidatePreviewInstance()
	p.open = false
	p.cancelProbe()
	p.cancelGrepSearch()
	p.preview.reset()
	return func() tea.Msg {
		return GrepExportMsg{Root: root, Query: query, Hits: hits, Total: total, Remote: p.opts.Remote, Notice: p.grep.notice, Truncated: p.grep.truncated, Owner: owner}
	}
}

// refreshGrep re-runs the search after a mode or scope changed. It numbers the search
// as a new one, so a result still in flight is dropped, and it keeps the rows on screen
// until the new result arrives.
func (p *Palette) refreshGrep() tea.Cmd {
	p.seq++
	p.grep.done = false
	p.grep.hits = nil
	p.grep.notice, p.grep.truncated = "", false
	return p.grepCmd(p.seq, p.input.Value())
}

// liveQuery is requery in Live Grep mode. An empty query has no rows and searches
// nothing. Any other query searches, and its rows arrive as a GrepResultMsg, so the
// rows on screen stay until then.
func (p *Palette) liveQuery(seq int, text string) tea.Cmd {
	if text == "" {
		p.cancelGrepSearch()
		p.rows = nil
		p.grep.done, p.grep.count, p.grep.err, p.grep.text = false, 0, nil, ""
		p.grep.notice, p.grep.truncated = "", false
		p.grep.hits = nil
		p.syncSize()
		p.nav.Select(0)
		return nil
	}
	if text != p.grep.text {
		p.grep.notice, p.grep.truncated = "", false
	}
	return p.grepCmd(seq, text)
}

// grepCmd returns the command that searches for text with the current modes and scopes.
// The file list and the search run in the command, never in Update or Draw. It returns
// nil when no Grepper is wired.
func (p *Palette) grepCmd(seq int, text string) tea.Cmd {
	// A newer query/scope supersedes the previous search: cancel its context before
	// starting another (the remote adapter stops the streaming authority command).
	p.cancelGrepSearch()
	if p.opts.Grep == nil || text == "" {
		return nil
	}
	owner := p.owner
	token := p.token
	g, files, root := p.opts.Grep, p.opts.Files, p.opts.Root
	buffers, terminals, query, sources := p.opts.SourceBuffers, p.opts.SourceTerminals, p.grep.opts, p.grep.sources
	diagnostics := p.opts.SourceDiagnostics
	contextGrep, cancellable := g.(ContextGrepper)
	run := func(ctx context.Context) tea.Msg {
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
		var result GrepResult
		var err error
		if cancellable {
			result, err = contextGrep.SearchContext(ctx, req)
		} else {
			result.Hits, err = g.Search(req)
		}
		if err == nil && sources.Diagnostics {
			var diagHits []GrepHit
			diagHits, err = searchDiagnostics(ctx, diagnostics, text, query, grepLimit-len(result.Hits))
			result.Hits = append(result.Hits, diagHits...)
		}
		return GrepResultMsg{Token: token, Seq: seq, Text: text, Hits: result.Hits, Err: err, Notice: result.Notice, Truncated: result.Truncated, Owner: owner}
	}
	// The command is always cancelable: a ContextGrepper uses the context to stop its
	// own IO, and the in-memory diagnostics scan checks it between messages. Update
	// never waits for either.
	ctx, cancel := context.WithCancel(context.Background())
	p.grep.cancel = cancel
	return func() tea.Msg { return run(ctx) }
}

// searchDiagnostics matches the Diagnostics scope's messages with the same query
// modes as the text sources. The snapshot is in memory, so this runs in the search
// command off the update goroutine; at most limit hits are returned, one per
// diagnostic, after the file/buffer/terminal hits.
func searchDiagnostics(ctx context.Context, diags []DiagnosticSource, text string, opts SearchOptions, limit int) ([]GrepHit, error) {
	if len(diags) == 0 || limit <= 0 || text == "" {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q := search.Query{Text: text, CaseSensitive: opts.CaseSensitive, WholeWord: opts.WholeWord, Regex: opts.Regex}
	var hits []GrepHit
	for _, d := range diags {
		if err := ctx.Err(); err != nil {
			return hits, err
		}
		if len(hits) >= limit {
			break
		}
		b := buffer.New(d.Text)
		matches, err := search.Find(b, q)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			continue
		}
		m := matches[0]
		start := runeOffsetInText(d.Text, m.Start)
		end := runeOffsetInText(d.Text, m.End)
		hits = append(hits, GrepHit{
			Rel: d.Rel, Line: d.Line, Col: d.Col, Text: d.Text,
			Source: GrepDiagnostics, Path: d.Path,
			TextCol: start + 1, TextLen: max(end-start, 1),
		})
	}
	return hits, nil
}

// runeOffsetInText is the rune offset of p in text, whose lines are separated by
// newlines. It is how a message match becomes a Text offset, independent of the
// document line/column the diagnostic points at.
func runeOffsetInText(text string, p buffer.Pos) int {
	lines := strings.Split(text, "\n")
	off := 0
	for i := 0; i < p.Line && i < len(lines); i++ {
		off += utf8.RuneCountInString(lines[i]) + 1
	}
	if p.Line < len(lines) {
		off += min(p.Col, utf8.RuneCountInString(lines[p.Line]))
	}
	return off
}

// applyGrep shows a live grep result when it answers the current query. The highlight
// goes to the top when the text changed. The result also sets the count the title
// shows.
func (p *Palette) applyGrep(msg GrepResultMsg) tea.Cmd {
	if msg.Token != p.token || msg.Owner != p.owner || msg.Seq != p.seq || !p.live {
		return nil
	}
	p.grep.done, p.grep.text, p.grep.err = true, msg.Text, msg.Err
	p.grep.cancel = nil
	p.grep.notice, p.grep.truncated = msg.Notice, msg.Truncated
	p.grep.count = len(msg.Hits)
	// Keep the hits for the quickfix export (Alt+Q); rows are display-only and lose
	// the raw positions/identity the export needs.
	p.grep.hits = append([]GrepHit(nil), msg.Hits...)
	p.rows = grepRows(msg)
	p.syncSize()
	if msg.Text != p.shown {
		p.nav.Select(0)
	}
	p.shown = msg.Text
	return nil
}

// restoreLive applies a Resume snapshot's controls. It never restores cached hits:
// file contents (and the search's result set) may have changed while the palette was
// closed, and there is no low-cost content fingerprint in the search port. Init then
// runs a normal search with the restored query/modes/scopes.
func (p *Palette) restoreLive(resume LiveResume) {
	p.grep.opts = resume.Options
	p.grep.sources = resume.Sources
	if !p.grep.buffers {
		p.grep.sources.Buffers = false
	}
	if !p.grep.terminals {
		p.grep.sources.Terminals = false
	}
	if !p.grep.diagnostics {
		p.grep.sources.Diagnostics = false
	}
}

// LiveResumeState snapshots the Live Grep controls for the root's Resume command: the
// query, match modes and scope toggles. Hits and the selected row are not captured,
// because Resume always searches again (fresh rows and content).
func (p *Palette) LiveResumeState() *LiveResume {
	if p == nil || !p.live {
		return nil
	}
	return &LiveResume{
		Text:    p.input.Value(),
		Options: p.grep.opts,
		Sources: p.grep.sources,
	}
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
	out := fmt.Sprintf("%s · %d%s matches", title, p.grep.count, mark)
	if p.grep.truncated {
		out += " · incomplete"
	}
	return out
}

// liveCompletenessHint is the non-fatal completeness signal shown on the Live Grep footer:
// the adapter's degradation/cap notice when present, or a short "result incomplete" for a
// truncated result without a longer explanation. The successful hits stay on screen.
func (p *Palette) liveCompletenessHint() string {
	if p.grep.notice != "" {
		return p.grep.notice
	}
	if p.grep.truncated {
		return "result incomplete"
	}
	return ""
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
	if p.grep.diagnostics {
		segs = append(segs, toolbarSeg{text: "Alt+D Diagnostics:" + onOff(p.grep.sources.Diagnostics) + "  ", on: p.grep.sources.Diagnostics})
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
	rawHit := h.Col - 1
	qlen = utf8.RuneCountInString(textsafe.Clean(query))
	if h.Source == GrepDiagnostics && h.TextCol > 0 {
		// Col is the document column; the diagnostic row highlights the match in
		// the message instead, and a regex match may be longer than the query.
		rawHit = h.TextCol - 1
		qlen = max(h.TextLen, 1)
	}
	rawHit = min(max(rawHit, 0), len(rawRunes))
	hitClean := textsafe.MapIndex(mapping, rawHit)
	display := []rune(cleanText)
	mcol = min(max(hitClean-cleanLead, 0), len(display))
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
		case GrepDiagnostics:
			r.source = "diagnostic"
			r.grepSource = GrepDiagnostics
			r.diagPath = h.Path
		case GrepTerminals:
			r.source = "terminal"
			r.grepSource = GrepTerminals
			r.index = h.Index
			r.termID = h.TerminalID
			r.termTotal = h.TerminalTotal
			r.termText = h.Text
			r.termBefore, r.termAfter = h.TerminalBefore, h.TerminalAfter
			r.termSnapshot = h.TerminalSnapshot
			r.termValid = h.TerminalValid
			r.termBeforeKnown, r.termAfterKnown = h.TerminalBeforeKnown, h.TerminalAfterKnown
			if h.Line > 0 {
				r.termRow = h.TranscriptFirst + h.Line - 1
			}
		}
		rows = append(rows, r)
	}
	return rows
}
