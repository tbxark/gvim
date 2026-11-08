package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/session"
)

func TestBuildRuntimeRecoversAfterACrashAndExitClearsTheCopy(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	must(t, os.WriteFile(b, []byte("two\n"), 0o644))
	sessDir := filepath.Join(dir, "session")

	// The run that crashed left its copy behind.
	crashed := apptest.NewSessionEditor()
	must(t, crashed.OpenFile(b))
	crashed.Do("insert_char", map[string]any{"text": "Z"})
	must(t, restore.NewRecoveryWriter(crashed, restore.RecoveryPath(sessDir)).Plan()())

	opts := projectRoot(t, "")
	opts.SessionDir = sessDir
	opts.Files = []FileTarget{{Path: a}}
	rt, err := buildRuntime(t.Context(), opts, true)
	must(t, err)
	t.Cleanup(rt.close)

	names, active := apptest.TabNames(rt.ed)
	if !slices.Equal(names, []string{"a.txt", "b.txt"}) || active != 0 {
		t.Fatalf("tabs %v, active %d, want a.txt and the recovered b.txt, with a.txt active", names, active)
	}
	rt.ed.ActivateTab(1)
	if got := rt.ed.Active().Buffer.Text(); got != "Ztwo\n" {
		t.Fatalf("recovered text = %q, want the unsaved text", got)
	}
	if want := restore.OwnRecoveryDir(restore.RecoveryPath(sessDir)); rt.recovery != want {
		t.Fatalf("runtime recovery dir = %q, want %q", rt.recovery, want)
	}

	rt.exit()
	if left := apptest.StashFiles(t, restore.RecoveryPath(sessDir)); len(left) != 0 {
		t.Fatalf("recovery stashes left after a clean exit: %v", left)
	}
	if _, err := os.Stat(rt.recovery); err == nil {
		t.Fatal("the directory of the run is left behind after a clean exit")
	}
}

func TestInterruptedExitKeepsTheRecoveryCopyWhenNoSessionIsSaved(t *testing.T) {
	tests := []struct {
		name        string
		interrupted bool
		wantKept    bool
	}{
		{"ended by a signal", true, true},
		{"quit by the user", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateConfig(t)
			dir := t.TempDir()
			a := filepath.Join(dir, "a.txt")
			must(t, os.WriteFile(a, []byte("one\n"), 0o644))
			sessDir := filepath.Join(dir, "session")

			opts := projectRoot(t, "")
			opts.SessionDir = sessDir
			opts.Files = []FileTarget{{Path: a}}
			rt, err := buildRuntime(t.Context(), opts, true)
			must(t, err)
			t.Cleanup(rt.close)
			rt.ed.Do("insert_char", map[string]any{"text": "Z"})
			rt.interrupted = tt.interrupted
			rt.exit()

			s, ok, err := session.Load(rt.recovery)
			must(t, err)
			if kept := ok && len(s.Files) == 1; kept != tt.wantKept {
				t.Fatalf("recovery copy kept = %v (%+v), want %v", kept, s, tt.wantKept)
			}
			if tt.wantKept {
				if text, err := session.ReadStash(rt.recovery, s.Files[0].StashFile); err != nil || text != "Zone\n" {
					t.Fatalf("stash = %q, %v, want the unsaved text", text, err)
				}
			}
		})
	}
}

func TestRecoveredTextIsCopiedBeforeTheOldCopyGoes(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	b := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(b, []byte("two\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	writeCrashCopy(t, b, "Z", filepath.Join(restore.RecoveryPath(sessDir), "222"))

	opts := projectRoot(t, "")
	opts.SessionDir = sessDir
	rt, err := buildRuntime(t.Context(), opts, true)
	must(t, err)
	t.Cleanup(rt.close)

	// No tick has run. The copy of this process must hold the text already.
	s, ok, err := session.Load(rt.recovery)
	must(t, err)
	if !ok || len(s.Files) != 1 || s.Files[0].Path != b {
		t.Fatalf("own recovery copy = %+v (found %v), want b.txt", s, ok)
	}
	if text, err := session.ReadStash(rt.recovery, s.Files[0].StashFile); err != nil || text != "Ztwo\n" {
		t.Fatalf("own stash = %q, %v, want the recovered text", text, err)
	}
}

// writeCrashCopy leaves the unsaved text of a document of path, with the edit text typed at
// its start, in dir, as a run that crashed would.
func writeCrashCopy(t *testing.T, path, text, dir string) {
	t.Helper()
	ed := apptest.NewSessionEditor()
	must(t, ed.OpenFile(path))
	ed.Do("insert_char", map[string]any{"text": text})
	must(t, restore.NewRecoveryWriter(ed, dir).Plan()())
}
