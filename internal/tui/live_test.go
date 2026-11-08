package tui

import (
	"errors"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

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

// TestResumeLiveGrepReopensTheLastQuery checks that resume_live_grep searches the
// query of the last live grep again.
func TestResumeLiveGrepReopensTheLastQuery(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}}
	m, _ := newLiveModel(t, g)
	must(t, m.Run("open_live_grep"))
	typeInto(m, "todo")
	m.Update(special(tea.KeyEscape))

	g.queries = nil
	must(t, m.Run("resume_live_grep"))
	feed(m, m.pending)
	m.pending = nil
	if !slices.Equal(g.queries, []string{"todo"}) {
		t.Fatalf("resume searched %v, want the last query todo", g.queries)
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
