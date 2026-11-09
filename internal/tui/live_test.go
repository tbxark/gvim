package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/theme"
)

// lineEditor is a fake editor that records the files it opens and the lines it
// moves to, so the live grep tests can see where a match went.
type lineEditor struct {
	*fakeEditor
	opened []string
	gotos  [][2]int
}

func (e *lineEditor) OpenFile(path string) error {
	e.opened = append(e.opened, path)
	return nil
}

func (e *lineEditor) OpenPreview(path string) error { return e.OpenFile(path) }

func (e *lineEditor) GotoLine(line, col int) error {
	e.gotos = append(e.gotos, [2]int{line, col})
	return nil
}

// grepPort is a fake live grep that answers every query with hits, and records the
// requests it was asked.
type grepPort struct {
	hits    []palette.GrepHit
	queries []string
	reqs    []palette.GrepRequest
}

func (g *grepPort) Search(req palette.GrepRequest) ([]palette.GrepHit, error) {
	g.queries = append(g.queries, req.Text)
	g.reqs = append(g.reqs, req)
	return g.hits, nil
}

// newLiveModel returns a model over a line editor with the live grep port g.
func newLiveModel(t *testing.T, g palette.Grepper) (*Model, *lineEditor) {
	t.Helper()
	ed := &lineEditor{fakeEditor: newFakeEditor("x")}
	return newLiveModelOver(t, g, ed), ed
}

// newLiveModelOver returns a model over ed with the live grep port g, for a test that
// adds optional editor ports to the fake.
func newLiveModelOver(t *testing.T, g palette.Grepper, ed EditorState) *Model {
	t.Helper()
	m := New(Deps{
		Editor:   ed,
		Explorer: newFakeTree(1),
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Root:     RootPorts{Grep: g},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m
}

// typeInto sends the text to the top overlay, and feeds every command it starts
// back to the model, as the program does.
func typeInto(m *Model, s string) {
	for _, c := range s {
		_, cmd := m.Update(typed(string(c)))
		feed(m, cmd)
	}
}

// feed runs cmd and passes each message it produces to the model, until none is
// left. Commands that wait, such as the watcher, must not be fed.
func feed(m *Model, cmd tea.Cmd) {
	for _, msg := range collect(cmd) {
		_, next := m.Update(msg)
		feed(m, next)
	}
}

// TestOpenLiveGrepThenChooseMatchOpensTheLine checks the full path: open_live_grep
// shows the palette in live mode, the typed text is searched, and Enter on a match
// opens its file at the line and column.
func TestOpenLiveGrepThenChooseMatchOpensTheLine(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "src/a.go", Line: 7, Col: 2, Text: "todo"}}}
	m, ed := newLiveModel(t, g)
	must(t, m.Run("open_live_grep"))
	if len(m.overlays) == 0 {
		t.Fatal("open_live_grep did not open the palette")
	}
	typeInto(m, "todo")
	if !slices.Contains(g.queries, "todo") {
		t.Fatalf("searches = %v, want todo", g.queries)
	}
	// The request carries Fresh's defaults: regex on, word off, case folded, files on.
	req := g.reqs[len(g.reqs)-1]
	if req.Root != "/ws" || req.Text != "todo" {
		t.Fatalf("request root %q text %q, want /ws and todo", req.Root, req.Text)
	}
	if !req.Query.Regex || req.Query.WholeWord || req.Query.CaseSensitive {
		t.Fatalf("request modes = %+v, want Fresh's defaults", req.Query)
	}
	if !req.Sources.Files || req.Sources.Buffers || req.Sources.Ignored {
		t.Fatalf("request scopes = %+v, want files on only", req.Sources)
	}
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(ed.opened) != 1 || ed.opened[0] != "/ws/src/a.go" {
		t.Fatalf("opened %v, want the matched file", ed.opened)
	}
	if !slices.Equal(ed.gotos[len(ed.gotos)-1][:], []int{7, 2}) {
		t.Fatalf("moved to %v, want line 7 column 2", ed.gotos)
	}
}

// runKey sends one key press and returns the command it started.
func runKey(m *Model, k tea.KeyPressMsg) tea.Cmd {
	_, cmd := m.Update(k)
	return cmd
}

