package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/session"
	"github.com/tbxark/gvim/internal/ui"
)

// stashFiles lists the stash files in dir.
func stashFiles(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "stash-*.txt"))
	must(t, err)
	return names
}

func TestRecoveryWriterStashesOnlyUnsavedTextOnce(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	must(t, os.WriteFile(b, []byte("two\n"), 0o644))
	rec := filepath.Join(dir, "recovery")

	ed := newSessionEditor()
	must(t, ed.OpenFile(a))
	must(t, ed.OpenFile(b))
	w := newRecoveryWriter(ed, rec)

	// Nothing is modified, so nothing is written, not even the directory.
	must(t, w.Plan()())
	if _, err := os.Stat(rec); err == nil {
		t.Fatal("recovery directory created with nothing to recover")
	}

	// Edit b.txt.
	ed.Do("insert_char", map[string]any{"text": "Z"})
	must(t, w.Plan()())
	s, ok, err := session.Load(rec)
	must(t, err)
	if !ok || len(s.Files) != 1 || s.Files[0].Path != b || s.Files[0].StashFile == "" {
		t.Fatalf("recovery session = %+v (found %v), want b.txt with a stash", s, ok)
	}
	if text, err := session.ReadStash(rec, s.Files[0].StashFile); err != nil || text != "Ztwo\n" {
		t.Fatalf("stash = %q, %v, want the edited text", text, err)
	}
	first := s.Files[0].StashFile

	// A second plan with no edit reuses the stash.
	must(t, w.Plan()())
	s, _, _ = session.Load(rec)
	if len(s.Files) != 1 || s.Files[0].StashFile != first || len(stashFiles(t, rec)) != 1 {
		t.Fatalf("an unchanged document was stashed again: %+v, files %v", s.Files, stashFiles(t, rec))
	}

	// A new edit replaces it, and the old stash goes.
	ed.Do("insert_char", map[string]any{"text": "Y"})
	must(t, w.Plan()())
	s, _, _ = session.Load(rec)
	if s.Files[0].StashFile == first || len(stashFiles(t, rec)) != 1 {
		t.Fatalf("stash after a new edit = %q (old %q), files %v", s.Files[0].StashFile, first, stashFiles(t, rec))
	}

	// Saving the document leaves nothing to recover.
	must(t, ed.Save())
	must(t, w.Plan()())
	s, _, _ = session.Load(rec)
	if len(s.Files) != 0 || len(stashFiles(t, rec)) != 0 {
		t.Fatalf("recovery after the save = %+v, stashes %v, want none", s.Files, stashFiles(t, rec))
	}
}

func TestRecoveryKeepsUntitledTextAndCursors(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "recovery")
	ed := newSessionEditor()
	ed.Do("new", nil)
	ed.Do("insert_char", map[string]any{"text": "notes"})
	must(t, newRecoveryWriter(ed, rec).Plan()())

	fresh := newSessionEditor()
	failed, n := recoverCrash(fresh, rec)
	if len(failed) != 0 || n != 1 {
		t.Fatalf("recoverCrash = %d recovered, failed %v, want 1 and none", n, failed)
	}
	if d := fresh.Active(); d == nil || d.Path != "" || d.Buffer.Text() != "notes" || !d.Modified() {
		t.Fatalf("recovered document = %+v, want an untitled buffer with the text", d)
	}
}

func TestRecoverCrashRestoresUnsavedEditsOverTheOpenFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	must(t, os.WriteFile(b, []byte("two\n"), 0o644))
	rec := filepath.Join(dir, "recovery")

	crashed := newSessionEditor()
	must(t, crashed.OpenFile(a))
	must(t, crashed.OpenFile(b))
	crashed.Do("insert_char", map[string]any{"text": "Z"})
	must(t, newRecoveryWriter(crashed, rec).Plan()())

	// The next start opens a.txt from the command line, and recovery adds b.txt.
	ed := newSessionEditor()
	must(t, ed.OpenFile(a))
	failed, n := recoverCrash(ed, rec)
	if len(failed) != 0 || n != 1 {
		t.Fatalf("recoverCrash = %d recovered, failed %v, want 1 and none", n, failed)
	}
	names, active := tabNames(ed)
	if !slices.Equal(names, []string{"a.txt", "b.txt"}) || active != 0 {
		t.Fatalf("tabs %v, active %d, want a.txt, b.txt and the first tab still active", names, active)
	}
	ed.ActivateTab(1)
	if d := ed.Active(); d.Buffer.Text() != "Ztwo\n" || !d.Modified() {
		t.Fatalf("recovered b.txt = %q (modified %v), want the unsaved text", d.Buffer.Text(), d.Modified())
	}
	if got, _ := os.ReadFile(b); string(got) != "two\n" {
		t.Fatalf("recovery wrote the file: %q", got)
	}
	// The recovery state is spent: a second start finds nothing.
	if _, n := recoverCrash(newSessionEditor(), rec); n != 0 {
		t.Fatalf("second recoverCrash recovered %d files, want 0", n)
	}
	if left := stashFiles(t, rec); len(left) != 0 {
		t.Fatalf("stash files left after recovery: %v", left)
	}
}

