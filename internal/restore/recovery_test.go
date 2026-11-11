package restore

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

func TestRecoveryWriterStashesOnlyUnsavedTextOnce(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	apptest.Must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	apptest.Must(t, os.WriteFile(b, []byte("two\n"), 0o644))
	rec := filepath.Join(dir, "recovery")

	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(a))
	apptest.Must(t, ed.OpenFile(b))
	w := NewRecoveryWriter(ed, rec)

	// Nothing is modified, so nothing is written, not even the directory.
	apptest.Must(t, w.Plan()())
	if _, err := os.Stat(rec); err == nil {
		t.Fatal("recovery directory created with nothing to recover")
	}

	// Edit b.txt.
	ed.Do("insert_char", map[string]any{"text": "Z"})
	apptest.Must(t, w.Plan()())
	s, ok, err := session.Load(rec)
	apptest.Must(t, err)
	if !ok || len(s.Files) != 1 || s.Files[0].Path != b || s.Files[0].StashFile == "" {
		t.Fatalf("recovery session = %+v (found %v), want b.txt with a stash", s, ok)
	}
	if text, err := session.ReadStash(rec, s.Files[0].StashFile); err != nil || text != "Ztwo\n" {
		t.Fatalf("stash = %q, %v, want the edited text", text, err)
	}
	first := s.Files[0].StashFile

	// A second plan with no edit reuses the stash.
	apptest.Must(t, w.Plan()())
	s, _, _ = session.Load(rec)
	if len(s.Files) != 1 || s.Files[0].StashFile != first || len(apptest.StashFiles(t, rec)) != 1 {
		t.Fatalf("an unchanged document was stashed again: %+v, files %v", s.Files, apptest.StashFiles(t, rec))
	}

	// A new edit replaces it, and the old stash goes.
	ed.Do("insert_char", map[string]any{"text": "Y"})
	apptest.Must(t, w.Plan()())
	s, _, _ = session.Load(rec)
	if s.Files[0].StashFile == first || len(apptest.StashFiles(t, rec)) != 1 {
		t.Fatalf("stash after a new edit = %q (old %q), files %v", s.Files[0].StashFile, first, apptest.StashFiles(t, rec))
	}

	// Saving the document leaves nothing to recover.
	apptest.Must(t, ed.Save())
	apptest.Must(t, w.Plan()())
	s, _, _ = session.Load(rec)
	if len(s.Files) != 0 || len(apptest.StashFiles(t, rec)) != 0 {
		t.Fatalf("recovery after the save = %+v, stashes %v, want none", s.Files, apptest.StashFiles(t, rec))
	}
}

func TestRecoveryKeepsUntitledTextAndCursors(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "recovery")
	ed := apptest.NewSessionEditor()
	ed.Do("new", nil)
	ed.Do("insert_char", map[string]any{"text": "notes"})
	apptest.Must(t, NewRecoveryWriter(ed, rec).Plan()())

	fresh := apptest.NewSessionEditor()
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
	apptest.Must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	apptest.Must(t, os.WriteFile(b, []byte("two\n"), 0o644))
	rec := filepath.Join(dir, "recovery")

	crashed := apptest.NewSessionEditor()
	apptest.Must(t, crashed.OpenFile(a))
	apptest.Must(t, crashed.OpenFile(b))
	crashed.Do("insert_char", map[string]any{"text": "Z"})
	apptest.Must(t, NewRecoveryWriter(crashed, rec).Plan()())

	// The next start opens a.txt from the command line, and recovery adds b.txt.
	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(a))
	failed, n := recoverCrash(ed, rec)
	if len(failed) != 0 || n != 1 {
		t.Fatalf("recoverCrash = %d recovered, failed %v, want 1 and none", n, failed)
	}
	names, active := apptest.TabNames(ed)
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
	if _, n := recoverCrash(apptest.NewSessionEditor(), rec); n != 0 {
		t.Fatalf("second recoverCrash recovered %d files, want 0", n)
	}
	if left := apptest.StashFiles(t, rec); len(left) != 0 {
		t.Fatalf("stash files left after recovery: %v", left)
	}
}

func TestRecoverCrashKeepsWhatItCannotRestore(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "recovery")
	name, err := session.StashContent(rec, "lost\n")
	apptest.Must(t, err)
	// The stash names a directory in place of a file, which cannot be opened.
	apptest.Must(t, session.Save(rec, session.Session{Files: []session.FileState{{Path: dir, StashFile: name}}}))

	failed, n := recoverCrash(apptest.NewSessionEditor(), rec)
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
	apptest.Must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(a))
	ed.Do("insert_char", map[string]any{"text": "Z"})
	w := NewRecoveryWriter(ed, rec)
	apptest.Must(t, w.Plan()())
	apptest.Must(t, ClearRecovery(rec))
	if left := apptest.StashFiles(t, rec); len(left) != 0 {
		t.Fatalf("stash files after a clean exit: %v", left)
	}
	if _, n := recoverCrash(apptest.NewSessionEditor(), rec); n != 0 {
		t.Fatalf("recovered %d files after a clean exit", n)
	}
}

