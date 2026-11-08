package editor

import (
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestOpenDetectsLineEnding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		content  string
		wantEOL  string
		wantText string
	}{
		{"all CRLF", "a\r\nb\r\n", "CRLF", "a\nb\n"},
		{"all LF", "a\nb\n", "LF", "a\nb\n"},
		{"CRLF majority", "a\r\nb\nc\r\n", "CRLF", "a\nb\nc\n"},
		{"LF majority", "a\nb\nc\r\n", "LF", "a\nb\nc\n"},
		{"tie goes to LF", "a\r\nb\n", "LF", "a\nb\n"},
		{"no line break", "one line", "LF", "one line"},
		{"empty file", "", "LF", ""},
		{"CRLF without final newline", "a\r\nb", "CRLF", "a\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, _ := openFixture(t, Options{}, tt.content)
			if got := e.Active().LineEnding(); got != tt.wantEOL {
				t.Fatalf("LineEnding() = %q, want %q", got, tt.wantEOL)
			}
			if got := text(e); got != tt.wantText {
				t.Fatalf("buffer text = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestCRLFFileRoundTripsByteForByte(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"a\r\nb\r\n",
		"a\r\nb",
		"a\r\n\r\nb\r\n",
		"x\r\r\ny\r",
		"a\r\n",
	}
	for _, in := range inputs {
		e, path := openFixture(t, Options{}, in)
		if err := e.Save(); err != nil {
			t.Fatalf("Save(%q): %v", in, err)
		}
		if got := readFile(t, path); got != in {
			t.Fatalf("saved %q, want the original bytes %q", got, in)
		}
	}
}

func TestCRLFCaretStopsBeforeTheLineEnding(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "ab\r\ncd\r\n")
	run(t, e, "move_line_end")
	if got := e.Active().Cursor; got != (buffer.Pos{Line: 0, Col: 2}) {
		t.Fatalf("caret after move_line_end = %+v, want column 2 (the \\r is not text)", got)
	}
}

func TestCRLFEditSavesWithCRLF(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "a\r\nb")
	run(t, e, "move_document_end", "insert_newline")
	typeText(t, e, "c")
	run(t, e, "save")
	if got, want := readFile(t, path), "a\r\nb\r\nc"; got != want {
		t.Fatalf("saved %q, want %q", got, want)
	}
}

func TestMixedEndingsKeepTheirEndingOnSave(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "a\r\nb\nc\r\n")
	run(t, e, "save")
	if got, want := readFile(t, path), "a\r\nb\nc\r\n"; got != want {
		t.Fatalf("saved %q, want %q (each line keeps its ending)", got, want)
	}
}

func TestNewDocumentUsesDefaultLineEnding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		defaultEOL string
		Want       string
		wantFile   string
	}{
		{"empty means LF", "", "LF", "one\ntwo"},
		{"LF", "LF", "LF", "one\ntwo"},
		{"CRLF", "CRLF", "CRLF", "one\r\ntwo"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{DefaultLineEnding: tt.defaultEOL})
			if got := e.Active().LineEnding(); got != tt.Want {
				t.Fatalf("placeholder LineEnding() = %q, want %q", got, tt.Want)
			}
			run(t, e, "new")
			if got := e.Active().LineEnding(); got != tt.Want {
				t.Fatalf("untitled LineEnding() = %q, want %q", got, tt.Want)
			}
			typeText(t, e, "one")
			run(t, e, "insert_newline")
			typeText(t, e, "two")
			path := filepath.Join(t.TempDir(), "new.txt")
			if err := e.SaveAs(path); err != nil {
				t.Fatalf("SaveAs: %v", err)
			}
			if got := readFile(t, path); got != tt.wantFile {
				t.Fatalf("saved %q, want %q", got, tt.wantFile)
			}
		})
	}
}

func TestMissingFileUsesDefaultLineEnding(t *testing.T) {
	t.Parallel()
	e := New(Options{DefaultLineEnding: "CRLF"})
	path := filepath.Join(t.TempDir(), "missing.txt")
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if got := e.Active().LineEnding(); got != "CRLF" {
		t.Fatalf("LineEnding() of a new path = %q, want CRLF", got)
	}
	typeText(t, e, "a")
	run(t, e, "insert_newline")
	typeText(t, e, "b")
	run(t, e, "save")
	if got, want := readFile(t, path), "a\r\nb"; got != want {
		t.Fatalf("saved %q, want %q", got, want)
	}
}

func TestSetLineEndingChangesTheNextSave(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		file    string // the bytes on disk
		Args    map[string]any
		wantErr bool
		wantEOL string // LineEnding() after the action
		Want    string // the bytes on disk after save
	}{
		{"LF file to CRLF", "a\nb\n", map[string]any{"value": "crlf"}, false, "CRLF", "a\r\nb\r\n"},
		{"CRLF file to LF", "a\r\nb\r\n", map[string]any{"value": "lf"}, false, "LF", "a\nb\n"},
		{"a CRLF setting on an LF file is not a no-op", "a\n", map[string]any{"value": "crlf"}, false, "CRLF", "a\r\n"},
		{"an unknown value is refused", "a\n", map[string]any{"value": "cr"}, true, "LF", "a\n"},
		{"a value in another case is refused", "a\n", map[string]any{"value": "CRLF"}, true, "LF", "a\n"},
		{"a missing value is refused", "a\r\n", map[string]any{}, true, "CRLF", "a\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, tt.file)
			r := e.Do("set_line_ending", tt.Args)
			if (r.Err != nil) != tt.wantErr {
				t.Fatalf("set_line_ending err = %v, want error %v", r.Err, tt.wantErr)
			}
			if got := e.Active().LineEnding(); got != tt.wantEOL {
				t.Fatalf("LineEnding() = %q, want %q", got, tt.wantEOL)
			}
			run(t, e, "save")
			if got := readFile(t, path); got != tt.Want {
				t.Fatalf("saved %q, want %q", got, tt.Want)
			}
		})
	}
}
