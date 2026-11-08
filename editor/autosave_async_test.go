package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openForAutoSave writes content to a new file, opens it in an editor with
// auto-save on, and returns the editor and the file path.
func openForAutoSave(t *testing.T, content string) (*Editor, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := New(Options{AutoSaveInterval: time.Second})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	return ed, path
}

// typeIntoDoc inserts text at the caret of the active document.
func typeIntoDoc(t *testing.T, ed *Editor, text string) {
	t.Helper()
	if res := ed.Do("insert_char", map[string]any{"text": text}); res.Err != nil {
		t.Fatal(res.Err)
	}
}

// readText returns the content of the file at path.
func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestPlanAutoSaveWritesNothing checks that planning the auto-save does no disk
// work: the file keeps its old text, and the document stays modified until the
// result is applied.
func TestPlanAutoSaveWritesNothing(t *testing.T) {
	t.Parallel()
	ed, path := openForAutoSave(t, "abc\n")
	typeIntoDoc(t, ed, "x")
	jobs := ed.PlanAutoSave(time.Now())
	if len(jobs) != 1 {
		t.Fatalf("PlanAutoSave returned %d jobs, want 1", len(jobs))
	}
	if got := readText(t, path); got != "abc\n" {
		t.Fatalf("file after the plan = %q, want the old text", got)
	}
	if !ed.Active().Modified() {
		t.Fatal("the plan marked the document saved before any write")
	}
}

// TestSaveJobRunWritesOrRefuses checks the disk work of a planned save: it writes
// the planned text, or it refuses to overwrite a file that changed after the plan.
func TestSaveJobRunWritesOrRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		outside  string // written to the file after the plan, when set
		wantErr  error
		wantFile string
	}{
		{name: "writes the planned text", wantFile: "xabc\n"},
		{name: "refuses a file changed on disk", outside: "other\n", wantErr: ErrChangedOnDisk, wantFile: "other\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed, path := openForAutoSave(t, "abc\n")
			typeIntoDoc(t, ed, "x")
			jobs := ed.PlanAutoSave(time.Now())
			if tt.outside != "" {
				if err := os.WriteFile(path, []byte(tt.outside), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			res := jobs[0].Run()
			if tt.wantErr == nil && res.Err() != nil {
				t.Fatalf("Run() error = %v, want nil", res.Err())
			}
			if tt.wantErr != nil && !errors.Is(res.Err(), tt.wantErr) {
				t.Fatalf("Run() error = %v, want %v", res.Err(), tt.wantErr)
			}
			if got := readText(t, path); got != tt.wantFile {
				t.Fatalf("file after Run = %q, want %q", got, tt.wantFile)
			}
		})
	}
}

// TestApplySavesMarksSavedOnlyWhenTextIsUnchanged checks that a written save marks
// the document saved when the text did not change after the plan, and that a
// later explicit save is not refused as changed on disk. An edit made while the
// write ran keeps the document modified.
func TestApplySavesMarksSavedOnlyWhenTextIsUnchanged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		editAfter    bool
		wantModified bool
	}{
		{name: "no edit while writing", editAfter: false, wantModified: false},
		{name: "an edit while writing", editAfter: true, wantModified: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed, path := openForAutoSave(t, "abc\n")
			typeIntoDoc(t, ed, "x")
			jobs := ed.PlanAutoSave(time.Now())
			if tt.editAfter {
				typeIntoDoc(t, ed, "y")
			}
			res := jobs[0].Run()
			if err := ed.ApplySaves([]SaveResult{res}); err != nil {
				t.Fatalf("ApplySaves() error = %v", err)
			}
			if got := ed.Active().Modified(); got != tt.wantModified {
				t.Fatalf("Modified() = %v, want %v", got, tt.wantModified)
			}
			if !tt.editAfter {
				if err := ed.Save(); err != nil {
					t.Fatalf("explicit save after the apply error = %v, want nil", err)
				}
			}
			if got := readText(t, path); !strings.HasPrefix(got, "x") {
				t.Fatalf("file = %q, want the planned text", got)
			}
		})
	}
}

// TestApplySavesRecordsAFailureAndKeepsTheEdits checks that a save refused for a
// changed file keeps the document's edits, shows the error in its status, and
// returns the error.
func TestApplySavesRecordsAFailureAndKeepsTheEdits(t *testing.T) {
	t.Parallel()
	ed, path := openForAutoSave(t, "abc\n")
	typeIntoDoc(t, ed, "x")
	jobs := ed.PlanAutoSave(time.Now())
	if err := os.WriteFile(path, []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ed.ApplySaves([]SaveResult{jobs[0].Run()})
	if !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("ApplySaves() error = %v, want ErrChangedOnDisk", err)
	}
	doc := ed.Active()
	if !doc.Modified() {
		t.Fatal("a refused save marked the document saved")
	}
	if !strings.Contains(doc.status, "changed on disk") {
		t.Fatalf("status = %q, want the changed-on-disk error", doc.status)
	}
}
