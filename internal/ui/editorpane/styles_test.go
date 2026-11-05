package editorpane

import (
	"image/color"
	"testing"

	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/theme"
)

func TestNewStylesKeepsTheTextBackgroundOnEverySyntaxKind(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	st := NewStyles(th)
	bg := color.RGBA{R: 30, G: 30, B: 30, A: 255}
	if st.Text.BG != bg {
		t.Fatalf("text bg = %v, want editor bg", st.Text.BG)
	}
	for k := range st.Syntax {
		if st.Syntax[k].BG != bg {
			t.Errorf("kind %v bg = %v, want editor bg", syntax.Kind(k), st.Syntax[k].BG)
		}
	}
	if st.Selection.BG != (color.RGBA{R: 50, G: 50, B: 60, A: 255}) {
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
