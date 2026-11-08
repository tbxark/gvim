package searchpanel

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// typedKey is a printable key press.
func typedKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// specialKey is a named key press.
func specialKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// altKey is an Alt+letter press.
func altKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModAlt} }

// fakeSearcher records each project search request and answers with result or err.
type fakeSearcher struct {
	result SearchResult
	err    error
	reqs   []SearchRequest
}

func (f *fakeSearcher) ProjectSearch(req SearchRequest) (SearchResult, error) {
	f.reqs = append(f.reqs, req)
	return f.result, f.err
}

// fakeGrepper records each git grep request and answers with result or err.
type fakeGrepper struct {
	result GitResult
	err    error
	reqs   []GitRequest
}

func (f *fakeGrepper) GitGrep(req GitRequest) (GitResult, error) {
	f.reqs = append(f.reqs, req)
	return f.result, f.err
}

// fakeFiles is the file index snapshot.
type fakeFiles struct{ files []string }

func (f fakeFiles) Snapshot() ([]string, bool) { return f.files, false }

// sampleResult is a result with two files and three matches.
func sampleResult() SearchResult {
	return SearchResult{Files: []FileMatches{
		{Path: "/w/a.go", Name: "a.go", Hash: 11, Matches: []Match{
			{Line: 2, Col: 3, Len: 3, Text: "func foo() {"},
			{Line: 5, Col: 1, Len: 3, Text: "foo bar"},
		}},
		{Path: "/w/b.go", Name: "b.go", Hash: 22, Dirty: true, Matches: []Match{
			{Line: 1, Col: 1, Len: 3, Text: "foo"},
		}},
	}}
}

// searchOptions are the options of a panel over a fake searcher and file list.
func searchOptions(s *fakeSearcher) Options {
	return Options{
		Root:     "/w",
		Files:    fakeFiles{files: []string{"a.go", "b.go"}},
		Searcher: s,
		Limit:    100,
		OpenFiles: []SourceFile{
			{Path: "/w/b.go", Name: "b.go", Text: "foo", Dirty: true},
		},
		Current: &SourceFile{Path: "/w/b.go", Name: "b.go", Text: "foo", Dirty: true},
	}
}

// step runs cmd to its messages, giving each to the panel, until the panel asks for no
// more work or closes. It returns the messages it delivered.
func step(t *testing.T, p *Panel, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var msgs []tea.Msg
	for i := 0; cmd != nil && i < 20; i++ {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				msgs = append(msgs, step(t, p, sub)...)
			}
			return msgs
		}
		msgs = append(msgs, msg)
		next, nextCmd := p.Update(msg)
		if next == nil {
			return msgs
		}
		cmd = nextCmd
	}
	if cmd != nil {
		t.Fatalf("panel still asks for work after 20 steps")
	}
	return msgs
}

// typeText types text into the panel and returns the last command the typing started.
func typeText(p *Panel, text string) tea.Cmd {
	var cmd tea.Cmd
	for _, r := range text {
		_, cmd = p.Update(typedKey(r))
	}
	return cmd
}

// search runs one full search for text and leaves the panel with its result.
func search(t *testing.T, p *Panel, text string) {
	t.Helper()
	step(t, p, typeText(p, text))
}

// draw paints the panel into a w by h grid.
func draw(p *Panel, w, h int) *grid.Grid {
	g := grid.New(w, h)
	p.Draw(g)
	return g
}

// hasRow reports whether some row of g contains s.
func hasRow(g *grid.Grid, s string) bool {
	for _, r := range g.Plain() {
		if strings.Contains(r, s) {
			return true
		}
	}
	return false
}

func TestSearchRunsAfterDebounceOverThePorts(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	p := New(searchOptions(s))
	cmd := typeText(p, "foo")
	if cmd == nil {
		t.Fatal("typing returned no debounce command")
	}
	if len(s.reqs) != 0 {
		t.Fatalf("typing searched %d times in Update, want 0", len(s.reqs))
	}
	step(t, p, cmd)
	if len(s.reqs) != 1 {
		t.Fatalf("searches = %d, want 1", len(s.reqs))
	}
	req := s.reqs[0]
	if req.Root != "/w" || req.Query != "foo" || req.Scope != AllFiles {
		t.Fatalf("request = %+v, want the workspace root and the query", req)
	}
	if len(req.Files) != 2 || len(req.OpenFiles) != 1 || req.Current == nil {
		t.Fatalf("request sources = files %v open %v current %v", req.Files, req.OpenFiles, req.Current)
	}
	if matches, files, truncated := p.Stats(); matches != 3 || files != 2 || truncated {
		t.Fatalf("stats = %d matches in %d files, truncated %v; want 3 in 2", matches, files, truncated)
	}
	g := draw(p, 100, 30)
	for _, want := range []string{"3 matches in 2 files", "a.go (2/2)", "b.go (1/1)", "2:3", "foo bar"} {
		if !hasRow(g, want) {
			t.Errorf("the panel does not show %q:\n%s", want, g)
		}
	}
}

func TestSkippedFilesAreNamedInTheTitle(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: SearchResult{
		Files:        sampleResult().Files,
		SkippedCount: 3,
		Skipped:      []string{"2 binary files", "1 large file"},
	}}
	p := New(searchOptions(s))
	search(t, p, "foo")
	g := draw(p, 100, 30)
	if !hasRow(g, "3 skipped (2 binary files, 1 large file)") {
		t.Fatalf("the title does not name the skipped files:\n%s", g)
	}
}

func TestStaleSearchResultIsDropped(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	p := New(searchOptions(s))
	step(t, p, typeText(p, "a"))
	// A result for an older search does not replace the current rows.
	p.Update(searchDoneMsg{seq: p.seq - 1, result: SearchResult{Files: []FileMatches{{Name: "old.go"}}}})
	if hasRow(draw(p, 100, 30), "old.go") {
		t.Fatal("a stale result replaced the shown ones")
	}
}

