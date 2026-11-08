package editorpane

import (
	"image/color"
	"testing"

	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
)

func TestNewStylesKeepsTheTextBackgroundOnEverySyntaxKind(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	st := NewStyles(th)
	bg := color.RGBA{R: 13, G: 17, B: 23, A: 255}
	if st.Text.BG != bg {
		t.Fatalf("text bg = %v, want editor bg", st.Text.BG)
	}
	for k := range st.Syntax {
		if st.Syntax[k].BG != bg {
			t.Errorf("kind %v bg = %v, want editor bg", syntax.Kind(k), st.Syntax[k].BG)
		}
	}
	if st.Selection.BG != (color.RGBA{R: 23, G: 51, B: 90, A: 255}) {
		t.Errorf("selection bg = %v", st.Selection.BG)
	}
}

func TestSyntaxForegroundRules(t *testing.T) {
	th, err := theme.Parse([]byte(`{
		"editor": {"fg": [10, 20, 30], "bg": [0, 0, 0]},
		"syntax": {
			"keyword": [1, 2, 3],
			"string": "Yellow",
			"comment": "Default"
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	st := NewStyles(th)
	editorFG := color.RGBA{R: 10, G: 20, B: 30, A: 255}
	cases := []struct {
		name string
		kind syntax.Kind
		want color.Color
	}{
		{name: "rgb value", kind: syntax.KindKeyword, want: color.RGBA{R: 1, G: 2, B: 3, A: 255}},
		{name: "named value falls back to the editor foreground", kind: syntax.KindString, want: editorFG},
		{name: "terminal default has no foreground", kind: syntax.KindComment, want: nil},
		{name: "missing key falls back to the editor foreground", kind: syntax.KindFunction, want: editorFG},
		{name: "plain text is the editor foreground", kind: syntax.KindPlain, want: editorFG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := st.Syntax[tc.kind].FG; got != tc.want {
				t.Errorf("fg = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestZeroStylesPaintWithTerminalDefaults(t *testing.T) {
	st := NewStyles(theme.Theme{})
	if st.Text.FG != nil || st.Text.BG != nil {
		t.Errorf("zero theme gave text colors %v %v", st.Text.FG, st.Text.BG)
	}
}

func TestNewStylesSearchMatchColors(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	st := NewStyles(th)
	if st.Match.BG != (color.RGBA{R: 82, G: 65, B: 27, A: 255}) {
		t.Errorf("match bg = %v, want the dark theme match color", st.Match.BG)
	}
	if st.CurrentMatch.BG != (color.RGBA{R: 227, G: 179, B: 65, A: 255}) {
		t.Errorf("current match bg = %v, want the dark theme current match color", st.CurrentMatch.BG)
	}
	if st.CurrentMatch.FG != (color.RGBA{R: 13, G: 17, B: 23, A: 255}) || !st.CurrentMatch.Bold {
		t.Errorf("current match = %+v, want black and bold", st.CurrentMatch)
	}
}

func TestNewStylesScrollbarGlyphColors(t *testing.T) {
	th, err := theme.Parse([]byte(`{
		"ui": {"scrollbar_thumb_fg": [255, 96, 255], "scrollbar_track_fg": [48, 47, 57]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	st := NewStyles(th)
	if want := (color.RGBA{R: 255, G: 96, B: 255, A: 255}); st.Thumb.FG != want {
		t.Errorf("thumb fg = %v, want %v", st.Thumb.FG, want)
	}
	if want := (color.RGBA{R: 48, G: 47, B: 57, A: 255}); st.Scrollbar.FG != want {
		t.Errorf("track fg = %v, want %v", st.Scrollbar.FG, want)
	}
}

func TestNewStylesReadsTheDisplayRoles(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	st := NewStyles(th)
	tests := []struct {
		name string
		got  grid.Style
		fg   any
		bg   any
	}{
		{"secondary caret", st.SecondaryCaret, color.RGBA{R: 230, G: 237, B: 243, A: 255}, color.RGBA{R: 72, G: 79, B: 88, A: 255}},
		{"current line", st.CurrentLine, nil, color.RGBA{R: 24, G: 28, B: 34, A: 255}},
		{"ruler", st.Ruler, nil, color.RGBA{R: 26, G: 30, B: 36, A: 255}},
		{"whitespace", st.Whitespace, color.RGBA{R: 72, G: 79, B: 88, A: 255}, color.RGBA{R: 13, G: 17, B: 23, A: 255}},
		{"occurrence", st.Occurrence, nil, color.RGBA{R: 39, G: 43, B: 49, A: 255}},
		{"error", st.DiagError, color.RGBA{R: 255, G: 123, B: 114, A: 255}, nil},
	}
	for _, tc := range tests {
		if tc.got.FG != tc.fg {
			t.Errorf("%s fg = %v, want %v", tc.name, tc.got.FG, tc.fg)
		}
		if tc.got.BG != tc.bg {
			t.Errorf("%s bg = %v, want %v", tc.name, tc.got.BG, tc.bg)
		}
	}
}

// A kind's attributes reach its style, and a theme without variable_builtin paints
// builtins with the variable colour.
func TestSyntaxStylesCarryAttributesAndFallbacks(t *testing.T) {
	th, err := theme.Parse([]byte(`{
		"editor": {"fg": [10, 20, 30], "bg": [0, 0, 0]},
		"syntax": {"variable": [4, 5, 6], "keyword": [1, 2, 3], "constant": [7, 8, 9]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	st := NewStyles(th)
	attrs := func(k syntax.Kind) [4]bool {
		s := st.Syntax[k]
		return [4]bool{s.Bold, s.Italic, s.Underline, s.Strike}
	}
	tests := []struct {
		kind syntax.Kind
		want [4]bool
	}{
		{syntax.KindHeading, [4]bool{true, false, false, false}},
		{syntax.KindStrong, [4]bool{true, false, false, false}},
		{syntax.KindEmphasis, [4]bool{false, true, false, false}},
		{syntax.KindStrongEmphasis, [4]bool{true, true, false, false}},
		{syntax.KindLink, [4]bool{false, false, true, false}},
		{syntax.KindStrike, [4]bool{false, false, false, true}},
		{syntax.KindKeyword, [4]bool{}},
		{syntax.KindPlain, [4]bool{}},
	}
	for _, tt := range tests {
		if got := attrs(tt.kind); got != tt.want {
			t.Errorf("%v attributes (bold, italic, underline, strike) = %v, want %v", tt.kind, got, tt.want)
		}
	}
	if got, want := st.Syntax[syntax.KindBuiltin].FG, (color.RGBA{R: 4, G: 5, B: 6, A: 255}); got != want {
		t.Errorf("builtin fg = %v, want the variable colour %v", got, want)
	}
	if got, want := st.Syntax[syntax.KindAttribute].FG, (color.RGBA{R: 7, G: 8, B: 9, A: 255}); got != want {
		t.Errorf("attribute fg = %v, want the constant colour %v", got, want)
	}
	if got, want := st.Syntax[syntax.KindHeading].FG, (color.RGBA{R: 1, G: 2, B: 3, A: 255}); got != want {
		t.Errorf("heading fg = %v, want the keyword colour %v", got, want)
	}
}
