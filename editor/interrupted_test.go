package editor

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/session"
)

// TestInterruptedSavesListsTheStashedFiles checks that the list is read from the saved
// session in SessionDir. A file with no stash is left out, and an untitled buffer has an
// empty path. A stash name with a directory part is left out too, so the list never names
// a file outside the session directory.
func TestInterruptedSavesListsTheStashedFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name string
		// build saves the session in dir and returns the entries the list should give.
		build func(t *testing.T, dir string) []StashEntry
	}{
		{
			Name: "stashed files and an untitled buffer, in session order",
			build: func(t *testing.T, dir string) []StashEntry {
				fileStash := stashIn(t, dir, "unsaved edit")
				untitledStash := stashIn(t, dir, "scratch")
				must(t, session.Save(dir, session.Session{Files: []session.FileState{
					{Path: "/ws/a.txt", StashFile: fileStash},
					{Path: "/ws/clean.txt"},
					{StashFile: untitledStash},
				}}))
				return []StashEntry{
					{Path: "/ws/a.txt", Stash: filepath.Join(dir, fileStash)},
					{Path: "", Stash: filepath.Join(dir, untitledStash)},
				}
			},
		},
		{
			Name: "a stash name with a directory part is left out",
			build: func(t *testing.T, dir string) []StashEntry {
				must(t, session.Save(dir, session.Session{Files: []session.FileState{
					{Path: "/ws/x.txt", StashFile: "../outside.txt"},
				}}))
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			want := tt.build(t, dir)
			e := New(Options{SessionDir: dir})
			got, err := e.InterruptedSaves()
			if err != nil {
				t.Fatalf("InterruptedSaves: %v", err)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("InterruptedSaves = %v, want %v", got, want)
			}
		})
	}
}

// stashIn stores text as a stash file in dir and returns its name.
func stashIn(t *testing.T, dir, text string) string {
	t.Helper()
	name, err := session.StashContent(dir, text)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

// must fails the test when err is not nil.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestInterruptedSavesWithoutASessionIsEmpty checks the cases with nothing to list: no
// session directory, a directory with no session, and a session with no stash.
func TestInterruptedSavesWithoutASessionIsEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name string
		dir  func(t *testing.T) string
	}{
		{
			Name: "no session directory",
			dir:  func(t *testing.T) string { return "" },
		},
		{
			Name: "a directory with no session file",
			dir:  func(t *testing.T) string { return t.TempDir() },
		},
		{
			Name: "a session with no stashed file",
			dir: func(t *testing.T) string {
				dir := t.TempDir()
				if err := session.Save(dir, session.Session{Files: []session.FileState{{Path: "/ws/a.txt"}}}); err != nil {
					t.Fatal(err)
				}
				return dir
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{SessionDir: tt.dir(t)})
			got, err := e.InterruptedSaves()
			if err != nil || len(got) != 0 {
				t.Fatalf("InterruptedSaves = %v, %v; want nothing and no error", got, err)
			}
		})
	}
}

func TestInterruptedSavesOfAnUnreadableSessionFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(`{"version": 99, "files": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{SessionDir: dir}).InterruptedSaves(); err == nil {
		t.Fatal("InterruptedSaves of a session with another version returned no error")
	}
}
