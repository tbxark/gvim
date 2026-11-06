package app

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/session"
	"github.com/tbxark/gvim/internal/ui"
)

// TestInterruptedListsTheStashedFilesOfTheSession checks that the review reads the
// saved session, and lists the files with a stash in the order the session keeps them.
// A file with no stash is left out, and an untitled buffer has an empty path.
func TestInterruptedListsTheStashedFilesOfTheSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fileStash, err := session.StashContent(dir, "unsaved edit")
	if err != nil {
		t.Fatal(err)
	}
	untitledStash, err := session.StashContent(dir, "scratch")
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Save(dir, session.Session{Files: []session.FileState{
		{Path: "/ws/a.txt", StashFile: fileStash},
		{Path: "/ws/clean.txt"},
		{StashFile: untitledStash},
	}}); err != nil {
		t.Fatal(err)
	}

	got, err := sessionStashes{dir: dir}.Interrupted()
	if err != nil {
		t.Fatalf("Interrupted: %v", err)
	}
	want := []ui.Interrupted{
		{Path: "/ws/a.txt", Stash: filepath.Join(dir, fileStash)},
		{Path: "", Stash: filepath.Join(dir, untitledStash)},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Interrupted = %v, want %v", got, want)
	}
}

// TestInterruptedWithNoSessionIsEmpty checks that a directory with no saved session has
// nothing to review, and no error.
func TestInterruptedWithNoSessionIsEmpty(t *testing.T) {
	t.Parallel()
	got, err := sessionStashes{dir: t.TempDir()}.Interrupted()
	if err != nil || len(got) != 0 {
		t.Fatalf("Interrupted = %v, %v; want nothing and no error", got, err)
	}
}

// TestStashesNeedASessionDirectory checks that the review has a port only when there is
// a session directory to read.
func TestStashesNeedASessionDirectory(t *testing.T) {
	t.Parallel()
	if stashesFor("") != nil {
		t.Fatal("stashesFor(\"\") is not nil, so the review would read the working directory")
	}
	if stashesFor(t.TempDir()) == nil {
		t.Fatal("stashesFor(dir) is nil, so the review has no port")
	}
}
