package palette

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeGrepper records each search request and answers with hits, or with err.
type fakeGrepper struct {
	hits []GrepHit
	err  error
	reqs []GrepRequest
}

func (g *fakeGrepper) Search(req GrepRequest) ([]GrepHit, error) {
	g.reqs = append(g.reqs, req)
	if g.err != nil {
		return nil, g.err
	}
	var out []GrepHit
	for _, h := range g.hits {
		if strings.Contains(h.Text, req.Text) {
			out = append(out, h)
		}
	}
	return out, nil
}

// liveOptions are the options of a live grep palette over files and g.
func liveOptions(query string, files *fakeFiles, g *fakeGrepper) Options {
	return Options{Live: true, Query: query, Root: "/w", Files: files, Grep: g}
}

// altKey is an Alt+letter press, the Live Grep toggle key.
func altKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: tea.ModAlt})
}

// TestLiveGrepSearchesInACommandOverTheFiles checks that the search runs in the
// command the palette returns, over the file list under the root, and that typing
// does not search in Update. It also pins the starting modes and scopes: Fresh's
// defaults (regex on, word off, files on).
func TestLiveGrepSearchesInACommandOverTheFiles(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{}
	files := &fakeFiles{files: []string{"a.go", "src/b.go"}}
	p := New(liveOptions("", files, g))
	draw(p, 100, 30)
	cmd := p.update(typed("t"))
	if len(g.reqs) != 0 {
		t.Fatalf("typing searched %d times in Update, want 0", len(g.reqs))
	}
	if cmd == nil {
		t.Fatal("typing returned no search command")
	}
	cmd()
	if len(g.reqs) != 1 || g.reqs[0].Text != "t" {
		t.Fatalf("searches = %v, want one search for t", g.reqs)
	}
	req := g.reqs[0]
	if req.Root != "/w" || len(req.Files) != 2 {
		t.Fatalf("search root %q over %v, want /w over both files", req.Root, req.Files)
	}
	if req.Limit != fileLimit {
		t.Fatalf("limit = %d, want %d", req.Limit, fileLimit)
	}
	if !req.Query.Regex || req.Query.WholeWord || req.Query.CaseSensitive {
		t.Fatalf("starting modes = %+v, want regex on, word off, case folded", req.Query)
	}
	if !req.Sources.Files || req.Sources.Buffers || req.Sources.Ignored {
		t.Fatalf("starting scopes = %+v, want files on only", req.Sources)
	}
}

// TestLiveGrepShowsMatchesAndEnterOpensTheLine checks the rows of the hits, and that
// Enter on a row opens its file under the root at the match's line and column.
func TestLiveGrepShowsMatchesAndEnterOpensTheLine(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{hits: []GrepHit{{Rel: "src/b.go", Line: 3, Col: 5, Text: "	func todo() {}"}}}
	files := &fakeFiles{files: []string{"src/b.go"}}
	p := New(liveOptions("", files, g))
	draw(p, 100, 30)
	settle(t, p, p.update(typed("t")), 8)
	for _, c := range "odo" {
		settle(t, p, p.update(typed(string(c))), 8)
	}
	g2 := draw(p, 100, 30)
	if !hasRow(g2, "src/b.go:3: func todo() {}") {
		t.Fatalf("hit row not shown:\n%s", g2)
	}
	want := OpenLineMsg{Path: "/w/src/b.go", Line: 3, Col: 5}
	if got := runCmd(p.update(key(tea.KeyEnter))); got != want {
		t.Fatalf("Enter emitted %#v, want %#v", got, want)
	}
	if p.Active() {
		t.Fatal("palette still open after a match was chosen")
	}
}

// TestLiveGrepEmptyQueryHasNoRowsAndNoSearch checks that an empty query searches
// nothing and shows the hint to type.
func TestLiveGrepEmptyQueryHasNoRowsAndNoSearch(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{}
	p := New(liveOptions("", &fakeFiles{files: []string{"a.go"}}, g))
	if cmd := p.Init(); cmd != nil {
		t.Fatal("Init with an empty query returned a search")
	}
	g2 := draw(p, 100, 30)
	if !hasRow(g2, "Type to search the files") {
		t.Fatalf("empty live grep does not say so:\n%s", g2)
	}
	if len(g.reqs) != 0 {
		t.Fatalf("empty query searched %v", g.reqs)
	}
}

