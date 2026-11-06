package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/explorer/watch"
	"github.com/tbxark/gvim/internal/theme"
)

// countingTree is a fake tree that counts its reloads.
type countingTree struct {
	*fakeTree
	reloads int
}

func (t *countingTree) Reload() error {
	t.reloads++
	return nil
}

// newWatchModel returns a model whose tree counts its reloads, with the watcher
// channel ch and no git or auto-save ports, so Init starts only the watch.
func newWatchModel(t *testing.T, ch <-chan watch.Changed, tree *countingTree) *Model {
	t.Helper()
	m := New(Deps{
		Editor:   newFakeEditor("x"),
		Explorer: tree,
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Root:     RootPorts{Watch: ch},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m
}

// TestWatchChangeReloadsTheTreeAndWaitsAgain checks that a change in the tree root
// reloads the tree, as Ctrl+R does, and that the model waits for the next change.
func TestWatchChangeReloadsTheTreeAndWaitsAgain(t *testing.T) {
	t.Parallel()
	ch := make(chan watch.Changed, 1)
	ch <- watch.Changed{Dirs: []string{"/ws"}}
	tree := &countingTree{fakeTree: newFakeTree(2)}
	m := newWatchModel(t, ch, tree)

	msgs := collect(m.Init())
	if len(msgs) != 1 {
		t.Fatalf("Init sent %d messages, want the change", len(msgs))
	}
	_, cmd := m.Update(msgs[0])
	if tree.reloads != 1 {
		t.Fatalf("reloads = %d, want 1", tree.reloads)
	}
	if cmd == nil {
		t.Fatal("the model did not wait for the next change")
	}
}

// TestWatchClosedChannelStopsWaiting checks that a closed watcher sends nothing and
// is not waited on again, so the program does not block on it.
func TestWatchClosedChannelStopsWaiting(t *testing.T) {
	t.Parallel()
	ch := make(chan watch.Changed)
	close(ch)
	m := newWatchModel(t, ch, &countingTree{fakeTree: newFakeTree(2)})
	if msgs := collect(m.Init()); len(msgs) != 0 {
		t.Fatalf("a closed watcher sent %d messages", len(msgs))
	}
}

// TestWatchChangeAlsoRefreshesGitStatus checks that a change in the tree root asks
// for the git status again, because the change may be a commit or a checkout.
func TestWatchChangeAlsoRefreshesGitStatus(t *testing.T) {
	t.Parallel()
	ch := make(chan watch.Changed, 1)
	ch <- watch.Changed{Dirs: []string{"/ws"}}
	g := &fakeGit{}
	m := New(Deps{
		Editor:   newFakeEditor("x"),
		Explorer: &countingTree{fakeTree: newFakeTree(2)},
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Root:     RootPorts{Watch: ch, Git: g},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	_, cmd := m.Update(watchMsg{})
	var got bool
	for _, msg := range collect(cmd) {
		if _, ok := msg.(gitStatusMsg); ok {
			got = true
		}
	}
	if !got {
		t.Fatal("a change did not start a git status read")
	}
}

// fakeWatchDirs records the folders that are added to the file watcher, in order.
type fakeWatchDirs struct {
	added []string
}

func (f *fakeWatchDirs) Add(dir string) error {
	f.added = append(f.added, dir)
	return nil
}

// TestWatchAddsTheFoldersOfOpenDocumentsOffTheUpdateLoop checks that the folders of the open
// documents are added to the watcher, as well as the tree root. The adds run in a command, not
// in Update, and a folder is added once.
func TestWatchAddsTheFoldersOfOpenDocumentsOffTheUpdateLoop(t *testing.T) {
	t.Parallel()
	dirs := &fakeWatchDirs{}
	ed := newAutoTabsEditor(0, openDoc("/ws/a.txt", false), openDoc("/other/b.txt", false), openDoc("", false))
	m := New(Deps{
		Editor: ed,
		Keys:   testKeys(),
		Theme:  theme.Theme{},
		Clock:  testClock,
		Root:   RootPorts{Watch: make(chan watch.Changed), WatchDirs: dirs},
	})
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if len(dirs.added) != 0 {
		t.Fatalf("Update added %q to the watcher, want the adds in the command", dirs.added)
	}
	collect(cmd)
	if want := []string{"/ws", "/other"}; !slices.Equal(dirs.added, want) {
		t.Fatalf("watched folders = %q, want %q", dirs.added, want)
	}
	_, cmd = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	collect(cmd)
	if want := []string{"/ws", "/other"}; !slices.Equal(dirs.added, want) {
		t.Fatalf("watched folders after a second Update = %q, want %q", dirs.added, want)
	}
}

// TestWatchAddsOpenFoldersAgainAfterAProjectSwitch checks that a project switch, which moves the
// watcher to the new root and so removes the old one, asks again for the folders of the
// documents that stay open.
func TestWatchAddsOpenFoldersAgainAfterAProjectSwitch(t *testing.T) {
	t.Parallel()
	dirs := &fakeWatchDirs{}
	ed := newAutoTabsEditor(0, openDoc("/ws/a.txt", false))
	m := New(Deps{
		Editor:   ed,
		Explorer: treeAt("/ws", 2),
		Keys:     testKeys(),
		Theme:    theme.Theme{},
		Clock:    testClock,
		Root:     RootPorts{Watch: make(chan watch.Changed), WatchDirs: dirs},
	})
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	collect(cmd)
	_, cmd = m.Update(projectOpenedMsg{root: "/next", project: Project{Tree: treeAt("/next", 2)}})
	collect(cmd)
	if want := []string{"/ws", "/ws"}; !slices.Equal(dirs.added, want) {
		t.Fatalf("watched folders = %q, want %q: the folder of the open document is asked again", dirs.added, want)
	}
}

// TestInitAddsTheFoldersOfTheDocumentsOpenAtStart checks that the documents open when the
// program starts have their folders watched by Init.
func TestInitAddsTheFoldersOfTheDocumentsOpenAtStart(t *testing.T) {
	t.Parallel()
	ch := make(chan watch.Changed)
	close(ch)
	dirs := &fakeWatchDirs{}
	ed := newAutoTabsEditor(0, openDoc("/ws/a.txt", false))
	m := New(Deps{
		Editor: ed,
		Keys:   testKeys(),
		Theme:  theme.Theme{},
		Clock:  testClock,
		Root:   RootPorts{Watch: ch, WatchDirs: dirs},
	})
	collect(m.Init())
	if want := []string{"/ws"}; !slices.Equal(dirs.added, want) {
		t.Fatalf("watched folders at start = %q, want %q", dirs.added, want)
	}
}

// TestWatchNilChannelWaitsForNothing checks that Init does not wait on a watcher
// that is not wired.
func TestWatchNilChannelWaitsForNothing(t *testing.T) {
	t.Parallel()
	m := newWatchModel(t, nil, &countingTree{fakeTree: newFakeTree(2)})
	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init waits on a watcher with no channel")
	}
}
