package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/theme"
)

// projEd is the editor a project-search test runs over: it answers the tabs and the
// open buffers' text, loads closed files, and records the edits it applies.
type projEd struct {
	*fakeEditor
	ptabs   []TabInfo
	texts   []BufferText
	opened  []string
	applied map[string][]editor.LSPEdit
}

func newProjEd(tabs []TabInfo, texts []BufferText) *projEd {
	return &projEd{fakeEditor: newFakeEditor("x"), ptabs: tabs, texts: texts, applied: map[string][]editor.LSPEdit{}}
}

func (e *projEd) Tabs() []TabInfo { return e.ptabs }
func (e *projEd) Active() Document {
	for _, t := range e.ptabs {
		if t.Active {
			return nil
		}
	}
	return nil
}
func (e *projEd) BufferTexts() ([]BufferText, error) { return e.texts, nil }

func (e *projEd) LoadFunc(path string) func() (*editor.Document, error) {
	return func() (*editor.Document, error) { return &editor.Document{Path: path}, nil }
}

func (e *projEd) OpenLoaded(d *editor.Document) {
	e.opened = append(e.opened, d.Path)
	e.ptabs = append(e.ptabs, TabInfo{Name: filepath.Base(d.Path), Path: d.Path, Modified: true})
}

func (e *projEd) ApplyLSPEdits(path string, edits []editor.LSPEdit) error {
	open := false
	for _, t := range e.ptabs {
		if t.Path == path {
			open = true
			break
		}
	}
	if !open {
		return fmt.Errorf("%w: %s", editor.ErrNotOpen, path)
	}
	e.applied[path] = append(e.applied[path], edits...)
	return nil
}

func (e *projEd) GotoLine(line, col int) error  { return nil }
func (e *projEd) OpenFile(path string) error    { e.opened = append(e.opened, path); return nil }
func (e *projEd) OpenPreview(path string) error { return e.OpenFile(path) }

// fakeProjectSearcher answers the project search with a fixed result.
type fakeProjectSearcher struct {
	result searchpanel.SearchResult
	err    error
	reqs   []searchpanel.SearchRequest
}

func (f *fakeProjectSearcher) ProjectSearch(req searchpanel.SearchRequest) (searchpanel.SearchResult, error) {
	f.reqs = append(f.reqs, req)
	return f.result, f.err
}

// fakeProjectReplacer answers the replacement with a fixed plan.
type fakeProjectReplacer struct {
	result searchpanel.ReplaceResult
	err    error
	reqs   []searchpanel.ReplaceRequest
}

func (f *fakeProjectReplacer) ProjectReplace(req searchpanel.ReplaceRequest) (searchpanel.ReplaceResult, error) {
	f.reqs = append(f.reqs, req)
	return f.result, f.err
}

// fakeGitGrepper answers git grep with a fixed result.
type fakeGitGrepper struct {
	result searchpanel.GitResult
	err    error
	reqs   []searchpanel.GitRequest
}

func (f *fakeGitGrepper) GitGrep(req searchpanel.GitRequest) (searchpanel.GitResult, error) {
	f.reqs = append(f.reqs, req)
	return f.result, f.err
}