func TestRecoveryWriterDoesNotReuseAStashAfterReloadWithEncoding(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	apptest.Must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	rec := filepath.Join(dir, "recovery")

	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(a))
	w := NewRecoveryWriter(ed, rec)

	ed.Do("insert_char", map[string]any{"text": "A"})
	apptest.Must(t, w.Plan()())
	if r := ed.Do("reload_with_encoding", map[string]any{"name": "utf-8", "force": true}); r.Err != nil {
		t.Fatal(r.Err)
	}
	ed.Do("insert_char", map[string]any{"text": "B"})
	apptest.Must(t, w.Plan()())

	s, ok, err := session.Load(rec)
	apptest.Must(t, err)
	if !ok || len(s.Files) != 1 {
		t.Fatalf("recovery session = %+v (found %v), want one file", s, ok)
	}
	want := ed.Active().Buffer.Text()
	if text, err := session.ReadStash(rec, s.Files[0].StashFile); err != nil || text != want {
		t.Fatalf("stash = %q, %v, want the current text %q", text, err, want)
	}
}

func writeCrashCopy(t *testing.T, path, text, dir string) {
	t.Helper()
	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(path))
	ed.Do("insert_char", map[string]any{"text": text})
	apptest.Must(t, NewRecoveryWriter(ed, dir).Plan()())
}

func TestRecoverAbandonedLeavesRunningProcessesAlone(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	apptest.Must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	apptest.Must(t, os.WriteFile(b, []byte("two\n"), 0o644))
	root := filepath.Join(dir, "recovery")
	live := filepath.Join(root, "111")
	gone := filepath.Join(root, "222")
	writeCrashCopy(t, a, "L", live)
	writeCrashCopy(t, b, "G", gone)

	ed := apptest.NewSessionEditor()
	failed, n := recoverAbandoned(ed, root, func(pid int) bool { return pid == 111 }, nil)
	if len(failed) != 0 || n != 1 {
		t.Fatalf("recoverAbandoned = %d recovered, failed %v, want 1 and none", n, failed)
	}
	if d := ed.Active(); d == nil || d.Path != b || d.Buffer.Text() != "Gtwo\n" {
		t.Fatalf("recovered document = %+v, want b.txt with the text of the run that is gone", d)
	}
	if s, ok, err := session.Load(live); err != nil || !ok || len(s.Files) != 1 || len(apptest.StashFiles(t, live)) != 1 {
		t.Fatalf("the copy of the running process = %+v (found %v, err %v), want it kept", s, ok, err)
	}
	if _, err := os.Stat(gone); err == nil {
		t.Fatal("the directory of the run that is gone is left behind")
	}
}

func TestRecoverAbandonedTakesTheLeftoverOfOwnPid(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	apptest.Must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	root := filepath.Join(dir, "recovery")
	writeCrashCopy(t, a, "X", OwnRecoveryDir(root))

	_, n := recoverAbandoned(apptest.NewSessionEditor(), root, func(int) bool { return true }, nil)
	if n != 1 {
		t.Fatalf("recovered %d files, want 1", n)
	}
}

func TestRecoverAbandonedKeepsWhatItCannotRestore(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "recovery")
	gone := filepath.Join(root, "222")
	name, err := session.StashContent(gone, "lost\n")
	apptest.Must(t, err)
	apptest.Must(t, session.Save(gone, session.Session{Files: []session.FileState{{Path: dir, StashFile: name}}}))

	failed, n := recoverAbandoned(apptest.NewSessionEditor(), root, func(int) bool { return false }, nil)
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
	apptest.Must(t, cmd.Run())
	if processAlive(cmd.Process.Pid) {
		t.Fatal("processAlive(pid of an exited process) = true")
	}
}