// TestLiveGrepResumedQueryRunsAtOpen checks that a palette opened with a query, as
// resume does, searches that query at once.
func TestLiveGrepResumedQueryRunsAtOpen(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{hits: []GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}}
	p := New(liveOptions("todo", &fakeFiles{files: []string{"a.go"}}, g))
	draw(p, 100, 30)
	settle(t, p, p.Init(), 8)
	if !hasRow(draw(p, 100, 30), "a.go:1: todo") {
		t.Fatal("the resumed query shows no rows")
	}
}

// TestLiveGrepQueryHasNoModePrefix checks that a query that starts with ">" is
// searched as text. Live grep has no prefixes.
func TestLiveGrepQueryHasNoModePrefix(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{}
	p := New(liveOptions(">x", &fakeFiles{}, g))
	settle(t, p, p.Init(), 8)
	if len(g.reqs) != 1 || g.reqs[0].Text != ">x" {
		t.Fatalf("searches = %v, want the whole query >x", g.reqs)
	}
}

// TestLiveGrepStaleResultIsDropped checks that a result for a query that was typed
// over does not replace the rows of the current query.
func TestLiveGrepStaleResultIsDropped(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("", &fakeFiles{}, &fakeGrepper{}))
	draw(p, 100, 30)
	p.update(typed("a"))
	p.update(typed("b"))
	p.update(GrepResultMsg{Token: p.token, Seq: 2, Text: "a", Hits: []GrepHit{{Rel: "old.go", Line: 1, Col: 1, Text: "a"}}})
	if hasRow(draw(p, 100, 30), "old.go") {
		t.Fatal("a stale result replaced the rows of the current query")
	}
}

// TestLiveGrepErrorIsShownAsARow checks that a failed search shows its error in the
// box, not as an empty result, and that the title says error.
func TestLiveGrepErrorIsShownAsARow(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("", &fakeFiles{}, &fakeGrepper{}))
	draw(p, 100, 30)
	p.update(typed("a"))
	p.update(GrepResultMsg{Token: p.token, Seq: p.seq, Text: "a", Err: errors.New("read failed")})
	g := draw(p, 100, 30)
	if !hasRow(g, "Live Grep: read failed") {
		t.Fatal("the search error is not shown")
	}
	if !hasRow(g, "Live Grep · error") {
		t.Fatalf("the title does not say error:\n%s", g)
	}
}

// TestLiveGrepTitleShowsTheCount checks the match count in the title, the "N+" mark at
// the row cap, and the no-results state.
func TestLiveGrepTitleShowsTheCount(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("", &fakeFiles{}, &fakeGrepper{}))
	draw(p, 100, 30)
	p.update(typed("a"))
	p.update(GrepResultMsg{Token: p.token, Seq: p.seq, Text: "a", Hits: []GrepHit{
		{Rel: "a.go", Line: 1, Col: 1, Text: "a"},
		{Rel: "a.go", Line: 2, Col: 1, Text: "a"},
	}})
	if g := draw(p, 100, 30); !hasRow(g, "Live Grep · 2 matches") {
		t.Fatalf("the title does not show the count:\n%s", g)
	}
	// The cap is marked: a full result may have more hits.
	full := make([]GrepHit, grepLimit)
	for i := range full {
		full[i] = GrepHit{Rel: "a.go", Line: i + 1, Col: 1, Text: "a"}
	}
	p.update(GrepResultMsg{Token: p.token, Seq: p.seq, Text: "a", Hits: full})
	if g := draw(p, 100, 30); !hasRow(g, "Live Grep · 100+ matches") {
		t.Fatalf("the capped count is not marked:\n%s", g)
	}
	p.update(GrepResultMsg{Token: p.token, Seq: p.seq, Text: "a"})
	g := draw(p, 100, 30)
	if !hasRow(g, "Live Grep · no matches") || !hasRow(g, "No matches") {
		t.Fatalf("the no-results state is not shown:\n%s", g)
	}
}

