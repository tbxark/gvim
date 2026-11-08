package editor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fileOrEmpty returns the text of path, or "" when it cannot be read. An untitled
// document has no path, so its state is compared as empty.
func fileOrEmpty(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// TestPlanSaveStates checks an explicit save by the state of its document. Planning
// writes nothing in any state. Running the plan writes the text in the states that
// allow a save, and refuses the others with the error and the status Save gives.
func TestPlanSaveStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		setup   func(t *testing.T) (*Editor, string)
		Disk    func(t *testing.T, path string) // changes the file before the plan, or nil
		PlanErr error                           // what PlanSave returns
		runErr  error                           // what ApplySavePlan returns for the run
		file    string                          // the file's text after the run
		dirty   bool                            // the document is modified after the run
		Status  string                          // the status after the plan or the run
	}{
		{
			Name:   "clean",
			setup:  func(t *testing.T) (*Editor, string) { return openFixture(t, Options{}, "abc\n") },
			file:   "abc\n",
			Status: "Saved",
		},
		{
			Name: "modified",
			setup: func(t *testing.T) (*Editor, string) {
				e, path := openFixture(t, Options{}, "abc\n")
				typeText(t, e, "x")
				return e, path
			},
			file:   "xabc\n",
			Status: "Saved",
		},
		{
			Name: "read-only",
			setup: func(t *testing.T) (*Editor, string) {
				e, path := openFixture(t, Options{}, "abc\n")
				typeText(t, e, "x")
				run(t, e, "toggle_read_only")
				return e, path
			},
			PlanErr: ErrReadOnly,
			file:    "abc\n",
			dirty:   true,
			Status:  "read-only",
		},
		{
			Name: "untitled",
			setup: func(t *testing.T) (*Editor, string) {
				e := New(Options{})
				run(t, e, "new")
				typeText(t, e, "x")
				return e, ""
			},
			PlanErr: ErrNoPath,
			dirty:   true,
			Status:  "buffer has no file name",
		},
		{
			Name: "changed on disk",
			setup: func(t *testing.T) (*Editor, string) {
				e, path := openFixture(t, Options{}, "abc\n")
				typeText(t, e, "x")
				return e, path
			},
			Disk: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("other\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			runErr: ErrChangedOnDisk,
			file:   "other\n",
			dirty:  true,
			Status: "doc.txt changed on disk since it was opened; not saved. Save As keeps your version",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, path := tt.setup(t)
			if tt.Disk != nil {
				tt.Disk(t, path)
			}
			before := fileOrEmpty(path)

			p, err := e.PlanSave()
			if !errors.Is(err, tt.PlanErr) {
				t.Fatalf("PlanSave err = %v, want %v", err, tt.PlanErr)
			}
			if got := fileOrEmpty(path); got != before {
				t.Fatalf("PlanSave changed the file to %q, want %q", got, before)
			}
			if tt.PlanErr != nil {
				if e.Status() != tt.Status {
					t.Fatalf("status after the refused plan = %q, want %q", e.Status(), tt.Status)
				}
				return
			}

			err = e.ApplySavePlan(p, p.Run())
			if !errors.Is(err, tt.runErr) {
				t.Fatalf("ApplySavePlan err = %v, want %v", err, tt.runErr)
			}
			if path != "" {
				if got := readFile(t, path); got != tt.file {
					t.Fatalf("file after the run = %q, want %q", got, tt.file)
				}
			}
			if got := e.Active().Modified(); got != tt.dirty {
				t.Fatalf("modified after the run = %v, want %v", got, tt.dirty)
			}
			if e.Status() != tt.Status {
				t.Fatalf("status after the run = %q, want %q", e.Status(), tt.Status)
			}
		})
	}
}

// TestPlanSaveAsStates checks a Save As by its target. A plan that is refused changes
// nothing: the document keeps its path, and the file it names is not written. A write
// that the plan allows takes the path only once the write has worked.
func TestPlanSaveAsStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		target  func(dir string) string // the path to save to, relative to dir
		other   bool                    // another tab holds the target path
		Exists  bool                    // the target file exists before the plan
		force   bool
		PlanErr error
		runErr  error
		renamed bool // the document takes the target path after the run
	}{
		{Name: "new file", target: func(dir string) string { return filepath.Join(dir, "new.txt") }, renamed: true},
		{Name: "existing file without force", target: func(dir string) string { return filepath.Join(dir, "other.txt") }, Exists: true, runErr: ErrExists},
		{Name: "existing file with force", target: func(dir string) string { return filepath.Join(dir, "other.txt") }, Exists: true, force: true, renamed: true},
		{Name: "open in another tab", target: func(dir string) string { return filepath.Join(dir, "other.txt") }, other: true, Exists: true, force: true, PlanErr: ErrAlreadyOpen},
		{Name: "empty path", target: func(string) string { return "" }, PlanErr: ErrEmptyPath},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			src := filepath.Join(dir, "doc.txt")
			if err := os.WriteFile(src, []byte("abc\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			target := tt.target(dir)
			if tt.Exists {
				if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			e := New(Options{})
			if err := e.OpenFile(src); err != nil {
				t.Fatal(err)
			}
			if tt.other {
				if err := e.OpenFile(target); err != nil {
					t.Fatal(err)
				}
				run(t, e, "prev_buffer")
			}
			typeText(t, e, "x")

			p, err := e.PlanSaveAs(target, tt.force)
			if !errors.Is(err, tt.PlanErr) {
				t.Fatalf("PlanSaveAs err = %v, want %v", err, tt.PlanErr)
			}
			if tt.PlanErr == nil {
				err = e.ApplySavePlan(p, p.Run())
				if !errors.Is(err, tt.runErr) {
					t.Fatalf("ApplySavePlan err = %v, want %v", err, tt.runErr)
				}
			}
			if tt.renamed != (e.Active().Path == target) {
				t.Fatalf("document path = %q after Save As to %q, want renamed = %v", e.Active().Path, target, tt.renamed)
			}
			if tt.runErr == nil && tt.PlanErr == nil {
				if got := readFile(t, target); got != "xabc\n" {
					t.Fatalf("target after Save As = %q, want the typed text", got)
				}
				return
			}
			if tt.Exists {
				if got := readFile(t, target); got != "keep\n" {
					t.Fatalf("refused Save As changed the target to %q", got)
				}
			}
		})
	}
}

// TestPlanSaveAllWritesOnlyWhenRun checks that planning Save All writes no file, and
// that running the plan writes every planned document.
func TestPlanSaveAllWritesOnlyWhenRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")}
	e := New(Options{})
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("abc\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := e.OpenFile(path); err != nil {
			t.Fatal(err)
		}
		typeText(t, e, "x")
	}

	p, err := e.PlanSaveAll()
	if err != nil {
		t.Fatalf("PlanSaveAll err = %v", err)
	}
	for _, path := range paths {
		if got := readFile(t, path); got != "abc\n" {
			t.Fatalf("file %s after the plan = %q, want the old text", path, got)
		}
	}
	if err := e.ApplySavePlan(p, p.Run()); err != nil {
		t.Fatalf("ApplySavePlan err = %v", err)
	}
	for _, path := range paths {
		if got := readFile(t, path); got != "xabc\n" {
			t.Fatalf("file %s after the run = %q, want the typed text", path, got)
		}
	}
	if e.Status() != "Saved 2 files" {
		t.Fatalf("status = %q, want %q", e.Status(), "Saved 2 files")
	}
}
