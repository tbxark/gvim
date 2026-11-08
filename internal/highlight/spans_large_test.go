package highlight

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/syntax"
)

const (
	spanFuncs = 30_000 // lines of the generated Go file
	rawAt     = 20_000 // the line where a raw string starts
	rawLen    = 300    // the lines the raw string spans
)

// goFile writes a Go file of spanFuncs lines (about 800 KiB), with a raw string that
// starts at line rawAt and spans rawLen lines. The line extra, when it is not negative,
// is replaced by a line of extraLen bytes.
func goFile(t *testing.T, dir string, extra, extraLen int) string {
	t.Helper()
	var sb strings.Builder
	for i := range spanFuncs {
		switch {
		case i == extra:
			sb.WriteString("var long = \"" + strings.Repeat("y", extraLen) + "\"\n")
		case i == rawAt:
			sb.WriteString("var raw = `\n")
		case i > rawAt && i < rawAt+rawLen:
			sb.WriteString("    x := 1\n")
		case i == rawAt+rawLen:
			sb.WriteString("`\n")
		default:
			fmt.Fprintf(&sb, "func f%d() int { return %d }\n", i, i)
		}
	}
	path := filepath.Join(dir, "big.go")
	apptest.Must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	return path
}

// spanAdapter opens path as a large document and returns the editor with a fresh
// cache for it.
func spanAdapter(t *testing.T, path string) (*editor.Editor, *Cache) {
	t.Helper()
	return apptest.OpenBig(t, path), New()
}

// hasKind reports whether a span of kind covers the whole of text in spans.
func hasKind(spans []syntax.Span, kind syntax.Kind) bool {
	for _, s := range spans {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

func TestCacheLargeNeverBuildsFullText(t *testing.T) {
	t.Parallel()
	ed, c := spanAdapter(t, goFile(t, t.TempDir(), -1, 0))
	doc := ed.Active()
	b := doc.Buffer
	before := b.Stats()
	const top, rows = 15_000, 50
	for i := top; i < top+rows; i++ {
		if !hasKind(c.LineSpans(doc, i), syntax.KindKeyword) {
			t.Fatalf("line %d has no keyword span: %v", i, c.LineSpans(doc, i))
		}
	}
	st := b.Stats()
	lexed := st.LineCalls - before.LineCalls
	// The window is the lines of one screen and its margins, and the context above it.
	if lexed > 1500 || lexed < rows {
		t.Errorf("lexing a frame decoded %d lines, want between %d and 1500 of %d", lexed, rows, spanFuncs)
	}
	if st.OverCap != 0 || st.MaterializedBytes-before.MaterializedBytes > 100_000 {
		t.Errorf("the frame materialized %d bytes (over cap %d)", st.MaterializedBytes-before.MaterializedBytes, st.OverCap)
	}
	// A line outside any window the frame needed has no spans until it is asked for.
	if got := c.LineSpans(doc, -1); got != nil {
		t.Errorf("LineSpans(-1) = %v", got)
	}
	if got := c.LineSpans(doc, spanFuncs+5); got != nil {
		t.Errorf("LineSpans past the end = %v", got)
	}
}

func TestCacheCursorMoveDoesNotRelex(t *testing.T) {
	t.Parallel()
	ed, c := spanAdapter(t, goFile(t, t.TempDir(), -1, 0))
	doc := ed.Active()
	b := doc.Buffer
	ed.Do("goto_line", map[string]any{"line": 15000})
	c.LineSpans(doc, 15_000)
	lexed := 0 // the lines the spans cost, apart from what the actions read
	frame := func() {
		before := b.Stats().LineCalls
		for i := 14_990; i < 15_060; i++ {
			c.LineSpans(doc, i)
		}
		lexed += int(b.Stats().LineCalls - before)
	}
	for range 5 {
		ed.Do("move_down", nil)
		ed.Do("move_right", nil)
		frame()
	}
	if lexed != 0 {
		t.Errorf("moving the caret made the spans decode %d lines", lexed)
	}
	ed.Do("insert_char", map[string]any{"text": "z"})
	frame()
	if lexed == 0 {
		t.Errorf("a typed character did not lex the window again")
	}
}

func TestSpanWindowsServeScrollingAndPanes(t *testing.T) {
	t.Parallel()
	ed, c := spanAdapter(t, goFile(t, t.TempDir(), -1, 0))
	doc := ed.Active()
	b := doc.Buffer
	c.LineSpans(doc, 10_000)
	one := b.Stats().LineCalls
	// Scrolling a page down and a few lines up stays in the window.
	for _, i := range []int{10_001, 10_040, 10_150, 9_950} {
		c.LineSpans(doc, i)
	}
	if got := b.Stats().LineCalls; got != one {
		t.Errorf("scrolling inside the window decoded %d lines", got-one)
	}
	// A second pane far away has its own window, and the first keeps its own.
	c.LineSpans(doc, 2_000)
	two := b.Stats().LineCalls
	if two == one {
		t.Fatal("a far line did not get a window")
	}
	c.LineSpans(doc, 10_020)
	c.LineSpans(doc, 2_010)
	if got := b.Stats().LineCalls; got != two {
		t.Errorf("two panes lexed each other's lines again (%d lines)", got-two)
	}
	// Scrolling past the window moves it.
	c.LineSpans(doc, 10_000+windowAhead+1)
	if got := b.Stats().LineCalls; got == two {
		t.Errorf("a line past the window was answered without lexing")
	}
}

func TestCacheLongLinesArePlainAndUnread(t *testing.T) {
	t.Parallel()
	const long = 15_000
	ed, c := spanAdapter(t, goFile(t, t.TempDir(), long, 200_000))
	doc := ed.Active()
	b := doc.Buffer
	before := b.Stats()
	if got := c.LineSpans(doc, long); got != nil {
		t.Errorf("a line of 200 KB has spans %v, want none", got)
	}
	if !hasKind(c.LineSpans(doc, long+1), syntax.KindKeyword) || !hasKind(c.LineSpans(doc, long-1), syntax.KindKeyword) {
		t.Errorf("the lines around the long line are not highlighted")
	}
	if got := b.Stats().MaterializedBytes - before.MaterializedBytes; got > 100_000 {
		t.Errorf("the long line was read: %d bytes materialized", got)
	}
}

func TestCacheContextComesFromTheConfig(t *testing.T) {
	t.Parallel()
	const inside = rawAt + 250
	tests := []struct {
		name    string
		context int // -1 leaves the default
		want    bool
	}{
		{"default context reaches the opening", -1, true},
		{"configured context reaches the opening", 20_000, true},
		{"no context does not", 0, false},
	}
	path := goFile(t, t.TempDir(), -1, 0)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed, c := spanAdapter(t, path)
			doc := ed.Active()
			if tt.context >= 0 {
				c.SetContext(tt.context)
			}
			got := hasKind(c.LineSpans(doc, inside), syntax.KindString)
			if got != tt.want {
				t.Errorf("line %d inside the raw string: string span = %v, want %v (spans %v)", inside, got, tt.want, c.LineSpans(doc, inside))
			}
		})
	}
}

