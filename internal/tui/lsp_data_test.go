package tui

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// lspPane builds the pane the root paints the active document with, hooks
// included.
func lspPane(lt *lspTest) editorpane.Pane {
	lt.t.Helper()
	return lt.m.paneGeometry(lt.m.layout().FocusedPane(), lt.m.ed.Active())
}

// lspRender renders the active document through its pane.
func lspRender(lt *lspTest, width, height int) *grid.Grid {
	lt.t.Helper()
	return lspPane(lt).Render(width, height)
}

// lspRowText returns the text of grid row y.
func lspRowText(g *grid.Grid, y int) string {
	var b strings.Builder
	for x := range g.Width() {
		b.WriteString(g.At(x, y).Text)
	}
	return b.String()
}

// TestInlayHintsSpliceInlineAtThePosition checks the tier-2 rendering: the hint cells
// are inserted at the protocol column inside the line, faint, and never part of the
// document; turning the hints off drops them from the next frame.
func TestInlayHintsSpliceInlineAtThePosition(t *testing.T) {
	port := newFakeLSP("go")
	tab := &lspTab{path: "/p/a.go", lines: []string{"foo(bar)"}}
	port.hints = []InlayHint{{Line: 0, Col: 4, Text: "x:"}}
	lt := newLSPTest(t, port, tab)
	lt.open()
	// The hints start on with the default view options, so the open refreshes them.
	lt.settle(lt.m.lspDataArm(lspDataHints), 3)
	if !lt.m.lsp.inlay || len(lt.m.lsp.hints["/p/a.go"]) != 1 {
		t.Fatalf("inlay hints = %v, inlay %v", lt.m.lsp.hints, lt.m.lsp.inlay)
	}
	g := lspRender(lt, 40, 6)
	row := lspRowText(g, 0)
	if !strings.Contains(row, "foo(x: bar)") {
		t.Fatalf("row = %q, want the hint spliced before b", row)
	}
	x := strings.Index(row, "x: ")
	if !g.At(x, 0).Style.Faint {
		t.Fatalf("hint cell is not faint: %+v", g.At(x, 0).Style)
	}
	// The document keeps its text: the hint cells are display-only.
	if got := tab.lines[0]; got != "foo(bar)" {
		t.Fatalf("line = %q, want the text unchanged", got)
	}

	// Turning the hints off drops them from the next frame.
	lt.runMsgs(lt.act("toggle_inlay_hints"))
	if row := lspRowText(lspRender(lt, 40, 6), 0); strings.Contains(row, "x: ") {
		t.Fatalf("row after turning hints off = %q", row)
	}
}

// TestInlayHintCellsSpacing checks Fresh's spacing: a hint inside a line is followed by
// a space, one at the line's end is wrapped in spaces, and protocol padding adds a space
// on the named side.
func TestInlayHintCellsSpacing(t *testing.T) {
	tests := []struct {
		name string
		hint InlayHint
		line int
		want string
	}{
		{"inside", InlayHint{Col: 1, Text: "x:"}, 5, "x: "},
		{"at the end", InlayHint{Col: 5, Text: "int"}, 5, " int "},
		{"padding left", InlayHint{Col: 1, Text: "x", PaddingLeft: true}, 5, " x "},
		{"padding right", InlayHint{Col: 1, Text: "x", PaddingRight: true}, 5, "x  "},
		{"padding both at the end", InlayHint{Col: 5, Text: "x", PaddingLeft: true, PaddingRight: true}, 5, "  x  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lspInlayHintCells(tt.hint, tt.line); got != tt.want {
				t.Fatalf("cells = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestInlayHintsFallBackToTheEndOfLineInPageView checks the fallback: the page view does
// not splice hint cells, so the hint is painted after the line, still faint.
func TestInlayHintsFallBackToTheEndOfLineInPageView(t *testing.T) {
	port := newFakeLSP("go")
	port.hints = []InlayHint{{Line: 0, Col: 4, Text: "x:"}}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"foo(bar)"}})
	lt.open()
	lt.settle(lt.m.lspDataArm(lspDataHints), 3)
	doc := lt.m.ed.Active()
	pane := lt.m.lspData(editorpane.Pane{Doc: doc, PageView: editorpane.PageView{On: true, Measure: 12}}, doc)
	g := pane.Render(40, 4)
	row := lspRowText(g, 0)
	if !strings.Contains(row, "foo(bar)") || !strings.Contains(row, "x:") {
		t.Fatalf("row = %q, want the text and the end-of-line hint", row)
	}
	if strings.Contains(row, "foo(x:bar)") || strings.Contains(row, "foo(x: bar)") {
		t.Fatalf("row = %q, the page view must not splice the hint", row)
	}
}

// TestInlayHintsStartFromTheViewOptions: the Toggle Inlay Hints state starts from
// editor.enable_inlay_hints (default true), not from off, so a server that serves
// hints paints them without the user toggling first.
func TestInlayHintsStartFromTheViewOptions(t *testing.T) {
	build := func(view *ViewOptions) *Model {
		ed := &lspEd{tabs: []*lspTab{{path: "/p/a.go", lines: []string{"let x = 1"}}}}
		return New(Deps{Editor: ed, Theme: theme.Theme{}, Clock: testClock, View: view})
	}
	// Nil View is the defaults, whose enable_inlay_hints is true.
	if m := build(nil); !m.lsp.inlay {
		t.Fatal("inlay hints start off with the default view options, want on")
	}
	view := DefaultViewOptions()
	view.EnableInlayHints = true
	if m := build(&view); !m.lsp.inlay {
		t.Fatal("inlay hints start off with enable_inlay_hints true")
	}
	view.EnableInlayHints = false
	if m := build(&view); m.lsp.inlay {
		t.Fatal("inlay hints start on with enable_inlay_hints false")
	}
}

// testError is a plain error for the tests.
type testError struct{ text string }

func (e *testError) Error() string { return e.text }

// TestInlayHintErrorStopsTheRefresh checks a server that does not serve inlay
// hints: the toggle reports the reason and the refreshes stop.
func TestInlayHintErrorStopsTheRefresh(t *testing.T) {
	port := newFakeLSP("go")
	port.dataErr = &testError{"no server serves inlay_hints"}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"let x = 1"}})
	lt.open()
	// The hints start on, so the open request fails once and the reason is kept.
	if !strings.Contains(lt.m.notice, "inlay_hints") {
		t.Fatalf("notice = %q, want the port's reason", lt.m.notice)
	}
	if got := port.hintCalls; got != 1 {
		t.Fatalf("hint requests = %d, want one", got)
	}
	// A second refresh for the language does not ask again.
	lt.drain(lt.m.lspDataArm(lspDataHints))
	if port.hintCalls != 1 {
		t.Fatalf("hint requests = %d after the failure, want still one", port.hintCalls)
	}
}

