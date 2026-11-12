package editor

import (
	"bytes"
	"errors"
	"image"
	"image/png"
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
		Name         string
		Edit         bool           // type "x" at the start after opening
		newDisk      string         // the file content after opening, if the file changes
		Args         map[string]any // arguments of revert
		wantErr      error
		wantText     string
		wantModified bool
	}{
		{
			Name:     "an unmodified buffer reloads the changed file",
			newDisk:  "b\n",
			wantText: "b\n",
		},
		{
			Name:         "a modified buffer is refused without force and keeps its edit",
			Edit:         true,
			newDisk:      "b\n",
			wantErr:      ErrUnsaved,
			wantText:     "xa\n",
			wantModified: true,
		},
		{
			Name:     "a modified buffer reloads with force",
			Edit:     true,
			Args:     map[string]any{"force": true},
			wantText: "a\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, path := openRevertable(t, "a\n")
			if tt.Edit {
				typeText(t, e, "x")
			}
			if tt.newDisk != "" {
				putFile(t, path, tt.newDisk)
			}
			r := e.Do("revert", tt.Args)
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

// pngFixture returns a 1x1 PNG, so a file the editor opens takes the image
// document path rather than the text one.
func pngFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestRevertRefusesAnImage checks the manual revert of an image: its picture is read
// when the tab opens, so the reload is refused with ErrImage and the summary stays.
func TestRevertRefusesAnImage(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(path, pngFixture(t), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if !e.Active().IsImage() {
		t.Fatal("the opened PNG is not an image document")
	}
	plan, err := e.PlanRevert(false)
	if !errors.Is(err, ErrImage) || plan.Path != "" {
		t.Fatalf("PlanRevert = (%+v, %v), want an ErrImage refusal", plan, err)
	}
	if err := e.Revert(false); !errors.Is(err, ErrImage) {
		t.Fatalf("Revert = %v, want ErrImage", err)
	}
	if !e.Active().IsImage() {
		t.Error("the refused revert lost the image document")
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
	status := d.Status
	plan := e.PlanRevertPath(path)
	if err := e.ApplyRevert(plan, RunRevert(plan)); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if d.Status != status {
		t.Errorf("status = %q, want %q", d.Status, status)
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
	if d.Status != "Reverted a.txt" {
		t.Errorf("status = %q", d.Status)
	}
	if !d.Disk.SameAs(diskStateOf(path)) {
		t.Error("the disk state was not adopted")
	}
}