func TestTogglesRequeryWithTheNewOptions(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	p := New(searchOptions(s))
	search(t, p, "foo")
	tests := []struct {
		name  string
		key   tea.KeyPressMsg
		check func(SearchRequest) bool
	}{
		{"case on", altKey('c'), func(r SearchRequest) bool { return r.Options.CaseSensitive }},
		{"regex on", altKey('r'), func(r SearchRequest) bool { return r.Options.Regex }},
		{"whole on", altKey('w'), func(r SearchRequest) bool { return r.Options.WholeWord }},
		{"current file", altKey('a'), func(r SearchRequest) bool { return r.Scope == CurrentFile }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s.reqs = nil
			cmd := p.fieldKey(t, tt.key)
			if cmd == nil {
				t.Fatalf("%s returned no debounce command", tt.name)
			}
			step(t, p, cmd)
			if len(s.reqs) != 1 || !tt.check(s.reqs[0]) {
				t.Fatalf("%s searched with %+v", tt.name, s.reqs)
			}
		})
	}
}

// fieldKey sends a key that does not come from a field.
func (p *Panel) fieldKey(t *testing.T, k tea.KeyPressMsg) tea.Cmd {
	t.Helper()
	next, cmd := p.Update(k)
	if next == nil {
		t.Fatalf("the panel closed on a toggle")
	}
	return cmd
}

func TestFocusCycleFoldingAndOpening(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	p := New(searchOptions(s))
	search(t, p, "foo")
	// Tab walks search -> replace -> files -> results.
	p.Update(specialKey(tea.KeyTab))
	p.Update(specialKey(tea.KeyTab))
	if p.focus != focusGlob {
		t.Fatalf("second Tab gave focus %v, want the files field", p.focus)
	}
	p.Update(specialKey(tea.KeyTab))
	if p.focus != focusResults {
		t.Fatalf("focus = %v, want the result rows", p.focus)
	}
	// The first row is the a.go header; Down selects its first match, Down its second.
	p.Update(specialKey(tea.KeyDown))
	p.Update(specialKey(tea.KeyDown))
	if p.focus != focusResults || p.rows[p.selected].kind != rowMatch || p.rows[p.selected].line != 5 {
		t.Fatalf("selection = %+v, want the second match of a.go", p.rows[p.selected])
	}
	cmd := p.Select()
	if cmd == nil {
		t.Fatal("Enter on a match returned no command")
	}
	if got := cmd(); got != (OpenMsg{Path: "/w/a.go", Line: 5, Col: 1}) {
		t.Fatalf("Enter emitted %#v", got)
	}
	// Home goes to the file header; Enter folds it and the matches go away.
	p.Update(specialKey(tea.KeyHome))
	if p.rows[p.selected].kind != rowFile {
		t.Fatal("Home did not select the file header")
	}
	p.Select()
	if got := len(p.rows); got != 3 {
		t.Fatalf("rows after folding = %d, want the two headers and b.go's match", got)
	}
	if !hasRow(draw(p, 100, 30), "[x] ▸ a.go (2/2)") {
		t.Fatal("the folded file row has no folded mark")
	}
}

func TestReplaceRequestAndCount(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	p := New(searchOptions(s))
	search(t, p, "foo")
	p.Update(specialKey(tea.KeyTab)) // replacement field
	if cmd := typeText(p, "bar"); cmd != nil {
		t.Fatalf("typing in the replacement field started a search")
	}
	if matches, files := p.ReplaceCount(); matches != 3 || files != 2 {
		t.Fatalf("replace count = %d in %d, want 3 in 2", matches, files)
	}
	cmd := p.fieldKey(t, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if cmd == nil {
		t.Fatal("Alt+Enter returned no command")
	}
	msg, ok := cmd().(ReplaceRequestMsg)
	if !ok {
		t.Fatalf("Alt+Enter emitted %T, want the replace request", cmd())
	}
	req := msg.Request
	if req.Query != "foo" || req.Replacement != "bar" || len(req.Files) != 2 {
		t.Fatalf("replace request = %+v", req)
	}
	if req.Files[1].Hash != 22 || !req.Files[1].Dirty {
		t.Fatalf("second file = %+v, want its hash and the dirty flag", req.Files[1])
	}
}

func TestReplaceWithoutMatchesSaysSo(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: SearchResult{}}
	p := New(searchOptions(s))
	search(t, p, "foo")
	if cmd := p.replaceCmd(false); cmd != nil {
		t.Fatal("Alt+Enter with no matches returned a command")
	}
	if p.errText != "No matches to replace" {
		t.Fatalf("errText = %q", p.errText)
	}
}

func TestNextMatchWalksTheResultAndWraps(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	p := New(searchOptions(s))
	search(t, p, "foo")
	want := []OpenMsg{
		{Path: "/w/a.go", Line: 2, Col: 3},
		{Path: "/w/a.go", Line: 5, Col: 1},
		{Path: "/w/b.go", Line: 1, Col: 1},
		{Path: "/w/a.go", Line: 2, Col: 3},
	}
	for i, w := range want {
		got, ok := p.NextMatch(true)
		if !ok || got != w {
			t.Fatalf("NextMatch step %d = %+v, %v; want %+v", i, got, ok, w)
		}
	}
	if got, ok := p.NextMatch(false); !ok || got != want[2] {
		t.Fatalf("NextMatch(back) = %+v, %v; want %+v", got, ok, want[2])
	}
}

func TestSearchErrorIsShown(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{err: errors.New("invalid regex")}
	p := New(searchOptions(s))
	search(t, p, "(")
	if p.errText != "invalid regex" {
		t.Fatalf("errText = %q", p.errText)
	}
	g := draw(p, 100, 30)
	if !hasRow(g, "Error: invalid regex") || !hasRow(g, "error") {
		t.Fatalf("the error is not shown:\n%s", g)
	}
}

func TestReSearchUsesTheNewSourcesWithoutDebounce(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	p := New(searchOptions(s))
	search(t, p, "foo")
	s.reqs = nil
	p.SetSources([]SourceFile{{Path: "/w/a.go", Name: "a.go", Text: "foo fresh", Dirty: true}}, nil)
	msgs := step(t, p, p.ReSearch())
	if len(s.reqs) != 1 {
		t.Fatalf("searches = %d, want the immediate one", len(s.reqs))
	}
	if len(s.reqs[0].OpenFiles) != 1 || s.reqs[0].OpenFiles[0].Text != "foo fresh" {
		t.Fatalf("open files = %+v, want the refreshed snapshot", s.reqs[0].OpenFiles)
	}
	_ = msgs
}