// TestLiveGrepToolbarShowsModesAndScopes checks the toolbar row: every toggle with its
// key and state, Fresh's defaults, and the Buffers scope left out when unavailable.
func TestLiveGrepToolbarShowsModesAndScopes(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("", &fakeFiles{}, &fakeGrepper{}))
	g := draw(p, 120, 30)
	geo := p.layout(120, 30)
	if geo.toolbarY < 0 || geo.toolbarY >= geo.rowsTop {
		t.Fatalf("toolbar row = %d, rows start at %d", geo.toolbarY, geo.rowsTop)
	}
	row := rowText(g, geo.toolbarY)
	for _, want := range []string{
		"Alt+C Case:off", "Alt+G Regex:on", "Alt+O Word:off", "Alt+L Files:on", "Alt+I Ignored:off",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("toolbar %q does not show %q", row, want)
		}
	}
	if strings.Contains(row, "Alt+U Buffers") {
		t.Errorf("toolbar %q offers the Buffers scope with no buffer snapshot", row)
	}
	if strings.Contains(row, "Alt+T Terminals") {
		t.Errorf("toolbar %q offers the Terminals scope with no dock snapshot", row)
	}
	// With a snapshot the scope is offered, and it starts off.
	opts := liveOptions("", &fakeFiles{}, &fakeGrepper{})
	opts.SourceBuffers = []BufferSource{{Index: 0, Name: "a.go", Text: "x"}}
	withBuffers := New(opts)
	row = rowText(draw(withBuffers, 120, 30), withBuffers.layout(120, 30).toolbarY)
	if !strings.Contains(row, "Alt+U Buffers:off") {
		t.Errorf("toolbar %q does not offer the Buffers scope", row)
	}
	// The Terminals scope is offered only with a dock snapshot, and starts on
	// (Fresh's default for terminal scrollback).
	termOpts := liveOptions("", &fakeFiles{}, &fakeGrepper{})
	termOpts.SourceTerminals = []TerminalSource{{Index: 0, Name: "shell", Lines: []string{"x"}}}
	withTerms := New(termOpts)
	row = rowText(draw(withTerms, 140, 30), withTerms.layout(140, 30).toolbarY)
	if !strings.Contains(row, "Alt+T Terminals:on") {
		t.Errorf("toolbar %q does not offer the Terminals scope on by default", row)
	}
}

// TestLiveGrepToggleKeysChangeTheRequest checks each toggle key: it flips the state
// shown in the toolbar and the next search carries the new mode or scope, in a new
// request (the result of the old one is dropped by its sequence number).
func TestLiveGrepToggleKeysChangeTheRequest(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{}
	files := &fakeFiles{files: []string{"a.go"}}
	opts := liveOptions("t", files, g)
	opts.SourceBuffers = []BufferSource{{Index: 0, Name: "a.go", Text: "t"}}
	p := New(opts)
	draw(p, 120, 30)
	p.update(GrepResultMsg{Token: p.token, Seq: p.seq, Text: "t"})

	tests := []struct {
		name  string
		key   tea.KeyPressMsg
		check func(GrepRequest) bool
	}{
		{"case on", altKey('c'), func(r GrepRequest) bool { return r.Query.CaseSensitive }},
		{"regex off", altKey('g'), func(r GrepRequest) bool { return !r.Query.Regex }},
		{"word on", altKey('o'), func(r GrepRequest) bool { return r.Query.WholeWord }},
		{"files off", altKey('l'), func(r GrepRequest) bool { return !r.Sources.Files }},
		{"buffers on", altKey('u'), func(r GrepRequest) bool { return r.Sources.Buffers }},
		{"ignored on", altKey('i'), func(r GrepRequest) bool { return r.Sources.Ignored }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g.reqs = nil
			cmd := p.update(tt.key)
			if cmd == nil {
				t.Fatalf("%s returned no new search", tt.name)
			}
			cmd()
			if len(g.reqs) != 1 || !tt.check(g.reqs[0]) {
				t.Fatalf("%s searched with %+v", tt.name, g.reqs)
			}
		})
	}
}

// TestLiveGrepBufferHitActivatesTheTab checks that a hit from the Buffers scope is
// tagged in its row and that Enter activates its tab at the match instead of opening a
// file.
func TestLiveGrepBufferHitActivatesTheTab(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{hits: []GrepHit{
		{Rel: "notes.md", Line: 4, Col: 2, Text: "todo here", Source: GrepBuffers, Index: 1},
	}}
	opts := liveOptions("todo", &fakeFiles{files: []string{"a.go"}}, g)
	opts.SourceBuffers = []BufferSource{{Index: 1, Name: "notes.md", Text: "todo here"}}
	p := New(opts)
	draw(p, 120, 30)
	settle(t, p, p.Init(), 8)
	if !hasRow(draw(p, 120, 30), "notes.md:4: todo here") {
		t.Fatal("the buffer hit is not shown")
	}
	if got := runCmd(p.update(key(tea.KeyEnter))); got != (OpenBufferLineMsg{Index: 1, Line: 4, Col: 2}) {
		t.Fatalf("Enter emitted %#v, want the buffer activation", got)
	}
}