// newProjectModel returns a model over ed with the given project-search ports.
func newProjectPanelModel(t *testing.T, ed EditorState, s searchpanel.Searcher, r searchpanel.Replacer, g searchpanel.GitGrepper) *Model {
	t.Helper()
	m := New(Deps{
		Editor:   ed,
		Explorer: newFakeTree(1),
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Root:     RootPorts{ProjectSearch: s, ProjectReplace: r, GitGrep: g},
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// projectSearch runs a query through the open panel: the keys are typed first, so the
// debounce of each earlier prefix is superseded, and the last one runs the search.
func projectSearch(t *testing.T, m *Model, query string) {
	t.Helper()
	var cmd tea.Cmd
	for _, r := range query {
		_, cmd = m.Update(typed(string(r)))
	}
	feed(m, cmd)
}

// openSearchPanel opens the project panel and returns it.
func openSearchPanel(t *testing.T, m *Model) *searchpanel.Panel {
	t.Helper()
	if err := m.Run("start_search_replace"); err != nil {
		t.Fatalf("start_search_replace: %v", err)
	}
	for _, o := range m.overlays {
		if p, ok := o.(*searchpanel.Panel); ok {
			return p
		}
	}
	t.Fatalf("the project search panel did not open")
	return nil
}

func TestProjectSearchPanelSearchesThroughThePort(t *testing.T) {
	t.Parallel()
	s := &fakeProjectSearcher{result: searchpanel.SearchResult{Files: []searchpanel.FileMatches{{
		Path: "/ws/a.go", Name: "a.go", Hash: 7,
		Matches: []searchpanel.Match{{Line: 2, Col: 1, Len: 3, Text: "foo bar"}},
	}}}}
	ed := newProjEd([]TabInfo{{Name: "a.go", Path: "/ws/a.go", Active: true}}, []BufferText{{Name: "a.go", Path: "/ws/a.go", Text: "foo bar"}})
	m := newProjectPanelModel(t, ed, s, nil, nil)
	p := openSearchPanel(t, m)
	projectSearch(t, m, "foo")
	if len(s.reqs) != 1 || s.reqs[0].Query != "foo" {
		t.Fatalf("searches = %+v", s.reqs)
	}
	if s.reqs[0].Scope != searchpanel.AllFiles {
		t.Fatalf("request = %+v, want the all-files scope", s.reqs[0])
	}
	if len(s.reqs[0].OpenFiles) != 1 || s.reqs[0].OpenFiles[0].Text != "foo bar" {
		t.Fatalf("open buffers = %+v, want the unsaved text snapshot", s.reqs[0].OpenFiles)
	}
	if matches, files, _ := p.Stats(); matches != 1 || files != 1 {
		t.Fatalf("panel stats = %d in %d", matches, files)
	}
	// The next match action jumps to the result through the editor.
	if err := m.Run("search_replace_next_match"); err != nil {
		t.Fatalf("search_replace_next_match: %v", err)
	}
	if len(ed.opened) == 0 || ed.opened[0] != "/ws/a.go" {
		t.Fatalf("next match opened %v", ed.opened)
	}
}

func TestGitGrepPanelOpensReadOnly(t *testing.T) {
	t.Parallel()
	g := &fakeGitGrepper{result: searchpanel.GitResult{Files: []searchpanel.FileMatches{{
		Path: "a.go", Name: "a.go", Matches: []searchpanel.Match{{Line: 1, Col: 1, Len: 3, Text: "foo"}},
	}}}}
	m := newProjectPanelModel(t, newProjEd(nil, nil), nil, nil, g)
	if err := m.Run("start_git_grep"); err != nil {
		t.Fatal(err)
	}
	var p *searchpanel.Panel
	for _, o := range m.overlays {
		if v, ok := o.(*searchpanel.Panel); ok {
			p = v
		}
	}
	if p == nil || !p.Git() {
		t.Fatalf("the Git Grep panel did not open: %v", p)
	}
}

func TestProjectReplaceAppliesToOpenDocuments(t *testing.T) {
	t.Parallel()
	text := "foo bar"
	ed := newProjEd(
		[]TabInfo{{Name: "a.go", Path: "/ws/a.go", Active: true}},
		[]BufferText{{Name: "a.go", Path: "/ws/a.go", Text: text}},
	)
	m := newProjectPanelModel(t, ed, &fakeProjectSearcher{}, &fakeProjectReplacer{}, nil)
	p := searchpanel.New(searchpanel.Options{Root: "/ws"})
	m.proj.panel = p
	msg := projectReplaceDoneMsg{result: searchpanel.ReplaceResult{Files: []searchpanel.FileEdits{{
		Path: "/ws/a.go", Name: "a.go", Dirty: true, Hash: hashText(text),
		Edits: []searchpanel.ReplaceEdit{{From: searchpanel.Position{}, To: searchpanel.Position{Line: 0, Col: 3}, Text: "baz"}},
	}}}}
	m.Update(msg)
	edits := ed.applied["/ws/a.go"]
	if len(edits) != 1 || edits[0].Text != "baz" || edits[0].From != (buffer.Pos{}) || edits[0].To != (buffer.Pos{Line: 0, Col: 3}) {
		t.Fatalf("applied = %+v, want the planned edit", edits)
	}
	if !strings.Contains(m.notice, "1 file changed in 1 tab; Save All to write") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestProjectReplaceSkipsStaleOpenDocument(t *testing.T) {
	t.Parallel()
	ed := newProjEd(
		[]TabInfo{{Name: "a.go", Path: "/ws/a.go", Active: true}},
		[]BufferText{{Name: "a.go", Path: "/ws/a.go", Text: "changed since the search"}},
	)
	m := newProjectPanelModel(t, ed, &fakeProjectSearcher{}, &fakeProjectReplacer{}, nil)
	m.proj.panel = searchpanel.New(searchpanel.Options{Root: "/ws"})
	m.Update(projectReplaceDoneMsg{result: searchpanel.ReplaceResult{Files: []searchpanel.FileEdits{{
		Path: "/ws/a.go", Name: "a.go", Dirty: true, Hash: hashText("the searched text"),
		Edits: []searchpanel.ReplaceEdit{{Text: "baz"}},
	}}}})
	if len(ed.applied) != 0 {
		t.Fatalf("a stale file was changed: %+v", ed.applied)
	}
	if !strings.Contains(m.notice, "0 files changed") || !strings.Contains(m.notice, "1 skipped") {
		t.Fatalf("notice = %q, want the skip", m.notice)
	}
}

func TestProjectReplaceLoadsClosedFiles(t *testing.T) {
	t.Parallel()
	ed := newProjEd([]TabInfo{{Name: "a.go", Path: "/ws/a.go", Active: true}}, []BufferText{{Name: "a.go", Path: "/ws/a.go", Text: "x"}})
	m := newProjectPanelModel(t, ed, &fakeProjectSearcher{}, &fakeProjectReplacer{}, nil)
	m.proj.panel = searchpanel.New(searchpanel.Options{Root: "/ws"})
	cmd := m.projectReplaceDone(projectReplaceDoneMsg{result: searchpanel.ReplaceResult{Files: []searchpanel.FileEdits{{
		Path: "/ws/new.go", Name: "new.go",
		Edits: []searchpanel.ReplaceEdit{{From: searchpanel.Position{}, To: searchpanel.Position{Line: 0, Col: 3}, Text: "baz"}},
	}}}})
	if cmd == nil {
		t.Fatal("a closed file started no load")
	}
	feed(m, cmd)
	if len(ed.opened) != 1 || ed.opened[0] != "/ws/new.go" {
		t.Fatalf("opened = %v, want the loaded file", ed.opened)
	}
	if len(ed.applied["/ws/new.go"]) != 1 {
		t.Fatalf("applied = %+v, want the edit on the loaded file", ed.applied)
	}
	if !strings.Contains(m.notice, "1 file changed in 1 tab; Save All to write") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestProjectReplaceConfirmationRunsThePort(t *testing.T) {
	t.Parallel()
	r := &fakeProjectReplacer{result: searchpanel.ReplaceResult{}}
	m := newProjectPanelModel(t, newProjEd(nil, nil), &fakeProjectSearcher{}, r, nil)
	req := searchpanel.ReplaceRequest{Root: "/ws", Query: "foo", Replacement: "bar", Matches: 2}
	m.askProjectReplace(req)
	if len(m.overlays) == 0 {
		t.Fatal("the confirmation did not open")
	}
	cmd := m.projectReplaceAnswer(projectReplaceTag{req: req}, prompt.Done{Outcome: prompt.Yes})
	if cmd == nil {
		t.Fatal("a confirmed replacement started no command")
	}
	feed(m, cmd)
	if len(r.reqs) != 1 || r.reqs[0].Replacement != "bar" {
		t.Fatalf("replacement requests = %+v", r.reqs)
	}
}
