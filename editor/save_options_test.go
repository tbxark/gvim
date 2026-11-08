package editor

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestSaveTrimsTrailingWhitespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"spaces and tabs", "foo  \n\tbar\t \nbaz", "foo\n\tbar\nbaz"},
		{"whitespace-only line becomes empty", "   \nx", "\nx"},
		{"inner blanks are kept", "keep  inner  spaces  ", "keep  inner  spaces"},
		{"CRLF file", "a  \r\nb\t\r\n", "a\r\nb\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{TrimTrailingWhitespace: true}, tt.content)
			run(t, e, "save")
			if got := readFile(t, path); got != tt.want {
				t.Fatalf("saved %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSaveWithoutTrimKeepsTrailingWhitespace(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "a  \n")
	run(t, e, "save")
	if got := readFile(t, path); got != "a  \n" {
		t.Fatalf("saved %q, want the trailing blanks kept", got)
	}
}

func TestSaveEnsuresFinalNewline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"adds the newline", "abc", "abc\n"},
		{"keeps an existing newline", "abc\n", "abc\n"},
		{"keeps a blank last line", "abc\n\n", "abc\n\n"},
		{"empty file stays empty", "", ""},
		{"CRLF file gets a CRLF", "a\r\nb", "a\r\nb\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{EnsureFinalNewline: true}, tt.content)
			run(t, e, "save")
			if got := readFile(t, path); got != tt.want {
				t.Fatalf("saved %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSaveOptionsLeaveBufferMatchingSavedText(t *testing.T) {
	t.Parallel()
	opts := Options{TrimTrailingWhitespace: true, EnsureFinalNewline: true}
	e, path := openFixture(t, opts, "x  \ny")
	d := e.Active()
	run(t, e, "save")
	want := "x\ny\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
	if got := text(e); got != want {
		t.Fatalf("buffer = %q, want the saved text %q", got, want)
	}
	if got := strings.Join(d.SavedLines(), "\n"); got != want {
		t.Fatalf("SavedLines joined = %q, want %q", got, want)
	}
	if d.Modified() {
		t.Fatal("document reports Modified after a save")
	}
	if changed := d.LinesChangedSinceSave(); changed != nil {
		t.Fatalf("LinesChangedSinceSave() after save = %v, want nil", changed)
	}
}

func TestSaveTrimClampsCaret(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{TrimTrailingWhitespace: true}, "foo   ")
	run(t, e, "move_line_end")
	run(t, e, "save")
	d := e.Active()
	if want := (buffer.Pos{Line: 0, Col: 3}); d.Cursor != want {
		t.Fatalf("caret after save = %+v, want %+v", d.Cursor, want)
	}
	if d.Sel != nil {
		t.Fatalf("selection after save = %+v, want none", d.Sel)
	}
}

func TestSaveAsAppliesSaveOptions(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{TrimTrailingWhitespace: true, EnsureFinalNewline: true}, "a  ")
	path := filepath.Join(t.TempDir(), "copy.txt")
	if err := e.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if got := readFile(t, path); got != "a\n" {
		t.Fatalf("SaveAs wrote %q, want %q", got, "a\n")
	}
}

func TestSaveUntitledWithOptionsStillNeedsAName(t *testing.T) {
	t.Parallel()
	e := New(Options{TrimTrailingWhitespace: true, EnsureFinalNewline: true})
	typeText(t, e, "x  ")
	if err := e.Save(); !errors.Is(err, ErrNoPath) {
		t.Fatalf("Save() = %v, want ErrNoPath", err)
	}
	if got := text(e); got != "x  " {
		t.Fatalf("buffer = %q, want the text untouched", got)
	}
}