// TestResumeLiveGrepRestoresCachedMatches: resume reopens the last Live Grep without
// searching again; the cached row is still there and Enter opens it as before.
func TestResumeLiveGrepRestoresCachedMatches(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}}
	m, ed := newLiveModel(t, g)
	must(t, m.Run("open_live_grep"))
	typeInto(m, "todo")
	m.Update(special(tea.KeyEscape))

	g.queries = nil
	must(t, m.Run("resume_live_grep"))
	feed(m, m.pending)
	m.pending = nil
	if len(g.queries) != 0 {
		t.Fatalf("resume re-ran the search: %v", g.queries)
	}
	if len(m.overlays) == 0 {
		t.Fatal("resume did not open the palette")
	}
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(ed.opened) != 1 || ed.opened[0] != "/ws/a.go" {
		t.Fatalf("resume cached row opened %v, want /ws/a.go", ed.opened)
	}
}

// TestResumeLiveGrepRestoresTheSelectedRow: the selected index is part of the
// resumed state, not just the hits.
func TestResumeLiveGrepRestoresTheSelectedRow(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{
		{Rel: "a.go", Line: 1, Col: 1, Text: "todo"},
		{Rel: "b.go", Line: 2, Col: 1, Text: "todo"},
	}}
	m, ed := newLiveModel(t, g)
	must(t, m.Run("open_live_grep"))
	typeInto(m, "todo")
	feed(m, runKey(m, special(tea.KeyDown)))
	m.Update(special(tea.KeyEscape))

	g.queries = nil
	must(t, m.Run("resume_live_grep"))
	feed(m, m.pending)
	m.pending = nil
	if len(g.queries) != 0 {
		t.Fatalf("resume re-ran the search: %v", g.queries)
	}
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(ed.opened) != 1 || ed.opened[0] != "/ws/b.go" {
		t.Fatalf("resume selection opened %v, want the second row /ws/b.go", ed.opened)
	}
}

// TestResumeLiveGrepHonestlyRerunsAfterAQueryEdit: changing the query invalidates the
// cached matches (Fresh's rule); resume keeps the text but searches it again.
func TestResumeLiveGrepHonestlyRerunsAfterAQueryEdit(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}}
	m, _ := newLiveModel(t, g)
	must(t, m.Run("open_live_grep"))
	typeInto(m, "todo")
	// Type without feeding the new search: the old result is still on screen but the
	// query no longer matches it, so Resume must not restore the stale hits.
	m.Update(typed("x"))
	m.Update(special(tea.KeyEscape))

	g.queries = nil
	must(t, m.Run("resume_live_grep"))
	feed(m, m.pending)
	m.pending = nil
	if !slices.Equal(g.queries, []string{"todox"}) {
		t.Fatalf("resume after an edit searched %v, want todox", g.queries)
	}
}

// genFiles is a file index with the generation the palette uses to decide whether a
// cached Live Grep result is still current.
type genFiles struct {
	*fakeIndex
	gen uint64
}

func (g *genFiles) Generation() uint64 { return g.gen }

