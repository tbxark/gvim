package editorpane

import (
	"image/color"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

func htmlStyles(t *testing.T) Styles {
	t.Helper()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	return NewStyles(th)
}

func TestSelectionHTMLColoursTheSelectedSyntax(t *testing.T) {
	st := htmlStyles(t)
	kw := cssColor(st.Syntax[syntax.KindKeyword].FG, "")
	if kw == "" || kw == cssColor(st.Text.FG, "") {
		t.Fatalf("setup: the keyword colour %q is not distinct from the text", kw)
	}
	doc := &fakeDoc{
		lines:    []string{"func a() {", "}"},
		selected: true,
		anchor:   buffer.Pos{Line: 0, Col: 0},
		head:     buffer.Pos{Line: 0, Col: 10},
		spans:    map[int][]syntax.Span{0: {{Start: 0, End: 4, Kind: syntax.KindKeyword}}},
	}
	got, ok := SelectionHTML(doc, st, "func a() {")
	if !ok {
		t.Fatal("no html")
	}
	want := `<span style="color:` + kw + `;`
	if !strings.Contains(got, want) || !strings.Contains(got, ">func</span> a() {</pre>") {
		t.Errorf("html = %s\nwant a span with %q around func", got, want)
	}
	if !strings.HasPrefix(got, "<pre style=\"background-color:"+cssColor(st.Text.BG, "")) {
		t.Errorf("html does not start with the editor background: %s", got)
	}
}

func TestSelectionHTMLPaintsDiffRows(t *testing.T) {
	st := htmlStyles(t)
	st.DiffAdd = grid.Style{BG: color.RGBA{G: 255, A: 255}}
	st.DiffRemove = grid.Style{BG: color.RGBA{R: 255, A: 255}}
	doc := &fakeDoc{
		lines:    []string{"+added", "-removed"},
		selected: true,
		anchor:   buffer.Pos{},
		head:     buffer.Pos{Line: 1, Col: 8},
		spans: map[int][]syntax.Span{
			0: {{Start: 0, End: 6, Kind: syntax.KindDiffAdd}},
			1: {{Start: 0, End: 8, Kind: syntax.KindDiffRemove}},
		},
	}
	got, ok := SelectionHTML(doc, st, "+added\n-removed")
	if !ok {
		t.Fatal("no html")
	}
	if !strings.Contains(got, "background-color:#00ff00") {
		t.Errorf("html has no added-row background: %s", got)
	}
	if !strings.Contains(got, "background-color:#ff0000") {
		t.Errorf("html has no removed-row background: %s", got)
	}
}

func TestSelectionHTMLEscapesAndFallsBackToPlain(t *testing.T) {
	st := htmlStyles(t)
	tests := []struct {
		name string
		doc  Document
		text string
		want string // a substring of the body
	}{
		{"escapes markup", nil, `a<b && "c"`, "a&lt;b &amp;&amp; &quot;c&quot;"},
		{"text that is not the selection is not coloured", &fakeDoc{lines: []string{"zzz"}, selected: true, head: buffer.Pos{Col: 3},
			spans: map[int][]syntax.Span{0: {{Start: 0, End: 3, Kind: syntax.KindKeyword}}}}, "other", ">other</pre>"},
	}
	for _, tt := range tests {
		got, ok := SelectionHTML(tt.doc, st, tt.text)
		if !ok || !strings.Contains(got, tt.want) {
			t.Errorf("%s: html %q ok %v, want it to hold %q", tt.name, got, ok, tt.want)
		}
		if strings.Contains(got, "<span") {
			t.Errorf("%s: coloured: %s", tt.name, got)
		}
	}
	if _, ok := SelectionHTML(nil, st, ""); ok {
		t.Error("empty text gave html")
	}
}
