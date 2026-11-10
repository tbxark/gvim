package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestToggleReadOnlyFlipsTheFlag(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "abc\n")
	d := e.Active()
	run(t, e, "toggle_read_only")
	if !d.ReadOnly() {
		t.Fatal("toggle_read_only did not set read-only")
	}
	run(t, e, "toggle_read_only")
	if d.ReadOnly() {
		t.Fatal("second toggle_read_only did not clear read-only")
	}
}

func TestReadOnlyRefusesTextChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action Action
		Args   map[string]any
	}{
		{"insert_char", map[string]any{"text": "x"}},
		{"insert_newline", nil},
		{"insert_tab", nil},
		{"delete_backward", nil},
		{"delete_forward", nil},
		{"delete_word_backward", nil},
		{"delete_word_forward", nil},
		{"delete_to_line_start", nil},
		{"delete_to_line_end", nil},
		{"delete_line", nil},
		{"open_line", nil},
		{"duplicate_line", nil},
		{"move_line_up", nil},
		{"move_line_down", nil},
		{"to_upper_case", nil},
		{"to_lower_case", nil},
		{"transpose_chars", nil},
		{"toggle_comment", nil},
		{"dedent_selection", nil},
		{"cut", nil},
		{"paste", nil},
		{"undo", nil},
		{"redo", nil},
		{"reload_with_encoding", map[string]any{"name": "utf-8", "force": true}},
		{"dabbrev_expand", nil},
		{"shell_command_replace", map[string]any{"command": "echo X"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, "abc\ndef\n")
			run(t, e, "select_all", "toggle_read_only")
			r := e.Do(tt.action, tt.Args)
			if !errors.Is(r.Err, ErrReadOnly) {
				t.Fatalf("err = %v, want ErrReadOnly", r.Err)
			}
			if e.Status() != "read-only" {
				t.Fatalf("status = %q, want %q", e.Status(), "read-only")
			}
			if got := e.Active().Buffer.Text(); got != "abc\ndef\n" {
				t.Fatalf("refused %s changed the text to %q", tt.action, got)
			}
			if got := readFile(t, path); got != "abc\ndef\n" {
				t.Fatalf("refused %s changed the file to %q", tt.action, got)
			}
		})
	}
}

func TestReadOnlyAllowsMovementSelectionAndSaveAs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action Action
		Args   map[string]any
	}{
		{"move_right", nil},
		{"move_document_end", nil},
		{"select_word", nil},
		{"select_all", nil},
		{"copy", nil},
		{"scroll_down", nil},
		{"recenter", nil},
		{"goto_line", nil},
		{"set_encoding", map[string]any{"name": "utf-8"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			t.Parallel()
			e, _ := openFixture(t, Options{}, "abc\ndef\n")
			run(t, e, "select_all", "toggle_read_only")
			if r := e.Do(tt.action, tt.Args); r.Err != nil {
				t.Fatalf("Do(%s) on a read-only document: %v", tt.action, r.Err)
			}
			if got := e.Active().Buffer.Text(); got != "abc\ndef\n" {
				t.Fatalf("%s changed the text to %q", tt.action, got)
			}
			if !e.Active().ReadOnly() {
				t.Fatalf("%s cleared read-only", tt.action)
			}
		})
	}

	t.Run("save_as writes a copy and stays read-only", func(t *testing.T) {
		t.Parallel()
		e, _ := openFixture(t, Options{}, "abc\ndef\n")
		run(t, e, "toggle_read_only")
		copyPath := filepath.Join(t.TempDir(), "copy.txt")
		if r := e.Do("save_as", map[string]any{"path": copyPath}); r.Err != nil {
			t.Fatalf("save_as on a read-only document: %v", r.Err)
		}
		if got := readFile(t, copyPath); got != "abc\ndef\n" {
			t.Fatalf("copy = %q", got)
		}
		if d := e.Active(); d.Path != copyPath || !d.ReadOnly() {
			t.Fatalf("after save_as: path %q, read-only %v", d.Path, d.ReadOnly())
		}
	})
}

