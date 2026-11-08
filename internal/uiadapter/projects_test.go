package uiadapter

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/filewatch"
)

// resolved returns dir with its symbolic links resolved, as the project opener
// records and shows it.
func resolved(t *testing.T, dir string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(dir)
	apptest.Must(t, err)
	return r
}

// writeTree writes files into root, making directories as needed.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, name)
		apptest.Must(t, os.MkdirAll(filepath.Dir(path), 0o755))
		apptest.Must(t, os.WriteFile(path, []byte(body), 0o644))
	}
}

func TestProjectOpenReRootsTheTreeAndTheIndex(t *testing.T) {
	apptest.IsolateConfig(t)
	first, second := t.TempDir(), t.TempDir()
	writeTree(t, second, map[string]string{"new.txt": "x"})
	p := &Projects{ctx: t.Context()}

	_, err := p.Open(first)
	apptest.Must(t, err)
	left := p.index
	b, err := p.Open(second)
	apptest.Must(t, err)

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

func TestProjectOpenMissingFolderIsAnError(t *testing.T) {
	apptest.IsolateConfig(t)
	p := &Projects{ctx: t.Context()}
	if _, err := p.Open(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("Open of a missing folder returned no error")
	}
	if p.index != nil {
		t.Fatal("a failed open replaced the file index")
	}
}

func TestProjectOpenMovesTheWatcher(t *testing.T) {
	apptest.IsolateConfig(t)
	w, err := filewatch.New(filewatch.Options{Delay: 20 * time.Millisecond})
	apptest.Must(t, err)
	defer func() { _ = w.Close() }()
	first, second := t.TempDir(), t.TempDir()
	p := &Projects{ctx: t.Context(), watcher: w}
	_, err = p.Open(first)
	apptest.Must(t, err)
	_, err = p.Open(second)
	apptest.Must(t, err)

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

func TestProjectCloseStopsTheIndex(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	p := &Projects{ctx: t.Context()}
	b, err := p.Open(dir)
	apptest.Must(t, err)
	p.Close()
	select {
	case <-b.Files.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the index was still running after close")
	}
}