func TestRecoverCrashKeepsWhatItCannotRestore(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "recovery")
	name, err := session.StashContent(rec, "lost\n")
	must(t, err)
	// The stash names a directory in place of a file, which cannot be opened.
	must(t, session.Save(rec, session.Session{Files: []session.FileState{{Path: dir, StashFile: name}}}))

	failed, n := recoverCrash(newSessionEditor(), rec)
	if n != 0 || len(failed) != 1 {
		t.Fatalf("recoverCrash = %d recovered, failed %v, want 0 and one failure", n, failed)
	}
	if _, err := os.Stat(filepath.Join(rec+".failed", name)); err != nil {
		t.Fatalf("the stash of the file that failed was not kept: %v", err)
	}
}

func TestClearRecoveryRemovesTheStashes(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "recovery")
	a := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	ed := newSessionEditor()
	must(t, ed.OpenFile(a))
	ed.Do("insert_char", map[string]any{"text": "Z"})
	w := newRecoveryWriter(ed, rec)
	must(t, w.Plan()())
	must(t, clearRecovery(rec))
	if left := stashFiles(t, rec); len(left) != 0 {
		t.Fatalf("stash files after a clean exit: %v", left)
	}
	if _, n := recoverCrash(newSessionEditor(), rec); n != 0 {
		t.Fatalf("recovered %d files after a clean exit", n)
	}
}

func TestBuildRuntimeRecoversAfterACrashAndExitClearsTheCopy(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	must(t, os.WriteFile(b, []byte("two\n"), 0o644))
	sessDir := filepath.Join(dir, "session")

	// The run that crashed left its copy behind.
	crashed := newSessionEditor()
	must(t, crashed.OpenFile(b))
	crashed.Do("insert_char", map[string]any{"text": "Z"})
	must(t, newRecoveryWriter(crashed, recoveryDir(sessDir)).Plan()())

	opts := projectRoot(t, "")
	opts.SessionDir = sessDir
	opts.Files = []FileTarget{{Path: a}}
	rt, err := buildRuntime(t.Context(), opts, true)
	must(t, err)
	t.Cleanup(rt.close)

	names, active := tabNames(rt.ed)
	if !slices.Equal(names, []string{"a.txt", "b.txt"}) || active != 0 {
		t.Fatalf("tabs %v, active %d, want a.txt and the recovered b.txt, with a.txt active", names, active)
	}
	rt.ed.ActivateTab(1)
	if got := rt.ed.Active().Buffer.Text(); got != "Ztwo\n" {
		t.Fatalf("recovered text = %q, want the unsaved text", got)
	}
	if want := ownRecoveryDir(recoveryDir(sessDir)); rt.recovery != want {
		t.Fatalf("runtime recovery dir = %q, want %q", rt.recovery, want)
	}

	rt.exit()
	if left := stashFiles(t, recoveryDir(sessDir)); len(left) != 0 {
		t.Fatalf("recovery stashes left after a clean exit: %v", left)
	}
	if _, err := os.Stat(rt.recovery); err == nil {
		t.Fatal("the directory of the run is left behind after a clean exit")
	}
}