// TestResumeLiveGrepRerunsWhenTheIndexGenerationChanges: an index Restart invalidates
// the cached matches even if the query and selection are unchanged; resume searches
// again rather than presenting rows from the old index.
func TestResumeLiveGrepRerunsWhenTheIndexGenerationChanges(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}}
	ed := &lineEditor{fakeEditor: newFakeEditor("x")}
	files := &genFiles{fakeIndex: &fakeIndex{}, gen: 1}
	m := New(Deps{
		Editor:   ed,
		Explorer: newFakeTree(1),
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Files:    files,
		Root:     RootPorts{Grep: g},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	must(t, m.Run("open_live_grep"))
	typeInto(m, "todo")
	m.Update(special(tea.KeyEscape))

	files.gen = 2
	g.queries = nil
	must(t, m.Run("resume_live_grep"))
	feed(m, m.pending)
	m.pending = nil
	if !slices.Equal(g.queries, []string{"todo"}) {
		t.Fatalf("resume after a generation change searched %v, want todo", g.queries)
	}
}

// TestLiveGrepWithoutPortIsANotice checks that live grep with no port shows a
// notice and opens nothing.
func TestLiveGrepWithoutPortIsANotice(t *testing.T) {
	t.Parallel()
	m, _ := newLiveModel(t, nil)
	must(t, m.Run("open_live_grep"))
	if len(m.overlays) != 0 {
		t.Fatal("live grep without a port opened the palette")
	}
	if got := m.statusView().Message; got != "Live Grep is not supported in this port" {
		t.Fatalf("status = %q, want the not-supported notice", got)
	}
}

// TestLiveGrepSearchErrorIsNotAFailure checks that a search that fails shows its
// error in the palette and does not reach the notice row.
func TestLiveGrepSearchErrorIsNotAFailure(t *testing.T) {
	t.Parallel()
	m, _ := newLiveModel(t, failingGrep{errors.New("bad read")})
	must(t, m.Run("open_live_grep"))
	typeInto(m, "x")
	if got := m.statusView().Message; got == "bad read" {
		t.Fatal("a search error went to the notice row, want the palette")
	}
}

// bufferEditor is a lineEditor that also snapshots its buffers, as the app adapter does
// for Live Grep's Buffers scope.
type bufferEditor struct {
	*lineEditor
	texts []BufferText
}

func (e *bufferEditor) BufferTexts() ([]BufferText, error) { return e.texts, nil }

// TestLiveGrepBuffersScope: the root snapshots the open buffers when the editor offers
// them, the Buffers toggle sends them, and Enter on a buffer hit activates its tab and
// moves the caret instead of opening a file.
func TestLiveGrepBuffersScope(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{
		Rel: "a.go", Line: 2, Col: 1, Text: "todo", Source: palette.GrepBuffers, Index: 0,
	}}}
	ed := &bufferEditor{
		lineEditor: &lineEditor{fakeEditor: newFakeEditor("x")},
		texts:      []BufferText{{Name: "a.go", Path: "/ws/a.go", Text: "one\ntodo\n"}},
	}
	m := newLiveModelOver(t, g, ed)
	must(t, m.Run("open_live_grep"))
	typeInto(m, "todo")
	// The snapshot is taken at open; the Buffers scope starts off, so it is not sent.
	req := g.reqs[len(g.reqs)-1]
	if req.Sources.Buffers || req.Buffers != nil {
		t.Fatalf("the Buffers scope starts on: %+v, buffers %+v", req.Sources, req.Buffers)
	}
	// Alt+U turns the scope on and searches again with the snapshot.
	feed(m, runKey(m, altKey('u')))
	req = g.reqs[len(g.reqs)-1]
	if !req.Sources.Buffers || len(req.Buffers) != 1 || req.Buffers[0].Name != "a.go" {
		t.Fatalf("the Buffers scope did not turn on: %+v buffers %+v", req.Sources, req.Buffers)
	}
	// Enter on the buffer hit activates its tab and moves the caret there.
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(ed.opened) != 0 {
		t.Fatalf("a buffer hit opened a file: %v", ed.opened)
	}
	if got := ed.gotos[len(ed.gotos)-1]; got != [2]int{2, 1} {
		t.Fatalf("moved to %v, want line 2 column 1", ed.gotos)
	}
}

// TestLiveGrepStartsFromTheSearchConfig: the case mode starts from
// editor.search.case_sensitive.
func TestLiveGrepStartsFromTheSearchConfig(t *testing.T) {
	t.Parallel()
	g := &grepPort{}
	m, _ := newLiveModel(t, g)
	m.view.opts.Search.CaseSensitive = true
	must(t, m.Run("open_live_grep"))
	typeInto(m, "x")
	if req := g.reqs[len(g.reqs)-1]; !req.Query.CaseSensitive {
		t.Fatalf("case mode = %+v, want on from the config", req.Query)
	}
}

// failingGrep fails every search with err.
type failingGrep struct{ err error }

func (f failingGrep) Search(palette.GrepRequest) ([]palette.GrepHit, error) {
	return nil, f.err
}

// TestRemoteLiveGrepRefusesLocalSearch checks a remote launch never opens Live Grep
// over the local grep port: the local project must not be searched silently for
// remote-looking rows. The notice says why.
func TestRemoteLiveGrepRefusesLocalSearch(t *testing.T) {
	t.Parallel()
	g := &grepPort{}
	ed := &lineEditor{fakeEditor: newFakeEditor("x")}
	m := New(Deps{
		Editor:   ed,
		Explorer: newFakeTree(1),
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Root:     RootPorts{Grep: g},
		FileRoot: "/remote/w",
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	must(t, m.Run("open_live_grep"))
	if len(m.overlays) != 0 {
		t.Fatal("Live Grep opened the local palette in a remote workspace")
	}
	if got := m.statusView().Message; !strings.Contains(got, "remote workspace") {
		t.Fatalf("status = %q, want the remote-not-available notice", got)
	}
	if len(g.queries) != 0 {
		t.Fatalf("the local grep port was searched in a remote workspace: %v", g.queries)
	}
	// resume_live_grep must refuse too, not replay a local search.
	must(t, m.Run("resume_live_grep"))
	if len(m.overlays) != 0 || len(g.queries) != 0 {
		t.Fatalf("resume opened/searched locally: overlays %d queries %v", len(m.overlays), g.queries)
	}
}

// TestResumeCacheResetOnConfigAndProjectChanges: a configuration change and a project
// root change both drop the cached Live Grep state, so Resume cannot replay matches
// from the old root or configuration.
func TestResumeCacheResetOnConfigAndProjectChanges(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), newFakeTree(1), 80, 24)
	m.root.lastGrep = &palette.LiveResume{Text: "todo", Done: true}
	m.applySettingsChange(SettingsChange{})
	if m.root.lastGrep != nil {
		t.Fatal("config change kept the resume cache")
	}
	m.root.lastGrep = &palette.LiveResume{Text: "todo", Done: true}
	m.util.switchKeymap = func(string) (Keymap, error) { return testKeys(), nil }
	m.switchKeymap("emacs")
	if m.root.lastGrep != nil {
		t.Fatal("keymap/config switch kept the resume cache")
	}
	m.root.lastGrep = &palette.LiveResume{Text: "todo", Done: true}
	m.adoptProject(projectOpenedMsg{root: "/next", project: Project{Tree: treeAt("/next", 1), Files: &fakeIndex{}}})
	if m.root.lastGrep != nil {
		t.Fatal("project root change kept the resume cache")
	}
}