func TestReadOnlyRefusesSave(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "abc\n")
	typeText(t, e, "x")
	run(t, e, "toggle_read_only")
	r := e.Do("save", nil)
	if !errors.Is(r.Err, ErrReadOnly) {
		t.Fatalf("save err = %v, want ErrReadOnly", r.Err)
	}
	if err := e.Save(); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Save err = %v, want ErrReadOnly", err)
	}
	if got := readFile(t, path); got != "abc\n" {
		t.Fatalf("refused save wrote %q", got)
	}
	if !e.Active().Modified() {
		t.Fatal("refused save cleared modified")
	}
}

// TestEveryActionLeavesReadOnlyTextAlone runs every action on a read-only
// document, once with the caret only and once with a selection, and with
// arguments and a clipboard that would change text if they were not refused. A
// new text-changing action that is missing from textChanging fails here.
func TestEveryActionLeavesReadOnlyTextAlone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const want = "Zabc\ndef\n"
	for _, a := range Actions() {
		t.Run(string(a), func(t *testing.T) {
			t.Parallel()
			for _, selected := range []bool{false, true} {
				path := filepath.Join(dir, fmt.Sprintf("%s-%t.txt", a, selected))
				if err := os.WriteFile(path, []byte("abc\ndef\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				e := New(Options{})
				if err := e.OpenFile(path); err != nil {
					t.Fatal(err)
				}
				typeText(t, e, "Z")
				if selected {
					run(t, e, "select_all")
				}
				run(t, e, "toggle_read_only")
				e.clipboard = "Q"
				d := e.Active()
				args := map[string]any{
					"text":  "x",
					"name":  "utf-8",
					"path":  filepath.Join(dir, fmt.Sprintf("%s-%t.saved", a, selected)),
					"force": true,
				}
				e.Do(a, args)
				if got := d.Buffer.Text(); got != want {
					t.Fatalf("read-only %s (selected %t) changed the text to %q", a, selected, got)
				}
			}
		})
	}
}

func TestTextChangingActionsAreRegistered(t *testing.T) {
	t.Parallel()
	for name := range textChanging {
		if _, ok := handlers[name]; !ok {
			t.Errorf("textChanging lists %q, which is not a registered action", name)
		}
	}
}

func TestSaveAllSkipsReadOnlyDocuments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	roPath := filepath.Join(dir, "ro.txt")
	rwPath := filepath.Join(dir, "rw.txt")
	for _, p := range []string{roPath, rwPath} {
		if err := os.WriteFile(p, []byte("abc\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e := New(Options{})
	if err := e.OpenFile(roPath); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "R")
	run(t, e, "toggle_read_only")
	if err := e.OpenFile(rwPath); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "W")
	run(t, e, "save_all")
	if got := readFile(t, roPath); got != "abc\n" {
		t.Fatalf("save_all wrote the read-only file: %q", got)
	}
	if got := readFile(t, rwPath); got != "Wabc\n" {
		t.Fatalf("save_all did not save the writable file: %q", got)
	}
	if want := "Saved 1 file; skipped 1 read-only"; e.Status() != want {
		t.Fatalf("status = %q, want %q", e.Status(), want)
	}
}

func TestDocumentSaveRefusesReadOnly(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "abc\n")
	typeText(t, e, "x")
	run(t, e, "toggle_read_only")
	if err := e.Save(); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Save err = %v, want ErrReadOnly", err)
	}
	if got := readFile(t, path); got != "abc\n" {
		t.Fatalf("Save wrote %q to a read-only document", got)
	}
}

// TestEditorReadOnlyOptionProtectsFiles verifies that Options.ReadOnly opens files in
// code-viewer / read-only mode so edits are refused.
func TestEditorReadOnlyOptionProtectsFiles(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{ReadOnly: true}, "hello world\n")
	if !e.Active().ReadOnly() {
		t.Fatal("e.Active().ReadOnly() = false, want true")
	}
	r := e.Do("insert_char", map[string]any{"text": "x"})
	if !errors.Is(r.Err, ErrReadOnly) {
		t.Fatalf("insert_char err = %v, want ErrReadOnly", r.Err)
	}
}
