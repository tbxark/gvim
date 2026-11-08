package palette

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	model "github.com/tbxark/gvim/internal/palette"
)

// grepRow builds a grep row the way grepRows does, from one hit and the query.
func grepRow(h GrepHit, query string) row {
	return grepRows(GrepResultMsg{Text: query, Hits: []GrepHit{h}})[0]
}

// boldRunes returns the runes of label at the offsets hl.
func boldRunes(label string, hl []int) string {
	runes := []rune(label)
	var b strings.Builder
	for _, i := range hl {
		b.WriteRune(runes[i])
	}
	return b.String()
}

func TestFitGrep(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("filler words ", 12) // 156 cells before the match
	tests := []struct {
		name      string
		hit       GrepHit
		query     string
		width     int
		wantFull  string // the whole label, when it fits
		wantParts []string
		wantBold  string
	}{
		{
			name:     "fits whole",
			hit:      GrepHit{Rel: "docs/api.md", Line: 12, Col: 5, Text: "the limits here"},
			query:    "limits",
			width:    60,
			wantFull: "docs/api.md:12: the limits here",
			wantBold: "limits",
		},
		{
			name:      "long path keeps its base name and the line number",
			hit:       GrepHit{Rel: "internal/very/deep/nested/package/directory/layout.go", Line: 103, Col: 1, Text: "limits()"},
			query:     "limits",
			width:     40,
			wantParts: []string{"…/layout.go:103: limits()"},
			wantBold:  "limits",
		},
		{
			name:      "a match far into a long line stays visible",
			hit:       GrepHit{Rel: "README.md", Line: 137, Col: len([]rune(long)) + 5, Text: long + "the limits of it all and more text after"},
			query:     "limits",
			width:     50,
			wantParts: []string{"README.md:137: …", "limits"},
			wantBold:  "limits",
		},
	}
	for _, tt := range tests {
		r := grepRow(tt.hit, tt.query)
		got, hl := fitGrep(r, tt.width)
		if ansi.StringWidth(got) > tt.width {
			t.Errorf("%s: %q is %d cells, over %d", tt.name, got, ansi.StringWidth(got), tt.width)
		}
		if tt.wantFull != "" && got != tt.wantFull {
			t.Errorf("%s: label %q, want %q", tt.name, got, tt.wantFull)
		}
		for _, part := range tt.wantParts {
			if !strings.Contains(got, part) {
				t.Errorf("%s: label %q does not contain %q", tt.name, got, part)
			}
		}
		if b := boldRunes(got, hl); !strings.EqualFold(b, tt.wantBold) {
			t.Errorf("%s: bold runes %q in %q, want %q", tt.name, b, got, tt.wantBold)
		}
	}
}

func TestFitGrepNeverExceedsTheWidth(t *testing.T) {
	t.Parallel()
	r := grepRow(GrepHit{Rel: "a/b/c/d/e/f/g/h/long_file_name.go", Line: 99999, Col: 30, Text: "中文 text with a match 中文中文中文中文中文中文"}, "match")
	for width := 1; width <= 90; width++ {
		got, hl := fitGrep(r, width)
		if w := ansi.StringWidth(got); w > width {
			t.Fatalf("width %d: %q is %d cells", width, got, w)
		}
		for _, i := range hl {
			if i < 0 || i >= len([]rune(got)) {
				t.Fatalf("width %d: bold offset %d outside %q", width, i, got)
			}
		}
	}
}

func TestFitFileKeepsTheBaseNameAndTheMatchedRunes(t *testing.T) {
	t.Parallel()
	name := "internal/plugin/bundled/plugins/git_log/main.lua"
	// The runes of "main" are the matched ones.
	start := strings.Index(name, "main")
	hl := []int{start, start + 1, start + 2, start + 3}
	got, out := fitName(row{kind: rowFile, name: name, hl: hl}, 30)
	if !strings.HasSuffix(got, "/main.lua") || !strings.Contains(got, "…") {
		t.Fatalf("shortened path %q: want the middle cut and the base name kept", got)
	}
	if b := boldRunes(got, out); b != "main" {
		t.Errorf("bold runes %q in %q, want main", b, got)
	}
}

func TestCommandNamesUseTheWidthOfTheBox(t *testing.T) {
	t.Parallel()
	name := "Toggle Line Numbers For The Current Buffer Only Please"
	p := New(Options{Query: ">Toggle", Commands: []model.Entry{{Name: name, Action: "x", Keybinding: "Ctrl+Shift+L", Source: model.SourceBuiltin}}})
	g := draw(p, 160, 30)
	if !hasRow(g, name) {
		t.Fatalf("a %d-cell command name is cut in a 160-column frame:\n%s", len(name), g)
	}
	narrow := draw(New(Options{Query: ">Toggle", Commands: []model.Entry{{Name: name, Action: "x", Source: model.SourceBuiltin}}}), 50, 30)
	if !hasRow(narrow, "Toggle Line Numbers") || hasRow(narrow, name) {
		t.Errorf("a 50-column frame must cut the long name with an ellipsis:\n%s", narrow)
	}
}

func TestTheBoxKeepsAMarginFromTheEdgesAndPadsItsText(t *testing.T) {
	t.Parallel()
	p := New(commandOptions(">", nil))
	g := draw(p, 100, 30)
	geo := layoutFor(100, 30)
	if geo.left != 1 || geo.right != 98 {
		t.Fatalf("box columns %d..%d, want 1..98 in 100 columns", geo.left, geo.right)
	}
	y := geo.top + 1
	for _, x := range []int{0, 99} {
		if c := g.At(x, y).Text; c != " " {
			t.Errorf("margin cell (%d,%d) holds %q, want blank", x, y, c)
		}
	}
	// One blank cell separates the border from the name.
	if g.At(geo.left+1, y).Text != " " || g.At(geo.nameX, y).Text == " " {
		t.Errorf("row %q: want a padding cell before the name", rowText(g, y))
	}
	// A frame below 60 columns has no margin.
	if narrow := layoutFor(50, 30); narrow.left != 0 || narrow.right != 49 {
		t.Errorf("a 50-column box spans %d..%d, want the whole width", narrow.left, narrow.right)
	}
}

func TestClicksOnTheMarginAndBordersDoNotReachARow(t *testing.T) {
	t.Parallel()
	p := New(commandOptions(">", nil))
	draw(p, 100, 30)
	geo := layoutFor(100, 30)
	y := geo.top + 1
	for _, x := range []int{0, geo.left, geo.right, 99} {
		if i, ok := p.rowAt(x, y); ok {
			t.Errorf("rowAt(%d, %d) = row %d, want none", x, y, i)
		}
	}
	for _, x := range []int{geo.left + 1, 50, geo.right - 1} {
		if _, ok := p.rowAt(x, y); !ok {
			t.Errorf("rowAt(%d, %d) found no row", x, y)
		}
	}
}
