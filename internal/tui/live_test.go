package tui

import (
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

// TestResumeLiveGrepRerunsWithPriorState: Resume keeps the prior query, modes and
// scopes but always searches again, because file contents may have changed while the
// palette was closed; the cached row is not reused.
func TestResumeLiveGrepRerunsWithPriorState(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "a.go", Line: 1, Col: 1, Text: "todo"}}}
	m, ed := newLiveModel(t, g)
	must(t, m.Run("open_live_grep"))
	typeInto(m, "todo")
	feed(m, runKey(m, altKey('o'))) // Whole Word on, and its search result arrives
	m.Update(special(tea.KeyEscape))

	// The file content changed while the palette was closed: the old a.go hit must not
	// be reused; the fresh search returns b.go.
	g.hits = []palette.GrepHit{{Rel: "b.go", Line: 2, Col: 1, Text: "todo changed"}}
	g.queries, g.reqs = nil, nil
	must(t, m.Run("resume_live_grep"))
	feed(m, m.pending)
	m.pending = nil
	if !slices.Equal(g.queries, []string{"todo"}) {
		t.Fatalf("resume searched %v, want exactly the prior query todo", g.queries)
	}
	if len(g.reqs) != 1 || !g.reqs[0].Query.WholeWord {
		t.Fatalf("resume request = %+v, want the prior Whole Word mode", g.reqs)
	}
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(ed.opened) != 1 || ed.opened[0] != "/ws/b.go" {
		t.Fatalf("resume row opened %v, want the fresh /ws/b.go, not the stale a.go", ed.opened)
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

// TestResumeCacheResetOnConfigAndProjectChanges: a configuration change and a project
// root change both drop the cached Live Grep state, so Resume cannot replay matches
// from the old root or configuration.
func TestResumeCacheResetOnConfigAndProjectChanges(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), newFakeTree(1), 80, 24)
	m.root.lastGrep = &palette.LiveResume{Text: "todo"}
	m.applySettingsChange(SettingsChange{})
	if m.root.lastGrep != nil {
		t.Fatal("config change kept the resume cache")
	}
	m.root.lastGrep = &palette.LiveResume{Text: "todo"}
	m.util.switchKeymap = func(string) (Keymap, error) { return testKeys(), nil }
	m.switchKeymap("emacs")
	if m.root.lastGrep != nil {
		t.Fatal("keymap/config switch kept the resume cache")
	}
	m.root.lastGrep = &palette.LiveResume{Text: "todo"}
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

// TestRemoteLiveGrepUsesTheRemoteRoot: a remote workspace's Live Grep uses the remote
// root (not the local process root) for the GrepRequest and opens a file row through the
// remote document access path.
func TestRemoteLiveGrepUsesTheRemoteRoot(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "src/a.go", Line: 7, Col: 2, Text: "todo"}}}
	m, ed := newLiveModel(t, g)
	m.fileRoot = "/remote/w"
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	must(t, m.Run("open_live_grep"))
	if m.notice != "" {
		t.Fatalf("remote Live Grep notice = %q, want no refusal", m.notice)
	}
	typeInto(m, "todo")
	req := g.reqs[len(g.reqs)-1]
	if req.Root != "/remote/w" || req.Text != "todo" {
		t.Fatalf("remote request = root %q text %q, want /remote/w todo", req.Root, req.Text)
	}
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(ed.opened) != 1 || ed.opened[0] != "/remote/w/src/a.go" {
		t.Fatalf("opened %v, want the remote path", ed.opened)
	}
}

// TestRemoteLiveGrepExportOpensTheRemotePath: Alt+Q of a remote file hit keeps the remote
// flag in the export message, so the list Enter joins with '/' and opens through the
// remote document access instead of the local filepath path.
func TestRemoteLiveGrepExportOpensTheRemotePath(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "src/a.go", Line: 7, Col: 2, Text: "todo"}}}
	m, ed := newLiveModel(t, g)
	m.fileRoot = "/remote/w"
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	must(t, m.Run("open_live_grep"))
	typeInto(m, "todo")
	feed(m, runKey(m, altKey('q')))
	if len(m.overlays) != 1 {
		t.Fatalf("overlays after remote export = %d, want the quickfix list", len(m.overlays))
	}
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(ed.opened) != 1 || ed.opened[0] != "/remote/w/src/a.go" {
		t.Fatalf("opened %v from the remote quickfix list, want the remote path", ed.opened)
	}
}

// TestRemoteLiveGrepResumeSearchesRemoteAgain: Resume in a remote workspace restores
// the controls but re-runs the search through the remote port and remote root; it never
// replays local or stale hits.
func TestRemoteLiveGrepResumeSearchesRemoteAgain(t *testing.T) {
	t.Parallel()
	g := &grepPort{hits: []palette.GrepHit{{Rel: "src/a.go", Line: 7, Col: 2, Text: "todo"}}}
	m, ed := newLiveModel(t, g)
	m.fileRoot = "/remote/w"
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	must(t, m.Run("open_live_grep"))
	typeInto(m, "todo")
	m.Update(special(tea.KeyEscape))

	g.queries, g.reqs = nil, nil
	must(t, m.Run("resume_live_grep"))
	feed(m, m.pending)
	m.pending = nil
	if !slices.Equal(g.queries, []string{"todo"}) {
		t.Fatalf("remote resume searched %v, want exactly todo again", g.queries)
	}
	if len(g.reqs) != 1 || g.reqs[0].Root != "/remote/w" {
		t.Fatalf("remote resume request = %+v, want root /remote/w", g.reqs)
	}
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(ed.opened) != 1 || ed.opened[0] != "/remote/w/src/a.go" {
		t.Fatalf("remote resume opened %v, want the remote path", ed.opened)
	}
}
