package editor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openRevertable opens a file holding disk and returns the editor and the path.
func openRevertable(t *testing.T, disk string) (*Editor, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.txt")
	putFile(t, path, disk)
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	return e, path
}

func TestRevertReloadsFromDisk(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		edit         bool           // type "x" at the start after opening
		newDisk      string         // the file content after opening, if the file changes
		args         map[string]any // arguments of revert
		wantErr      error
		wantText     string
		wantModified bool
	}{
		{
			name:     "an unmodified buffer reloads the changed file",
			newDisk:  "b\n",
			wantText: "b\n",
		},
		{
			name:         "a modified buffer is refused without force and keeps its edit",
			edit:         true,
			newDisk:      "b\n",
			wantErr:      ErrUnsaved,
			wantText:     "xa\n",
			wantModified: true,
		},
		{
			name:     "a modified buffer reloads with force",
			edit:     true,
			args:     map[string]any{"force": true},
			wantText: "a\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openRevertable(t, "a\n")
			if tt.edit {
				typeText(t, e, "x")
			}
			if tt.newDisk != "" {
				putFile(t, path, tt.newDisk)
			}
			r := e.Do("revert", tt.args)
			if !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("revert err = %v, want %v", r.Err, tt.wantErr)
			}
			d := e.Active()
			if got := d.Buffer.Text(); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if d.Modified() != tt.wantModified {
				t.Fatalf("modified = %t, want %t", d.Modified(), tt.wantModified)
			}
		})
	}
}

func TestRevertIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e, _ := openRevertable(t, "a\n")
	typeText(t, e, "x")
	if r := e.Do("revert", map[string]any{"force": true}); r.Err != nil {
		t.Fatalf("revert: %v", r.Err)
	}
	run(t, e, "undo")
	if got := e.Active().Buffer.Text(); got != "xa\n" {
		t.Fatalf("after undo text = %q, want the edit back", got)
	}
}

func TestRevertWithoutPathFails(t *testing.T) {
	t.Parallel()
	e := pathDoc(Options{}, "", "text")
	if r := e.Do("revert", nil); !errors.Is(r.Err, ErrNoPath) {
		t.Fatalf("err = %v, want ErrNoPath", r.Err)
	}
}

func TestRevertOfDeletedFileFails(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	r := e.Do("revert", map[string]any{"force": true})
	if !errors.Is(r.Err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", r.Err)
	}
	if got := e.Active().Buffer.Text(); got != "a\n" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
}

func TestRevertRefusedOnReadOnlyDocument(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	run(t, e, "toggle_read_only")
	putFile(t, path, "b\n")
	r := e.Do("revert", map[string]any{"force": true})
	if !errors.Is(r.Err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", r.Err)
	}
	if got := e.Active().Buffer.Text(); got != "a\n" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
}

// A reload that finds the file the editor itself just saved changes nothing, so it must not
// replace the "Saved" status with a "Reverted" one: the watcher reports every own save.
func TestRevertOfTheEditorsOwnSaveKeepsTheStatus(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	typeText(t, e, "x")
	run(t, e, "save")
	d := e.Active()
	if d.Modified() {
		t.Fatal("document is modified after the save")
	}
	status := d.status
	plan := e.PlanRevertPath(path)
	if err := e.ApplyRevert(plan, RunRevert(plan)); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if d.status != status {
		t.Errorf("status = %q, want %q", d.status, status)
	}
	if got := d.Buffer.Text(); got != "xa\n" {
		t.Errorf("text = %q", got)
	}
}

func TestRevertOfARewrittenFileWithTheSameTextSaysReverted(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	d := e.Active()
	putFile(t, path, "a\n")
	if err := os.Chtimes(path, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	plan := e.PlanRevertPath(path)
	if err := e.ApplyRevert(plan, RunRevert(plan)); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if d.status != "Reverted a.txt" {
		t.Errorf("status = %q", d.status)
	}
	if !d.disk.sameAs(diskStateOf(path)) {
		t.Error("the disk state was not adopted")
	}
}