// TestRecoveryWriterDoesNotReuseAStashAfterReloadWithEncoding checks that a reload, which
// gives the document a new buffer whose version starts again at 0, does not make a later
// edit with the same version number look like the text that was stashed before the reload.
func TestRecoveryWriterDoesNotReuseAStashAfterReloadWithEncoding(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	rec := filepath.Join(dir, "recovery")

	ed := newSessionEditor()
	must(t, ed.OpenFile(a))
	w := newRecoveryWriter(ed, rec)

	ed.Do("insert_char", map[string]any{"text": "A"})
	must(t, w.Plan()())
	if r := ed.Do("reload_with_encoding", map[string]any{"name": "utf-8", "force": true}); r.Err != nil {
		t.Fatal(r.Err)
	}
	ed.Do("insert_char", map[string]any{"text": "B"})
	must(t, w.Plan()())

	s, ok, err := session.Load(rec)
	must(t, err)
	if !ok || len(s.Files) != 1 {
		t.Fatalf("recovery session = %+v (found %v), want one file", s, ok)
	}
	want := ed.Active().Buffer.Text()
	if text, err := session.ReadStash(rec, s.Files[0].StashFile); err != nil || text != want {
		t.Fatalf("stash = %q, %v, want the current text %q", text, err, want)
	}
}

// writeCrashCopy leaves the unsaved text of a document of path, with the edit text typed at
// its start, in dir, as a run that crashed would.
func writeCrashCopy(t *testing.T, path, text, dir string) {
	t.Helper()
	ed := newSessionEditor()
	must(t, ed.OpenFile(path))
	ed.Do("insert_char", map[string]any{"text": text})
	must(t, newRecoveryWriter(ed, dir).Plan()())
}

// TestRecoverAbandonedLeavesRunningProcessesAlone checks that each run keeps its copy in a
// directory of its own, and that a start recovers the copies of the runs that are gone and
// not the copy of a run that is still editing.
func TestRecoverAbandonedLeavesRunningProcessesAlone(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	must(t, os.WriteFile(b, []byte("two\n"), 0o644))
	root := filepath.Join(dir, "recovery")
	live := filepath.Join(root, "111")
	gone := filepath.Join(root, "222")
	writeCrashCopy(t, a, "L", live)
	writeCrashCopy(t, b, "G", gone)

	ed := newSessionEditor()
	failed, n := recoverAbandoned(ed, root, func(pid int) bool { return pid == 111 }, nil)
	if len(failed) != 0 || n != 1 {
		t.Fatalf("recoverAbandoned = %d recovered, failed %v, want 1 and none", n, failed)
	}
	if d := ed.Active(); d == nil || d.Path != b || d.Buffer.Text() != "Gtwo\n" {
		t.Fatalf("recovered document = %+v, want b.txt with the text of the run that is gone", d)
	}
	if s, ok, err := session.Load(live); err != nil || !ok || len(s.Files) != 1 || len(stashFiles(t, live)) != 1 {
		t.Fatalf("the copy of the running process = %+v (found %v, err %v), want it kept", s, ok, err)
	}
	if _, err := os.Stat(gone); err == nil {
		t.Fatal("the directory of the run that is gone is left behind")
	}
}

// TestRecoverAbandonedTakesTheLeftoverOfOwnPid checks that a directory named for this
// process is recovered whatever liveness says: the pid is ours again after a crash.
func TestRecoverAbandonedTakesTheLeftoverOfOwnPid(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	root := filepath.Join(dir, "recovery")
	writeCrashCopy(t, a, "X", ownRecoveryDir(root))

	_, n := recoverAbandoned(newSessionEditor(), root, func(int) bool { return true }, nil)
	if n != 1 {
		t.Fatalf("recovered %d files, want 1", n)
	}
}

// TestRecoverAbandonedKeepsWhatItCannotRestore checks that the stash of a file that cannot
// be restored is kept beside its directory.
func TestRecoverAbandonedKeepsWhatItCannotRestore(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "recovery")
	gone := filepath.Join(root, "222")
	name, err := session.StashContent(gone, "lost\n")
	must(t, err)
	must(t, session.Save(gone, session.Session{Files: []session.FileState{{Path: dir, StashFile: name}}}))

	failed, n := recoverAbandoned(newSessionEditor(), root, func(int) bool { return false }, nil)
	if n != 0 || len(failed) != 1 {
		t.Fatalf("recoverAbandoned = %d recovered, failed %v, want 0 and one failure", n, failed)
	}
	if _, err := os.Stat(filepath.Join(gone+".failed", name)); err != nil {
		t.Fatalf("the stash of the file that failed was not kept: %v", err)
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("processAlive(own pid) = false")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	must(t, cmd.Run())
	if processAlive(cmd.Process.Pid) {
		t.Fatal("processAlive(pid of an exited process) = true")
	}
}

// TestInterruptedExitKeepsTheRecoveryCopyWhenNoSessionIsSaved checks that a run ended by a
// signal, with files named on the command line so that no session is saved, leaves the
// unsaved text in its recovery copy for the next start, and that a quit the user chose
// still clears it.
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

