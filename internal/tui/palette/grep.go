package palette

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Live Grep is the palette's text search over the workspace files and the open
// buffers. Its query is the whole query line, with no mode prefix, and its rows are the
// matching lines. Searching reads the files, so it runs in a tea.Cmd and its result
// comes back as a GrepResultMsg. The app adapter implements Grepper over internal/search.
//
// Live Grep has a toolbar row above its rows: the match modes (case, regex, whole
// word) and the scopes (files, buffers, and ignored/hidden files inside the file
// scope). Its keys are Fresh's prompt keys, handled here because gvim's overlays answer
// their keys in code: Alt+C case, Alt+G regex, Alt+O whole word, Alt+L files, Alt+U
// buffers and Alt+I ignored. The title shows the result count after a search.

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

// GrepSources select what a Live Grep query searches. Files and Buffers are the
// sources; Ignored widens the file source to hidden and .gitignore-matched files.
type GrepSources struct {
	Files   bool
	Buffers bool
	Ignored bool
}

// BufferSource is one open buffer's text, for the Buffers scope. Index is the tab's
// position in Options.Buffers, so a hit can activate it.
type BufferSource struct {
	Name  string // the tab's label
	Path  string // the buffer's file, when it has one
	Index int    // the tab position
	Text  string // the buffer's text, LF-normalised
}

// GrepRequest is the input of one Live Grep search. The palette fills what the
// sources need: the file index snapshot for Files and Ignored, the buffer snapshot for
// Buffers.
type GrepRequest struct {
	Root    string
	Text    string         // the query
	Query   SearchOptions  // how Text is matched
	Sources GrepSources    // what is searched
	Files   []string       // the file index snapshot, for the file sources
	Buffers []BufferSource // the open buffers, for the Buffers scope
	Limit   int            // the most hits to return
}

// Grepper searches the text the request names. It returns at most Limit hits, one per
// matching line: the file hits in file order, then the buffer hits in buffer order.
// Only text is searched. Search is called from a tea.Cmd, never from Update or Draw.
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
)

// GrepHit is one matching line. Rel is the file relative to the root, or the buffer's
// label; Line and Col are the 1-based position of the match, and Text is the line.
// Source says whether the hit is in a file or a buffer, and Index is the buffer's tab
// position for a buffer hit.
type GrepHit struct {
	Rel       string
	Line, Col int
	Text      string
	Source    GrepSource
	Index     int
}

// GrepResultMsg carries the hits of one Live Grep query. The palette applies it only
// when Seq is the query it sent last. Text is the query that was searched.
type GrepResultMsg struct {
	Seq  int
	Text string
	Hits []GrepHit
	Err  error
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
	scopeIgnored
)

// grepState is Live Grep's state: the match modes, the scope toggles, whether the
// Buffers scope is available, and the last result, which the title shows.
type grepState struct {
	opts    SearchOptions
	sources GrepSources

	count   int    // the last result's hit count
	done    bool   // a result arrived for the current query
	text    string // the query of the last result
	err     error  // the last search's error, if any
	buffers bool   // the Buffers scope is available: a buffer snapshot was supplied
}

// newGrepState returns the state of the palette's Live Grep modes and scopes. The match
// modes start at Options.LiveSearch, or at Fresh's defaults when it is nil: regex on,
// whole word off and case folded. The Files scope starts on, the Buffers scope on when a
// buffer snapshot was supplied, and the Ignored scope off.
func newGrepState(opts Options) grepState {
	st := grepState{sources: GrepSources{Files: true}}
	if opts.LiveSearch != nil {
		st.opts = *opts.LiveSearch
	} else {
		st.opts.Regex = true
	}
	st.buffers = opts.SourceBuffers != nil
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
	case "alt+i":
		return p.toggleScope(scopeIgnored), true
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
	case scopeIgnored:
		p.grep.sources.Ignored = !p.grep.sources.Ignored
	}
	return p.refreshGrep()
}

// refreshGrep re-runs the search after a mode or scope changed. It numbers the search
// as a new one, so a result still in flight is dropped, and it keeps the rows on screen
// until the new result arrives.
func (p *Palette) refreshGrep() tea.Cmd {
	p.seq++
	p.grep.done = false
	return p.grepCmd(p.seq, p.input.Value())
}

// liveQuery is requery in Live Grep mode. An empty query has no rows and searches
// nothing. Any other query searches, and its rows arrive as a GrepResultMsg, so the
// rows on screen stay until then.
func (p *Palette) liveQuery(seq int, text string) tea.Cmd {
	if text == "" {
		p.rows = nil
		p.grep.done, p.grep.count, p.grep.err, p.grep.text = false, 0, nil, ""
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
	buffers, query, sources := p.opts.SourceBuffers, p.grep.opts, p.grep.sources
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
		hits, err := g.Search(req)
		return GrepResultMsg{Seq: seq, Text: text, Hits: hits, Err: err}
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
	p.rows = grepRows(msg)
	p.syncSize()
	if msg.Text != p.shown {
		p.nav.Select(0)
	}
	p.shown = msg.Text
	return nil
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
	return append(segs, toolbarSeg{text: "Alt+I Ignored:" + onOff(p.grep.sources.Ignored), on: p.grep.sources.Ignored})
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
		spaced := strings.ReplaceAll(h.Text, "\t", " ")
		text := strings.TrimSpace(spaced)
		lead := utf8.RuneCountInString(spaced) - utf8.RuneCountInString(strings.TrimLeftFunc(spaced, unicode.IsSpace))
		r := row{
			kind: rowGrep,
			name: fmt.Sprintf("%s:%d: %s", h.Rel, h.Line, text),
			rel:  h.Rel,
			line: h.Line,
			col:  h.Col,
			text: text,
			mcol: max(h.Col-1-lead, 0),
			qlen: utf8.RuneCountInString(msg.Text),
		}
		if h.Source == GrepBuffers {
			r.source = "buffer"
			r.grepSource = GrepBuffers
			r.index = h.Index
		}
		rows = append(rows, r)
	}
	return rows
}
