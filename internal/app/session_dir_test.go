package app

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/session"
)

// TestBuildRuntimeEditorReportsTheSessionDirectory checks that the editor buildRuntime builds
// knows the session directory, so the interrupted-saves list of the saved session is reachable
// from the running editor. The session is read where Options.SessionDir points.
func TestBuildRuntimeEditorReportsTheSessionDirectory(t *testing.T) {
	isolateConfig(t)
	sessDir := t.TempDir()
	stash, err := session.StashContent(sessDir, "unsaved edit")
	must(t, err)
	must(t, session.Save(sessDir, session.Session{Files: []session.FileState{
		{Path: "/ws/a.txt", StashFile: stash},
	}}))
	opts := projectRoot(t, "")
	opts.SessionDir = sessDir
	rt, err := buildRuntime(t.Context(), opts, false)
	must(t, err)
	t.Cleanup(rt.close)

	got, err := rt.ed.InterruptedSaves()
	must(t, err)
	want := []editor.StashEntry{{Path: "/ws/a.txt", Stash: filepath.Join(sessDir, stash)}}
	if !slices.Equal(got, want) {
		t.Fatalf("InterruptedSaves = %v, want %v", got, want)
	}
}
