package editor

import (
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// openNamed writes text to a file called name in a temp dir and opens it, so
// the document has the path it has in the running app.
func openNamed(t *testing.T, name, text string) *Editor {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	putFile(t, path, text)
	e := New(Options{})
	e.SetLanguageSettings(configResolver())
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return e
}

func TestToggleCommentUsesTheLanguagePrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file string
		Want string
	}{
		{"main.go", "// x"},
		{"main.rs", "// x"},
		{"a.c", "// x"},
		{"a.h", "// x"},
		{"a.cpp", "// x"},
		{"a.js", "// x"},
		{"a.tsx", "// x"},
		{"Main.java", "// x"},
		{"a.py", "# x"},
		{"a.sh", "# x"},
		{"a.rb", "# x"},
		{"a.yaml", "# x"},
		{"a.toml", "# x"},
		{"Makefile", "# x"},
		{"Dockerfile", "# x"},
		{"a.lua", "-- x"},
		{"a.sql", "-- x"},
		{"a.hs", "-- x"},
		{"a.tex", "% x"},
		{"a.erl", "% x"},
		{"a.ini", "; x"},
		{"a.f90", "! x"},
		{"a.php", "// x"},
		{"a.swift", "// x"},
		{"a.zig", "// x"},
		{"a.nix", "# x"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()
			e := openNamed(t, tt.file, "x")
			run(t, e, "toggle_comment")
			if got := text(e); got != tt.Want {
				t.Fatalf("text = %q, want %q", got, tt.Want)
			}
		})
	}
}

func TestToggleCommentFollowsSetLanguage(t *testing.T) {
	t.Parallel()
	e := openNamed(t, "main.go", "x")
	e.SetLanguage("python")
	run(t, e, "toggle_comment")
	if got := text(e); got != "# x" {
		t.Fatalf("text = %q, want %q", got, "# x")
	}
}

func TestToggleCommentUsesTheConfiguredPrefix(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "x")
	e.SetLanguageSettings(func(string, string) LanguageSettings {
		return LanguageSettings{CommentPrefix: "REM"}
	})
	run(t, e, "toggle_comment")
	if got := text(e); got != "REM x" {
		t.Fatalf("text = %q, want %q", got, "REM x")
	}
	run(t, e, "toggle_comment")
	if got := text(e); got != "x" {
		t.Fatalf("text after the second toggle = %q, want x", got)
	}
}

func TestToggleCommentWithoutAResolverHasNoPrefix(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "x")
	if r := e.Do("toggle_comment", nil); r.Err != nil {
		t.Fatalf("toggle_comment without any language settings = %v, want a silent no-op", r.Err)
	}
	if got := text(e); got != "x" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
}

func TestToggleCommentCaretRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		file       string
		Text       string
		caret      buffer.Pos
		wantText   string
		wantCursor buffer.Pos
	}{
		{
			Name:       "caret at column 0 moves past the new prefix",
			file:       "main.go",
			Text:       "foo",
			caret:      buffer.Pos{Line: 0, Col: 0},
			wantText:   "// foo",
			wantCursor: buffer.Pos{Line: 0, Col: 3},
		},
		{
			Name:       "caret in the text moves with it",
			file:       "main.go",
			Text:       "foo",
			caret:      buffer.Pos{Line: 0, Col: 1},
			wantText:   "// foo",
			wantCursor: buffer.Pos{Line: 0, Col: 4},
		},
		{
			Name:       "python uses hash",
			file:       "script.py",
			Text:       "def main():",
			caret:      buffer.Pos{Line: 0, Col: 0},
			wantText:   "# def main():",
			wantCursor: buffer.Pos{Line: 0, Col: 2},
		},
		{
			Name:       "uncomment removes the token and its space",
			file:       "main.go",
			Text:       "// foo",
			caret:      buffer.Pos{Line: 0, Col: 4},
			wantText:   "foo",
			wantCursor: buffer.Pos{Line: 0, Col: 1},
		},
		{
			Name:       "uncomment accepts a token with no space",
			file:       "main.go",
			Text:       "//foo",
			caret:      buffer.Pos{Line: 0, Col: 0},
			wantText:   "foo",
			wantCursor: buffer.Pos{Line: 0, Col: 0},
		},
		{
			Name:       "uncomment after indentation keeps the indentation",
			file:       "main.go",
			Text:       "  // foo",
			caret:      buffer.Pos{Line: 0, Col: 6},
			wantText:   "  foo",
			wantCursor: buffer.Pos{Line: 0, Col: 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := openNamed(t, tt.file, tt.Text)
			e.SetCursor(tt.caret, false)
			run(t, e, "toggle_comment")
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := e.Active().Cursor; got != tt.wantCursor {
				t.Fatalf("caret = %+v, want %+v", got, tt.wantCursor)
			}
			if sel := e.Active().Sel; sel != nil {
				t.Fatalf("selection = %+v, want none after toggling an empty caret", sel)
			}
		})
	}
}

