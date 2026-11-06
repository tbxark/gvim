package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
)

// openAdapterFile writes a file called name holding text, opens it in a new editor and returns
// the adapter over that editor, the editor and the path.
func openAdapterFile(t *testing.T, name, text string) (editorAdapter, *editor.Editor, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	must(t, os.WriteFile(path, []byte(text), 0o644))
	ed := editor.New(editor.Options{})
	must(t, ed.OpenFile(path))
	return editorAdapter{ed: ed}, ed, path
}

// TestAdapterRevertReloadsACleanFileAfterAnOutsideChange checks the revert through the
// adapter's four steps: a clean buffer is planned, its file changes outside the editor, and
// the read and the apply put the new text in place.
func TestAdapterRevertReloadsACleanFileAfterAnOutsideChange(t *testing.T) {
	t.Parallel()
	a, ed, path := openAdapterFile(t, "a.txt", "a\n")
	plan, err := a.PlanRevert(false)
	must(t, err)
	must(t, os.WriteFile(path, []byte("b\n"), 0o644))
	res := a.RunRevert(plan)
	must(t, res.Err)
	must(t, a.ApplyRevert(plan, res))
	if got := ed.Active().Buffer.Text(); got != "b\n" {
		t.Fatalf("text = %q, want the file text %q", got, "b\n")
	}
	if ed.Active().Modified() {
		t.Fatal("document is modified after the revert, want it saved")
	}
}

// TestAdapterPlanRevertRefusesAnUnsavedFileWithoutForce checks that the adapter refuses a
// modified buffer unless force is set, and that the refused plan reports Modified.
func TestAdapterPlanRevertRefusesAnUnsavedFileWithoutForce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		force        bool
		wantErr      error
		wantModified bool
	}{
		{name: "without force the edit refuses the plan", force: false, wantErr: editor.ErrUnsaved, wantModified: true},
		{name: "with force the plan is made", force: true, wantModified: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a, ed, _ := openAdapterFile(t, "a.txt", "a\n")
			if r := ed.Do("insert_char", map[string]any{"text": "x"}); r.Err != nil {
				t.Fatalf("insert_char: %v", r.Err)
			}
			plan, err := a.PlanRevert(tt.force)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("PlanRevert err = %v, want %v", err, tt.wantErr)
			}
			if plan.Modified != tt.wantModified {
				t.Fatalf("plan.Modified = %t, want %t", plan.Modified, tt.wantModified)
			}
			if got := ed.Active().Buffer.Text(); got != "xa\n" {
				t.Fatalf("text = %q, want the edit kept", got)
			}
		})
	}
}

// TestAdapterApplyRevertOfIdenticalTextChangesNothing checks that a revert whose file already
// holds the buffer's text, as after a save, leaves the text and the undo history alone.
func TestAdapterApplyRevertOfIdenticalTextChangesNothing(t *testing.T) {
	t.Parallel()
	a, ed, path := openAdapterFile(t, "a.txt", "a\n")
	if r := ed.Do("insert_char", map[string]any{"text": "x"}); r.Err != nil {
		t.Fatalf("insert_char: %v", r.Err)
	}
	if r := ed.Do("save", nil); r.Err != nil {
		t.Fatalf("save: %v", r.Err)
	}
	plan, err := a.PlanRevert(false)
	must(t, err)
	res := a.RunRevert(plan)
	must(t, res.Err)
	must(t, a.ApplyRevert(plan, res))
	if got := ed.Active().Buffer.Text(); got != "xa\n" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "xa\n" {
		t.Fatalf("file = %q, %v, want the saved text", data, err)
	}
	if r := ed.Do("undo", nil); r.Err != nil {
		t.Fatalf("undo: %v", r.Err)
	}
	if got := ed.Active().Buffer.Text(); got != "a\n" {
		t.Fatalf("after undo text = %q, want %q: the undo history was replaced", got, "a\n")
	}
}

// TestAdapterApplyRevertReloadsABackgroundDocumentByItsPath checks that a plan made for a tab
// that is not active reloads that tab, found by its path, and leaves the active tab alone.
func TestAdapterApplyRevertReloadsABackgroundDocumentByItsPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(pathA, []byte("a\n"), 0o644))
	must(t, os.WriteFile(pathB, []byte("b\n"), 0o644))
	ed := editor.New(editor.Options{})
	must(t, ed.OpenFile(pathA))
	must(t, ed.OpenFile(pathB))
	a := editorAdapter{ed: ed}

	ed.ActivateTab(0)
	plan, err := a.PlanRevert(false)
	must(t, err)
	must(t, os.WriteFile(pathA, []byte("c\n"), 0o644))
	ed.ActivateTab(1)
	if r := ed.Do("insert_char", map[string]any{"text": "y"}); r.Err != nil {
		t.Fatalf("insert_char: %v", r.Err)
	}
	res := a.RunRevert(plan)
	must(t, res.Err)
	must(t, a.ApplyRevert(plan, res))

	if got := ed.DocAt(0).Buffer.Text(); got != "c\n" {
		t.Fatalf("planned tab text = %q, want %q", got, "c\n")
	}
	if got := ed.Active().Path; got != pathB {
		t.Fatalf("active tab = %q, want %q: the revert changed the active tab", got, pathB)
	}
	if got := ed.Active().Buffer.Text(); got != "yb\n" {
		t.Fatalf("active tab text = %q, want the edit kept", got)
	}
}

// TestAdapterHasFormatterFollowsTheLanguageOrExtension checks the adapter's formatter
// answer, which the menu's Format Buffer row reads, for a file opened from disk.
func TestAdapterHasFormatterFollowsTheLanguageOrExtension(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		file       string
		formatters map[string]string
		want       bool
	}{
		{name: "the extension of an opened file has a formatter", file: "a.go", formatters: map[string]string{".go": "gofmt"}, want: true},
		{name: "a file with another extension has none", file: "a.txt", formatters: map[string]string{".go": "gofmt"}, want: false},
		{name: "a language key does not match an extension", file: "a.go", formatters: map[string]string{"go": "gofmt"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a, ed, _ := openAdapterFile(t, tt.file, "abc\n")
			ed.SetFormatters(tt.formatters)
			if got := a.HasFormatter(); got != tt.want {
				t.Fatalf("HasFormatter = %t, want %t", got, tt.want)
			}
		})
	}
}

// TestConfiguredExtensionFormatterRunsOnAnOpenedFile checks the shipped path: a formatter keyed
// by the extension of the file, in the project config, formats the file opened from disk with
// no language set.
func TestConfiguredExtensionFormatterRunsOnAnOpenedFile(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	writeTestFile(t, file, "hello\n")
	writeTestFile(t, filepath.Join(root, ".gvim", "config.json"), `{"formatters": {".go": "tr a-z A-Z"}}`)
	rt, err := buildRuntime(t.Context(), Options{Root: root, Files: []FileTarget{{Path: file}}}, false)
	must(t, err)
	t.Cleanup(rt.close)
	if r := rt.ed.Do("format_buffer", nil); r.Err != nil {
		t.Fatalf("format_buffer: %v", r.Err)
	}
	if got := rt.ed.Active().Buffer.Text(); got != "HELLO\n" {
		t.Fatalf("text = %q, want %q", got, "HELLO\n")
	}
}