// TestLiveGrepExportOpensQuickfixList: Alt+Q from Live Grep closes the palette and opens
// the shared list overlay with the hits; Enter on a row jumps to it while the list stays
// open (the same walking behavior as the project-search export).
func TestLiveGrepExportOpensQuickfixList(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{
		{Rel: "src/a.go", Line: 7, Col: 2, Text: "todo a"},
		{Rel: "src/b.go", Line: 3, Col: 1, Text: "todo b"},
	}}
	m, ed := newLiveModel(t, g)
	must(t, m.Run("open_live_grep"))
	typeInto(m, "todo")
	feed(m, runKey(m, altKey('q')))
	if len(m.overlays) != 1 {
		t.Fatalf("overlays after export = %d, want the quickfix list only", len(m.overlays))
	}
	v, ok := m.overlays[0].(*list.View)
	if !ok {
		t.Fatalf("top overlay = %T, want the quickfix list", m.overlays[len(m.overlays)-1])
	}
	if !strings.Contains(v.Title, "Live Grep: todo") || !strings.Contains(v.Title, "2 matches") {
		t.Fatalf("list title = %q", v.Title)
	}
	if len(v.Rows) != 2 || !strings.Contains(v.Rows[0].Left, "src/a.go:7") {
		t.Fatalf("list rows = %+v", v.Rows)
	}
	// Enter on the first row opens the file and keeps the list open for the next row.
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(ed.opened) != 1 || ed.opened[0] != "/ws/src/a.go" {
		t.Fatalf("opened %v from the quickfix list, want the first hit", ed.opened)
	}
	if len(m.overlays) == 0 {
		t.Fatal("the quickfix list closed after Enter")
	}
}

// TestLiveGrepExportSanitizesDisplayOnly: the quickfix list label/right text are cleaned,
// but the row Value still carries the raw path/index so Enter opens the real target.
func TestLiveGrepExportSanitizesDisplayOnly(t *testing.T) {
	t.Parallel()
	m := newTerminalModel(t, newFakeTree(1), dockKeys(), &shellsStarter{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	rawRel := "\u202Eevil.go"
	cmd := m.openLiveGrepResultsList(palette.GrepExportMsg{
		Root:  "/ws",
		Query: "todo\u202E",
		Hits: []palette.GrepHit{{
			Source: palette.GrepFiles, Rel: rawRel, Line: 2, Col: 1, Text: "\u202Etodo\u0080",
		}},
	})
	_ = cmd
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want one export list", len(m.overlays))
	}
	v, ok := m.overlays[0].(*list.View)
	if !ok {
		t.Fatalf("overlay = %T", m.overlays[0])
	}
	if len(v.Rows) != 1 {
		t.Fatalf("rows = %+v", v.Rows)
	}
	row := v.Rows[0]
	if strings.ContainsAny(row.Left+row.Right, "\u202e\u0080") {
		t.Fatalf("export display kept spoof bytes: %+v", row)
	}
	if !strings.HasPrefix(row.Value, "f\t") || !strings.Contains(row.Value, rawRel) {
		t.Fatalf("export value = %q, want raw path target %q", row.Value, rawRel)
	}
	if strings.Contains(v.Title, "\u202e") {
		t.Fatalf("export title kept bidi: %q", v.Title)
	}
}
