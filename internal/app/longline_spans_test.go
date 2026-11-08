package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/syntax"
)

// A segment of a very long line has syntax colors, found by lexing the segment with a stretch of the
// text before it, so a token that began above it is not read from its middle.
func TestVeryLongLineSegmentsAreHighlighted(t *testing.T) {
	isolateConfig(t)
	// A string in the middle of the line, that began within the stretch of context before the
	// segment that holds its middle.
	line := strings.Repeat(`{"a":1},`, 2000) + `"` + strings.Repeat("s", 300) + `"` + strings.Repeat(`{"b":2},`, 50)
	rt := openLong(t, "a.json", line+"\n")
	d := rt.ed.Active()
	ad := editorAdapter{ed: rt.ed, spans: &spanCache{}, others: map[*editor.Document]*spanCache{}}
	da, ok := ad.Active().(documentAdapter)
	if !ok {
		t.Fatalf("Active is %T", ad.Active())
	}
	kindAt := func(col int) syntax.Kind {
		n := 600
		text := d.Buffer.LineSlice(0, col, n)
		for _, s := range da.LineSpansIn(0, col, text) {
			if s.Start == 0 {
				return s.Kind
			}
		}
		return syntax.KindPlain
	}
	// Near the start the line is {"a":1}: the key is lexed as a name, and the digit as a number.
	text := d.Buffer.LineSlice(0, 0, 40)
	spans := da.LineSpansIn(0, 0, text)
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
	for _, s := range da.LineSpansIn(0, 8000, text) {
		if s.Start != end || s.End <= s.Start || s.End > len(text) {
			t.Fatalf("span %+v after %d in a text of %d bytes", s, end, len(text))
		}
		end = s.End
	}
	if end != len(text) {
		t.Errorf("the spans end at %d, the text at %d", end, len(text))
	}
	// A plain text file has no spans.
	rt2 := openLong(t, "a.txt", line+"\n")
	ad2 := editorAdapter{ed: rt2.ed, spans: &spanCache{}, others: map[*editor.Document]*spanCache{}}
	if got := ad2.Active().(documentAdapter).LineSpansIn(0, 0, line[:50]); len(got) != 0 {
		t.Errorf("plain text has spans %v", got)
	}
}

func containsKind(kinds []syntax.Kind, k syntax.Kind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// With line wrap off, a line of tens of megabytes can be moved over to its real end, and the
// window of columns follows the caret there.
func TestHugeLineWithoutWrapScrollsToItsEnd(t *testing.T) {
	isolateConfig(t)
	const n = 12 << 20
	content := longText(n, "THE-TAIL-MARK")
	rt := openLong(t, "big.json", content)
	d := rt.ed.Active()
	if !d.Large() {
		t.Fatal("not large")
	}
	if err := rt.model.Run("toggle_line_wrap"); err != nil {
		t.Fatal(err)
	}
	screen := key(rt, tea.KeyPressMsg{Code: tea.KeyEnd})
	if got := d.Cursor; got.Line != 0 || got.Col != len(content) {
		t.Fatalf("End: caret %+v, want column %d, the real end of the line", got, len(content))
	}
	if !strings.Contains(screen, "THE-TAIL-MARK") {
		t.Errorf("the window does not show the end of the line:\n%s", screen)
	}
	screen = key(rt, tea.KeyPressMsg{Code: tea.KeyHome})
	if got := d.Cursor; got != (buffer.Pos{}) {
		t.Fatalf("Home: caret %+v", got)
	}
	if !strings.Contains(screen, `{"k":"v`) {
		t.Errorf("the window does not show the start of the line:\n%s", screen)
	}
}
