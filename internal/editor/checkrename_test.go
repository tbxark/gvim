package editor

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

// tabPaths returns the path of every open tab, in tab order.
func tabPaths(e *Editor) []string {
	var paths []string
	for _, tab := range e.Tabs() {
		paths = append(paths, tab.Path)
	}
	return paths
}

// openAll opens every path in order. Each path must be openable.
func openAll(t *testing.T, paths ...string) *Editor {
	t.Helper()
	e := New(Options{})
	for _, p := range paths {
		if err := e.OpenFile(p); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func TestCheckRenameIsADryRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	e := openAll(t, a, b)
	before := tabPaths(e)
	status := e.Status()

	if err := e.CheckRename(a, c); err != nil {
		t.Fatalf("CheckRename to a free path: %v", err)
	}
	if err := e.CheckRename(a, b); !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("CheckRename onto an open tab: err = %v, want ErrAlreadyOpen", err)
	}
	if got := tabPaths(e); !slices.Equal(got, before) {
		t.Fatalf("CheckRename changed the tabs: %v, was %v", got, before)
	}
	if e.Status() != status {
		t.Fatalf("CheckRename changed the status to %q", e.Status())
	}
}

func TestCheckRenameAgreesWithRenamePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	sub := filepath.Join(dir, "sub")
	other := filepath.Join(dir, "other")
	tests := []struct {
		name     string
		open     []string
		from, to string
		want     error
	}{
		{"free name", []string{a, b}, a, c, nil},
		{"same name", []string{a, b}, a, a, nil},
		{"onto an open tab", []string{a, b}, a, b, ErrAlreadyOpen},
		{"folder onto an open tab", []string{filepath.Join(sub, "x.txt"), filepath.Join(other, "x.txt")}, sub, other, ErrAlreadyOpen},
		{"empty old path", []string{a}, "", c, ErrEmptyPath},
		{"empty new path", []string{a}, a, "", ErrEmptyPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := openAll(t, tt.open...)
			check := e.CheckRename(tt.from, tt.to)
			rename := e.RenamePath(tt.from, tt.to)
			if !errors.Is(check, tt.want) {
				t.Errorf("CheckRename = %v, want %v", check, tt.want)
			}
			if !errors.Is(rename, tt.want) {
				t.Errorf("RenamePath = %v, want %v", rename, tt.want)
			}
		})
	}
}
