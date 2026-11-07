package style

import (
	"image/color"
	"testing"

	"github.com/tbxark/gvim/internal/decor"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
)

func TestResolveColor(t *testing.T) {
	dark := loadDark(t)
	tests := []struct {
		name string
		th   theme.Theme
		c    decor.Color
		want color.Color
	}{
		{"unset", dark, decor.Color{}, nil},
		{"literal", dark, decor.Color{R: 1, G: 2, B: 3, Set: true}, rgba(1, 2, 3)},
		{"theme key", dark, decor.Color{Key: "editor.selection_bg"}, rgba(23, 51, 90)},
		{"theme key of another section", dark, decor.Color{Key: "search.match_bg"}, rgba(82, 65, 27)},
		{"semantic error follows the theme", dark, decor.Color{Key: "error"}, rgba(255, 123, 114)},
		{"semantic change is the warning color", dark, decor.Color{Key: "change"}, Color(dark, "diagnostic", "warning_fg")},
		{"semantic with no theme keys falls back", theme.Theme{}, decor.Color{Key: "error"}, rgba(255, 85, 85)},
		{"unknown theme key", dark, decor.Color{Key: "editor.nope"}, nil},
		{"unknown section", dark, decor.Color{Key: "nope.bg"}, nil},
		{"a name that is neither a key nor semantic", dark, decor.Color{Key: "banana"}, nil},
		{"a key beats literal components", dark, decor.Color{Key: "editor.selection_bg", R: 9, Set: true}, rgba(23, 51, 90)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveColor(tc.th, tc.c); got != tc.want {
				t.Fatalf("ResolveColor = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveColorFollowsTheThemeChange(t *testing.T) {
	light, err := theme.Load("light")
	if err != nil {
		t.Fatal(err)
	}
	c := decor.Color{Key: "editor.selection_bg"}
	if ResolveColor(loadDark(t), c) == ResolveColor(light, c) {
		t.Fatal("the same key resolves to the same color in two themes")
	}
}

func TestDecorStyle(t *testing.T) {
	dark := loadDark(t)
	got := DecorStyle(dark, decor.Style{
		FG:   decor.Color{Key: "error"},
		BG:   decor.Color{R: 4, G: 5, B: 6, Set: true},
		Bold: true, Underline: true, Strike: true, Extend: true,
	})
	want := grid.Style{FG: rgba(255, 123, 114), BG: rgba(4, 5, 6), Bold: true, Underline: true, Strike: true}
	if got != want {
		t.Fatalf("DecorStyle = %+v, want %+v", got, want)
	}
	if DecorStyle(dark, decor.Style{}) != (grid.Style{}) {
		t.Fatal("an empty style must be an empty patch")
	}
}