// TestRecoverCrashDoesNotOverwriteAFileChangedSinceTheCopy checks that recovered text is
// based on the file as it was when the copy was taken: when the file changed on disk after
// the crash, a save refuses to overwrite it, as it would had the file changed during the
// edit.
func TestRecoverCrashDoesNotOverwriteAFileChangedSinceTheCopy(t *testing.T) {
	tests := []struct {
		name     string
		change   func(path string)
		wantSave bool
	}{
		{"file changed on disk", func(path string) {
			must(t, os.WriteFile(path, []byte("one\ntwo\nTEAMMATE\n"), 0o644))
		}, false},
		{"file untouched", func(string) {}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			a := filepath.Join(dir, "a.txt")
			must(t, os.WriteFile(a, []byte("one\ntwo\n"), 0o644))
			rec := filepath.Join(dir, "recovery")
			writeCrashCopy(t, a, "MINE", rec)
			tt.change(a)

			ed := newSessionEditor()
			if failed, n := recoverCrash(ed, rec); len(failed) != 0 || n != 1 {
				t.Fatalf("recoverCrash = %d recovered, failed %v", n, failed)
			}
			if got := ed.Active().Buffer.Text(); got != "MINEone\ntwo\n" {
				t.Fatalf("recovered text = %q", got)
			}
			err := ed.Save()
			if tt.wantSave {
				must(t, err)
				return
			}
			if !errors.Is(err, editor.ErrChangedOnDisk) {
				t.Fatalf("Save = %v, want ErrChangedOnDisk", err)
			}
			if got, _ := os.ReadFile(a); string(got) != "one\ntwo\nTEAMMATE\n" {
				t.Fatalf("file on disk = %q, want the newer content kept", got)
			}
		})
	}
}

// TestRestoreSessionKeepsTheBaselineOfStashedText is the same check for the stash that a
// saved session keeps.
func TestRestoreSessionKeepsTheBaselineOfStashedText(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	ed := newSessionEditor()
	must(t, ed.OpenFile(a))
	ed.Do("insert_char", map[string]any{"text": "Z"})
	must(t, saveSession(ed, sessDir, ui.ExplorerState{}))
	must(t, os.WriteFile(a, []byte("one\nnew\n"), 0o644))

	s, ok, err := session.Load(sessDir)
	must(t, err)
	if !ok {
		t.Fatal("no session saved")
	}
	fresh := newSessionEditor()
	if failed := restoreSession(fresh, sessDir, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %v", failed)
	}
	if err := fresh.Save(); !errors.Is(err, editor.ErrChangedOnDisk) {
		t.Fatalf("Save = %v, want ErrChangedOnDisk", err)
	}
}

// TestRecoveredTextIsCopiedBeforeTheOldCopyGoes checks that a start which restores the copy
// of a crashed run writes its own copy at once, so a second crash before the first timer
// tick does not lose the text.
func TestRecoveredTextIsCopiedBeforeTheOldCopyGoes(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	b := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(b, []byte("two\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	writeCrashCopy(t, b, "Z", filepath.Join(recoveryDir(sessDir), "222"))

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

// TestRecoverAbandonedKeepsTheOldCopyWhenTheNewOneCannotBeWritten checks that the old copy
// is removed only after the protect step worked.
func TestRecoverAbandonedKeepsTheOldCopyWhenTheNewOneCannotBeWritten(t *testing.T) {
	tests := []struct {
		name        string
		protect     func() error
		wantOldLeft bool
	}{
		{"protect fails", func() error { return errors.New("disk full") }, true},
		{"protect works", func() error { return nil }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			b := filepath.Join(dir, "b.txt")
			must(t, os.WriteFile(b, []byte("two\n"), 0o644))
			root := filepath.Join(dir, "recovery")
			gone := filepath.Join(root, "222")
			writeCrashCopy(t, b, "G", gone)

			_, n := recoverAbandoned(newSessionEditor(), root, func(int) bool { return false }, tt.protect)
			if n != 1 {
				t.Fatalf("recovered %d files, want 1", n)
			}
			s, ok, _ := session.Load(gone)
			if left := ok && len(s.Files) == 1 && len(stashFiles(t, gone)) == 1; left != tt.wantOldLeft {
				t.Fatalf("old copy left = %v, want %v", left, tt.wantOldLeft)
			}
		})
	}
}