func TestGitModeIsReadOnly(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{result: GitResult{Files: []FileMatches{
		{Path: "a.go", Name: "a.go", Matches: []Match{{Line: 3, Col: 2, Len: 3, Text: "foo bar"}}},
	}}}
	p := New(Options{Root: "/r", Git: true, Grepper: g})
	search(t, p, "foo")
	if len(g.reqs) != 1 || g.reqs[0].Query != "foo" || g.reqs[0].Root != "/r" {
		t.Fatalf("git requests = %+v", g.reqs)
	}
	if g.reqs[0].Limit != gitResultLimit || g.reqs[0].CaseSensitive {
		t.Fatalf("git request = %+v, want the cap and case folded", g.reqs[0])
	}
	g2 := draw(p, 100, 30)
	if !hasRow(g2, "Git Grep · 1 match in 1 file") {
		t.Fatalf("the Git Grep title is wrong:\n%s", g2)
	}
	if !hasRow(g2, "Case (Alt+C)") || hasRow(g2, "Regex (Alt+R)") || hasRow(g2, "Replace All") {
		t.Fatalf("the Git Grep options row is not read-only:\n%s", g2)
	}
	// The match toggles and the replacement do nothing in Git Grep mode.
	for _, k := range []tea.KeyPressMsg{altKey('r'), altKey('w'), altKey('a'), altKey('c')} {
		if cmd := p.fieldKey(t, k); cmd != nil && k.Mod == tea.ModAlt && k.Code != 'c' {
			t.Fatalf("key %v started a command in Git Grep mode", k)
		}
	}
	if cmd := p.replaceCmd(false); cmd != nil {
		t.Fatal("Git Grep offered a replacement")
	}
	if p.errText != "Git Grep is read-only" {
		t.Fatalf("errText = %q", p.errText)
	}
}

func TestNoticeReplacesTheHintsUntilTheNextKey(t *testing.T) {
	t.Parallel()
	p := New(searchOptions(&fakeSearcher{}))
	p.SetNotice("2 files changed in 2 tabs; Save All to write")
	if !hasRow(draw(p, 100, 30), "Save All to write") {
		t.Fatal("the notice is not on the bottom border")
	}
	p.Update(specialKey(tea.KeyTab))
	if !hasRow(draw(p, 100, 30), "Tab next") {
		t.Fatal("the hints did not come back after a key")
	}
}

func TestTinyFramePaintsNothing(t *testing.T) {
	t.Parallel()
	if got := layoutFor(40, 5, false); got.top != 0 || got.results != 0 {
		t.Fatalf("layoutFor(40, 5) = %+v, want the zero geometry", got)
	}
	p := New(searchOptions(&fakeSearcher{}))
	g := draw(p, 40, 5)
	if strings.TrimSpace(strings.Join(g.Plain(), "")) != "" {
		t.Fatalf("a tiny frame painted something:\n%s", g)
	}
	if p.Covers(20, 4, 40, 5) {
		t.Fatal("a tiny frame's panel covers cells")
	}
}

func TestCoversAndCursor(t *testing.T) {
	t.Parallel()
	p := New(searchOptions(&fakeSearcher{}))
	g := draw(p, 100, 30)
	geo := layoutFor(100, 30, false)
	if !p.Covers(geo.left+1, geo.top, 100, 30) || p.Covers(0, 0, 100, 30) {
		t.Fatal("Covers does not follow the card")
	}
	x, y, ok := p.Cursor(100, 30)
	if !ok || y != geo.searchY {
		t.Fatalf("Cursor = %d, %d, %v; want the search row", x, y, ok)
	}
	p.Update(specialKey(tea.KeyTab))
	if _, y, ok := p.Cursor(100, 30); !ok || y != geo.replY {
		t.Fatalf("Cursor after Tab = %d, %v; want the replacement row", y, ok)
	}
	p.Update(specialKey(tea.KeyTab))
	if _, y, ok := p.Cursor(100, 30); !ok || y != geo.globY {
		t.Fatalf("Cursor after the second Tab = %d, %v; want the files row", y, ok)
	}
	p.Update(specialKey(tea.KeyTab)) // results: no caret
	if _, _, ok := p.Cursor(100, 30); ok {
		t.Fatal("the result rows show a caret")
	}
	_ = g
}

func TestInitialQuerySearchesAtOpen(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	o := searchOptions(s)
	o.Query = "foo"
	p := New(o)
	step(t, p, p.Init())
	if len(s.reqs) != 1 || s.reqs[0].Query != "foo" {
		t.Fatalf("searches = %+v, want the initial query", s.reqs)
	}
}

func TestEscCloses(t *testing.T) {
	t.Parallel()
	p := New(searchOptions(&fakeSearcher{}))
	next, _ := p.Update(specialKey(tea.KeyEscape))
	if next != nil || p.Active() {
		t.Fatal("Esc did not close the panel")
	}
}

// resultPanel returns a panel with sampleResult searched, ready for the row keys.
func resultPanel(t *testing.T) (*Panel, *fakeSearcher) {
	t.Helper()
	s := &fakeSearcher{result: sampleResult()}
	p := New(searchOptions(s))
	search(t, p, "foo")
	return p, s
}

// selectResultRows moves the panel's focus to the result rows.
func selectResultRows(p *Panel) {
	p.Update(specialKey(tea.KeyTab))
	p.Update(specialKey(tea.KeyTab))
	p.Update(specialKey(tea.KeyTab))
}

