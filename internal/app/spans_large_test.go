package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// TestRootPaintsALargeGoFileThroughOneWindow runs the real adapter under the real root:
// a frame in the middle of a file of 30000 lines reads the lines of the window and of
// the rows, and the frames after it read the rows only.
func TestRootPaintsALargeGoFileThroughOneWindow(t *testing.T) {
	t.Parallel()
	ed := openBig(t, goFile(t, t.TempDir(), -1, 0))
	m := tui.New(newUIDeps(ed, keymap.Default(), theme.Theme{}, nil, nil))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	must(t, ed.GotoLine(15_001, 1))
	b := ed.Active().Buffer
	before := b.Stats().LineCalls
	first := ansi.Strip(m.View().Content)
	firstCalls := b.Stats().LineCalls - before
	if !strings.Contains(first, "func f15000() int") {
		t.Fatalf("the frame does not show line 15001:\n%s", first)
	}
	if firstCalls > 2000 {
		t.Errorf("the first frame decoded %d lines, want the window and the rows (under 2000 of %d)", firstCalls, spanFuncs)
	}
	before = b.Stats().LineCalls
	for range 3 {
		_ = m.View()
	}
	if per := (b.Stats().LineCalls - before) / 3; per > 300 {
		t.Errorf("a frame of the same text decoded %d lines, want the rows only", per)
	}
	if st := b.Stats(); st.OverCap != 0 {
		t.Errorf("painting went over the cap: %+v", st)
	}
}

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
	must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	return path
}

// spanAdapter opens path as a large document and returns the adapter of the editor with
// its document.
func spanAdapter(t *testing.T, path string) (editorAdapter, documentAdapter) {
	t.Helper()
	ed := openBig(t, path)
	a := editorAdapter{ed: ed, spans: &spanCache{}, others: map[*editor.Document]*spanCache{}}
	doc, ok := a.Active().(documentAdapter)
	if !ok {
		t.Fatalf("Active is %T", a.Active())
	}
	return a, doc
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

func TestSpanCacheLargeNeverBuildsFullText(t *testing.T) {
	t.Parallel()
	_, doc := spanAdapter(t, goFile(t, t.TempDir(), -1, 0))
	b := doc.doc.Buffer
	before := b.Stats()
	const top, rows = 15_000, 50
	for i := top; i < top+rows; i++ {
		if !hasKind(doc.LineSpans(i), syntax.KindKeyword) {
			t.Fatalf("line %d has no keyword span: %v", i, doc.LineSpans(i))
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
	if got := doc.LineSpans(-1); got != nil {
		t.Errorf("LineSpans(-1) = %v", got)
	}
	if got := doc.LineSpans(spanFuncs + 5); got != nil {
		t.Errorf("LineSpans past the end = %v", got)
	}
}

func TestSpanCacheCursorMoveDoesNotRelex(t *testing.T) {
	t.Parallel()
	a, doc := spanAdapter(t, goFile(t, t.TempDir(), -1, 0))
	b := doc.doc.Buffer
	a.Do("goto_line", map[string]any{"line": 15000})
	doc.LineSpans(15_000)
	lexed := 0 // the lines the spans cost, apart from what the actions read
	frame := func() {
		before := b.Stats().LineCalls
		for i := 14_990; i < 15_060; i++ {
			doc.LineSpans(i)
		}
		lexed += int(b.Stats().LineCalls - before)
	}
	for range 5 {
		a.Do("move_down", nil)
		a.Do("move_right", nil)
		frame()
	}
	if lexed != 0 {
		t.Errorf("moving the caret made the spans decode %d lines", lexed)
	}
	a.Do("insert_char", map[string]any{"text": "z"})
	frame()
	if lexed == 0 {
		t.Errorf("a typed character did not lex the window again")
	}
}

func TestSpanWindowsServeScrollingAndPanes(t *testing.T) {
	t.Parallel()
	_, doc := spanAdapter(t, goFile(t, t.TempDir(), -1, 0))
	b := doc.doc.Buffer
	doc.LineSpans(10_000)
	one := b.Stats().LineCalls
	// Scrolling a page down and a few lines up stays in the window.
	for _, i := range []int{10_001, 10_040, 10_150, 9_950} {
		doc.LineSpans(i)
	}
	if got := b.Stats().LineCalls; got != one {
		t.Errorf("scrolling inside the window decoded %d lines", got-one)
	}
	// A second pane far away has its own window, and the first keeps its own.
	doc.LineSpans(2_000)
	two := b.Stats().LineCalls
	if two == one {
		t.Fatal("a far line did not get a window")
	}
	doc.LineSpans(10_020)
	doc.LineSpans(2_010)
	if got := b.Stats().LineCalls; got != two {
		t.Errorf("two panes lexed each other's lines again (%d lines)", got-two)
	}
	// Scrolling past the window moves it.
	doc.LineSpans(10_000 + windowAhead + 1)
	if got := b.Stats().LineCalls; got == two {
		t.Errorf("a line past the window was answered without lexing")
	}
}

func TestSpanCacheLongLinesArePlainAndUnread(t *testing.T) {
	t.Parallel()
	const long = 15_000
	_, doc := spanAdapter(t, goFile(t, t.TempDir(), long, 200_000))
	b := doc.doc.Buffer
	before := b.Stats()
	if got := doc.LineSpans(long); got != nil {
		t.Errorf("a line of 200 KB has spans %v, want none", got)
	}
	if !hasKind(doc.LineSpans(long+1), syntax.KindKeyword) || !hasKind(doc.LineSpans(long-1), syntax.KindKeyword) {
		t.Errorf("the lines around the long line are not highlighted")
	}
	if got := b.Stats().MaterializedBytes - before.MaterializedBytes; got > 100_000 {
		t.Errorf("the long line was read: %d bytes materialized", got)
	}
}

func TestSpanCacheContextComesFromTheConfig(t *testing.T) {
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
			a, doc := spanAdapter(t, path)
			if tt.context >= 0 {
				a.spans.setContext(tt.context)
			}
			got := hasKind(doc.LineSpans(inside), syntax.KindString)
			if got != tt.want {
				t.Errorf("line %d inside the raw string: string span = %v, want %v (spans %v)", inside, got, tt.want, doc.LineSpans(inside))
			}
		})
	}
}