func TestToggleCommentEmptyCaretsLeaveNoSelection(t *testing.T) {
	t.Parallel()
	e := openNamed(t, "main.go", "foo\nbar")
	e.Active().SetCarets([]caret{
		{Anchor: caretPos(0, 0), Head: caretPos(0, 0), Primary: true},
		{Anchor: caretPos(1, 0), Head: caretPos(1, 0)},
	})
	run(t, e, "toggle_comment")
	want := carets{
		Primary: Caret{Cursor: caretPos(0, 3)},
		extras:  []Caret{{Cursor: caretPos(1, 3)}},
	}
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets = %s, want %s", got, want)
	}
}

func TestToggleCommentSelectionRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Text     string
		Sel      buffer.Selection
		wantText string
		wantSel  buffer.Selection
	}{
		{
			Name:     "comments the selected lines and keeps the selection",
			Text:     "line1\nline2\nline3\nline4",
			Sel:      buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2}},
			wantText: "// line1\n// line2\nline3\nline4",
			wantSel:  buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2}},
		},
		{
			Name:     "uncomments when every selected line is commented",
			Text:     "// line1\n// line2\n// line3\nline4",
			Sel:      buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2}},
			wantText: "line1\nline2\n// line3\nline4",
			wantSel:  buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2}},
		},
		{
			Name:     "comments every line when only some are commented",
			Text:     "// a\nb",
			Sel:      buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 1, Col: 1}},
			wantText: "// // a\n// b",
			wantSel:  buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 1, Col: 4}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := openNamed(t, "main.go", tt.Text)
			e.SelectRange(tt.Sel.Anchor, tt.Sel.Head)
			run(t, e, "toggle_comment")
			d := e.Active()
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if d.Sel == nil || *d.Sel != tt.wantSel || d.Cursor != tt.wantSel.Head {
				t.Fatalf("selection = %+v caret = %+v, want %+v", d.Sel, d.Cursor, tt.wantSel)
			}
		})
	}
}

func TestToggleCommentTwiceIsIdentity(t *testing.T) {
	t.Parallel()
	const doc = "line1\nline2\nline3"
	e := openNamed(t, "lib.rs", doc)
	e.SelectRange(buffer.Pos{}, buffer.Pos{Line: 2, Col: 5})
	run(t, e, "toggle_comment", "toggle_comment")
	if got := text(e); got != doc {
		t.Fatalf("text = %q, want %q", got, doc)
	}
	if d := e.Active(); d.Sel == nil || d.Sel.Head != (buffer.Pos{Line: 2, Col: 5}) {
		t.Fatalf("selection = %+v, want it back on line 2 column 5", d.Sel)
	}
}

func TestToggleCommentWithoutACommentPrefixDoesNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name string
		file string // empty for an untitled buffer
	}{
		{"untitled buffer", ""},
		{"txt file", "notes.txt"},
		{"unknown extension", "data.xyz"},
		{"json has none", "data.json"},
		{"markdown has none", "README.md"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, "x")
			e.SetLanguageSettings(configResolver())
			if tt.file != "" {
				e = openNamed(t, tt.file, "x")
			}
			if r := e.Do("toggle_comment", nil); r.Err != nil {
				t.Fatalf("Do(toggle_comment) error = %v, want a silent no-op", r.Err)
			}
			if got := text(e); got != "x" {
				t.Fatalf("text = %q, want it unchanged", got)
			}
		})
	}
}

func TestToggleCommentAtEveryCaret(t *testing.T) {
	t.Parallel()
	e := openNamed(t, "main.go", "a\n  b\nc\nd")
	e.Active().SetCarets([]caret{
		{Anchor: caretPos(0, 1), Head: caretPos(0, 1), Primary: true},
		{Anchor: caretPos(1, 3), Head: caretPos(1, 3)},
		{Anchor: caretPos(3, 0), Head: caretPos(3, 1)},
	})
	run(t, e, "toggle_comment")
	if got := text(e); got != "// a\n//   b\nc\n// d" {
		t.Fatalf("text = %q", got)
	}
	want := carets{
		Primary: Caret{Cursor: caretPos(0, 4)},
		extras: []Caret{
			{Cursor: caretPos(1, 6)},
			{Cursor: caretPos(3, 4), Sel: caretSel(caretPos(3, 0), caretPos(3, 4))},
		},
	}
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets = %s, want %s", got, want)
	}
	run(t, e, "undo")
	if got := text(e); got != "a\n  b\nc\nd" {
		t.Fatalf("text after undo = %q, want one step", got)
	}
}

func TestToggleCommentUncommentsOnlyWhenEveryCaretLineIsCommented(t *testing.T) {
	t.Parallel()
	e := openNamed(t, "main.go", "// a\nb\n// c")
	e.Active().SetCarets([]caret{
		{Anchor: caretPos(0, 0), Head: caretPos(0, 0), Primary: true},
		{Anchor: caretPos(1, 0), Head: caretPos(1, 0)},
	})
	run(t, e, "toggle_comment")
	if got := text(e); got != "// // a\n// b\n// c" {
		t.Fatalf("text = %q, want both caret lines commented", got)
	}
	e.Active().SetCarets([]caret{
		{Anchor: caretPos(0, 0), Head: caretPos(0, 0), Primary: true},
		{Anchor: caretPos(2, 0), Head: caretPos(2, 0)},
	})
	run(t, e, "toggle_comment")
	if got := text(e); got != "// a\n// b\nc" {
		t.Fatalf("text = %q, want both uncommented", got)
	}
}