func TestRecoverCrashDoesNotOverwriteAFileChangedSinceTheCopy(t *testing.T) {
	tests := []struct {
		name     string
		change   func(path string)
		wantSave bool
	}{
		{"file changed on disk", func(path string) {
			apptest.Must(t, os.WriteFile(path, []byte("one\ntwo\nTEAMMATE\n"), 0o644))
		}, false},
		{"file untouched", func(string) {}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			a := filepath.Join(dir, "a.txt")
			apptest.Must(t, os.WriteFile(a, []byte("one\ntwo\n"), 0o644))
			rec := filepath.Join(dir, "recovery")
			writeCrashCopy(t, a, "MINE", rec)
			tt.change(a)

			ed := apptest.NewSessionEditor()
			if failed, n := recoverCrash(ed, rec); len(failed) != 0 || n != 1 {
				t.Fatalf("recoverCrash = %d recovered, failed %v", n, failed)
			}
			if got := ed.Active().Buffer.Text(); got != "MINEone\ntwo\n" {
				t.Fatalf("recovered text = %q", got)
			}
			err := ed.Save()
			if tt.wantSave {
				apptest.Must(t, err)
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

func TestRestoreSessionKeepsTheBaselineOfStashedText(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	apptest.Must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(a))
	ed.Do("insert_char", map[string]any{"text": "Z"})
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{}))
	apptest.Must(t, os.WriteFile(a, []byte("one\nnew\n"), 0o644))

	s, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("no session saved")
	}
	fresh := apptest.NewSessionEditor()
	if failed := RestoreSession(fresh, sessDir, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %v", failed)
	}
	if err := fresh.Save(); !errors.Is(err, editor.ErrChangedOnDisk) {
		t.Fatalf("Save = %v, want ErrChangedOnDisk", err)
	}
}

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
			apptest.Must(t, os.WriteFile(b, []byte("two\n"), 0o644))
			root := filepath.Join(dir, "recovery")
			gone := filepath.Join(root, "222")
			writeCrashCopy(t, b, "G", gone)

			_, n := recoverAbandoned(apptest.NewSessionEditor(), root, func(int) bool { return false }, tt.protect)
			if n != 1 {
				t.Fatalf("recovered %d files, want 1", n)
			}
			s, ok, _ := session.Load(gone)
			if left := ok && len(s.Files) == 1 && len(apptest.StashFiles(t, gone)) == 1; left != tt.wantOldLeft {
				t.Fatalf("old copy left = %v, want %v", left, tt.wantOldLeft)
			}
		})
	}
}

// recentPaths maps the editor's MRU order to tab paths, dropping virtual/empty tabs.
func recentPaths(ed *editor.Editor) []string {
	tabs := ed.Tabs()
	var out []string
	for _, i := range ed.RecentTabIndices() {
		if i < 0 || i >= len(tabs) {
			continue
		}
		name := tabs[i].Path
		if name == "" {
			name = tabs[i].Name
		}
		out = append(out, name)
	}
	return out
}

// TestRecoveryWriterSavesAndRestoresMRU: the crash-recovery copy carries the editor's
// most-recently-used order (and active tab), so completion/dabbrev order after an
// unclean exit is the order from before the crash, not tab order.
func TestRecoveryWriterSavesAndRestoresMRU(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	for _, p := range []string{a, b, c} {
		apptest.Must(t, os.WriteFile(p, []byte(filepath.Base(p)+"\n"), 0o644))
	}
	rec := filepath.Join(dir, "recovery")
	crashed := apptest.NewSessionEditor()
	for _, p := range []string{a, b, c} {
		apptest.Must(t, crashed.OpenFile(p))
	}
	// Opening a, b, c activates c; then a and b make the MRU b, a, c with b active.
	crashed.ActivateTab(0)
	crashed.ActivateTab(1)
	for i := range 3 {
		crashed.ActivateTab(i)
		crashed.Do("insert_char", map[string]any{"text": "X"})
	}
	// Leave the MRU b, a, c with b active.
	crashed.ActivateTab(0)
	crashed.ActivateTab(1)
	if got, want := recentPaths(crashed), []string{b, a, c}; !slices.Equal(got, want) {
		t.Fatalf("source MRU = %v, want %v", got, want)
	}
	apptest.Must(t, NewRecoveryWriter(crashed, rec).Plan()())

	s, ok, err := session.Load(rec)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("no recovery session was saved")
	}
	wantMru := []session.MruFile{{Path: b}, {Path: a}, {Path: c}}
	if !slices.Equal(s.MRU, wantMru) {
		t.Fatalf("recovery MRU = %+v, want %+v", s.MRU, wantMru)
	}
	if s.Active != 1 {
		t.Fatalf("recovery active = %d, want 1 (b.txt)", s.Active)
	}

	fresh := apptest.NewSessionEditor()
	failed, n := recoverCrash(fresh, rec)
	if len(failed) != 0 || n != 3 {
		t.Fatalf("recoverCrash = %d recovered, failed %v, want 3 and none", n, failed)
	}
	if got, want := recentPaths(fresh), []string{b, a, c}; !slices.Equal(got, want) {
		t.Fatalf("restored MRU = %v, want %v", got, want)
	}
	if d := fresh.Active(); d == nil || d.Path != b {
		t.Fatalf("active document = %+v, want b.txt", d)
	}
}