func TestSpanCacheOtherTabsShareTheContextSetting(t *testing.T) {
	t.Parallel()
	c := &spanCache{}
	c.setContext(1234)
	if f := c.fresh(); !f.contextSet || f.contextBytes != 1234 {
		t.Errorf("fresh cache = %+v, want the context of the shared cache", f)
	}
	var none *spanCache
	if f := none.fresh(); f == nil || f.contextSet {
		t.Errorf("fresh of a nil cache = %+v", f)
	}
	none.setContext(5) // ignored, no panic
}

func TestSpanCacheLexerComesFromTheHeadOfTheText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env python3\n")
	for i := range 40_000 {
		fmt.Fprintf(&sb, "def f%d(): return %d\n", i, i)
	}
	path := filepath.Join(dir, "script")
	must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	_, doc := spanAdapter(t, path)
	if !hasKind(doc.LineSpans(30_000), syntax.KindKeyword) {
		t.Errorf("the shebang did not choose the Python lexer: %v", doc.LineSpans(30_000))
	}
	if st := doc.doc.Buffer.Stats(); st.OverCap != 0 || st.MaterializedBytes > 200_000 {
		t.Errorf("choosing the lexer read too much: %+v", st)
	}
}

func TestSpanCacheOfAPlainLargeDocumentReadsNoLines(t *testing.T) {
	t.Parallel()
	_, doc := spanAdapter(t, bigLines(t, t.TempDir()))
	doc.LineSpans(0) // the first call reads the head of the text to choose the lexer
	before := doc.doc.Buffer.Stats()
	for i := range 100 {
		if got := doc.LineSpans(i); got != nil {
			t.Fatalf("LineSpans(%d) = %v for a plain text file", i, got)
		}
	}
	if got := doc.doc.Buffer.Stats().LineCalls - before.LineCalls; got != 0 {
		t.Errorf("a plain document decoded %d lines for its spans", got)
	}
}
