package editor

import (
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// openNamed writes text to a file called name in a temp dir and opens it, so
// the document has the path it has in the running app.
func openNamed(t *testing.T, name, text string) *Editor {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	putFile(t, path, text)
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return e
}

func TestToggleCommentUsesTheExtensionToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file string
		want string
	}{
		{"main.go", "// x"},
		{"main.rs", "// x"},
		{"a.c", "// x"},
		{"a.h", "// x"},
		{"a.cc", "// x"},
		{"a.cpp", "// x"},
		{"a.cxx", "// x"},
		{"a.hpp", "// x"},
		{"a.js", "// x"},
		{"a.jsx", "// x"},
		{"a.mjs", "// x"},
		{"a.ts", "// x"},
		{"a.tsx", "// x"},
		{"Main.java", "// x"},
		{"MAIN.GO", "// x"},
		{"a.py", "# x"},
		{"a.sh", "# x"},
		{"a.bash", "# x"},
		{"a.zsh", "# x"},
		{"a.rb", "# x"},
		{"a.yaml", "# x"},
		{"a.yml", "# x"},
		{"a.toml", "# x"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()
			e := openNamed(t, tt.file, "x")
			run(t, e, "toggle_comment")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToggleCommentIgnoresTheLanguageName(t *testing.T) {
	t.Parallel()
	e := openNamed(t, "main.go", "x")
	e.SetLanguage("python")
	run(t, e, "toggle_comment")
	if got := text(e); got != "// x" {
		t.Fatalf("text = %q, want %q", got, "// x")
	}
}

func TestToggleCommentCaretRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		file       string
		text       string
		caret      buffer.Pos
		wantText   string
		wantCursor buffer.Pos
	}{
		{
			name:       "caret at column 0 moves past the new prefix",
			file:       "main.go",
			text:       "foo",
			caret:      buffer.Pos{Line: 0, Col: 0},
			wantText:   "// foo",
			wantCursor: buffer.Pos{Line: 0, Col: 3},
		},
		{
			name:       "caret in the text moves with it",
			file:       "main.go",
			text:       "foo",
			caret:      buffer.Pos{Line: 0, Col: 1},
			wantText:   "// foo",
			wantCursor: buffer.Pos{Line: 0, Col: 4},
		},
		{
			name:       "python uses hash",
			file:       "script.py",
			text:       "def main():",
			caret:      buffer.Pos{Line: 0, Col: 0},
			wantText:   "# def main():",
			wantCursor: buffer.Pos{Line: 0, Col: 2},
		},
		{
			name:       "uncomment removes the token and its space",
			file:       "main.go",
			text:       "// foo",
			caret:      buffer.Pos{Line: 0, Col: 4},
			wantText:   "foo",
			wantCursor: buffer.Pos{Line: 0, Col: 1},
		},
		{
			name:       "uncomment accepts a token with no space",
			file:       "main.go",
			text:       "//foo",
			caret:      buffer.Pos{Line: 0, Col: 0},
			wantText:   "foo",
			wantCursor: buffer.Pos{Line: 0, Col: 0},
		},
		{
			name:       "uncomment after indentation keeps the indentation",
			file:       "main.go",
			text:       "  // foo",
			caret:      buffer.Pos{Line: 0, Col: 6},
			wantText:   "  foo",
			wantCursor: buffer.Pos{Line: 0, Col: 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := openNamed(t, tt.file, tt.text)
			e.SetCursor(tt.caret, false)
			run(t, e, "toggle_comment")
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := e.Active().Cursor; got != tt.wantCursor {
				t.Fatalf("caret = %+v, want %+v", got, tt.wantCursor)
			}
		})
	}
}

func TestToggleCommentSelectionRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		sel      buffer.Selection
		wantText string
		wantSel  buffer.Selection
	}{
		{
			name:     "comments the selected lines and keeps the selection",
			text:     "line1\nline2\nline3\nline4",
			sel:      buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2}},
			wantText: "// line1\n// line2\nline3\nline4",
			wantSel:  buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2}},
		},
		{
			name:     "uncomments when every selected line is commented",
			text:     "// line1\n// line2\n// line3\nline4",
			sel:      buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2}},
			wantText: "line1\nline2\n// line3\nline4",
			wantSel:  buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2}},
		},
		{
			name:     "comments every line when only some are commented",
			text:     "// a\nb",
			sel:      buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 1, Col: 1}},
			wantText: "// // a\n// b",
			wantSel:  buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 1, Col: 4}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := openNamed(t, "main.go", tt.text)
			e.SelectRange(tt.sel.Anchor, tt.sel.Head)
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

func TestToggleCommentWithoutACommentTokenFails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string // empty for an untitled buffer
		want string
	}{
		{"untitled buffer", "", "toggle_comment: no comment token for untitled"},
		{"txt file", "notes.txt", "toggle_comment: no comment token for .txt"},
		{"unknown extension", "data.xyz", "toggle_comment: no comment token for .xyz"},
		{"file with no extension", "Makefile", "toggle_comment: no comment token for Makefile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, "x")
			if tt.file != "" {
				e = openNamed(t, tt.file, "x")
			}
			r := e.Do("toggle_comment", nil)
			if r.Err == nil || r.Err.Error() != tt.want {
				t.Fatalf("Do(toggle_comment) error = %v, want %q", r.Err, tt.want)
			}
			if got := text(e); got != "x" {
				t.Fatalf("text = %q, want it unchanged", got)
			}
		})
	}
}
