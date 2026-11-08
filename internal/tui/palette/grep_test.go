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
	p.update(GrepResultMsg{Seq: 2, Text: "a", Hits: []GrepHit{{Rel: "old.go", Line: 1, Col: 1, Text: "a"}}})
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
	p.update(GrepResultMsg{Seq: p.seq, Text: "a", Err: errors.New("read failed")})
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
	p.update(GrepResultMsg{Seq: p.seq, Text: "a", Hits: []GrepHit{
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
	p.update(GrepResultMsg{Seq: p.seq, Text: "a", Hits: full})
	if g := draw(p, 100, 30); !hasRow(g, "Live Grep · 100+ matches") {
		t.Fatalf("the capped count is not marked:\n%s", g)
	}
	p.update(GrepResultMsg{Seq: p.seq, Text: "a"})
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
	// With a snapshot the scope is offered, and it starts off.
	opts := liveOptions("", &fakeFiles{}, &fakeGrepper{})
	opts.SourceBuffers = []BufferSource{{Index: 0, Name: "a.go", Text: "x"}}
	withBuffers := New(opts)
	row = rowText(draw(withBuffers, 120, 30), withBuffers.layout(120, 30).toolbarY)
	if !strings.Contains(row, "Alt+U Buffers:off") {
		t.Errorf("toolbar %q does not offer the Buffers scope", row)
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
	p.update(GrepResultMsg{Seq: p.seq, Text: "t"})

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

// TestLiveGrepNoSourcesSaysSo checks the state where both sources are off.
func TestLiveGrepNoSourcesSaysSo(t *testing.T) {
	t.Parallel()
	p := New(liveOptions("todo", &fakeFiles{}, &fakeGrepper{}))
	p.grep.sources.Files = false
	g := draw(p, 100, 30)
	if !hasRow(g, "Turn on Files or Buffers to search") {
		t.Fatalf("both sources off does not say so:\n%s", g)
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
