package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/syntax"
)

func openGoAdapter(t *testing.T, text string) editorAdapter {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	return editorAdapter{ed: ed, spans: &spanCache{}, others: map[*editor.Document]*spanCache{}}
}

// A cursor move or a scroll does not change the text, so the spans of the whole
// document are not lexed again.
func TestSpanCacheKeepsSpansAcrossCursorMoves(t *testing.T) {
	a := openGoAdapter(t, "package main\n\nfunc main() {}\n")
	first := a.spans.lines(a.ed.Active())
	if len(first) == 0 {
		t.Fatal("no spans")
	}
	for _, act := range []editor.Action{"move_down", "move_right", "move_end", "page_down", "move_doc_end"} {
		a.Do(act, nil)
		got := a.spans.lines(a.ed.Active())
		if &got[0] != &first[0] {
			t.Fatalf("%s lexed the document again", act)
		}
	}
	a.Do("insert_char", map[string]any{"text": "x"})
	if got := a.spans.lines(a.ed.Active()); &got[0] == &first[0] {
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
	a := openGoAdapter(t, sb.String())
	a.Do("move_doc_end", nil)
	a.Do("insert_char", map[string]any{"text": "x"})
	doc := a.Active()
	last := a.ed.Active().Buffer.Lines() - 1
	spans := doc.LineSpans(last - 1)
	has := false
	for _, sp := range spans {
		if sp.Kind == syntax.KindKeyword {
			has = true
		}
	}
	if !has {
		t.Fatalf("the window gave no keyword span: %v", spans)
	}
	if a.spans.valid {
		t.Fatal("the whole document was lexed")
	}
	if n := len(a.spans.large.wins); n == 0 {
		t.Fatal("no window was lexed")
	}
}

// A line longer than maxLexedLineBytes is not lexed: it has no spans, and the lines around
// it keep theirs. Typing a long paragraph would lex it on every key press otherwise.
func TestSpanCacheDoesNotLexVeryLongLines(t *testing.T) {
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
			a := openGoAdapter(t, sb.String())
			doc := a.Active()
			if len(doc.LineSpans(1)) != 0 {
				t.Errorf("the line of %d bytes has spans: %v", maxLexedLineBytes+13, doc.LineSpans(1))
			}
			if len(doc.LineSpans(0)) == 0 || len(doc.LineSpans(2)) == 0 {
				t.Errorf("a short line has no spans: %v, %v", doc.LineSpans(0), doc.LineSpans(2))
			}
		})
	}
}