// TestLiveGrepDiagnosticsScope: Alt+D offers the Diagnostics scope when the root
// supplied a diagnostic snapshot, toggles it off by default, and the search command
// converges the diagnostic message into a row whose Enter opens the document through
// OpenDiagnosticMsg.
func TestLiveGrepDiagnosticsScope(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{}
	opts := liveOptions("", &fakeFiles{}, g)
	opts.SourceDiagnostics = []DiagnosticSource{
		{Path: "/w/a.go", Rel: "a.go", Line: 2, Col: 40, Text: "todo bad"},
	}
	p := New(opts)
	draw(p, 140, 30)
	row := rowText(draw(p, 140, 30), p.layout(140, 30).toolbarY)
	if !strings.Contains(row, "Alt+D Diagnostics:off") {
		t.Fatalf("toolbar %q does not offer the Diagnostics scope", row)
	}
	settle(t, p, p.update(typed("t")), 8)
	settle(t, p, p.update(altKey('d')), 8)
	if got := g.reqs[len(g.reqs)-1]; !got.Sources.Diagnostics {
		t.Fatalf("Alt+D did not turn Diagnostics on: %+v", got.Sources)
	}
	if !hasRow(draw(p, 140, 30), "a.go:2: todo bad") {
		t.Fatalf("diagnostic row not shown:\n%s", draw(p, 140, 30))
	}
	want := OpenDiagnosticMsg{Path: "/w/a.go", Line: 2, Col: 40}
	if got := runCmd(p.update(key(tea.KeyEnter))); got != want {
		t.Fatalf("Enter emitted %#v, want %#v", got, want)
	}
}

// TestDiagnosticHighlightUsesMessageMatch: a diagnostic's document column (where Enter
// jumps) is not an offset into its message. The row highlight follows the regex match
// inside the message, including a match longer than the query, while the row keeps the
// document column for Enter/quickfix.
func TestDiagnosticHighlightUsesMessageMatch(t *testing.T) {
	t.Parallel()
	diags := []DiagnosticSource{{
		Path: "/w/a.go", Rel: "a.go", Line: 2, Col: 40,
		Text: "prefix ttt suffix",
	}}
	hits, err := searchDiagnostics(diags, "t+", SearchOptions{Regex: true}, 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("searchDiagnostics = %v, %v; want one hit", hits, err)
	}
	hit := hits[0]
	if hit.Line != 2 || hit.Col != 40 {
		t.Fatalf("document position = %d:%d, want 2:40", hit.Line, hit.Col)
	}
	if hit.TextCol != 8 || hit.TextLen != 3 {
		t.Fatalf("message match = col %d len %d, want col 8 len 3", hit.TextCol, hit.TextLen)
	}
	grepLine := grepRow(hit, "t+")
	if grepLine.line != 2 || grepLine.col != 40 {
		t.Fatalf("row target = %d:%d, want document 2:40", grepLine.line, grepLine.col)
	}
	if grepLine.mcol != 7 || grepLine.qlen != 3 {
		t.Fatalf("row highlight = offset %d len %d, want 7 and 3", grepLine.mcol, grepLine.qlen)
	}
	if !strings.Contains(grepLine.name, "a.go:2: prefix ttt suffix") {
		t.Fatalf("row label = %q", grepLine.name)
	}
	// The rendered match is bold at the message offset, not at the document column.
	p := New(Options{})
	p.rows = []row{grepLine}
	p.nav.SetSize(1, 10)
	g := draw(p, 100, 30)
	geo := layoutFor(100, 30)
	label := rowText(g, geo.top+1)
	runes := []rune(label)
	at := -1
	for i := 0; i+3 <= len(runes); i++ {
		if string(runes[i:i+3]) == "ttt" {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("row %q has no match text", label)
	}
	for i := 0; i < 3; i++ {
		if !g.At(at+i, geo.top+1).Style.Bold {
			t.Fatalf("match rune %d is not bold in %q", i, label)
		}
	}
}

// TestLiveGrepNoSourcesSaysSo checks the state where both sources are off.
func TestLiveGrepNoSourcesSaysSo(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("todo", &fakeFiles{}, &fakeGrepper{}))
	p.grep.sources.Files = false
	g := draw(p, 100, 30)
	if !hasRow(g, "Turn on Files, Buffers, Terminals or Diagnostics to search") {
		t.Fatalf("all sources off does not say so:\n%s", g)
	}
}