// TestSemanticTokensPaintStyledSpans checks the semantic-token overlay: the token
// range takes the syntax color of its type, the text outside it does not, and the
// document text is untouched.
func TestSemanticTokensPaintStyledSpans(t *testing.T) {
	port := newFakeLSP("go")
	tab := &lspTab{path: "/p/a.go", lines: []string{"func main"}}
	lt := newLSPTest(t, port, tab)
	lt.open()
	port.tokens = []SemanticToken{{Line: 0, Start: 0, End: 4, Type: "keyword"}}
	lt.drain(lt.m.lspDataArm(lspDataTokens))
	if port.tokenCalls == 0 {
		t.Fatal("no semantic token request")
	}
	keyword := color.RGBA{R: 0, G: 120, B: 200, A: 255}
	lt.m.panes.Syntax[syntax.KindKeyword] = grid.Style{FG: keyword}
	g := lspRender(lt, 30, 6)
	row := lspRowText(g, 0)
	off := strings.Index(row, "func main")
	if off < 0 {
		t.Fatalf("row = %q, want the text", row)
	}
	want := lt.m.panes.Syntax[syntax.KindKeyword]
	if got := g.At(off, 0).Style; got.FG != want.FG {
		t.Fatalf("keyword cell FG = %v, want the syntax keyword %v", got.FG, want.FG)
	}
	if got := g.At(off+5, 0).Style; got.FG == want.FG {
		t.Fatal("the text outside the token took the token color")
	}
	if got := tab.lines[0]; got != "func main" {
		t.Fatalf("line = %q, want the text unchanged", got)
	}
}

// TestSemanticTokenModifiers checks the modifier mapping: deprecated takes the
// warning color, declaration is bold, readonly underlined, async italic.
func TestSemanticTokenModifiers(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"x"}})
	lt.open()
	warning := color.RGBA{R: 200, G: 100, B: 0, A: 255}
	lt.m.panes.DiagWarning.FG = warning
	tests := []struct {
		name string
		mods []string
		want func(grid.Style) bool
	}{
		{"deprecated", []string{"deprecated"}, func(s grid.Style) bool { return s.FG == warning }},
		{"declaration", []string{"declaration"}, func(s grid.Style) bool { return s.Bold }},
		{"readonly", []string{"readonly"}, func(s grid.Style) bool { return s.Underline }},
		{"async", []string{"async"}, func(s grid.Style) bool { return s.Italic }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := lt.m.semanticTokenStyle(SemanticToken{Type: "variable", Modifiers: tt.mods})
			if !tt.want(st) {
				t.Fatalf("style = %+v does not have the modifier's face", st)
			}
		})
	}
}

// TestFoldingRangesReachTheEditor checks the fold refresh: the server's ranges are
// pushed into the editor as fold.Range, where they merge with indentation folds.
func TestFoldingRangesReachTheEditor(t *testing.T) {
	port := newFakeLSP("go")
	tab := &lspTab{path: "/p/a.go", lines: []string{"func a() {", "  x", "}"}}
	lt := newLSPTest(t, port, tab)
	lt.open()
	port.folds = []FoldRange{{StartLine: 0, EndLine: 2}}
	lt.drain(lt.m.lspDataArm(lspDataFolds))
	if got := port.foldCalls; got == 0 {
		t.Fatal("no folding range request")
	}
	want := []fold.Range{{Start: 0, End: 2}}
	if got := lt.ed.externalFolds["/p/a.go"]; !slices.Equal(got, want) {
		t.Fatalf("external folds = %v, want %v", got, want)
	}
}

// TestFoldRequestStopsAfterAFailure checks a server without folding ranges: the
// language's refresh stops instead of asking on every edit.
func TestFoldRequestStopsAfterAFailure(t *testing.T) {
	port := newFakeLSP("go")
	port.dataErr = &testError{"no server serves folding_range"}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"a"}})
	lt.open()
	lt.drain(lt.m.lspDataArm(lspDataFolds))
	first := port.foldCalls
	lt.drain(lt.m.lspDataArm(lspDataFolds))
	if port.foldCalls != first {
		t.Fatalf("fold requests = %d after the failure, want %d", port.foldCalls, first)
	}
}
