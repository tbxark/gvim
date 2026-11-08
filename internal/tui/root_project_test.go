package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/browser"
	"github.com/tbxark/gvim/theme"
)

// fakeProjects answers Open with the project it holds, or with err, and records
// the paths it was asked for.
type fakeProjects struct {
	project Project
	err     error
	opened  []string
}

func (p *fakeProjects) Open(path string) (Project, error) {
	p.opened = append(p.opened, path)
	return p.project, p.err
}

// fakeIndex is a file index with a fixed list of files.
type fakeIndex struct{ files []string }

func (f *fakeIndex) Snapshot() ([]string, bool) { return f.files, false }
func (f *fakeIndex) Done() <-chan struct{}      { return nil }
func (f *fakeIndex) Close()                     {}

// rootedTree is a fake tree with a root of its own.
type rootedTree struct {
	*fakeTree
	root string
}

func (r *rootedTree) Root() string { return r.root }

// treeAt returns a fake tree of n rows whose root is path.
func treeAt(path string, n int) *rootedTree {
	return &rootedTree{fakeTree: newFakeTree(n), root: path}
}

// newProjectModel returns a model over an editor with one tab, a tree rooted at
// /ws, a file index of one file and the project port p, sized to 80 by 20.
func newProjectModel(t *testing.T, p Projects) (*Model, *fakeEditor) {
	t.Helper()
	ed := newFakeEditor("x")
	m := New(Deps{
		Editor:   ed,
		Explorer: newFakeTree(3),
		Files:    &fakeIndex{files: []string{"old.go"}},
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Recents:  []string{"/ws"},
		Root:     RootPorts{Projects: p},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m, ed
}

// firstOf returns the first message of type T in msgs, and fails the test when
// there is none.
func firstOf[T tea.Msg](t *testing.T, msgs []tea.Msg) T {
	t.Helper()
	for _, msg := range msgs {
		if v, ok := msg.(T); ok {
			return v
		}
	}
	var zero T
	t.Fatalf("no %T among %d messages", zero, len(msgs))
	return zero
}

// TestChoosingAFolderNamesTheRootInTheEditor checks that a project switch gives the editor the
// new root, so copy_relative_file_path measures from the folder that is open now. A failed open
// leaves the editor's root as it was.
func TestChoosingAFolderNamesTheRootInTheEditor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		wantRoot string
	}{
		{name: "a switched project is the editor's root", wantRoot: "/next"},
		{name: "a failed open leaves the root unset", err: errors.New("open failed"), wantRoot: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &fakeProjects{err: tt.err, project: Project{Tree: treeAt("/next", 2), Files: &fakeIndex{}}}
			m, ed := newProjectModel(t, p)

			_, cmd := m.Update(browser.Choose{Path: "/next"})
			m.Update(firstOf[projectOpenedMsg](t, collect(cmd)))

			if ed.root != tt.wantRoot {
				t.Fatalf("editor root = %q, want %q", ed.root, tt.wantRoot)
			}
		})
	}
}

// TestOpenFolderOpensTheFolderOnlyBrowser checks that open_folder shows the browser
// in folder mode, which lists the folders and no files.
func TestOpenFolderOpensTheFolderOnlyBrowser(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644))
	tree, err := explorer.Open(dir)
	must(t, err)
	m := newTestModel(t, newFakeEditor("x"), tree, 80, 20)
	must(t, m.Run("open_folder"))

	b := topBrowser(t, m)
	var dirs, files int
	for _, r := range b.Rows() {
		switch r.Kind {
		case browser.RowDir:
			dirs++
		case browser.RowFile:
			files++
		}
	}
	if dirs == 0 || files != 0 {
		t.Fatalf("folder browser lists %d folders and %d files, want folders only", dirs, files)
	}
}