// Live Grep has no mode prefixes, so its hint row must not advertise the quick-open ones (PC54).
func TestLiveGrepHintRowAdvertisesNoPrefixes(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("", &fakeFiles{}, &fakeGrepper{}))
	g := draw(p, 100, 30)
	foot := rowText(g, p.layout(100, 30).bottom)
	if strings.Contains(foot, ">command") || strings.Contains(foot, "#buffer") || strings.Contains(foot, ":line") {
		t.Errorf("hint row %q lists quick-open prefixes", foot)
	}
	if !strings.Contains(foot, "Enter open") {
		t.Errorf("hint row %q does not say what Enter does", foot)
	}
	q := draw(New(Options{Query: ">save"}), 100, 30)
	if got := rowText(q, layoutFor(100, 30).bottom); !strings.Contains(got, ">command") {
		t.Errorf("the quick-open hint row changed: %q", got)
	}
}

// TestLiveResumeRestoresControlsAndSearchesAgain: Resume restores query, modes and
// scopes but never cached rows; Init runs a fresh search because file contents may
// have changed while the palette was closed.
func TestLiveResumeRestoresControlsAndSearchesAgain(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{hits: []GrepHit{{Rel: "fresh.go", Line: 2, Col: 1, Text: "todo fresh"}}}
	files := &fakeFiles{files: []string{"fresh.go"}}
	opts := liveOptions("todo", files, g)
	opts.Resume = &LiveResume{
		Text:    "todo",
		Options: SearchOptions{Regex: true, WholeWord: true},
		Sources: GrepSources{Files: true},
	}
	p := New(opts)
	if !p.grep.opts.WholeWord || !p.grep.sources.Files {
		t.Fatalf("resume controls = %+v %+v", p.grep.opts, p.grep.sources)
	}
	if len(p.rows) != 0 {
		t.Fatalf("resume restored rows before searching: %+v", p.rows)
	}
	cmd := p.Init()
	if cmd == nil {
		t.Fatal("resume did not start a search")
	}
	if len(g.reqs) != 0 {
		t.Fatalf("resume searched in New/Init synchronously: %+v", g.reqs)
	}
	p.update(cmd())
	if len(g.reqs) != 1 || g.reqs[0].Text != "todo" || !g.reqs[0].Query.WholeWord {
		t.Fatalf("searches = %+v, want one todo search with Whole Word", g.reqs)
	}
	if len(p.rows) == 0 || !strings.Contains(p.rows[0].name, "fresh.go") {
		t.Fatalf("rows = %+v, want the fresh result", p.rows)
	}
	state := p.LiveResumeState()
	if state == nil || state.Text != "todo" || !state.Options.WholeWord || !state.Sources.Files {
		t.Fatalf("resume snapshot = %+v", state)
	}
}

// TestLiveResumeRerunsWhenBuffersOrTerminalsAreScoped: buffer and terminal hits carry
// an index into an in-memory tab snapshot that can change while the palette is closed,
// so Resume must not restore them; it keeps the query/scopes and searches again.
func TestLiveResumeRerunsWhenBuffersOrTerminalsAreScoped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func(*Options)
		sources GrepSources
		want    func(GrepRequest) bool
	}{
		{
			name: "buffers",
			setup: func(o *Options) {
				o.SourceBuffers = []BufferSource{{Index: 1, Name: "notes.md", Text: "todo"}}
			},
			sources: GrepSources{Files: true, Buffers: true},
			want:    func(r GrepRequest) bool { return r.Sources.Buffers && len(r.Buffers) == 1 },
		},
		{
			name: "terminals",
			setup: func(o *Options) {
				o.SourceTerminals = []TerminalSource{{Index: 1, Name: "term", Lines: []string{"todo"}}}
			},
			sources: GrepSources{Files: true, Terminals: true},
			want:    func(r GrepRequest) bool { return r.Sources.Terminals && len(r.Terminals) == 1 },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &fakeGrepper{hits: []GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}}
			files := &fakeFiles{files: []string{"a.go"}}
			opts := liveOptions("todo", files, g)
			tt.setup(&opts)
			opts.Resume = &LiveResume{
				Text:    "todo",
				Sources: tt.sources,
				Options: SearchOptions{Regex: true},
			}
			p := New(opts)
			cmd := p.Init()
			if cmd == nil {
				t.Fatal("resume did not start a search")
			}
			p.update(cmd())
			if len(g.reqs) != 1 || !tt.want(g.reqs[0]) {
				t.Fatalf("searches = %+v, want one request with the %s scope", g.reqs, tt.name)
			}
			if len(p.rows) == 0 || p.rows[0].name == "stale.go:9: todo" {
				t.Fatalf("rows = %+v, want the fresh result, not the stale hit", p.rows)
			}
		})
	}
}

