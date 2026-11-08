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

// containsKind reports whether kinds holds k.
func containsKind(kinds []syntax.Kind, k syntax.Kind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// A segment of a very long line has syntax colors, found by lexing the segment with a stretch of the
// text before it, so a token that began above it is not read from its middle.
func TestVeryLongLineSegmentsAreHighlighted(t *testing.T) {
	apptest.IsolateConfig(t)
	// A string in the middle of the line, that began within the stretch of context before the
	// segment that holds its middle.
	line := strings.Repeat(`{"a":1},`, 2000) + `"` + strings.Repeat("s", 300) + `"` + strings.Repeat(`{"b":2},`, 50)
	path := filepath.Join(t.TempDir(), "a.json")
	apptest.Must(t, os.WriteFile(path, []byte(line+"\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(path))
	t.Cleanup(ed.Close)
	d := ed.Active()
	c := New()
	kindAt := func(col int) syntax.Kind {
		n := 600
		text := d.Buffer.LineSlice(0, col, n)
		for _, s := range c.Segment(d, 0, col, text) {
			if s.Start == 0 {
				return s.Kind
			}
		}
		return syntax.KindPlain
	}
	// Near the start the line is {"a":1}: the key is lexed as a name, and the digit as a number.
	text := d.Buffer.LineSlice(0, 0, 40)
	spans := c.Segment(d, 0, 0, text)
	var kinds []syntax.Kind
	for _, s := range spans {
		kinds = append(kinds, s.Kind)
	}
	if !containsKind(kinds, syntax.KindNumber) || !containsKind(kinds, syntax.KindKeyword) {
		t.Errorf("the spans of the first segment have kinds %v, want a key and a number", kinds)
	}
	// A segment that starts inside the string, 100 runes after its opening quote, has the string
	// as its first token: the lexer was given the quote.
	mid := 2000*len(`{"a":1},`) + 100
	if got := kindAt(mid); got != syntax.KindString {
		t.Errorf("the kind at the start of a segment inside the string is %v, want a string", got)
	}
	// The spans cover the text and nothing else, in bytes of it.
	text = d.Buffer.LineSlice(0, 8000, 700)
	end := 0
	for _, s := range c.Segment(d, 0, 8000, text) {
		if s.Start != end || s.End <= s.Start || s.End > len(text) {
			t.Fatalf("span %+v after %d in a text of %d bytes", s, end, len(text))
		}
		end = s.End
	}
	if end != len(text) {
		t.Errorf("the spans end at %d, the text at %d", end, len(text))
	}
	// A plain text file has no spans.
	path2 := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path2, []byte(line+"\n"), 0o644))
	ed2 := editor.New(editor.Options{})
	apptest.Must(t, ed2.OpenFile(path2))
	t.Cleanup(ed2.Close)
	if got := New().Segment(ed2.Active(), 0, 0, line[:50]); len(got) != 0 {
		t.Errorf("plain text has spans %v", got)
	}
}
