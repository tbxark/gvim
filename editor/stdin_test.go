package editor

import (
	"os"
	"slices"
	"testing"
)

// The stdin buffer: an untitled, unmodified document with a label, opened in a tab (the
// test binary's pane options; no files).

func openStdinEditor(t *testing.T) *Editor {
	t.Helper()
	return New(Options{Rows: 24, Cols: 80, Empty: true})
}

func TestOpenStdinTextIsAnUnmodifiedLabelledBuffer(t *testing.T) {
	t.Parallel()
	e := openStdinEditor(t)
	d := e.OpenStdinText("hello from stdin\nsecond line\n", StdinOptions{Label: "[stdin]", Language: "markdown"})
	if d == nil || e.Active() != d {
		t.Fatalf("the stdin document is not active")
	}
	if got := d.Name(); got != "[stdin]" {
		t.Errorf("name = %q, want [stdin]", got)
	}
	if !d.Untitled() {
		t.Error("the stdin buffer must have no path (Save goes through Save As)")
	}
	if d.Modified() {
		t.Error("the stdin buffer is unmodified on open, like a file-born buffer")
	}
	if got := d.Saved; got != "hello from stdin\nsecond line\n" {
		t.Errorf("saved text = %q", got)
	}
	if got := d.Language(); got != "markdown" {
		t.Errorf("language = %q, want markdown", got)
	}
	if got := d.Buffer.Lines(); got != 3 {
		t.Errorf("line count = %d, want 3", got)
	}
	if d.Buffer.Line(0) != "hello from stdin" || d.Buffer.Line(1) != "second line" {
		t.Errorf("lines = %q, %q", d.Buffer.Line(0), d.Buffer.Line(1))
	}
	if tabs := e.Tabs(); len(tabs) != 1 || tabs[0].Name != "[stdin]" || !tabs[0].Active {
		t.Errorf("tabs = %+v, want one active [stdin] tab", tabs)
	}
}

func TestOpenStdinTextNormalizesLineEndings(t *testing.T) {
	t.Parallel()
	e := openStdinEditor(t)
	d := e.OpenStdinText("a\r\nb\r\n", StdinOptions{Label: "[stdin]"})
	if got := d.Saved; got != "a\nb\n" {
		t.Errorf("saved text = %q, want the LF-normalized text", got)
	}
	if got := d.Eol; got != "CRLF" {
		t.Errorf("Eol = %q, want CRLF remembered for Save As", got)
	}
	if got := d.Modified(); got {
		t.Error("a CRLF pipe is not a modification")
	}
}

func TestOpenStdinTextKeepsFilesAndAppendsATab(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/a.txt"
	e := openStdinEditor(t)
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	d := e.OpenStdinText("x\n", StdinOptions{Label: "[stdin]"})
	names := func() []string {
		var out []string
		for _, tab := range e.Tabs() {
			out = append(out, tab.Name)
		}
		return out
	}
	if got := names(); !slices.Equal(got, []string{"a.txt", "[stdin]"}) {
		t.Errorf("tabs = %v", got)
	}
	if e.Active() != d {
		t.Error("the stdin document did not become active")
	}
}