func TestCacheOtherTabsShareTheContextSetting(t *testing.T) {
	t.Parallel()
	c := &Cache{}
	c.SetContext(1234)
	if f := c.Fresh(); !f.contextSet || f.contextBytes != 1234 {
		t.Errorf("fresh cache = %+v, want the context of the shared cache", f)
	}
	var none *Cache
	if f := none.Fresh(); f == nil || f.contextSet {
		t.Errorf("fresh of a nil cache = %+v", f)
	}
	none.SetContext(5) // ignored, no panic
}

func TestCacheLexerComesFromTheHeadOfTheText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env python3\n")
	for i := range 40_000 {
		fmt.Fprintf(&sb, "def f%d(): return %d\n", i, i)
	}
	path := filepath.Join(dir, "script")
	apptest.Must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	ed, c := spanAdapter(t, path)
	doc := ed.Active()
	if !hasKind(c.LineSpans(doc, 30_000), syntax.KindKeyword) {
		t.Errorf("the shebang did not choose the Python lexer: %v", c.LineSpans(doc, 30_000))
	}
	if st := doc.Buffer.Stats(); st.OverCap != 0 || st.MaterializedBytes > 200_000 {
		t.Errorf("choosing the lexer read too much: %+v", st)
	}
}

func TestCacheOfAPlainLargeDocumentReadsNoLines(t *testing.T) {
	t.Parallel()
	ed, c := spanAdapter(t, apptest.BigLines(t, t.TempDir()))
	doc := ed.Active()
	c.LineSpans(doc, 0) // the first call reads the head of the text to choose the lexer
	before := doc.Buffer.Stats()
	for i := range 100 {
		if got := c.LineSpans(doc, i); got != nil {
			t.Fatalf("LineSpans(%d) = %v for a plain text file", i, got)
		}
	}
	if got := doc.Buffer.Stats().LineCalls - before.LineCalls; got != 0 {
		t.Errorf("a plain document decoded %d lines for its spans", got)
	}
}
