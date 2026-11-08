package app

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/filewatch"
)

// resolved returns dir with its symbolic links resolved, as the project opener
// records and shows it.
func resolved(t *testing.T, dir string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(dir)
	must(t, err)
	return r
}

// TestProjectOpenReRootsTheTreeAndTheIndex checks that opening a folder gives a
// tree and a file index of that folder, records it as recent, and stops the index of
// the folder that was left.
func TestProjectOpenReRootsTheTreeAndTheIndex(t *testing.T) {
	isolateConfig(t)
	first, second := t.TempDir(), t.TempDir()
	writeTree(t, second, map[string]string{"new.txt": "x"})
	p := &projectOpener{ctx: t.Context()}

	_, err := p.Open(first)
	must(t, err)
	left := p.index
	b, err := p.Open(second)
	must(t, err)

	if got, want := b.Tree.Root(), resolved(t, second); got != want {
		t.Fatalf("tree root = %q, want %q", got, want)
	}
	<-b.Files.Done()
	files, _ := b.Files.Snapshot()
	if !slices.Contains(files, "new.txt") {
		t.Fatalf("index of the new root = %v, want new.txt", files)
	}
	if len(b.Recents) == 0 || b.Recents[0] != resolved(t, second) {
		t.Fatalf("recents = %v, want the new folder first", b.Recents)
	}
	select {
	case <-left.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the index of the folder that was left still runs")
	}
}

// TestProjectOpenMissingFolderIsAnError checks that a folder that cannot be opened
// is an error, and that nothing is replaced.
func TestProjectOpenMissingFolderIsAnError(t *testing.T) {
	isolateConfig(t)
	p := &projectOpener{ctx: t.Context()}
	if _, err := p.Open(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("Open of a missing folder returned no error")
	}
	if p.index != nil {
		t.Fatal("a failed open replaced the file index")
	}
}

// TestProjectOpenMovesTheWatcher checks that the watcher follows the root: a change
// in the folder that was left is not reported, and a change in the new folder is.
func TestProjectOpenMovesTheWatcher(t *testing.T) {
	isolateConfig(t)
	w, err := filewatch.New(filewatch.Options{Delay: 20 * time.Millisecond})
	must(t, err)
	defer func() { _ = w.Close() }()
	first, second := t.TempDir(), t.TempDir()
	p := &projectOpener{ctx: t.Context(), watcher: w}
	_, err = p.Open(first)
	must(t, err)
	_, err = p.Open(second)
	must(t, err)

	writeTree(t, first, map[string]string{"left.txt": "x"})
	select {
	case c := <-w.Events():
		t.Fatalf("a change in the folder that was left was reported: %v", c.Dirs)
	case <-time.After(300 * time.Millisecond):
	}

	writeTree(t, second, map[string]string{"here.txt": "x"})
	select {
	case <-w.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("a change in the new folder was not reported")
	}
}

// TestProjectCloseStopsTheIndex checks that closing the opener stops the index it
// holds, so the program's exit leaves no enumeration running.
func TestProjectCloseStopsTheIndex(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	p := &projectOpener{ctx: t.Context()}
	b, err := p.Open(dir)
	must(t, err)
	p.close()
	select {
	case <-b.Files.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the index was still running after close")
	}
}

// TestBuildRuntimeStartsTheWatcherOnlyWhenRunning checks that the watcher of the
// tree root is started for a run and is closed with the runtime, and that a build
// for a test does not start one.
func TestBuildRuntimeStartsTheWatcherOnlyWhenRunning(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	quiet, err := buildRuntime(t.Context(), Options{Root: dir}, false)
	must(t, err)
	if quiet.watcher != nil {
		t.Fatal("a build without a run started a watcher")
	}
	quiet.close()

	running, err := buildRuntime(t.Context(), Options{Root: dir}, true)
	must(t, err)
	if running.watcher == nil {
		t.Fatal("a run did not start the watcher")
	}
	running.close()
}
