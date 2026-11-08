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
// queries it was asked.
type grepPort struct {
	hits    []palette.GrepHit
	queries []string
}

func (g *grepPort) Search(root string, files []string, query string, limit int) ([]palette.GrepHit, error) {
	g.queries = append(g.queries, query)
	return g.hits, nil
}

// newLiveModel returns a model over a line editor with the live grep port g.
func newLiveModel(t *testing.T, g palette.Grepper) (*Model, *lineEditor) {
	t.Helper()
	ed := &lineEditor{fakeEditor: newFakeEditor("x")}
	m := New(Deps{
		Editor:   ed,
		Explorer: newFakeTree(1),
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Root:     RootPorts{Grep: g},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m, ed
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

// failingGrep fails every search with err.
type failingGrep struct{ err error }

func (f failingGrep) Search(string, []string, string, int) ([]palette.GrepHit, error) {
	return nil, f.err
}