// TestRecoveryMRUKeepsUntitledLabel: a pathless buffer is named by its label in the
// recovery copy's MRU, so recovery can put it back at the right rank next to a file.
func TestRecoveryMRUKeepsUntitledLabel(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	apptest.Must(t, os.WriteFile(a, []byte("a\n"), 0o644))
	rec := filepath.Join(dir, "recovery")
	crashed := apptest.NewSessionEditor()
	apptest.Must(t, crashed.OpenFile(a))
	crashed.ActivateTab(0)
	crashed.Do("new", nil)
	crashed.Active().SetLabel("scratch")
	crashed.Do("insert_char", map[string]any{"text": "notes"})
	crashed.ActivateTab(0)
	crashed.Do("insert_char", map[string]any{"text": "A"})
	crashed.ActivateTab(1)
	if got, want := recentPaths(crashed), []string{"scratch", a}; !slices.Equal(got, want) {
		t.Fatalf("source MRU = %v, want %v", got, want)
	}
	apptest.Must(t, NewRecoveryWriter(crashed, rec).Plan()())

	s, ok, err := session.Load(rec)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("no recovery session was saved")
	}
	wantMru := []session.MruFile{{Label: "scratch"}, {Path: a}}
	if !slices.Equal(s.MRU, wantMru) {
		t.Fatalf("recovery MRU = %+v, want %+v", s.MRU, wantMru)
	}
	fresh := apptest.NewSessionEditor()
	failed, n := recoverCrash(fresh, rec)
	if len(failed) != 0 || n != 2 {
		t.Fatalf("recoverCrash = %d recovered, failed %v", n, failed)
	}
	if d := fresh.Active(); d == nil || d.Path != "" || d.Label() != "scratch" {
		t.Fatalf("active document = %+v, want the labelled untitled buffer", d)
	}
	if got, want := recentPaths(fresh), []string{"scratch", a}; !slices.Equal(got, want) {
		t.Fatalf("restored MRU = %v, want %v", got, want)
	}
}

// TestRecoverySessionWithoutMRUStillRestores: a crash-recovery copy written by an older
// build (no active/MRU fields) restores as before, keeping the activation-derived order
// instead of inventing references.
func TestRecoverySessionWithoutMRUStillRestores(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	apptest.Must(t, os.WriteFile(a, []byte("a\n"), 0o644))
	apptest.Must(t, os.WriteFile(b, []byte("b\n"), 0o644))
	rec := filepath.Join(dir, "recovery")
	crashed := apptest.NewSessionEditor()
	apptest.Must(t, crashed.OpenFile(a))
	apptest.Must(t, crashed.OpenFile(b))
	crashed.ActivateTab(0)
	crashed.Do("insert_char", map[string]any{"text": "X"})
	crashed.ActivateTab(1)
	crashed.Do("insert_char", map[string]any{"text": "X"})
	apptest.Must(t, NewRecoveryWriter(crashed, rec).Plan()())

	old, ok, err := session.Load(rec)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("no recovery session")
	}
	old.MRU, old.Active = nil, 0
	apptest.Must(t, session.Save(rec, old))

	fresh := apptest.NewSessionEditor()
	failed, n := recoverCrash(fresh, rec)
	if len(failed) != 0 || n != 2 {
		t.Fatalf("recoverCrash = %d recovered, failed %v", n, failed)
	}
	names, _ := apptest.TabNames(fresh)
	if !slices.Equal(names, []string{"a.txt", "b.txt"}) {
		t.Fatalf("restored tabs = %v, want the saved file order", names)
	}
}

// TestRecoverAbandonedLegacyFailureKeepsLiveCopies checks a legacy copy in the root that
// cannot be restored is moved aside without taking the per-process copies with it.
func TestRecoverAbandonedLegacyFailureKeepsLiveCopies(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "recovery")
	name, err := session.StashContent(root, "lost\n")
	apptest.Must(t, err)
	apptest.Must(t, session.Save(root, session.Session{Files: []session.FileState{{Path: dir, StashFile: name}}}))
	a := filepath.Join(dir, "a.txt")
	apptest.Must(t, os.WriteFile(a, []byte("one\n"), 0o644))
	live := filepath.Join(root, "111")
	writeCrashCopy(t, a, "X", live)

	failed, _ := recoverAbandoned(apptest.NewSessionEditor(), root, func(pid int) bool { return pid == 111 }, nil)
	if len(failed) != 1 {
		t.Fatalf("failed = %v, want the legacy file", failed)
	}
	if _, err := os.Stat(filepath.Join(root+".failed", name)); err != nil {
		t.Fatalf("the legacy stash was not kept aside: %v", err)
	}
	if s, ok, err := session.Load(live); err != nil || !ok || len(s.Files) != 1 {
		t.Fatalf("the running process's copy = %+v (found %v, err %v), want it in place", s, ok, err)
	}
}
