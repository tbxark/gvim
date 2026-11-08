package editor

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadFuncOpenLoadedMatchesOpenFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	putFile(t, path, "one\r\ntwo\n")

	want := New(Options{})
	if err := want.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	e := New(Options{})
	doc, err := e.LoadFunc(path)()
	if err != nil {
		t.Fatalf("LoadFunc: %v", err)
	}
	e.OpenLoaded(doc)

	if !slices.Equal(e.Tabs(), want.Tabs()) {
		t.Fatalf("Tabs = %+v, want %+v", e.Tabs(), want.Tabs())
	}
	if got, w := e.Active().LineEnding(), want.Active().LineEnding(); got != w {
		t.Fatalf("LineEnding = %q, want %q", got, w)
	}
	if got, w := e.Status(), want.Status(); got != w {
		t.Fatalf("Status = %q, want %q", got, w)
	}
}

func TestLoadFuncChangesNothingUntilOpened(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	putFile(t, path, "a\n")
	e := New(Options{})
	before := e.Tabs()

	if _, err := e.LoadFunc(path)(); err != nil {
		t.Fatalf("LoadFunc: %v", err)
	}
	if !slices.Equal(e.Tabs(), before) {
		t.Fatalf("Tabs = %+v after the read, want %+v", e.Tabs(), before)
	}
}

func TestOpenLoadedActivatesAnOpenPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	putFile(t, a, "a\n")
	putFile(t, b, "b\n")
	e := New(Options{})
	for _, p := range []string{a, b} {
		if err := e.OpenFile(p); err != nil {
			t.Fatalf("OpenFile(%s): %v", p, err)
		}
	}
	doc, err := e.LoadFunc(a)()
	if err != nil {
		t.Fatalf("LoadFunc: %v", err)
	}
	e.OpenLoaded(doc)

	tabs := e.Tabs()
	if len(tabs) != 2 {
		t.Fatalf("%d tabs, want 2: the loaded copy of an open file must not add a tab", len(tabs))
	}
	if !tabs[0].Active || tabs[0].Path != a {
		t.Fatalf("tabs = %+v, want the open a.txt active", tabs)
	}
}

func TestLoadFuncEmptyPathIsAnError(t *testing.T) {
	t.Parallel()
	_, err := New(Options{}).LoadFunc("")()
	if !errors.Is(err, ErrEmptyPath) {
		t.Fatalf("err = %v, want ErrEmptyPath", err)
	}
}

func TestLoadFuncMissingFileTakesDefaultLineEnding(t *testing.T) {
	t.Parallel()
	e := New(Options{DefaultLineEnding: "CRLF"})
	doc, err := e.LoadFunc(filepath.Join(t.TempDir(), "missing.txt"))()
	if err != nil {
		t.Fatalf("LoadFunc: %v", err)
	}
	if got := doc.LineEnding(); got != "CRLF" {
		t.Fatalf("LineEnding() of a new path = %q, want CRLF", got)
	}
}