// TestChoosingAFolderReRootsTheExplorerAndKeepsTheTabs checks that a folder pick
// asks the port for the new project, adopts its tree and file index, and leaves the
// open tabs and the editor alone.
func TestChoosingAFolderReRootsTheExplorerAndKeepsTheTabs(t *testing.T) {
	t.Parallel()
	p := &fakeProjects{project: Project{
		Tree:    treeAt("/next", 2),
		Files:   &fakeIndex{files: []string{"new.go"}},
		Recents: []string{"/next", "/ws"},
	}}
	m, ed := newProjectModel(t, p)
	tabs := len(ed.tabs)

	_, cmd := m.Update(browser.Choose{Path: "/next"})
	m.Update(firstOf[projectOpenedMsg](t, collect(cmd)))

	if len(p.opened) != 1 || p.opened[0] != "/next" {
		t.Fatalf("Open called with %v, want /next", p.opened)
	}
	if got := m.rootDir(); got != "/next" {
		t.Fatalf("tree root = %q, want /next", got)
	}
	if got := m.files.(*fakeIndex).files; len(got) != 1 || got[0] != "new.go" {
		t.Fatalf("file index = %v, want the new root's index", got)
	}
	if len(m.recents) != 2 || m.recents[0] != "/next" {
		t.Fatalf("recents = %v, want the new list", m.recents)
	}
	if len(ed.tabs) != tabs || len(ed.actions) != 0 {
		t.Fatalf("switch touched the editor: %d tabs, actions %v", len(ed.tabs), ed.actions)
	}
	if !m.explorerOpen || !m.explorerFocus {
		t.Fatal("the explorer of the new project is not shown and focused")
	}
}

// TestSwitchProjectKeepsTheExplorerSettings checks that the show-ignored setting of
// the old tree carries over to the new one.
func TestSwitchProjectKeepsTheExplorerSettings(t *testing.T) {
	t.Parallel()
	next := treeAt("/next", 1)
	m, _ := newProjectModel(t, &fakeProjects{project: Project{Tree: next}})
	m.tree.SetShowIgnored(true)

	_, cmd := m.Update(browser.Choose{Path: "/next"})
	m.Update(firstOf[projectOpenedMsg](t, collect(cmd)))
	if !next.ignored {
		t.Fatal("show-ignored did not carry over to the new tree")
	}
}

// TestSwitchProjectFailureIsANotice checks that a folder that cannot be opened
// leaves the tree as it was and shows the error.
func TestSwitchProjectFailureIsANotice(t *testing.T) {
	t.Parallel()
	m, _ := newProjectModel(t, &fakeProjects{err: errors.New("open /next: permission denied")})
	_, cmd := m.Update(browser.Choose{Path: "/next"})
	m.Update(firstOf[projectOpenedMsg](t, collect(cmd)))
	if got := m.rootDir(); got != "/ws" {
		t.Fatalf("tree root = %q, want the old root /ws", got)
	}
	if got := m.statusView().Message; got != "open /next: permission denied" {
		t.Fatalf("status = %q, want the open error", got)
	}
}

// TestSwitchProjectWithoutPortIsANotice checks that a model with no project port
// keeps the notice of the port-less build.
func TestSwitchProjectWithoutPortIsANotice(t *testing.T) {
	t.Parallel()
	m, _ := newProjectModel(t, nil)
	m.Update(browser.Choose{Path: "/next"})
	if got := m.statusView().Message; got != "Switch Project is not supported in this port yet" {
		t.Fatalf("status = %q, want the not-supported notice", got)
	}
}

// topBrowser returns the browser on top of the overlay stack.
func topBrowser(t *testing.T, m *Model) *browser.Browser {
	t.Helper()
	if len(m.overlays) == 0 {
		t.Fatal("no overlay is open")
	}
	o, ok := m.overlays[len(m.overlays)-1].(*browser.Browser)
	if !ok {
		t.Fatalf("top overlay is %T, want the browser", m.overlays[len(m.overlays)-1])
	}
	return o
}
