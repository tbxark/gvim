package style

import (
	"image/color"
	"testing"

	"github.com/tbxark/gvim/theme"
)

func rgba(r, g, b uint8) color.Color { return color.RGBA{R: r, G: g, B: b, A: 255} }

func TestDecorReadsTheThemeKeys(t *testing.T) {
	d := New(loadDark(t)).Decor
	tests := []struct {
		name string
		got  color.Color
		want color.Color
	}{
		{"indent_rainbow_1", Cell(d.IndentRainbow[0]).FG, rgba(88, 166, 255)},
		{"indent_rainbow_6", Cell(d.IndentRainbow[5]).FG, rgba(188, 140, 255)},
		{"bracket_rainbow_2", Cell(d.BracketRainbow[1]).FG, rgba(255, 123, 114)},
		{"bracket_match_fg", Cell(d.BracketMatch).FG, rgba(121, 192, 255)},
		{"whitespace_indicator_selected_fg", Cell(d.WhitespaceSelected).FG, rgba(110, 118, 129)},
		// dark.json has no indentation_guide_fg: the guide takes the whitespace color.
		{"indentation_guide_fg falls back", Cell(d.IndentGuide).FG, rgba(72, 79, 88)},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	if pair := Cell(d.BracketPair); !pair.Bold || !pair.Underline {
		t.Errorf("the default pair attributes = %+v, want bold and underlined", pair)
	}
}

func TestDecorUsesAnExplicitGuideColor(t *testing.T) {
	th, err := theme.Load("gruvbox")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Cell(New(th).Decor.IndentGuide).FG, rgba(80, 73, 69); got != want {
		t.Errorf("indentation_guide_fg = %v, want %v", got, want)
	}
}

func TestPartialThemeUsesTheBaseNotTheFallbackChain(t *testing.T) {
	th, err := theme.Parse([]byte(`{
		"editor": {"whitespace_indicator_fg": [1, 2, 3], "cursor": [4, 5, 6], "bracket_rainbow_match_modifier": ["italic"]},
		"syntax": {"keyword": [10, 0, 0], "variable": [60, 0, 0]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	d := New(th).Decor
	tests := []struct {
		name string
		got  color.Color
		want color.Color
	}{
		{"the override wins", Cell(d.IndentRainbow[0]).FG, rgba(88, 166, 255)},
		{"guide is the background base's", Cell(d.IndentGuide).FG, rgba(72, 79, 88)},
		{"rainbow 4 is the base's", Cell(d.IndentRainbow[3]).FG, rgba(63, 185, 80)},
		{"selected whitespace is the base's", Cell(d.WhitespaceSelected).FG, rgba(110, 118, 129)},
		{"bracket match is the base's", Cell(d.BracketMatch).FG, rgba(121, 192, 255)},
		// The base names the bracket rainbow colors itself, so the syntax
		// overrides do not reach them.
		{"bracket rainbow 1 is the base's", Cell(d.BracketRainbow[0]).FG, rgba(88, 166, 255)},
		{"bracket rainbow 6 is the base's", Cell(d.BracketRainbow[5]).FG, rgba(188, 140, 255)},
		{"bracket rainbow 2 is the base's function", Cell(d.BracketRainbow[1]).FG, rgba(255, 123, 114)},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	if pair := Cell(d.BracketPair); !pair.Italic || pair.Bold || pair.Underline {
		t.Errorf("the pair attributes = %+v, want only the theme's italic", pair)
	}
}