func TestCheckboxesCascadeAndReplaceAllCounts(t *testing.T) {
	t.Parallel()
	p, _ := resultPanel(t)
	if matches, files := p.ReplaceCount(); matches != 3 || files != 2 {
		t.Fatalf("a fresh result has %d checked matches in %d files, want all", matches, files)
	}
	if !hasRow(draw(p, 100, 30), "[x] ▾ a.go (2/2)") {
		t.Fatal("the file row does not show its checked count")
	}
	selectResultRows(p)
	// Space on the a.go header unchecks its two matches together.
	p.Update(specialKey(tea.KeySpace))
	if matches, files := p.ReplaceCount(); matches != 1 || files != 1 {
		t.Fatalf("after unchecking a.go: %d checked in %d files", matches, files)
	}
	g := draw(p, 100, 30)
	if !hasRow(g, "[ ] ▾ a.go (0/2)") || !hasRow(g, "1 selected") {
		t.Fatalf("the unchecked file and the selected count are not shown:\n%s", g)
	}
	// Space again checks the whole file.
	p.Update(specialKey(tea.KeySpace))
	if matches, _ := p.ReplaceCount(); matches != 3 {
		t.Fatalf("after rechecking a.go: %d checked", matches)
	}
	// A match row's own box toggles alone.
	p.Update(specialKey(tea.KeyDown))
	p.Update(specialKey(tea.KeySpace))
	if matches, files := p.ReplaceCount(); matches != 2 || files != 2 {
		t.Fatalf("after unchecking one match: %d checked in %d files", matches, files)
	}
	if !hasRow(draw(p, 100, 30), "[ ] ▾ a.go (1/2)") {
		t.Fatal("the partially checked file does not show 1/2")
	}
	// a checks all, n checks none.
	p.Update(typedKey('a'))
	if matches, _ := p.ReplaceCount(); matches != 3 {
		t.Fatalf("after a: %d checked", matches)
	}
	p.Update(typedKey('n'))
	if matches, _ := p.ReplaceCount(); matches != 0 {
		t.Fatalf("after n: %d checked", matches)
	}
	if cmd := p.replaceCmd(false); cmd != nil || p.errText != "No matches selected" {
		t.Fatalf("Replace All with nothing checked = %v, %q", cmd, p.errText)
	}
}

func TestReplaceAllRequestCarriesTheCheckedMatches(t *testing.T) {
	t.Parallel()
	p, _ := resultPanel(t)
	selectResultRows(p)
	p.Update(specialKey(tea.KeyDown)) // the first match of a.go
	p.Update(specialKey(tea.KeySpace))
	req, ok := p.ReplaceRequest()
	if !ok {
		t.Fatal("ReplaceAll with checked matches returned no request")
	}
	if req.Matches != 2 || len(req.Files) != 2 {
		t.Fatalf("request = %d matches in %d files, want the two still checked", req.Matches, len(req.Files))
	}
	if got := req.Files[0].Selected; len(got) != 1 || got[0] != (Position{Line: 4, Col: 0}) {
		t.Fatalf("a.go selected = %+v, want only its second match", got)
	}
	if got := req.Files[1].Selected; len(got) != 1 || got[0] != (Position{Line: 0, Col: 0}) {
		t.Fatalf("b.go selected = %+v, want its only match", got)
	}
}

func TestScopedReplaceUsesTheFocusedRow(t *testing.T) {
	t.Parallel()
	p, _ := resultPanel(t)
	selectResultRows(p)
	// The a.go header replaces its checked matches.
	if req, ok := p.ScopedReplaceRequest(); !ok || req.Matches != 2 || len(req.Files) != 1 || req.Files[0].Name != "a.go" {
		t.Fatalf("scoped on a file row = %+v, %v", req, ok)
	}
	// The first match row replaces that match alone.
	p.Update(specialKey(tea.KeyDown))
	req, ok := p.ScopedReplaceRequest()
	if !ok || req.Matches != 1 || len(req.Files) != 1 || len(req.Files[0].Selected) != 1 {
		t.Fatalf("scoped on a match row = %+v, %v", req, ok)
	}
	if got := req.Files[0].Selected[0]; got != (Position{Line: 1, Col: 2}) {
		t.Fatalf("scoped position = %+v, want the first match", got)
	}
	// An unchecked match row has nothing to replace.
	p.Update(specialKey(tea.KeySpace))
	if _, ok := p.ScopedReplaceRequest(); ok || p.errText != "The focused match is not checked" {
		t.Fatalf("scoped on an unchecked match = %v, %q", ok, p.errText)
	}
	// A file row with every match unchecked says so.
	p.Update(specialKey(tea.KeyUp)) // back to the a.go header
	p.Update(typedKey('n'))         // no match checked
	if _, ok := p.ScopedReplaceRequest(); ok || p.errText != "No checked matches in this file" {
		t.Fatalf("scoped on an unchecked file = %v, %q", ok, p.errText)
	}
}