// TestLiveGrepTerminalScope checks the Terminals scope: a supplied dock snapshot
// offers it, it starts on with the snapshot (stable ids and transcript offsets)
// copied into the request, Alt+T toggles it, and Enter on a terminal hit reports the
// stable dock tab id plus the absolute transcript row to reveal.
func TestLiveGrepTerminalScope(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{hits: []GrepHit{
		{Rel: "shell", Line: 2, Col: 4, Text: "xx todo output", Source: GrepTerminals, Index: 1,
			TerminalID: 9, TranscriptFirst: 40, TerminalTotal: 60, TerminalBefore: "out", TerminalAfter: "tail"},
	}}
	opts := liveOptions("todo", &fakeFiles{files: []string{"a.go"}}, g)
	opts.SourceTerminals = []TerminalSource{
		{ID: 7, Index: 0, Name: "first", Total: 20, Lines: []string{"nothing"}},
		{ID: 9, Index: 1, Name: "shell", First: 40, Total: 60, Lines: []string{"out", "xx todo output", "tail"}},
	}
	p := New(opts)
	draw(p, 140, 30)
	settle(t, p, p.Init(), 8)
	if len(g.reqs) != 1 || !g.reqs[0].Sources.Terminals || len(g.reqs[0].Terminals) != 2 {
		t.Fatalf("initial terminal request = %+v", g.reqs)
	}
	if got := g.reqs[0].Terminals; got[0].ID != 7 || got[0].Index != 0 || got[0].Name != "first" ||
		got[1].ID != 9 || got[1].First != 40 || got[1].Total != 60 || len(got[1].Lines) != 3 {
		t.Fatalf("terminal snapshot = %+v", got)
	}
	if !hasRow(draw(p, 140, 30), "shell:2: xx todo output") {
		t.Fatal("the terminal hit is not shown")
	}
	// Alt+T turns the scope off and re-runs with no terminal snapshot.
	g.reqs = nil
	settle(t, p, p.update(altKey('t')), 8)
	if len(g.reqs) != 1 || g.reqs[0].Sources.Terminals || len(g.reqs[0].Terminals) != 0 {
		t.Fatalf("terminal-off request = %+v", g.reqs)
	}
	g.reqs = nil
	settle(t, p, p.update(altKey('t')), 8)
	if len(g.reqs) != 1 || !g.reqs[0].Sources.Terminals {
		t.Fatalf("terminal-on request = %+v", g.reqs)
	}
	p.update(GrepResultMsg{Token: p.token, Seq: p.seq, Text: "todo", Hits: g.hits})
	want := OpenTerminalMsg{ID: 9, Row: 41, Index: 1, Total: 60,
		Text: "xx todo output", Before: "out", After: "tail"}
	if got := runCmd(p.update(key(tea.KeyEnter))); got != want {
		t.Fatalf("Enter emitted %#v, want the stable tab id + transcript row %#v", got, want)
	}
}

