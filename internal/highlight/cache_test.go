package highlight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/syntax"
)

// openGo opens a Go file with the text in a new editor and returns the editor and
// a fresh cache for it.
func openGo(t *testing.T, text string) (*editor.Editor, *Cache) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
	apptest.Must(t, os.WriteFile(path, []byte(text), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(path))
	t.Cleanup(ed.Close)
	return ed, New()
}

// A cursor move or a scroll does not change the text, so the spans of the whole
// document are not lexed again.
func TestCacheKeepsSpansAcrossCursorMoves(t *testing.T) {
	ed, c := openGo(t, "package main\n\nfunc main() {}\n")
	first := c.Lines(ed.Active())
	if len(first) == 0 {
		t.Fatal("no spans")
	}
	for _, act := range []editor.Action{"move_down", "move_right", "move_end", "page_down", "move_doc_end"} {
		ed.Do(act, nil)
		got := c.Lines(ed.Active())
		if &got[0] != &first[0] {
			t.Fatalf("%s lexed the document again", act)
		}
	}
	ed.Do("insert_char", map[string]any{"text": "x"})
	if got := c.Lines(ed.Active()); &got[0] == &first[0] {
		t.Fatal("an edit did not lex the document again")
	}
}

// A document too big to lex per keystroke is highlighted through windows, so an
// edit lexes the lines around the viewport and not the whole text.
func TestMediumDocumentSpansUseWindows(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	for sb.Len() < 2*windowedBytes {
		sb.WriteString("func f() { return }\n")
	}
	ed, c := openGo(t, sb.String())
	ed.Do("move_doc_end", nil)
	ed.Do("insert_char", map[string]any{"text": "x"})
	doc := ed.Active()
	last := doc.Buffer.Lines() - 1
	spans := c.LineSpans(doc, last-1)
	has := false
	for _, sp := range spans {
		if sp.Kind == syntax.KindKeyword {
			has = true
		}
	}
	if !has {
		t.Fatalf("the window gave no keyword span: %v", spans)
	}
	if c.valid {
		t.Fatal("the whole document was lexed")
	}
	if n := len(c.large.wins); n == 0 {
		t.Fatal("no window was lexed")
	}
}

// A line longer than maxLexedLineBytes is not lexed: it has no spans, and the lines around
// it keep theirs. Typing a long paragraph would lex it on every key press otherwise.
func TestCacheDoesNotLexVeryLongLines(t *testing.T) {
	tests := []struct {
		name string
		pad  int // bytes of filler lines, to make the document a big one
	}{
		{"whole document", 0},
		{"windowed document", 2 * windowedBytes},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder
			sb.WriteString("package main\n")
			sb.WriteString("var long = \"" + strings.Repeat("x", maxLexedLineBytes) + "\"\n")
			sb.WriteString("var short = 1\n")
			for sb.Len() < tc.pad {
				sb.WriteString("var pad = 2\n")
			}
			ed, c := openGo(t, sb.String())
			doc := ed.Active()
			if len(c.LineSpans(doc, 1)) != 0 {
				t.Errorf("the line of %d bytes has spans: %v", maxLexedLineBytes+13, c.LineSpans(doc, 1))
			}
			if len(c.LineSpans(doc, 0)) == 0 || len(c.LineSpans(doc, 2)) == 0 {
				t.Errorf("a short line has no spans: %v, %v", c.LineSpans(doc, 0), c.LineSpans(doc, 2))
			}
		})
	}
}