func TestShiftEnterEmitsTheScopedRequest(t *testing.T) {
	t.Parallel()
	p, _ := resultPanel(t)
	selectResultRows(p)
	cmd := p.fieldKey(t, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if cmd == nil {
		t.Fatal("Shift+Enter returned no command")
	}
	msg, ok := cmd().(ReplaceRequestMsg)
	if !ok || msg.Request.Matches != 2 {
		t.Fatalf("Shift+Enter emitted %#v", cmd())
	}
	// Alt+Enter still emits all checked matches.
	cmd = p.fieldKey(t, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if msg, ok := cmd().(ReplaceRequestMsg); !ok || msg.Request.Matches != 3 {
		t.Fatalf("Alt+Enter emitted %#v", cmd())
	}
}

func TestGlobFiltersTheFilesSentToTheSearch(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	o := Options{
		Root:     "/w",
		Files:    fakeFiles{files: []string{"a.go", "b.txt", "sub/c.go"}},
		Searcher: s,
		Glob:     "*.go",
	}
	p := New(o)
	search(t, p, "foo")
	if len(s.reqs) != 1 {
		t.Fatalf("searches = %d", len(s.reqs))
	}
	got := s.reqs[0].Files
	want := []string{"a.go", "sub/c.go"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("globbed files = %v, want %v", got, want)
	}
	if p.Glob() != "*.go" {
		t.Fatalf("panel glob = %q", p.Glob())
	}
}

func TestInvalidGlobBlocksTheSearchAndSaysSo(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	o := Options{Root: "/w", Files: fakeFiles{files: []string{"a.go"}}, Searcher: s, Glob: "src/["}
	p := New(o)
	search(t, p, "foo")
	if len(s.reqs) != 0 {
		t.Fatalf("an invalid glob still searched: %+v", s.reqs)
	}
	if !strings.Contains(p.errText, "Bad glob") {
		t.Fatalf("errText = %q", p.errText)
	}
	if !hasRow(draw(p, 100, 30), "Bad glob") {
		t.Fatal("the bad glob is not shown")
	}
}

func TestGlobFieldIsDrawnBetweenReplaceAndOptions(t *testing.T) {
	t.Parallel()
	p := New(searchOptions(&fakeSearcher{}))
	g := draw(p, 100, 30)
	if !hasRow(g, "Files:") {
		t.Fatalf("the files field row is not drawn:\n%s", g)
	}
	geo := layoutFor(100, 30, false)
	if geo.globY != geo.replY+1 || geo.globY != geo.optY-1 {
		t.Fatalf("geometry = %+v, want the glob row between the replacement and option rows", geo)
	}
}

func TestHistoryWalksTheSearchedPatterns(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: sampleResult()}
	p := New(searchOptions(s))
	search(t, p, "foo")
	p.query.SetValue("")
	search(t, p, "bar")
	if got := p.History(); len(got) != 2 || got[0] != "bar" || got[1] != "foo" {
		t.Fatalf("history = %v, want [bar foo]", got)
	}
	// Up walks to the newest earlier pattern and searches it.
	cmd := p.walkHistory(true)
	if cmd == nil {
		t.Fatal("Up returned no command")
	}
	step(t, p, cmd)
	if p.Query() != "bar" {
		t.Fatalf("first Up gave %q, want bar", p.Query())
	}
	// A second Up goes one older.
	step(t, p, p.walkHistory(true))
	if p.Query() != "foo" {
		t.Fatalf("second Up gave %q, want foo", p.Query())
	}
	// Down comes back, and past the newest restores the draft.
	step(t, p, p.walkHistory(false))
	if p.Query() != "bar" {
		t.Fatalf("Down gave %q, want bar", p.Query())
	}
	step(t, p, p.walkHistory(false))
	if p.Query() != "bar" {
		t.Fatalf("Down past the newest gave %q, want the draft it started from", p.Query())
	}
	if got := p.History(); len(got) != 2 {
		t.Fatalf("walking changed the history: %v", got)
	}
}

func TestExportItemsGroupTheMatches(t *testing.T) {
	t.Parallel()
	p, _ := resultPanel(t)
	items := p.ExportItems()
	if len(items) != 5 {
		t.Fatalf("items = %d, want two headers and three matches", len(items))
	}
	if !items[0].Header || items[0].Name != "a.go (2)" || items[1].Path != "/w/a.go" || items[1].Line != 2 {
		t.Fatalf("first items = %+v", items[:2])
	}
	if !items[3].Header || items[3].Name != "b.go (1)" || items[4].Text != "1:1  foo" {
		t.Fatalf("b.go items = %+v", items[3:])
	}
}

func TestAltQEmitsTheExport(t *testing.T) {
	t.Parallel()
	p, _ := resultPanel(t)
	cmd := p.fieldKey(t, tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
	if cmd == nil {
		t.Fatal("Alt+Q returned no command")
	}
	if _, ok := cmd().(ExportMsg); !ok {
		t.Fatalf("Alt+Q emitted %T", cmd())
	}
	empty := New(searchOptions(&fakeSearcher{}))
	empty.Update(typedKey('x')) // any key clears the notice
	cmd = empty.fieldKey(t, tea.KeyPressMsg{Code: 'q', Mod: tea.ModAlt})
	if cmd != nil || empty.errText != "No matches to list" {
		t.Fatalf("Alt+Q with no results = %v, %q", cmd, empty.errText)
	}
}

func TestGitModeCanExportItsResults(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{result: GitResult{Files: []FileMatches{{
		Path: "a.go", Name: "a.go", Matches: []Match{{Line: 1, Col: 1, Len: 3, Text: "foo"}},
	}}}}
	p := New(Options{Root: "/r", Git: true, Grepper: g})
	search(t, p, "foo")
	cmd := p.exportCmd()
	if cmd == nil {
		t.Fatal("Git Grep results could not be listed")
	}
	if items := p.ExportItems(); len(items) != 2 || !items[0].Header {
		t.Fatalf("git export items = %+v", items)
	}
}

func TestMultiLineMatchRowShowsItsRange(t *testing.T) {
	t.Parallel()
	s := &fakeSearcher{result: SearchResult{Files: []FileMatches{{
		Path: "/w/a.go", Name: "a.go", Hash: 9,
		Matches: []Match{{
			Line: 1, Col: 3, Len: 3, Text: "x foo", EndLine: 2, EndCol: 4,
		}},
	}}}}
	p := New(searchOptions(s))
	search(t, p, "foo")
	g := draw(p, 100, 30)
	if !hasRow(g, "1:3–2:4") {
		t.Fatalf("the multi-line match row has no end range:\n%s", g)
	}
}

// fakePreviewer records each preview request and answers with a fixed result.
type fakePreviewer struct {
	result PreviewResult
	err    error
	reqs   []PreviewRequest
}

func (f *fakePreviewer) Preview(req PreviewRequest) (PreviewResult, error) {
	f.reqs = append(f.reqs, req)
	return f.result, f.err
}

// previewPanel returns a sample-result panel with a previewer wired, searched.
func previewPanel(t *testing.T, pv *fakePreviewer) (*Panel, *fakeSearcher) {
	t.Helper()
	s := &fakeSearcher{result: sampleResult()}
	o := searchOptions(s)
	o.Previewer = pv
	p := New(o)
	search(t, p, "foo")
	return p, s
}

// focusResultsFeeding focuses the result rows and runs the preview commands, as the
// program does when the selection moves.
func focusResultsFeeding(t *testing.T, p *Panel) {
	t.Helper()
	for i := 0; i < 3; i++ {
		_, cmd := p.Update(specialKey(tea.KeyTab))
		feedPanel(t, p, cmd)
	}
}

// feedPanel runs a panel command and gives its message back to the panel.
func feedPanel(t *testing.T, p *Panel, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	p.Update(msg)
}

// previewTextStart is the column where the text of preview line number line (1-based)
// starts: the gutter, its space, and the divider " │ ".
func previewTextStart(geo geometry, line int) int {
	gutter := len(strconv.Itoa(max(line, 1)))
	return geo.prevX + gutter + 3
}

// previewGeo is the panel's geometry with the preview column, for tests.
func previewGeo(p *Panel) geometry {
	geo := layoutFor(100, 30, p.git)
	p.previewColumns(&geo)
	return geo
}

func TestPreviewShowsTheSelectedMatchContext(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{result: PreviewResult{Start: 0, Lines: []string{"x", "func foo() {", "y"}}}
	p, _ := previewPanel(t, pv)
	// With the focus in a field there is no preview column.
	if hasRow(draw(p, 100, 30), "a.go:2") {
		t.Fatal("the preview column is painted while the focus is in a field")
	}
	focusResultsFeeding(t, p)
	if len(pv.reqs) != 1 {
		t.Fatalf("preview requests = %d, want one", len(pv.reqs))
	}
	if req := pv.reqs[0]; req.Path != "/w/a.go" || req.Line != 2 || req.Col != 3 || req.Text != "" {
		t.Fatalf("preview request = %+v, want a closed a.go at 2:3 without text", req)
	}
	g := draw(p, 100, 30)
	if !hasRow(g, "a.go:2") {
		t.Fatalf("the preview header is not painted:\n%s", g)
	}
	geo := previewGeo(p)
	if !geo.preview {
		t.Fatal("the preview column is not reserved")
	}
	// "func foo() {" is file line 2; the match starts at rune 2 and is 3 runes long.
	y := geo.resultsTop + 1 + 1
	start := previewTextStart(geo, 2)
	if cell := g.At(start+2, y); !cell.Style.Bold {
		t.Fatalf("the matched runes are not bold at %d,%d: %+v", start+2, y, cell.Style)
	}
	if g.At(start, y).Style.Bold {
		t.Fatal("the runes before the match are bold")
	}
	// The match line takes the selected style.
	if got, want := g.At(geo.prevX, y).Style.BG, p.styles.selected.BG; got != want {
		t.Fatalf("match line background = %v, want %v", got, want)
	}
}

func TestPreviewHidesBelowTheWidthThreshold(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{result: PreviewResult{Start: 0, Lines: []string{"x", "func foo() {", "y"}}}
	p, _ := previewPanel(t, pv)
	focusResultsFeeding(t, p)
	if hasRow(draw(p, 60, 30), "a.go:2") {
		t.Fatal("the preview column is painted in a narrow card")
	}
	narrow := layoutFor(60, 30, p.git)
	p.previewColumns(&narrow)
	if narrow.preview {
		t.Fatal("the narrow card reserves the preview column")
	}
	if !hasRow(draw(p, 100, 30), "a.go:2") {
		t.Fatal("the preview column is hidden in a wide card")
	}
}

func TestPreviewStatesAndUnsavedText(t *testing.T) {
	t.Parallel()
	// The open buffer's snapshot travels with the request and marks the header.
	pv := &fakePreviewer{result: PreviewResult{Start: 1, Lines: []string{"one", "unsaved foo", "three"}}}
	s := &fakeSearcher{result: SearchResult{Files: []FileMatches{{
		Path: "/w/a.go", Name: "a.go", Hash: 3, Dirty: true,
		Matches: []Match{{Line: 2, Col: 9, Len: 3, Text: "unsaved foo"}},
	}}}}
	o := searchOptions(s)
	o.OpenFiles = []SourceFile{{Path: "/w/a.go", Name: "a.go", Text: "one\nunsaved foo\nthree", Dirty: true}}
	o.Previewer = pv
	p := New(o)
	search(t, p, "foo")
	focusResultsFeeding(t, p)
	if len(pv.reqs) != 1 || pv.reqs[0].Text != "one\nunsaved foo\nthree" || !pv.reqs[0].Dirty {
		t.Fatalf("preview request = %+v, want the unsaved text", pv.reqs)
	}
	g := draw(p, 100, 30)
	if !hasRow(g, "a.go:2 (unsaved)") {
		t.Fatalf("the header does not mark the unsaved buffer:\n%s", g)
	}
	// A preview reason is shown in the column.
	pv2 := &fakePreviewer{result: PreviewResult{Err: "binary file"}}
	p2, _ := previewPanel(t, pv2)
	focusResultsFeeding(t, p2)
	if !hasRow(draw(p2, 100, 30), "Preview unavailable: binary file") {
		t.Fatal("the preview reason is not shown")
	}
}

func TestPreviewLoadingAndDedupe(t *testing.T) {
	t.Parallel()
	pv := &fakePreviewer{result: PreviewResult{Start: 0, Lines: []string{"x", "func foo() {", "y"}}}
	p, _ := previewPanel(t, pv)
	// Entering the rows shows loading until the command runs.
	for i := 0; i < 3; i++ {
		_, cmd := p.Update(specialKey(tea.KeyTab))
		if i == 2 {
			if cmd == nil {
				t.Fatal("entering the rows started no preview")
			}
			if !hasRow(draw(p, 100, 30), "…") {
				t.Fatal("the loading state is not painted")
			}
			feedPanel(t, p, cmd)
		}
	}
	// The same target does not fetch again; a new selection does.
	if cmd := p.previewCmd(); cmd != nil {
		t.Fatal("the same preview target fetched again")
	}
	// The first Down stays on the file's first match (same preview); the second is a new
	// target.
	if _, cmd := p.Update(specialKey(tea.KeyDown)); cmd != nil {
		t.Fatal("moving to the same first match fetched the preview again")
	}
	_, cmd := p.Update(specialKey(tea.KeyDown))
	feedPanel(t, p, cmd)
	if len(pv.reqs) != 2 {
		t.Fatalf("preview requests = %d, want the second selection's", len(pv.reqs))
	}
	if req := pv.reqs[1]; req.Line != 5 {
		t.Fatalf("second request = %+v, want the next match's line", req)
	}
}

func TestPreviewMultiLineHighlightsTheWholeRange(t *testing.T) {
	t.Parallel()
	// The match spans lines 2-4: first line from column 7, middle line whole, last line
	// up to column 4.
	s := &fakeSearcher{result: SearchResult{Files: []FileMatches{{
		Path: "/w/a.go", Name: "a.go", Hash: 5,
		Matches: []Match{{Line: 2, Col: 7, Len: 3, Text: "start foo", EndLine: 4, EndCol: 4}},
	}}}}
	pv := &fakePreviewer{result: PreviewResult{Start: 0, Lines: []string{"pre", "start foo", "middle", "end bar", "post"}}}
	o := searchOptions(s)
	o.Previewer = pv
	p := New(o)
	search(t, p, "foo")
	focusResultsFeeding(t, p)
	g := draw(p, 100, 30)
	if !hasRow(g, "2:7–4:4") {
		t.Fatalf("the row has no multi-line range:\n%s", g)
	}
	geo := previewGeo(p)
	// Line 2: runes from 7 (0-based 6) to the end are bold.
	if !g.At(previewTextStart(geo, 2)+6, geo.resultsTop+2).Style.Bold {
		t.Fatal("the first line's matched runes are not bold")
	}
	// Line 3 (middle): bold from the start.
	if !g.At(previewTextStart(geo, 3), geo.resultsTop+3).Style.Bold {
		t.Fatal("the middle line is not fully bold")
	}
	// Line 4: bold from the start to column 4 (exclusive, so runes 0-2); the rest is not.
	if !g.At(previewTextStart(geo, 4)+2, geo.resultsTop+4).Style.Bold {
		t.Fatal("the last line's matched runes are not bold")
	}
	if g.At(previewTextStart(geo, 4)+4, geo.resultsTop+4).Style.Bold {
		t.Fatal("runes past the match end are bold on the last line")
	}
	// All three match lines take the selected style.
	for k := 1; k <= 3; k++ {
		if got, want := g.At(geo.prevX, geo.resultsTop+1+k).Style.BG, p.styles.selected.BG; got != want {
			t.Fatalf("line %d background = %v, want %v", k+1, got, want)
		}
	}
}

// fakeBatchSearcher streams fixed batches and records each request and cancel channel.
type fakeBatchSearcher struct {
	batches []SearchBatch
	reqs    []SearchRequest
	cancels []<-chan struct{}
}

func (f *fakeBatchSearcher) ProjectSearch(req SearchRequest) (SearchResult, error) {
	return SearchResult{}, nil
}

func (f *fakeBatchSearcher) ProjectSearchBatches(req SearchRequest, cancel <-chan struct{}) <-chan SearchBatch {
	f.reqs = append(f.reqs, req)
	f.cancels = append(f.cancels, cancel)
	ch := make(chan SearchBatch)
	batches := f.batches
	go func() {
		defer close(ch)
		for _, batch := range batches {
			select {
			case <-cancel:
				return
			case ch <- batch:
			}
		}
	}()
	return ch
}

// streamingPanel returns a panel over a fake batch searcher and starts a search for text,
// returning the command that reads the first batch. It does not drain the stream: each
// batch is read by the test, so the panel can be inspected between batches.
func streamingPanel(t *testing.T, bs *fakeBatchSearcher, text string) (*Panel, tea.Cmd) {
	t.Helper()
	o := searchOptions(nil)
	o.Searcher = bs
	p := New(o)
	tick := typeText(p, text)
	if tick == nil {
		t.Fatal("typing started no debounce")
	}
	_, first := p.Update(tick())
	if first == nil {
		t.Fatal("the debounce started no streaming read")
	}
	return p, first
}

// readBatch runs cmd and gives its message to the panel, returning the next read command.
func readBatch(t *testing.T, p *Panel, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("the streaming search ended without another command")
	}
	msg := cmd()
	next, nextCmd := p.Update(msg)
	if next == nil {
		t.Fatal("the panel closed while reading a batch")
	}
	return nextCmd
}

// fileBatch is one batch of one file with a single match.
func fileBatch(name string, scanned, total int, done bool) SearchBatch {
	return SearchBatch{
		Files:   []FileMatches{{Path: "/w/" + name, Name: name, Hash: uint64(len(name)), Matches: []Match{{Line: 1, Col: 1, Len: 1, Text: name}}}},
		Scanned: scanned, Total: total, Done: done,
	}
}

func TestStreamingBatchesAppendAndShowProgress(t *testing.T) {
	t.Parallel()
	bs := &fakeBatchSearcher{batches: []SearchBatch{
		fileBatch("a.go", 1, 3, false),
		fileBatch("b.go", 2, 3, false),
		fileBatch("c.go", 3, 3, true),
	}}
	p, cmd := streamingPanel(t, bs, "x")
	if p.loading == false {
		t.Fatal("the panel is not loading at the start of the stream")
	}
	if got := draw(p, 100, 30); !hasRow(got, "Searching…") {
		t.Fatalf("the empty state does not say it is searching:\n%s", got)
	}
	cmd = readBatch(t, p, cmd)
	if matches, files, _ := p.Stats(); matches != 1 || files != 1 {
		t.Fatalf("after one batch: %d matches in %d files", matches, files)
	}
	if got := draw(p, 100, 30); !hasRow(got, "searching 1/3 files") || !hasRow(got, "a.go") {
		t.Fatalf("the first batch is not shown with progress:\n%s", got)
	}
	// The selection stays on the row the user had while more files arrive.
	p.selected = 0
	before := p.rows[0]
	cmd = readBatch(t, p, cmd)
	if p.selected != 0 || p.rows[0] != before {
		t.Fatalf("the selection moved when a batch arrived: row %d %+v", p.selected, p.rows[0])
	}
	if got := draw(p, 100, 30); !hasRow(got, "searching 2/3 files") || !hasRow(got, "b.go") {
		t.Fatalf("the second batch is not shown:\n%s", got)
	}
	cmd = readBatch(t, p, cmd)
	if cmd != nil {
		t.Fatal("the done batch asked for another read")
	}
	if p.loading {
		t.Fatal("the panel is still loading after the done batch")
	}
	if matches, files, _ := p.Stats(); matches != 3 || files != 3 {
		t.Fatalf("after the done batch: %d matches in %d files", matches, files)
	}
	if got := draw(p, 100, 30); !hasRow(got, "3 matches in 3 files") {
		t.Fatalf("the done batch does not show the final counts:\n%s", got)
	}
	if len(bs.reqs) != 1 || bs.reqs[0].Query != "x" || len(bs.reqs[0].Files) != 2 || bs.reqs[0].Files[0] != "a.go" {
		t.Fatalf("requests = %+v", bs.reqs)
	}
	if hist := p.History(); len(hist) == 0 || hist[0] != "x" {
		t.Fatalf("the completed search is not in the history: %v", hist)
	}
}

func TestStreamingBatchForAnOlderSearchIsDropped(t *testing.T) {
	t.Parallel()
	bs := &fakeBatchSearcher{batches: []SearchBatch{fileBatch("a.go", 1, 1, false)}}
	p, cmd := streamingPanel(t, bs, "x")
	msg := cmd()
	// A new search starts before the batch is applied: the old message is stale.
	if p.requery() == nil {
		t.Fatal("the new query did not start a search")
	}
	oldCancel := bs.cancels[0]
	p.Update(msg)
	if matches, _, _ := p.Stats(); matches != 0 {
		t.Fatalf("a stale batch was applied: %d matches", matches)
	}
	select {
	case <-oldCancel:
	case <-time.After(time.Second):
		t.Fatal("the old stream was not cancelled")
	}
}

func TestStreamingCancelsOnDismiss(t *testing.T) {
	t.Parallel()
	bs := &fakeBatchSearcher{batches: []SearchBatch{fileBatch("a.go", 1, 2, false), fileBatch("b.go", 2, 2, true)}}
	p, cmd := streamingPanel(t, bs, "x")
	readBatch(t, p, cmd)
	if p.Active() != true {
		t.Fatal("the panel closed early")
	}
	cancel := bs.cancels[0]
	p.Dismiss()
	if p.Active() {
		t.Fatal("Dismiss left the panel open")
	}
	select {
	case <-cancel:
	case <-time.After(time.Second):
		t.Fatal("Dismiss did not cancel the stream")
	}
}

func TestStreamingReplaceUsesTheShownSet(t *testing.T) {
	t.Parallel()
	bs := &fakeBatchSearcher{batches: []SearchBatch{
		fileBatch("a.go", 1, 2, false),
		fileBatch("b.go", 2, 2, true),
	}}
	p, cmd := streamingPanel(t, bs, "x")
	cmd = readBatch(t, p, cmd)
	req, ok := p.ReplaceRequest()
	if !ok || len(req.Files) != 1 || req.Files[0].Name != "a.go" {
		t.Fatalf("partial replace request = %+v, %v", req, ok)
	}
	cmd = readBatch(t, p, cmd)
	if cmd != nil {
		t.Fatal("the done batch asked for another read")
	}
	req, ok = p.ReplaceRequest()
	if !ok || len(req.Files) != 2 {
		t.Fatalf("final replace request = %+v, %v", req, ok)
	}
}

func TestStreamingDoneErrorIsShown(t *testing.T) {
	t.Parallel()
	bs := &fakeBatchSearcher{batches: []SearchBatch{{Scanned: 1, Total: 1, Done: true, Err: "read failed"}}}
	p, cmd := streamingPanel(t, bs, "x")
	readBatch(t, p, cmd)
	if p.loading || p.errText != "read failed" {
		t.Fatalf("loading %v errText %q, want the done error", p.loading, p.errText)
	}
}

func TestGitModeHasAValidatedFilesField(t *testing.T) {
	t.Parallel()
	g := &fakeGrepper{result: GitResult{Files: []FileMatches{{
		Path: "a.go", Name: "a.go", Matches: []Match{{Line: 1, Col: 1, Len: 3, Text: "foo"}},
	}}}}
	p := New(Options{Root: "/r", Git: true, Grepper: g})
	search(t, p, "foo")
	if len(g.reqs) != 1 || len(g.reqs[0].Pathspecs) != 0 {
		t.Fatalf("first request = %+v", g.reqs)
	}
	if !hasRow(draw(p, 100, 30), "Files:") {
		t.Fatal("the Git Grep card has no Files row")
	}
	// Tab walks search -> files -> results -> search in Git Grep mode.
	p.Update(specialKey(tea.KeyTab))
	if p.focus != focusGlob {
		t.Fatalf("first Tab gave focus %v, want the files field", p.focus)
	}
	step(t, p, typeText(p, "*.go"))
	if len(g.reqs) != 2 || len(g.reqs[1].Pathspecs) != 1 || g.reqs[1].Pathspecs[0] != "*.go" {
		t.Fatalf("globbed request = %+v", g.reqs)
	}
	p.Update(specialKey(tea.KeyTab))
	if p.focus != focusResults {
		t.Fatalf("second Tab gave focus %v, want the result rows", p.focus)
	}
	p.Update(specialKey(tea.KeyTab))
	if p.focus != focusSearch {
		t.Fatalf("third Tab gave focus %v, want the search field", p.focus)
	}
	// An invalid pattern blocks the search and says why.
	p.Update(specialKey(tea.KeyTab)) // files
	p.glob.SetValue("src/[")
	step(t, p, p.requery())
	if len(g.reqs) != 2 {
		t.Fatalf("an invalid glob still searched: %d requests", len(g.reqs))
	}
	if !strings.Contains(p.errText, "Bad glob") || !hasRow(draw(p, 100, 30), "Bad glob") {
		t.Fatalf("errText = %q, want the invalid glob state", p.errText)
	}
}

func TestGitModeLayoutCarriesTheFilesRow(t *testing.T) {
	t.Parallel()
	geo := layoutFor(100, 30, true)
	if geo.globY != geo.optY-1 || geo.searchY != geo.globY-1 || geo.replY != -1 {
		t.Fatalf("git geometry = %+v, want search and files rows above the options", geo)
	}
}