// TestBoundTerminalSources caps the snapshot a request can copy: tab count, rows and
// bytes per tab, and total bytes. The newest rows are kept and First advances by the
// dropped prefix so a hit still maps to its combined scrollback row.
func TestBoundTerminalSources(t *testing.T) {
	t.Parallel()
	longRows := make([]string, 0, 600)
	for i := 0; i < 600; i++ {
		longRows = append(longRows, "line")
	}
	src := make([]TerminalSource, 0, 20)
	src = append(src, TerminalSource{ID: 3, Index: 0, Name: "many", First: 7, Lines: longRows})
	for i := 1; i < 20; i++ {
		src = append(src, TerminalSource{ID: uint64(i + 1), Index: i, Name: "t", Lines: []string{"x"}})
	}
	got := boundTerminalSources(src)
	if len(got) != GrepTerminalsMaxTabs {
		t.Fatalf("tabs = %d, want %d", len(got), GrepTerminalsMaxTabs)
	}
	if len(got[0].Lines) != GrepTerminalsMaxRows || got[0].Lines[len(got[0].Lines)-1] != "line" {
		t.Fatalf("rows = %d, want the newest %d", len(got[0].Lines), GrepTerminalsMaxRows)
	}
	if want := 7 + len(longRows) - GrepTerminalsMaxRows; got[0].First != want {
		t.Fatalf("First = %d, want %d after the tail crop", got[0].First, want)
	}
	// One over-long line cannot enter the per-tab byte budget; the tab keeps no lines
	// rather than truncating a line's middle, and its offset stays at its start.
	over := boundTerminalSources([]TerminalSource{{ID: 4, Index: 3, Name: "big", First: 11,
		Lines: []string{strings.Repeat("x", GrepTerminalsMaxTabBytes+1)}}})
	if len(over) != 1 || len(over[0].Lines) != 0 || over[0].First != 12 {
		t.Fatalf("over-long line snapshot = %+v", over)
	}
	// The total byte cap leaves later tabs out once it is spent.
	filler := strings.Repeat("x", 1000)
	rows := make([]string, 0, GrepTerminalsMaxRows)
	for i := 0; i < GrepTerminalsMaxRows; i++ {
		rows = append(rows, filler)
	}
	big := make([]TerminalSource, 0, 10)
	for i := 0; i < 10; i++ {
		big = append(big, TerminalSource{ID: uint64(i + 30), Index: i, Lines: rows})
	}
	total := 0
	for _, term := range boundTerminalSources(big) {
		for _, line := range term.Lines {
			total += len(line) + 1
		}
	}
	if total > GrepTerminalsMaxBytes {
		t.Fatalf("total snapshot bytes = %d, over the %d cap", total, GrepTerminalsMaxBytes)
	}
}

// TestLiveGrepRowOpenMessagesCarryPaletteOwner: the rowGrep path builds its open
// messages outside row.command's helper, so every source must still stamp the palette's
// workspace/model owner; a zero owner here would bypass the root guard.
func TestLiveGrepRowOpenMessagesCarryPaletteOwner(t *testing.T) {
	t.Parallel()
	owner := Owner{Workspace: "ws-a", Generation: 7}
	tests := []struct {
		name string
		hit  GrepHit
		want any
	}{
		{"file", GrepHit{Rel: "a.go", Line: 2, Col: 3, Text: "todo file"}, OpenLineMsg{}},
		{"buffer", GrepHit{Rel: "buf", Line: 4, Col: 2, Text: "todo buffer", Source: GrepBuffers, Index: 1}, OpenBufferLineMsg{}},
		{"terminal", GrepHit{Rel: "term", Line: 5, Col: 1, Text: "todo term", Source: GrepTerminals, Index: 1, TerminalID: 9, TranscriptFirst: 40}, OpenTerminalMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(Options{
				Live: true, Query: "todo", Root: "/w", Owner: owner,
				Files:         &fakeFiles{files: []string{"a.go"}},
				Grep:          &fakeGrepper{},
				SourceBuffers: []BufferSource{{Name: "buf", Index: 1, Text: "todo buffer"}},
				// A non-nil terminal snapshot offers the scope; the hit below is what
				// the row command opens.
				SourceTerminals: []TerminalSource{{Name: "term", Index: 1, Lines: []string{"todo term"}}},
			})
			p.seq = 1
			p.applyGrep(GrepResultMsg{Token: p.token, Seq: 1, Text: "todo", Hits: []GrepHit{tt.hit}, Owner: owner})
			if len(p.rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(p.rows))
			}
			msg := runCmd(p.update(key(tea.KeyEnter)))
			owned, ok := msg.(OwnedMsg)
			if !ok {
				t.Fatalf("Enter emitted %T, want an OwnedMsg", msg)
			}
			if got := owned.PaletteOwner(); got != owner {
				t.Fatalf("Enter emitted owner %+v, want %+v", got, owner)
			}
			switch msg.(type) {
			case OpenLineMsg, OpenBufferLineMsg, OpenTerminalMsg:
			default:
				t.Fatalf("Enter emitted %T, want %T family", msg, tt.want)
			}
		})
	}
}
